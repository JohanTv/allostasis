package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/allostasis/alo/internal/adapters/sqlite"
	"github.com/allostasis/alo/internal/domain/issue"
)

// e2ePlane simula Plane CE (API y almacenamiento firmado) para ejecutar el binario real de punta a punta.
type e2ePlane struct {
	mu             sync.Mutex
	attachments    map[string]string // id -> nombre
	nextID         int
	storedContent  map[string]string // nombre -> contenido recibido en el almacenamiento
	registerStatus int               // 0 = 200
	apiCalls       int               // llamadas a la API de adjuntos
	patched        []map[string]any  // cuerpos recibidos en PATCH de issues
}

func newE2EPlane(t *testing.T) (*e2ePlane, *httptest.Server) {
	t.Helper()
	p := &e2ePlane{attachments: map[string]string{}, storedContent: map[string]string{}}
	const base = "/api/v1/workspaces/ws/projects/proj-1/issues/iss-2/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
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
					p.storedContent[part.FileName()] = string(b)
				}
			}
			w.WriteHeader(http.StatusNoContent)
		case path == base+"issue-attachments/" && r.Method == http.MethodPost:
			p.apiCalls++
			if p.registerStatus != 0 {
				w.WriteHeader(p.registerStatus)
				_, _ = w.Write([]byte(`{"error":"x"}`))
				return
			}
			var meta map[string]any
			_ = json.NewDecoder(r.Body).Decode(&meta)
			p.nextID++
			id := "aaaaaaaa-0000-0000-0000-" + strings.Repeat("0", 11) + string(rune('0'+p.nextID))
			p.attachments[id] = meta["name"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"asset_id": id, "asset_url": "http://files/" + id,
				"upload_data": map[string]any{"url": "http://" + r.Host + "/bucket", "fields": map[string]string{"key": id}},
			})
		case path == base+"issue-attachments/" && r.Method == http.MethodGet:
			p.apiCalls++
			var items []map[string]any
			for id, name := range p.attachments {
				items = append(items, map[string]any{"id": id, "attributes": map[string]any{"name": name, "size": len(p.storedContent[name])}, "created_at": "2026-11-18T10:00:00Z"})
			}
			_ = json.NewEncoder(w).Encode(items)
		case strings.HasPrefix(path, base+"issue-attachments/") && r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(path, base+"issue-attachments/") && r.Method == http.MethodDelete:
			p.apiCalls++
			delete(p.attachments, strings.TrimSuffix(strings.TrimPrefix(path, base+"issue-attachments/"), "/"))
			w.WriteHeader(http.StatusNoContent)
		case path == base && r.Method == http.MethodPatch:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			p.patched = append(p.patched, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "iss-2"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return p, srv
}

func (p *e2ePlane) content(name string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.storedContent[name]
}

func (p *e2ePlane) has(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.attachments[id]
	return ok
}

func (p *e2ePlane) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.attachments)
}

func (p *e2ePlane) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.apiCalls
}

func (p *e2ePlane) patches() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any(nil), p.patched...)
}

func (p *e2ePlane) setRegisterStatus(code int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.registerStatus = code
}

type aloRun struct {
	stdout, stderr string
	exit           int
}

func (r aloRun) envelope(t *testing.T) (ok bool, data map[string]any, errObj map[string]any) {
	t.Helper()
	var env struct {
		OK    bool           `json:"ok"`
		Data  map[string]any `json:"data"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &env); err != nil {
		t.Fatalf("la salida no es un sobre JSON válido: %v\nstdout: %s\nstderr: %s", err, r.stdout, r.stderr)
	}
	return env.OK, env.Data, env.Error
}

// e2eEnv compila el binario real y prepara una base aislada con un proyecto, estados, etiquetas y la tarea OMNIFY-2.
type e2eEnv struct {
	t      *testing.T
	bin    string
	db     string
	apiURL string
	apiKey string
}

func newE2EEnv(t *testing.T, apiURL string) *e2eEnv {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "alo")
	build := exec.Command("go", "build", "-o", bin, "./cmd/alo")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("no se pudo compilar alo: %v\n%s", err, out)
	}

	db := filepath.Join(dir, "e2e.db")
	store, err := sqlite.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = store.SaveProject(ctx, issue.ProjectRecord{Identifier: "OMNIFY", ProjectID: "proj-1", Name: "Omnify"})
	_ = store.SaveState(ctx, issue.StateRecord{ID: "st-done", ProjectID: "proj-1", Name: "Completado", Slug: "completado", GroupName: "completed"})
	_ = store.SaveLabel(ctx, issue.Label{ID: "lbl-1", ProjectID: "proj-1", Name: "frontend"})
	if err := store.SaveIssue(ctx, &issue.WorkItem{ID: "iss-2", Key: "OMNIFY-2", ProjectID: "proj-1", ProjectKey: "OMNIFY", Title: "Ticket e2e", Priority: "low"}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	return &e2eEnv{t: t, bin: bin, db: db, apiURL: apiURL, apiKey: "k"}
}

func (e *e2eEnv) run(args ...string) aloRun {
	e.t.Helper()
	cmd := exec.Command(e.bin, append([]string{"--db", e.db}, args...)...)
	cmd.Env = append(os.Environ(), "ALLOSTASIS_DB_PATH="+e.db, "PLANE_DB_PATH="+e.db,
		"PLANE_BASE_URL="+e.apiURL, "PLANE_API_KEY="+e.apiKey, "PLANE_WORKSPACE_SLUG=ws")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	exit := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			e.t.Fatalf("no se pudo ejecutar alo: %v", err)
		}
		exit = ee.ExitCode()
	}
	return aloRun{stdout: out.String(), stderr: errb.String(), exit: exit}
}

func writeTmp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCLI_Attachment_EndToEnd(t *testing.T) {
	plane, srv := newE2EPlane(t)
	e := newE2EEnv(t, srv.URL)

	// add: dos archivos, contenido real, JSON limpio y stderr vacío.
	run := e.run("ticket", "attachment", "add", "OMNIFY-2", writeTmp(t, "nota.txt", "hola mundo"), writeTmp(t, "doc.md", "# titulo"), "--json")
	ok, data, _ := run.envelope(t)
	if !ok || run.exit != 0 || run.stderr != "" {
		t.Fatalf("add falló: exit=%d stderr=%q stdout=%s", run.exit, run.stderr, run.stdout)
	}
	if got := data["attachments"].([]any); len(got) != 2 {
		t.Fatalf("adjuntos devueltos = %d", len(got))
	}
	if plane.content("nota.txt") != "hola mundo" || plane.content("doc.md") != "# titulo" {
		t.Error("el almacenamiento debe recibir el contenido real")
	}

	// list: Plane es la fuente.
	run = e.run("ticket", "attachment", "list", "OMNIFY-2", "--json")
	_, data, _ = run.envelope(t)
	if data["source"] != "api" || len(data["attachments"].([]any)) != 2 {
		t.Fatalf("list inesperado: %s", run.stdout)
	}
	var oldID string
	for _, a := range data["attachments"].([]any) {
		if a.(map[string]any)["name"] == "nota.txt" {
			oldID = a.(map[string]any)["id"].(string)
		}
	}

	// replace: sube el nuevo y elimina el anterior.
	run = e.run("ticket", "attachment", "replace", "OMNIFY-2", oldID, writeTmp(t, "nota-v2.txt", "version 2"), "--json")
	if ok, _, _ := run.envelope(t); !ok || run.exit != 0 {
		t.Fatalf("replace falló: %s %s", run.stdout, run.stderr)
	}
	if plane.has(oldID) {
		t.Error("el adjunto anterior debe eliminarse de Plane")
	}
	if plane.content("nota-v2.txt") != "version 2" {
		t.Error("el adjunto nuevo debe subirse")
	}

	// delete: el último.
	run = e.run("ticket", "attachment", "list", "OMNIFY-2", "--json")
	_, data, _ = run.envelope(t)
	id := data["attachments"].([]any)[0].(map[string]any)["id"].(string)
	run = e.run("ticket", "attachment", "delete", "OMNIFY-2", id, "--json")
	if ok, _, _ := run.envelope(t); !ok {
		t.Fatalf("delete falló: %s", run.stdout)
	}
	if plane.count() != 1 {
		t.Errorf("quedan %d adjuntos en Plane, esperado 1", plane.count())
	}

	// Modo texto: legible y con la fuente declarada.
	run = e.run("ticket", "attachment", "list", "OMNIFY-2")
	if run.exit != 0 || !strings.Contains(run.stdout, "[fuente: api]") {
		t.Errorf("salida de texto inesperada: %q", run.stdout)
	}
}

// El contrato de errores: cada clase tiene su categoría y código de salida, en JSON y con stderr vacío.
func TestCLI_Attachment_ErrorContract(t *testing.T) {
	cases := []struct {
		name         string
		prepare      func(p *e2ePlane, e *e2eEnv)
		args         func(t *testing.T) []string
		wantExit     int
		wantCategory string
		wantNoCalls  bool
	}{
		{
			name:     "extensión no admitida: error de uso, Plane intacto",
			args:     func(t *testing.T) []string { return []string{"add", "OMNIFY-2", writeTmp(t, "dump.sql", "x")} },
			wantExit: 2, wantCategory: "user_input", wantNoCalls: true,
		},
		{
			name:     "archivo inexistente",
			args:     func(t *testing.T) []string { return []string{"add", "OMNIFY-2", "/no/existe.txt"} },
			wantExit: 2, wantCategory: "user_input", wantNoCalls: true,
		},
		{
			name:     "id de adjunto inválido: ni toca la red",
			args:     func(t *testing.T) []string { return []string{"delete", "OMNIFY-2", "no-es-uuid"} },
			wantExit: 2, wantCategory: "user_input", wantNoCalls: true,
		},
		{
			name:     "sin credenciales: error de uso con pista",
			prepare:  func(p *e2ePlane, e *e2eEnv) { e.apiKey = "" },
			args:     func(t *testing.T) []string { return []string{"add", "OMNIFY-2", writeTmp(t, "a.txt", "x")} },
			wantExit: 2, wantCategory: "user_input", wantNoCalls: true,
		},
		{
			name:     "credenciales rechazadas (401)",
			prepare:  func(p *e2ePlane, e *e2eEnv) { p.setRegisterStatus(401) },
			args:     func(t *testing.T) []string { return []string{"add", "OMNIFY-2", writeTmp(t, "a.txt", "x")} },
			wantExit: 3, wantCategory: "auth",
		},
		{
			name:     "Plane caído (503): se informa, no se encola",
			prepare:  func(p *e2ePlane, e *e2eEnv) { p.setRegisterStatus(503) },
			args:     func(t *testing.T) []string { return []string{"add", "OMNIFY-2", writeTmp(t, "a.txt", "x")} },
			wantExit: 5, wantCategory: "upstream_service",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plane, srv := newE2EPlane(t)
			e := newE2EEnv(t, srv.URL)
			if tc.prepare != nil {
				tc.prepare(plane, e)
			}
			run := e.run(append([]string{"ticket", "attachment"}, append(tc.args(t), "--json")...)...)
			ok, _, errObj := run.envelope(t)
			if ok || run.exit != tc.wantExit || errObj["category"] != tc.wantCategory {
				t.Fatalf("exit=%d (esperado %d) error=%v", run.exit, tc.wantExit, errObj)
			}
			if run.stderr != "" {
				t.Errorf("bajo --json stderr debe quedar vacío: %q", run.stderr)
			}
			if tc.wantNoCalls && plane.calls() != 0 {
				t.Errorf("no debía contactarse a Plane (%d llamadas)", plane.calls())
			}
			// Ningún fallo de adjuntos se encola jamás.
			outbox := e.run("ticket", "outbox", "list", "--json")
			if strings.Contains(outbox.stdout, "update_issue") || strings.Contains(outbox.stdout, "attachment") {
				t.Errorf("un adjunto no debe encolarse: %s", outbox.stdout)
			}
		})
	}
}

// Cierra el defecto de `ticket update`: un cambio hecho sin conexión queda encolado, visible, y llega a Plane al sincronizar.
func TestCLI_Update_OfflineThenSync(t *testing.T) {
	plane, srv := newE2EPlane(t)
	e := newE2EEnv(t, srv.URL)

	// Sin credenciales: no se pierde ni diverge en silencio.
	e.apiKey = ""
	run := e.run("ticket", "update", "OMNIFY-2", "--priority", "high", "--state", "Completado", "--labels", "frontend", "--json")
	ok, data, _ := run.envelope(t)
	if !ok || run.exit != 0 || data["pending_sync"] != true {
		t.Fatalf("el cambio debe quedar marcado pending_sync: %s (stderr %q)", run.stdout, run.stderr)
	}
	text := e.run("ticket", "update", "OMNIFY-2", "--priority", "medium")
	if !strings.Contains(text.stdout, "quedó en la cola local") {
		t.Errorf("el modo texto debe avisar que quedó pendiente: %q", text.stdout)
	}
	outbox := e.run("ticket", "outbox", "list")
	if !strings.Contains(outbox.stdout, "update_issue") {
		t.Fatalf("el cambio debe verse en la cola: %s", outbox.stdout)
	}

	// Vuelve la conexión: sync outbox aplica los cambios en orden.
	e.apiKey = "k"
	sync := e.run("sync", "outbox", "--json")
	if ok, _, _ := sync.envelope(t); !ok {
		t.Fatalf("sync falló: %s %s", sync.stdout, sync.stderr)
	}
	patched := plane.patches()
	if len(patched) != 2 {
		t.Fatalf("Plane debe recibir los 2 cambios encolados, recibió %d", len(patched))
	}
	first := patched[0]
	if first["priority"] != "high" || first["state"] != "st-done" {
		t.Errorf("primer cambio = %v (prioridad y estado resueltos a ID)", first)
	}
	if labels, _ := first["labels"].([]any); len(labels) != 1 || labels[0] != "lbl-1" {
		t.Errorf("etiquetas enviadas = %v, esperado [lbl-1]", first["labels"])
	}
	if patched[1]["priority"] != "medium" {
		t.Errorf("el segundo cambio debe aplicarse después: %v", patched[1])
	}
	if left := e.run("ticket", "outbox", "list"); !strings.Contains(left.stdout, "vac") && strings.Contains(left.stdout, "update_issue") {
		t.Errorf("la cola debe quedar vacía tras sincronizar: %s", left.stdout)
	}
}
