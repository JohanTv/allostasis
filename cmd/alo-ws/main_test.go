package main

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestStandaloneWS_VersionJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "version", "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ejecución de 'alo-ws version --json' falló: %v (salida: %s)", err, string(out))
	}

	var env struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("salida no es JSON válido: %v (salida: %s)", err, string(out))
	}
	if !env.OK {
		t.Fatalf("se esperaba ok=true en envelope, obtenido: %v", env.OK)
	}
	if env.Data["app"] != "alo-ws" {
		t.Errorf("se esperaba app='alo-ws', obtenido: '%v'", env.Data["app"])
	}
}

func TestStandaloneWS_AuditJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "audit", "--json", "--no-fetch", "--dir", "test_nonexistent_dir")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ejecución de 'alo-ws audit --json' falló: %v (salida: %s)", err, string(out))
	}

	var env struct {
		OK   bool          `json:"ok"`
		Data []interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("salida no es JSON válido: %v (salida: %s)", err, string(out))
	}
	if !env.OK {
		t.Fatalf("se esperaba ok=true, obtenido: %v", env.OK)
	}
	if len(env.Data) != 0 {
		t.Errorf("se esperaba lista vacía para dir inexistente, obtenido: %d", len(env.Data))
	}
}

func TestStandaloneWS_PruneDryRunJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "prune", "--json", "--no-fetch", "--dir", "test_nonexistent_dir")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ejecución de 'alo-ws prune --json' falló: %v (salida: %s)", err, string(out))
	}

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Mode       string   `json:"mode"`
			PrunedSafe []string `json:"pruned_safe"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("salida no es JSON válido: %v (salida: %s)", err, string(out))
	}
	if !env.OK {
		t.Fatalf("se esperaba ok=true, obtenido: %v", env.OK)
	}
	if env.Data.Mode != "dry-run" {
		t.Errorf("se esperaba mode='dry-run', obtenido: '%s'", env.Data.Mode)
	}
}
