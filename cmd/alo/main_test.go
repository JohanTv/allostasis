package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allostasis/alo/internal/domain/issue"
)

func TestCLI_VersionJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/alo", "version", "--json")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo version --json: %v (output: %s)", err, string(out))
	}

	var envelope struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("la salida no es un JSON válido: %v (raw: %s)", err, string(out))
	}

	if !envelope.OK {
		t.Errorf("se esperaba envelope.OK = true")
	}

	if envelope.Data["app"] != "allostasis" {
		t.Errorf("campo 'app' esperado 'allostasis', obtenido '%v'", envelope.Data["app"])
	}

	if _, ok := envelope.Data["version"]; !ok {
		t.Errorf("campo 'version' requerido en el contrato JSON")
	}

	// Verificar ausencia de secuencias ANSI
	if strings.Contains(string(out), "\x1b[") {
		t.Errorf("la salida JSON contiene secuencias de escape ANSI prohibidas: %s", string(out))
	}
}

func TestCLI_LoadJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/alo", "load", "--json")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo load --json: %v (output: %s)", err, string(out))
	}

	var envelope struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("la salida no es un JSON válido: %v (raw: %s)", err, string(out))
	}

	if !envelope.OK {
		t.Errorf("se esperaba envelope.OK = true")
	}

	requiredFields := []string{"wip_count", "projected_cycle_days", "toil_ratio", "deep_work_ratio", "status_summary"}
	for _, field := range requiredFields {
		if _, ok := envelope.Data[field]; !ok {
			t.Errorf("campo requerido '%s' no encontrado en payload JSON", field)
		}
	}

	if strings.Contains(string(out), "\x1b[") {
		t.Errorf("la salida JSON contiene secuencias de escape ANSI prohibidas: %s", string(out))
	}
}

func TestCLI_SyncJSONEnvelope(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/alo", "sync", "--json")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo sync --json: %v (output: %s)", err, string(out))
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("la salida no es un JSON envelope válido: %v (raw: %s)", err, string(out))
	}

	if ok, exists := payload["ok"]; !exists || ok != true {
		t.Errorf("campo 'ok' esperado true en JSON envelope, obtenido '%v'", ok)
	}

	if _, ok := payload["data"]; !ok {
		t.Errorf("campo 'data' esperado en JSON envelope")
	}
}

func TestCLI_TicketListJSONEnvelope(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/alo", "ticket", "list", "--json")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo ticket list --json: %v (output: %s)", err, string(out))
	}

	var envelope struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("la salida no es un JSON envelope válido: %v (raw: %s)", err, string(out))
	}
	if !envelope.OK {
		t.Errorf("se esperaba envelope.OK = true")
	}
	if _, ok := envelope.Data["results"]; !ok {
		t.Errorf("campo 'results' esperado en data de ticket list")
	}
}

func TestCLI_TicketList_MutuallyExclusiveFlags(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/alo", "ticket", "list", "--mine", "--created", "--json")
	cmd.Dir = "../.."
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("se esperaba fallo al combinar --mine y --created, pero comando tuvo éxito: %s", stdout.String())
	}

	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Code     string `json:"code"`
			Category string `json:"category"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("salida de error debe ser JSON válido: %v (raw: %s, stderr: %s)", err, stdout.String(), stderr.String())
	}
	if envelope.OK {
		t.Errorf("se esperaba envelope.OK = false")
	}
	if envelope.Error.Code != "MUTUALLY_EXCLUSIVE_FLAGS" {
		t.Errorf("código de error esperado 'MUTUALLY_EXCLUSIVE_FLAGS', obtenido '%s'", envelope.Error.Code)
	}
}

func TestCLI_ProjectsJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/alo", "projects", "--json")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo projects --json: %v (output: %s)", err, string(out))
	}

	var envelope struct {
		OK   bool        `json:"ok"`
		Data interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("la salida no es un JSON válido: %v (raw: %s)", err, string(out))
	}
	if !envelope.OK {
		t.Errorf("se esperaba envelope.OK = true")
	}
}

func TestCLI_StatesJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/alo", "states", "--json")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo states --json: %v (output: %s)", err, string(out))
	}

	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Count   int                      `json:"count"`
			Results []map[string]interface{} `json:"results"`
			View    string                   `json:"view"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("la salida no es un JSON válido: %v (raw: %s)", err, string(out))
	}
	if !envelope.OK {
		t.Errorf("se esperaba envelope.OK = true")
	}
	if envelope.Data.View != "states" {
		t.Errorf("se esperaba view 'states', obtenido '%s'", envelope.Data.View)
	}
}

func TestCLI_TicketSync_CatalogsContract(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/alo", "ticket", "sync", "--catalogs", "--json")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo ticket sync --catalogs --json: %v (output: %s)", err, string(out))
	}

	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("la salida no es un JSON válido: %v (raw: %s)", err, string(out))
	}
	if !envelope.OK {
		t.Errorf("se esperaba envelope.OK = true")
	}
	if envelope.Data.Status != "catalogs_synced" {
		t.Errorf("status esperado 'catalogs_synced', obtenido '%s'", envelope.Data.Status)
	}
}

func TestCLI_TicketList_BriefOutput(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/alo", "ticket", "list", "--brief", "-l", "2")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo ticket list --brief: %v (output: %s)", err, string(out))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 0 && lines[0] != "" {
		for _, l := range lines {
			if strings.Contains(l, "────") {
				t.Errorf("brief output no debe incluir cabeceras de tabla: %s", l)
			}
		}
	}
}

func TestCLI_TicketComment_And_GetFlags(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "worklogs.db")

	runAlo := func(args ...string) ([]byte, error) {
		cmdArgs := append([]string{"run", "./cmd/alo"}, args...)
		cmd := exec.Command("go", cmdArgs...)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "PLANE_DB_PATH="+dbPath)
		return cmd.CombinedOutput()
	}

	// 1. Crear tarea local
	out, err := runAlo("ticket", "create", "Tarea para auditoría de comentarios", "--project", "TEST", "--start-date", "2026-11-01", "--due-date", "2026-11-10", "--json")
	if err != nil {
		t.Fatalf("error creando tarea: %v (out: %s)", err, string(out))
	}
	var createdEnvelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &createdEnvelope); err != nil || createdEnvelope.Data.Key == "" {
		t.Fatalf("error parseando respuesta de create: %v (out: %s)", err, string(out))
	}
	ticketKey := createdEnvelope.Data.Key

	// 2. Actualizar tarea con --desc, --assign y --labels
	out, err = runAlo("ticket", "update", ticketKey, "--desc", "Descripción detallada", "--assign", "usr-tester", "--labels", "qa,audit", "--json")
	if err != nil {
		t.Fatalf("error actualizando tarea: %v (out: %s)", err, string(out))
	}
	var updateEnvelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Description string   `json:"description"`
			Assignee    string   `json:"assignee"`
			Labels      []string `json:"labels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &updateEnvelope); err != nil {
		t.Fatalf("error parseando update: %v (out: %s)", err, string(out))
	}
	if updateEnvelope.Data.Description != "Descripción detallada" || updateEnvelope.Data.Assignee != "usr-tester" {
		t.Errorf("datos no coinciden en update: %+v", updateEnvelope.Data)
	}

	// 3. Añadir comentario con horas (bitácora)
	out, err = runAlo("ticket", "comment", "add", ticketKey, "Trabajo en endpoints", "--hours", "1.5", "--start-time", "14:00", "--json")
	if err != nil {
		t.Fatalf("error añadiendo comentario: %v (out: %s)", err, string(out))
	}

	// 4. Listar comentarios con alo ticket comment list
	out, err = runAlo("ticket", "comment", "list", ticketKey, "--json")
	if err != nil {
		t.Fatalf("error listando comentarios: %v (out: %s)", err, string(out))
	}
	var commentsEnvelope struct {
		OK   bool                  `json:"ok"`
		Data []issue.CommentRecord `json:"data"`
	}
	if err := json.Unmarshal(out, &commentsEnvelope); err != nil || len(commentsEnvelope.Data) == 0 {
		t.Fatalf("error parseando comentarios: %v (out: %s)", err, string(out))
	}

	// 5. Verificar get con --last y --history
	out, err = runAlo("ticket", "get", ticketKey, "--last", "5", "--history", "--offline", "--json")
	if err != nil {
		t.Fatalf("error ejecutando get enriquecido: %v (out: %s)", err, string(out))
	}
	var getEnvelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Item     issue.WorkItem        `json:"item"`
			Comments []issue.CommentRecord `json:"comments"`
			History  *issue.TicketHistory  `json:"history"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &getEnvelope); err != nil {
		t.Fatalf("error parseando get enriquecido: %v (out: %s)", err, string(out))
	}
	if len(getEnvelope.Data.Comments) == 0 {
		t.Errorf("se esperaban comentarios en get enriquecido")
	}
	if getEnvelope.Data.History == nil || len(getEnvelope.Data.History.Assignments) == 0 {
		t.Errorf("se esperaban asignaciones en historial")
	}

	// 6. Probar alo log vinculando con outbox
	out, err = runAlo("log", ticketKey, "45", "Prueba de esfuerzo", "--json")
	if err != nil {
		t.Fatalf("error ejecutando alo log: %v (out: %s)", err, string(out))
	}

	// 7. Verificar que el outbox contiene la mutación de tipo comment
	out, err = runAlo("ticket", "outbox", "list", "--all", "--json")
	if err != nil {
		t.Fatalf("error listando outbox: %v (out: %s)", err, string(out))
	}
	var outboxEnvelope struct {
		OK   bool                 `json:"ok"`
		Data []issue.OutboxRecord `json:"data"`
	}
	if err := json.Unmarshal(out, &outboxEnvelope); err != nil || len(outboxEnvelope.Data) < 2 {
		t.Fatalf("se esperaban al menos 2 registros en outbox (creación y log), obtenidos: %d (out: %s)", len(outboxEnvelope.Data), string(out))
	}
}
