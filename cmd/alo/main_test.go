package main_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
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
