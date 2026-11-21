package main_test

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// serverWithAll simula Plane CE para create, get, attachments, links y bucket de almacenamiento.
type serverWithAll struct {
	mu           sync.Mutex
	createdIssues map[string]map[string]any
	attachments  map[string]map[string]any
	links        map[string]map[string]any
	storedFiles  map[string]string
	nextIssueID  int
	nextAttID    int
	apiCalls     int
}

func newServerWithAll(t *testing.T) (*serverWithAll, *httptest.Server) {
	t.Helper()
	s := &serverWithAll{
		createdIssues: make(map[string]map[string]any),
		attachments:   make(map[string]map[string]any),
		links:         make(map[string]map[string]any),
		storedFiles:   make(map[string]string),
	}

	const baseProj = "/api/v1/workspaces/ws/projects/proj-1/"
	const baseIssue = baseProj + "issues/iss-2/"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		path := r.URL.Path

		switch {
		case path == "/bucket":
			_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				if part.FormName() == "file" {
					b, _ := io.ReadAll(part)
					s.storedFiles[part.FileName()] = string(b)
				}
			}
			w.WriteHeader(http.StatusNoContent)

		case path == baseProj+"issues/" && r.Method == http.MethodPost:
			s.apiCalls++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.nextIssueID++
			newID := fmt.Sprintf("iss-%d", s.nextIssueID)
			seq := s.nextIssueID + 10
			created := map[string]any{
				"id":          newID,
				"name":        body["name"],
				"sequence_id": seq,
				"priority":    body["priority"],
				"project":     "proj-1",
				"state":       "st-done",
			}
			s.createdIssues[newID] = created
			_ = json.NewEncoder(w).Encode(created)

		case strings.HasSuffix(path, "/issue-attachments/") && r.Method == http.MethodPost:
			s.apiCalls++
			var meta map[string]any
			_ = json.NewDecoder(r.Body).Decode(&meta)
			s.nextAttID++
			attID := fmt.Sprintf("aaaaaaaa-0000-0000-0000-%012d", s.nextAttID)
			name := meta["name"].(string)
			s.attachments[attID] = map[string]any{
				"id":         attID,
				"name":       name,
				"attributes": map[string]any{"name": name, "size": meta["size"]},
				"created_at": "2026-11-21T12:00:00Z",
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"asset_id":  attID,
				"asset_url": "http://files/" + attID,
				"upload_data": map[string]any{
					"url":    "http://" + r.Host + "/bucket",
					"fields": map[string]string{"key": attID},
				},
			})

		case strings.HasSuffix(path, "/issue-attachments/") && r.Method == http.MethodGet:
			s.apiCalls++
			var items []map[string]any
			for _, a := range s.attachments {
				items = append(items, a)
			}
			_ = json.NewEncoder(w).Encode(items)

		case strings.HasSuffix(path, "/links/") && r.Method == http.MethodGet:
			s.apiCalls++
			var items []map[string]any
			for _, l := range s.links {
				items = append(items, l)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": items})

		case strings.HasPrefix(path, baseProj+"issues/") && r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusNoContent)

		case path == baseIssue && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "iss-2",
				"name":        "Ticket e2e",
				"sequence_id": 2,
				"project":     "proj-1",
				"state":       "st-done",
			})

		default:
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(srv.Close)
	return s, srv
}

func TestCLI_Ticket_Get_Include_Attachments_Links_JSON(t *testing.T) {
	plane, srv := newServerWithAll(t)
	plane.attachments["att-1"] = map[string]any{
		"id":         "11111111-1111-1111-1111-111111111111",
		"attributes": map[string]any{"name": "diagrama.png", "size": 2048},
		"created_at": "2026-11-21T12:00:00Z",
	}
	plane.links["lnk-1"] = map[string]any{
		"id":         "lnk-1",
		"title":      "Pull Request #42",
		"url":        "https://github.com/allostasis/alo/pull/42",
		"created_at": "2026-11-21T12:00:00Z",
	}

	e := newE2EEnv(t, srv.URL)

	// Invocación con --include attachments,links
	run := e.run("ticket", "get", "OMNIFY-2", "--include", "attachments,links", "--json")
	ok, data, errObj := run.envelope(t)
	if !ok || run.exit != 0 || run.stderr != "" {
		t.Fatalf("ticket get con --include falló: exit=%d stderr=%q stdout=%s errObj=%v", run.exit, run.stderr, run.stdout, errObj)
	}

	item, ok := data["item"].(map[string]any)
	if !ok || item["key"] != "OMNIFY-2" {
		t.Fatalf("item ausente o clave incorrecta: %v", data["item"])
	}

	atts, ok := data["attachments"].([]any)
	if !ok || len(atts) != 1 {
		t.Fatalf("attachments esperado de longitud 1, obtenido: %v", data["attachments"])
	}
	firstAtt := atts[0].(map[string]any)
	if firstAtt["name"] != "diagrama.png" {
		t.Errorf("nombre de adjunto esperado 'diagrama.png', obtenido: %v", firstAtt["name"])
	}

	links, ok := data["links"].([]any)
	if !ok || len(links) != 1 {
		t.Fatalf("links esperado de longitud 1, obtenido: %v", data["links"])
	}
	firstLink := links[0].(map[string]any)
	if firstLink["title"] != "Pull Request #42" {
		t.Errorf("título de enlace esperado 'Pull Request #42', obtenido: %v", firstLink["title"])
	}
}

func TestCLI_Ticket_Get_Include_Attachments_Links_Text(t *testing.T) {
	plane, srv := newServerWithAll(t)
	plane.attachments["att-1"] = map[string]any{
		"id":         "22222222-2222-2222-2222-222222222222",
		"attributes": map[string]any{"name": "manual.pdf", "size": 1024},
		"created_at": "2026-11-21T12:00:00Z",
	}
	plane.links["lnk-1"] = map[string]any{
		"id":         "lnk-99",
		"title":      "Doc Externa",
		"url":        "https://example.com/doc",
		"created_at": "2026-11-21T12:00:00Z",
	}

	e := newE2EEnv(t, srv.URL)

	// Invocación canónica en modo texto con --include attachments,links
	run := e.run("ticket", "get", "OMNIFY-2", "--include", "attachments,links")
	if run.exit != 0 || run.stderr != "" {
		t.Fatalf("ticket get con --include falló: exit=%d stderr=%q stdout=%s", run.exit, run.stderr, run.stdout)
	}

	if !strings.Contains(run.stdout, "Archivos Adjuntos (1)") || !strings.Contains(run.stdout, "manual.pdf") {
		t.Errorf("la salida humana debe contener el bloque de adjuntos: %s", run.stdout)
	}
	if !strings.Contains(run.stdout, "Enlaces Externos (1)") || !strings.Contains(run.stdout, "Doc Externa") {
		t.Errorf("la salida humana debe contener el bloque de enlaces: %s", run.stdout)
	}
}

func TestCLI_Ticket_Create_With_Attach_JSON(t *testing.T) {
	plane, srv := newServerWithAll(t)
	e := newE2EEnv(t, srv.URL)

	docFile := writeTmp(t, "especificacion.txt", "contenido de especificacion")
	imgFile := writeTmp(t, "mockup.png", "imagen binaria simulada")

	run := e.run("ticket", "create", "Nueva tarea con adjuntos",
		"--project", "OMNIFY",
		"--attach", docFile+","+imgFile,
		"--json",
	)
	ok, data, errObj := run.envelope(t)
	if !ok || run.exit != 0 || run.stderr != "" {
		t.Fatalf("ticket create --attach falló: exit=%d stderr=%q stdout=%s errObj=%v", run.exit, run.stderr, run.stdout, errObj)
	}

	if data["title"] != "Nueva tarea con adjuntos" {
		t.Errorf("título incorrecto: %v", data["title"])
	}

	atts, ok := data["attachments"].([]any)
	if !ok || len(atts) != 2 {
		t.Fatalf("se esperaban 2 adjuntos en la respuesta, obtenido: %v", data["attachments"])
	}

	// Verificar que el almacenamiento de Plane recibió los contenidos
	plane.mu.Lock()
	docContent := plane.storedFiles["especificacion.txt"]
	imgContent := plane.storedFiles["mockup.png"]
	plane.mu.Unlock()

	if docContent != "contenido de especificacion" {
		t.Errorf("contenido de especificacion.txt incorrecto: %q", docContent)
	}
	if imgContent != "imagen binaria simulada" {
		t.Errorf("contenido de mockup.png incorrecto: %q", imgContent)
	}
}

func TestCLI_Ticket_Create_Attach_PreValidation_Fails(t *testing.T) {
	plane, srv := newServerWithAll(t)
	e := newE2EEnv(t, srv.URL)

	// 1. Archivo inexistente
	run := e.run("ticket", "create", "Tarea invalida", "--project", "OMNIFY", "--attach", "/archivo/inexistente.txt", "--json")
	ok, _, errObj := run.envelope(t)
	if ok || run.exit == 0 {
		t.Fatalf("esperado fallo para archivo inexistente, obtenido éxito: %s", run.stdout)
	}
	if errObj["code"] != "ATTACHMENT_INVALID" {
		t.Errorf("código de error esperado ATTACHMENT_INVALID, obtenido: %v", errObj["code"])
	}

	// 2. Archivo .sql rechazado por política de Plane
	sqlFile := writeTmp(t, "dump.sql", "SELECT * FROM users;")
	run = e.run("ticket", "create", "Tarea con sql", "--project", "OMNIFY", "--attach", sqlFile, "--json")
	ok, _, errObj = run.envelope(t)
	if ok || run.exit == 0 {
		t.Fatalf("esperado fallo para archivo .sql, obtenido éxito: %s", run.stdout)
	}
	if errObj["code"] != "ATTACHMENT_INVALID" {
		t.Errorf("código de error esperado ATTACHMENT_INVALID, obtenido: %v", errObj["code"])
	}

	// Verificar que jamás se creó la tarea en Plane ante validación fallida
	plane.mu.Lock()
	count := len(plane.createdIssues)
	plane.mu.Unlock()
	if count != 0 {
		t.Errorf("no debió crearse ninguna tarea en Plane tras pre-validación fallida, creadas: %d", count)
	}
}

func TestCLI_Ticket_Create_Attach_Offline(t *testing.T) {
	// Sin conexión con Plane (apiURL inválida)
	e := newE2EEnv(t, "http://127.0.0.1:59999/down")

	txtFile := writeTmp(t, "offline-note.txt", "nota offline")

	// Modo humano: debe avisar que la tarea fue creada local y los adjuntos requieren Plane
	run := e.run("ticket", "create", "Tarea creada offline", "--project", "OMNIFY", "--attach", txtFile)
	if run.exit != 0 {
		t.Fatalf("la tarea debe crearse localmente en offline-first: exit=%d stdout=%s stderr=%s", run.exit, run.stdout, run.stderr)
	}
	if !strings.Contains(run.stdout, "Tarea creada exitosamente en local") || !strings.Contains(run.stdout, "Los adjuntos no se pueden subir offline") {
		t.Errorf("salida esperada con advertencia de adjuntos offline: %s", run.stdout)
	}
}
