package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JohanTv/allostasis/internal/domain/issue"
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "alo-cli-hermetic-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	isolatedDB := filepath.Join(tmpDir, "isolated_test.db")
	_ = os.Setenv("ALLOSTASIS_DB_PATH", isolatedDB)
	_ = os.Setenv("PLANE_DB_PATH", isolatedDB)

	os.Exit(m.Run())
}

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
	cmd.Env = append(os.Environ(), "PLANE_API_KEY=")
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

func TestCLI_TicketList_StateFilterFlags(t *testing.T) {
	// 1. Error de exclusión mutua: --closed y --all
	cmd := exec.Command("go", "run", "./cmd/alo", "ticket", "list", "--closed", "--all", "--json")
	cmd.Dir = "../.."
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err == nil {
		t.Fatalf("se esperaba fallo al combinar --closed y --all")
	}
	var env struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil || env.Error.Code != "MUTUALLY_EXCLUSIVE_FLAGS" {
		t.Errorf("error esperado MUTUALLY_EXCLUSIVE_FLAGS, obtenido: %+v", env)
	}

	// 2. Error de exclusión mutua: --closed y --open
	cmd = exec.Command("go", "run", "./cmd/alo", "ticket", "list", "--closed", "--open", "--json")
	cmd.Dir = "../.."
	stdout.Reset()
	cmd.Stdout = &stdout
	err = cmd.Run()
	if err == nil {
		t.Fatalf("se esperaba fallo al combinar --closed y --open")
	}

	// 3. Ejecución exitosa con --all
	cmd = exec.Command("go", "run", "./cmd/alo", "ticket", "list", "--all", "--limit", "2", "--json")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo ticket list --all --json: %v (out: %s)", err, string(out))
	}
	var envAll struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &envAll); err != nil || !envAll.OK {
		t.Fatalf("se esperaba respuesta exitosa con --all: %s", string(out))
	}

	// 4. Ejecución exitosa con --closed
	cmd = exec.Command("go", "run", "./cmd/alo", "ticket", "list", "--closed", "--limit", "2", "--json")
	cmd.Dir = "../.."
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo ticket list --closed --json: %v (out: %s)", err, string(out))
	}
	var envClosed struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &envClosed); err != nil || !envClosed.OK {
		t.Fatalf("se esperaba respuesta exitosa con --closed: %s", string(out))
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
		cmdArgs := append([]string{"run", "./cmd/alo", "--db", dbPath}, args...)
		cmd := exec.Command("go", cmdArgs...)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "PLANE_DB_PATH="+dbPath, "ALLOSTASIS_DB_PATH="+dbPath, "PLANE_API_KEY=")
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

func TestCLI_SyncUnified_GrammarAndTargets(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "worklogs.db")

	runAlo := func(args ...string) ([]byte, error) {
		cmdArgs := append([]string{"run", "./cmd/alo", "--db", dbPath}, args...)
		cmd := exec.Command("go", cmdArgs...)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "PLANE_DB_PATH="+dbPath, "ALLOSTASIS_DB_PATH="+dbPath, "PLANE_API_KEY=")
		return cmd.CombinedOutput()
	}

	// 1. alo sync (sin target, all)
	out, err := runAlo("sync", "--json")
	if err != nil {
		t.Fatalf("error ejecutando alo sync --json: %v (out: %s)", err, string(out))
	}
	var syncRep struct {
		OK   bool                    `json:"ok"`
		Data issue.SyncUnifiedReport `json:"data"`
	}
	if err := json.Unmarshal(out, &syncRep); err != nil {
		t.Fatalf("error parseando respuesta de alo sync: %v (out: %s)", err, string(out))
	}
	if syncRep.Data.Target != "all" {
		t.Errorf("target esperado 'all', obtenido: %s", syncRep.Data.Target)
	}

	// 2. alo sync outbox
	out, err = runAlo("sync", "outbox", "--json")
	if err != nil {
		t.Fatalf("error ejecutando alo sync outbox: %v (out: %s)", err, string(out))
	}
	var outboxRep struct {
		OK   bool                    `json:"ok"`
		Data issue.SyncUnifiedReport `json:"data"`
	}
	if err := json.Unmarshal(out, &outboxRep); err != nil || outboxRep.Data.Target != "outbox" {
		t.Errorf("resultado outbox inesperado: %+v", outboxRep)
	}

	// 3. alo sync tickets con --deep
	out, err = runAlo("sync", "tickets", "--deep", "--json")
	if err != nil {
		t.Fatalf("error ejecutando alo sync tickets --deep: %v (out: %s)", err, string(out))
	}
	var ticketsRep struct {
		OK   bool                    `json:"ok"`
		Data issue.SyncUnifiedReport `json:"data"`
	}
	if err := json.Unmarshal(out, &ticketsRep); err != nil || ticketsRep.Data.Target != "tickets" {
		t.Errorf("resultado tickets inesperado: %+v", ticketsRep)
	}
}

func TestCLI_CommaSeparated_Flags(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "worklogs.db")

	runAlo := func(args ...string) ([]byte, error) {
		cmdArgs := append([]string{"run", "./cmd/alo", "--db", dbPath}, args...)
		cmd := exec.Command("go", cmdArgs...)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "PLANE_DB_PATH="+dbPath, "ALLOSTASIS_DB_PATH="+dbPath, "PLANE_API_KEY=")
		return cmd.CombinedOutput()
	}

	// 1. Crear tarea con múltiples etiquetas separadas por comas (--labels "backend,auth,p0")
	out, err := runAlo("ticket", "create", "Tarea con múltiples etiquetas", "--project", "PRJA", "--labels", "backend,auth,p0", "--json")
	if err != nil {
		t.Fatalf("error creando tarea con --labels: %v (out: %s)", err, string(out))
	}
	var createdEnv struct {
		OK   bool `json:"ok"`
		Data struct {
			Key    string   `json:"key"`
			Labels []string `json:"labels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &createdEnv); err != nil || createdEnv.Data.Key == "" {
		t.Fatalf("error parseando creación: %v (out: %s)", err, string(out))
	}
	ticketKeyA := createdEnv.Data.Key
	if len(createdEnv.Data.Labels) != 3 {
		t.Errorf("esperadas 3 etiquetas, obtenidas: %+v", createdEnv.Data.Labels)
	}

	// 2. Crear segunda tarea en otro proyecto
	out, err = runAlo("ticket", "create", "Tarea en segundo proyecto", "--project", "PRJB", "--state", "Pendiente", "--json")
	if err != nil {
		t.Fatalf("error creando tarea PRJB: %v (out: %s)", err, string(out))
	}

	// 3. Consultar con ticket get --include "comments,history"
	out, err = runAlo("ticket", "get", ticketKeyA, "--include", "comments,history", "--json")
	if err != nil {
		t.Fatalf("error ejecutando ticket get --include: %v (out: %s)", err, string(out))
	}
	var getEnv struct {
		OK   bool `json:"ok"`
		Data struct {
			Item     issue.WorkItem        `json:"item"`
			Comments []issue.CommentRecord `json:"comments"`
			History  *issue.TicketHistory  `json:"history"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &getEnv); err != nil || getEnv.Data.Item.Key != ticketKeyA {
		t.Fatalf("error parseando ticket get con --include: %v (out: %s)", err, string(out))
	}
	if getEnv.Data.Comments == nil || getEnv.Data.History == nil {
		t.Errorf("se esperaba que --include habilitara comments e history: comments=%v, history=%v", getEnv.Data.Comments, getEnv.Data.History)
	}

	// 4. Filtrar múltiples proyectos con -p "PRJA,PRJB"
	out, err = runAlo("ticket", "list", "-p", "PRJA,PRJB", "--json")
	if err != nil {
		t.Fatalf("error en ticket list con múltiples proyectos: %v (out: %s)", err, string(out))
	}
	var listMultiProj struct {
		OK   bool `json:"ok"`
		Data struct {
			Count   int              `json:"count"`
			Results []issue.WorkItem `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &listMultiProj); err != nil || len(listMultiProj.Data.Results) < 2 {
		t.Errorf("se esperaban al menos 2 tareas de PRJA y PRJB, obtenidas %d: %+v", len(listMultiProj.Data.Results), listMultiProj)
	}

	// 5. Filtrar múltiples estados con -s "Pendiente,Backlog"
	out, err = runAlo("ticket", "list", "-s", "Pendiente,Backlog", "--json")
	if err != nil {
		t.Fatalf("error en ticket list con múltiples estados: %v (out: %s)", err, string(out))
	}
	var listMultiState struct {
		OK   bool `json:"ok"`
		Data struct {
			Count   int              `json:"count"`
			Results []issue.WorkItem `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &listMultiState); err != nil || len(listMultiState.Data.Results) < 2 {
		t.Errorf("se esperaban al menos 2 tareas con estados Pendiente o Backlog, obtenidas: %d", len(listMultiState.Data.Results))
	}
}
