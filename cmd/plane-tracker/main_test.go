package main

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestPlaneTracker_LabelsJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "labels", "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("labels --json falló: %v (salida: %s)", err, string(out))
	}

	var env struct {
		OK   bool          `json:"ok"`
		Data []interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("salida no es JSON válido: %v (salida: %s)", err, string(out))
	}
	if !env.OK {
		t.Errorf("se esperaba ok=true en envelope, obtenido: %v", env.OK)
	}
}

func TestPlaneTracker_FocusStatusJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "focus", "status", "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("focus status --json falló: %v (salida: %s)", err, string(out))
	}

	var env struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("salida no es JSON válido: %v (salida: %s)", err, string(out))
	}
	if !env.OK {
		t.Errorf("se esperaba ok=true en envelope, obtenido: %v", env.OK)
	}
}

func TestPlaneTracker_WorklogProposeJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "worklog", "propose", "OMNIFY-205", "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("worklog propose --json falló: %v (salida: %s)", err, string(out))
	}

	var env struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("salida no es JSON válido: %v (salida: %s)", err, string(out))
	}
	if !env.OK {
		t.Errorf("se esperaba ok=true en envelope, obtenido: %v", env.OK)
	}
	if env.Data["ticket_id"] != "OMNIFY-205" {
		t.Errorf("se esperaba ticket_id='OMNIFY-205', obtenido: '%v'", env.Data["ticket_id"])
	}
}

func TestPlaneTracker_ReportsJSONContract(t *testing.T) {
	// Report Time
	cmdTime := exec.Command("go", "run", "main.go", "report", "time", "--json")
	outTime, err := cmdTime.CombinedOutput()
	if err != nil {
		t.Fatalf("report time --json falló: %v (salida: %s)", err, string(outTime))
	}
	var envTime struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(outTime, &envTime); err != nil {
		t.Fatalf("salida no es JSON válido: %v", err)
	}
	if !envTime.OK {
		t.Errorf("se esperaba ok=true en report time")
	}

	// Report Flow
	cmdFlow := exec.Command("go", "run", "main.go", "report", "flow", "--json")
	outFlow, err := cmdFlow.CombinedOutput()
	if err != nil {
		t.Fatalf("report flow --json falló: %v (salida: %s)", err, string(outFlow))
	}
	var envFlow struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(outFlow, &envFlow); err != nil {
		t.Fatalf("salida no es JSON válido: %v", err)
	}
	if !envFlow.OK {
		t.Errorf("se esperaba ok=true en report flow")
	}
}

func TestPlaneTracker_OutboxListJSONContract(t *testing.T) {
	cmd := exec.Command("go", "run", "main.go", "outbox", "list", "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("outbox list --json falló: %v (salida: %s)", err, string(out))
	}

	var env struct {
		OK   bool          `json:"ok"`
		Data []interface{} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("salida no es JSON válido: %v (salida: %s)", err, string(out))
	}
	if !env.OK {
		t.Errorf("se esperaba ok=true en envelope, obtenido: %v", env.OK)
	}
}
