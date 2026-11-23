package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JohanTv/allostasis/internal/app"
	"github.com/JohanTv/allostasis/internal/domain/issue"
	"github.com/JohanTv/allostasis/internal/domain/ledger"
	"github.com/JohanTv/allostasis/internal/domain/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

// TestCockpitModel_Init verifica que Init retorna nil.
func TestCockpitModel_Init(t *testing.T) {
	m := initialModel(&mockCockpitReader{}, nil)
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init() debería retornar nil")
	}
}

// TestCockpitModel_Update_Quit verifica que las teclas de salida terminan el modelo.
func TestCockpitModel_Update_Quit(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"q", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}},
		{"ctrl+c", tea.KeyMsg{Type: tea.KeyCtrlC}},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := initialModel(&mockCockpitReader{}, nil)
			m2, cmd := m.Update(tt.msg)
			m2Cockpit := m2.(model)
			if !m2Cockpit.quitting {
				t.Error("quitting debería ser true")
			}
			if cmd == nil {
				t.Error("cmd no debería ser nil")
			}
		})
	}
}

// TestCockpitModel_Update_RefreshKey verifica que 'r' despacha el comando de refresh.
func TestCockpitModel_Update_RefreshKey(t *testing.T) {
	m := initialModel(&mockCockpitReader{}, nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Error("presionar 'r' debería despachar un comando de refresh")
	}
}

// TestCockpitModel_Update_SnapshotMsg verifica el manejo de mensajes de snapshot.
func TestCockpitModel_Update_SnapshotMsg(t *testing.T) {
	snap := newTestSnapshot()
	m := initialModel(&mockCockpitReader{}, nil)

	// Snapshot exitoso
	msg := fetchSnapshotResult{snap: snap, err: nil}
	m2, _ := m.Update(msg)
	m2Cockpit := m2.(model)

	if m2Cockpit.snapshot == nil {
		t.Error("snapshot debería no ser nil")
	}
	if m2Cockpit.errorMsg != "" {
		t.Errorf("errorMsg debería estar vacío, got: %s", m2Cockpit.errorMsg)
	}

	// Snapshot con error
	errMsg := fmt.Errorf("error de conexión")
	msgErr := fetchSnapshotResult{snap: nil, err: errMsg}
	m3, _ := m2.Update(msgErr)
	m3Cockpit := m3.(model)

	if m3Cockpit.errorMsg == "" {
		t.Error("errorMsg debería no estar vacío tras error")
	}
	if !strings.Contains(m3Cockpit.errorMsg, "Error al refrescar") {
		t.Errorf("errorMsg no contiene 'Error al refrescar': %s", m3Cockpit.errorMsg)
	}
}

// TestCockpitModel_Update_WindowSize verifica el manejo de redimensionamiento.
func TestCockpitModel_Update_WindowSize(t *testing.T) {
	m := initialModel(&mockCockpitReader{}, nil)

	// Tamaño normal
	msg := tea.WindowSizeMsg{Width: 100, Height: 30}
	m2, _ := m.Update(msg)
	m2Cockpit := m2.(model)

	if m2Cockpit.width != 100 {
		t.Errorf("width = %d, want 100", m2Cockpit.width)
	}
	if m2Cockpit.height != 30 {
		t.Errorf("height = %d, want 30", m2Cockpit.height)
	}

	// Tamaño muy pequeño: no debe panikear
	msgSmall := tea.WindowSizeMsg{Width: 10, Height: 5}
	m3, _ := m2.Update(msgSmall)
	m3Cockpit := m3.(model)
	_ = m3Cockpit.View()
}

// TestCockpitModel_View_NilSnapshot verifica que View muestra "Cargando" con snapshot nil.
func TestCockpitModel_View_NilSnapshot(t *testing.T) {
	m := initialModel(&mockCockpitReader{}, nil)
	view := m.View()

	if !strings.Contains(view, "ALLOSTASIS") {
		t.Error("View debería contener 'ALLOSTASIS'")
	}
	if !strings.Contains(view, "Cargando") {
		t.Error("View con snapshot nil debería contener 'Cargando'")
	}
}

// TestCockpitModel_View_WithSnapshot verifica que View muestra contenido con snapshot.
func TestCockpitModel_View_WithSnapshot(t *testing.T) {
	snap := newTestSnapshot()
	m := initialModel(&mockCockpitReader{}, snap)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(model)

	view := m.View()

	if !strings.Contains(view, "ALLOSTASIS") {
		t.Error("View debería contener 'ALLOSTASIS'")
	}
	if !strings.Contains(view, "OMNIFY-175") {
		t.Error("View debería contener el ticket activo")
	}
	if !strings.Contains(view, "test-slug") {
		t.Error("View debería contener el worktree slug")
	}
}

// TestCockpitModel_View_ErrorMsg verifica que el mensaje de error se muestra.
func TestCockpitModel_View_ErrorMsg(t *testing.T) {
	// Inicializar con un snapshot válido para que View no retorne temprano
	snap := newTestSnapshot()
	m := initialModel(&mockCockpitReader{}, snap)

	// Simular error de snapshot (errorMsg se muestra solo cuando snapshot no es nil)
	msg := fetchSnapshotResult{snap: nil, err: fmt.Errorf("conexión fallida")}
	tm, _ := m.Update(msg)
	m = tm.(model)

	// El errorMsg se almacena pero snapshot sigue siendo el anterior (no nil)
	// porque el código solo actualiza snapshot cuando msg.snap != nil
	view := m.View()
	if !strings.Contains(view, "Error al refrescar") {
		t.Error("View debería mostrar el mensaje de error")
	}
}

// TestCockpitModel_View_NoActiveIssues verifica el mensaje cuando no hay issues activos.
func TestCockpitModel_View_NoActiveIssues(t *testing.T) {
	snap := &app.CockpitSnapshot{
		Timestamp:          time.Now(),
		KleeMeasureMinutes: 0,
		AllostaticLoad:     ledger.AllostaticLoad{},
		ActiveIssues:       []issue.WorkItem{},
		ActiveWorktrees:    []workspace.Container{},
		StatusSummary:      "Estado nominal óptimo",
	}
	m := initialModel(&mockCockpitReader{}, snap)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(model)

	view := m.View()
	if !strings.Contains(view, "No hay tareas activas") {
		t.Error("View debería mostrar mensaje de no tareas activas")
	}
}

// TestInitialModel verifica que el factory del cockpit inicializa correctamente.
func TestInitialModel(t *testing.T) {
	snap := newTestSnapshot()
	mock := &mockCockpitReader{snapshot: snap}

	m := initialModel(mock, snap)

	if m.snapshot == nil {
		t.Error("snapshot debería no ser nil")
	}
	if m.service == nil {
		t.Error("service debería no ser nil")
	}
	if !m.issueTable.Focused() {
		t.Error("la tabla del cockpit debería estar enfocada por defecto")
	}
}

// TestInitialModel_NilSnapshot verifica que el factory no panikea con nil.
func TestInitialModel_NilSnapshot(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("initialModel(nil) panikeó: %v", r)
		}
	}()

	m := initialModel(&mockCockpitReader{}, nil)
	if m.snapshot != nil {
		t.Error("snapshot debería ser nil")
	}
}

// TestCockpitModel_Update_TableNavigation verifica que la tabla navega con flechas.
func TestCockpitModel_Update_TableNavigation(t *testing.T) {
	snap := &app.CockpitSnapshot{
		Timestamp:          time.Now(),
		KleeMeasureMinutes: 60,
		AllostaticLoad:     ledger.AllostaticLoad{WIPCount: 2},
		ActiveIssues: []issue.WorkItem{
			{Key: "A-1", State: "in_progress", Priority: "high", Title: "Primera"},
			{Key: "A-2", State: "in_progress", Priority: "medium", Title: "Segunda"},
		},
		StatusSummary: "Estado nominal óptimo",
	}
	m := initialModel(&mockCockpitReader{}, snap)

	// La tabla debería tener 2 filas; navegar abajo debería funcionar
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2Cockpit := m2.(model)
	// Verificamos que no panikea y que el modelo se actualiza
	_ = m2Cockpit.View()
}

// TestCockpitModel_Update_ErrorClearsOnSuccess verifica que el errorMsg se limpia con snapshot exitoso.
func TestCockpitModel_Update_ErrorClearsOnSuccess(t *testing.T) {
	m := initialModel(&mockCockpitReader{}, nil)

	// Primero un error
	msgErr := fetchSnapshotResult{snap: nil, err: fmt.Errorf("error")}
	tm, _ := m.Update(msgErr)
	m = tm.(model)
	if m.errorMsg == "" {
		t.Fatal("errorMsg debería no estar vacío")
	}

	// Luego un snapshot exitoso
	snap := newTestSnapshot()
	msgOK := fetchSnapshotResult{snap: snap, err: nil}
	tm2, _ := m.Update(msgOK)
	m2 := tm2.(model)

	if m2.errorMsg != "" {
		t.Errorf("errorMsg debería estar vacío tras snapshot exitoso, got: %s", m2.errorMsg)
	}
}
