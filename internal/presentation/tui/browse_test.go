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

func newTestSnapshot() *app.CockpitSnapshot {
	return &app.CockpitSnapshot{
		Timestamp:          time.Now(),
		KleeMeasureMinutes: 120,
		AllostaticLoad: ledger.AllostaticLoad{
			TotalTime:    120 * time.Minute,
			DeepWorkTime: 90 * time.Minute,
			ToilTime:     30 * time.Minute,
			WIPCount:     2,
		},
		ActiveIssues: []issue.WorkItem{
			{Key: "OMNIFY-175", State: "in_progress", Priority: "high", Title: "Test ticket"},
		},
		ActiveWorktrees: []workspace.Container{
			{
				TicketID: "OMNIFY-175",
				Slug:     "test-slug",
				Bindings: []workspace.WorktreeBinding{
					{RepoName: "repo-a", Branch: "main", IsClean: true},
				},
			},
		},
		CycleTimeDays: 4.0,
		StatusSummary: "Estado nominal óptimo",
	}
}

// TestBrowseModel_Init verifica que Init retorna nil (sin comandos iniciales).
func TestBrowseModel_Init(t *testing.T) {
	m := initialBrowseModel(&mockCockpitReader{}, nil)
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init() debería retornar nil")
	}
}

// TestBrowseModel_Update_Quit verifica que las teclas de salida terminan el modelo.
func TestBrowseModel_Update_Quit(t *testing.T) {
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
			m := initialBrowseModel(&mockCockpitReader{}, nil)
			m2, cmd := m.Update(tt.msg)
			m2Browse := m2.(browseModel)
			if !m2Browse.quitting {
				t.Error("quitting debería ser true")
			}
			if cmd == nil {
				t.Error("cmd no debería ser nil (debería ser tea.Quit)")
			}
		})
	}
}

// TestBrowseModel_Update_TabNavigation verifica la navegación con Tab/Shift+Tab.
func TestBrowseModel_Update_TabNavigation(t *testing.T) {
	m := initialBrowseModel(&mockCockpitReader{}, nil)

	// Tab avanza la pestaña
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2Browse := m2.(browseModel)
	if m2Browse.currentTab != 1 {
		t.Errorf("currentTab = %d, want 1", m2Browse.currentTab)
	}

	// Shift+Tab retrocede
	m3, _ := m2.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m3Browse := m3.(browseModel)
	if m3Browse.currentTab != 0 {
		t.Errorf("currentTab = %d, want 0", m3Browse.currentTab)
	}

	// Tab múltiple: ir más allá del último debe hacer wrap
	m4 := initialBrowseModel(&mockCockpitReader{}, nil)
	for i := 0; i < len(tabs); i++ {
		var tm tea.Model
		tm, _ = m4.Update(tea.KeyMsg{Type: tea.KeyTab})
		m4 = tm.(browseModel)
	}
	if m4.currentTab != 0 {
		t.Errorf("currentTab después de wrap = %d, want 0", m4.currentTab)
	}
}

// TestBrowseModel_Update_NumericShortcuts verifica los atajos numéricos 1-4.
func TestBrowseModel_Update_NumericShortcuts(t *testing.T) {
	m := initialBrowseModel(&mockCockpitReader{}, nil)

	for i := 1; i <= len(tabs); i++ {
		digitKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune('0' + i)}}
		m2, _ := m.Update(digitKey)
		m2Browse := m2.(browseModel)
		if m2Browse.currentTab != i-1 {
			t.Errorf("tecla %d: currentTab = %d, want %d", i, m2Browse.currentTab, i-1)
		}
	}

	// Tecla "5" no debe cambiar la pestaña (fuera de rango)
	m5 := initialBrowseModel(&mockCockpitReader{}, nil)
	m5r, _ := m5.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	m5Browse := m5r.(browseModel)
	if m5Browse.currentTab != 0 {
		t.Error("tecla 5 no debería cambiar la pestaña")
	}
}

// TestBrowseModel_Update_SnapshotMsg verifica el manejo de mensajes de snapshot.
func TestBrowseModel_Update_SnapshotMsg(t *testing.T) {
	snap := newTestSnapshot()
	m := initialBrowseModel(&mockCockpitReader{}, nil)

	// Snapshot exitoso
	msg := fetchSnapshotResult{snap: snap, err: nil}
	m2, _ := m.Update(msg)
	m2Browse := m2.(browseModel)

	if m2Browse.snapshot == nil {
		t.Error("snapshot debería no ser nil")
	}
	if m2Browse.errorMsg != "" {
		t.Errorf("errorMsg debería estar vacío, got: %s", m2Browse.errorMsg)
	}
	if len(m2Browse.ticketRows) != 1 {
		t.Errorf("ticketRows len = %d, want 1", len(m2Browse.ticketRows))
	}

	// Snapshot con error
	errMsg := fmt.Errorf("error de conexión")
	msgErr := fetchSnapshotResult{snap: nil, err: errMsg}
	m3, _ := m2.Update(msgErr)
	m3Browse := m3.(browseModel)

	if m3Browse.errorMsg == "" {
		t.Error("errorMsg debería no estar vacío tras error")
	}
	if !strings.Contains(m3Browse.errorMsg, "Error al refrescar") {
		t.Errorf("errorMsg no contiene 'Error al refrescar': %s", m3Browse.errorMsg)
	}
}

// TestBrowseModel_Update_WindowSize verifica el manejo de redimensionamiento.
func TestBrowseModel_Update_WindowSize(t *testing.T) {
	m := initialBrowseModel(&mockCockpitReader{}, nil)

	// Tamaño normal
	msg := tea.WindowSizeMsg{Width: 100, Height: 30}
	m2, _ := m.Update(msg)
	m2Browse := m2.(browseModel)

	if m2Browse.width != 100 {
		t.Errorf("width = %d, want 100", m2Browse.width)
	}
	if m2Browse.height != 30 {
		t.Errorf("height = %d, want 30", m2Browse.height)
	}

	// Tamaño muy pequeño: no debe panikear
	msgSmall := tea.WindowSizeMsg{Width: 10, Height: 5}
	m3, _ := m2.Update(msgSmall)
	m3Browse := m3.(browseModel)
	_ = m3Browse.View()
}

// TestBrowseModel_View_NilSnapshot verifica que View no panikea con snapshot nil.
func TestBrowseModel_View_NilSnapshot(t *testing.T) {
	m := initialBrowseModel(&mockCockpitReader{}, nil)
	view := m.View()

	if !strings.Contains(view, "ALLOSTASIS") {
		t.Error("View debería contener 'ALLOSTASIS'")
	}
	if !strings.Contains(view, "Cargando") {
		t.Error("View con snapshot nil debería contener 'Cargando'")
	}
}

// TestBrowseModel_View_WithSnapshot verifica que View muestra contenido con snapshot.
func TestBrowseModel_View_WithSnapshot(t *testing.T) {
	snap := newTestSnapshot()
	m := initialBrowseModel(&mockCockpitReader{}, snap)

	// Simular WindowSizeMsg para evitar ancho 0
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2Browse := m2.(browseModel)

	view := m2Browse.View()

	if !strings.Contains(view, "ALLOSTASIS") {
		t.Error("View debería contener 'ALLOSTASIS'")
	}
	if !strings.Contains(view, "📊 Cockpit") {
		t.Error("View debería contener la pestaña Cockpit")
	}
	// La pestaña activa por defecto es Cockpit; el ticket aparece en la pestaña Tickets
	// Ir a la pestaña Tickets (índice 1)
	m3, _ := m2Browse.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m3Browse := m3.(browseModel)
	viewTickets := m3Browse.View()
	if !strings.Contains(viewTickets, "OMNIFY-175") {
		t.Error("View de la pestaña Tickets debería contener el ticket activo")
	}
}

// TestBrowseModel_View_ErrorMsg verifica que el mensaje de error se muestra en la pestaña Tickets.
func TestBrowseModel_View_ErrorMsg(t *testing.T) {
	m := initialBrowseModel(&mockCockpitReader{}, nil)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2Browse := m2.(browseModel)

	// Simular error de snapshot
	msg := fetchSnapshotResult{snap: nil, err: fmt.Errorf("conexión fallida")}
	m3, _ := m2Browse.Update(msg)
	m3Browse := m3.(browseModel)

	// Ir a la pestaña Tickets (índice 1)
	m4, _ := m3Browse.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m4Browse := m4.(browseModel)

	view := m4Browse.View()
	if !strings.Contains(view, "Error al refrescar") {
		t.Error("View debería mostrar el mensaje de error en la pestaña Tickets")
	}
}

// TestBrowseModel_View_Tabs verifica que todas las pestañas se renderizan sin panic.
func TestBrowseModel_View_Tabs(t *testing.T) {
	snap := newTestSnapshot()
	m := initialBrowseModel(&mockCockpitReader{}, snap)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(browseModel)

	for i, tab := range tabs {
		// Ir a la pestaña i
		for j := 0; j < i; j++ {
			tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
			m = tm.(browseModel)
		}
		view := m.View()
		if !strings.Contains(view, tab.Label) {
			t.Errorf("pestaña %d (%s) no aparece en View", i, tab.Label)
		}
	}
}

// TestBrowseModel_Update_RefreshKey verifica que 'r' despacha el comando de refresh.
func TestBrowseModel_Update_RefreshKey(t *testing.T) {
	mock := &mockCockpitReader{
		snapshot: newTestSnapshot(),
	}
	m := initialBrowseModel(mock, nil)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Error("presionar 'r' debería despachar un comando de refresh")
	}
}

// TestInitialBrowseModel verifica que el factory inicializa correctamente.
func TestInitialBrowseModel(t *testing.T) {
	snap := newTestSnapshot()
	mock := &mockCockpitReader{snapshot: snap}

	m := initialBrowseModel(mock, snap)

	if m.currentTab != 0 {
		t.Errorf("currentTab = %d, want 0", m.currentTab)
	}
	if m.snapshot == nil {
		t.Error("snapshot debería no ser nil")
	}
	if m.cockpitSvc == nil {
		t.Error("cockpitSvc debería no ser nil")
	}
	if len(m.ticketRows) != 1 {
		t.Errorf("ticketRows len = %d, want 1", len(m.ticketRows))
	}
}

// TestInitialBrowseModel_NilSnapshot verifica que el factory no panikea con nil.
func TestInitialBrowseModel_NilSnapshot(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("initialBrowseModel(nil) panikeó: %v", r)
		}
	}()

	m := initialBrowseModel(&mockCockpitReader{}, nil)
	if m.snapshot != nil {
		t.Error("snapshot debería ser nil")
	}
	if len(m.ticketRows) != 0 {
		t.Error("ticketRows debería estar vacío con snapshot nil")
	}
}

// TestBrowseModel_TableNavigation verifica que las teclas de flecha se delegan
// a la tabla de tickets cuando la pestaña activa es Tickets.
func TestBrowseModel_TableNavigation(t *testing.T) {
	snap := newTestSnapshot()
	m := initialBrowseModel(&mockCockpitReader{}, snap)

	// Ir a la pestaña Tickets (índice 1)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m2Browse := m2.(browseModel)

	// Flecha abajo debería ser procesada por la tabla (no cambiar la pestaña)
	m3, _ := m2Browse.Update(tea.KeyMsg{Type: tea.KeyDown})
	m3Browse := m3.(browseModel)
	if m3Browse.currentTab != 1 {
		t.Errorf("currentTab después de flecha abajo = %d, want 1 (debería seguir en Tickets)", m3Browse.currentTab)
	}

	// Flecha arriba también debería ser procesada por la tabla
	m4, _ := m3Browse.Update(tea.KeyMsg{Type: tea.KeyUp})
	m4Browse := m4.(browseModel)
	if m4Browse.currentTab != 1 {
		t.Errorf("currentTab después de flecha arriba = %d, want 1", m4Browse.currentTab)
	}

	// Enter no debería cambiar la pestaña ni salir
	m5, _ := m4Browse.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m5Browse := m5.(browseModel)
	if m5Browse.currentTab != 1 {
		t.Errorf("currentTab después de Enter = %d, want 1", m5Browse.currentTab)
	}
	if m5Browse.quitting {
		t.Error("Enter no debería cerrar la TUI")
	}
}

// TestBrowseModel_TableNavigation_OtherTabs verifica que las flechas NO se delegan
// a la tabla cuando la pestaña activa no es Tickets.
func TestBrowseModel_TableNavigation_OtherTabs(t *testing.T) {
	snap := newTestSnapshot()
	m := initialBrowseModel(&mockCockpitReader{}, snap)

	// En la pestaña Cockpit (índice 0), las flechas no deberían cambiar la pestaña
	// (no hay tabla para delegar, y las flechas no son atajos de pestaña)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2Browse := m2.(browseModel)
	if m2Browse.currentTab != 0 {
		t.Errorf("currentTab después de flecha abajo en Cockpit = %d, want 0", m2Browse.currentTab)
	}
}

// TestBrowseModel_TableFocused verifica que la tabla de tickets se crea enfocada.
func TestBrowseModel_TableFocused(t *testing.T) {
	snap := newTestSnapshot()
	m := initialBrowseModel(&mockCockpitReader{}, snap)

	if !m.issueTable.Focused() {
		t.Error("issueTable debería estar enfocada para permitir navegación con flechas")
	}
}
