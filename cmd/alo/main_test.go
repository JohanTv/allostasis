package main_test

import (
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

func TestStandaloneWS_VersionJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "./cmd/alo-ws", "version", "--json")
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error ejecutando alo-ws version --json: %v (output: %s)", err, string(out))
	}

	var envelope struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("la salida no es un JSON válido: %v (raw: %s)", err, string(out))
	}

	if !envelope.OK || envelope.Data["app"] != "alo-ws" {
		t.Errorf("campo 'app' esperado 'alo-ws' en envelope, obtenido '%v'", envelope.Data["app"])
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
