package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JohanTv/allostasis/internal/app"
	"github.com/JohanTv/allostasis/internal/domain/issue"
	"github.com/JohanTv/allostasis/internal/domain/ledger"
	"github.com/JohanTv/allostasis/internal/domain/workspace"
	"github.com/charmbracelet/bubbles/table"
)

// TestSnapshotToRows_TableDriven verifica que la conversión de ActiveIssues a filas de tabla
// produce el resultado esperado en diversos escenarios.
func TestSnapshotToRows_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		snap     *app.CockpitSnapshot
		wantLen  int
		wantRows []table.Row
	}{
		{
			name:    "snapshot nil devuelve filas vacías",
			snap:    nil,
			wantLen: 0,
		},
		{
			name: "snapshot sin issues activos devuelve filas vacías",
			snap: &app.CockpitSnapshot{
				ActiveIssues: []issue.WorkItem{},
			},
			wantLen: 0,
		},
		{
			name: "un solo ticket se convierte correctamente",
			snap: &app.CockpitSnapshot{
				ActiveIssues: []issue.WorkItem{
					{Key: "OMNIFY-175", State: "in_progress", Priority: "high", Title: "Implementar validaciones"},
				},
			},
			wantLen: 1,
			wantRows: []table.Row{
				{"OMNIFY-175", "in_progress", "high", "Implementar validaciones"},
			},
		},
		{
			name: "múltiples tickets preservan orden",
			snap: &app.CockpitSnapshot{
				ActiveIssues: []issue.WorkItem{
					{Key: "OPS-4", State: "started", Priority: "urgent", Title: "Webhooks WhatsApp"},
					{Key: "OMNIFY-268", State: "in_progress", Priority: "high", Title: "Módulo mailing"},
					{Key: "OMNIFY-259", State: "in_progress", Priority: "medium", Title: "Seguridad directorio"},
				},
			},
			wantLen: 3,
			wantRows: []table.Row{
				{"OPS-4", "started", "urgent", "Webhooks WhatsApp"},
				{"OMNIFY-268", "in_progress", "high", "Módulo mailing"},
				{"OMNIFY-259", "in_progress", "medium", "Seguridad directorio"},
			},
		},
		{
			name: "ticket con prioridad vacía se convierte sin panic",
			snap: &app.CockpitSnapshot{
				ActiveIssues: []issue.WorkItem{
					{Key: "ALO-1", State: "backlog", Priority: "", Title: "Sin prioridad"},
				},
			},
			wantLen: 1,
			wantRows: []table.Row{
				{"ALO-1", "backlog", "", "Sin prioridad"},
			},
		},
		{
			name: "ticket con título largo se convierte sin truncar",
			snap: &app.CockpitSnapshot{
				ActiveIssues: []issue.WorkItem{
					{Key: "LONG-1", State: "in_progress", Priority: "low", Title: "Este es un título extremadamente largo que no debería truncarse en la conversión a filas de tabla"},
				},
			},
			wantLen: 1,
			wantRows: []table.Row{
				{"LONG-1", "in_progress", "low", "Este es un título extremadamente largo que no debería truncarse en la conversión a filas de tabla"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := snapshotToRows(tt.snap)

			if len(rows) != tt.wantLen {
				t.Errorf("snapshotToRows() len = %d, want %d", len(rows), tt.wantLen)
			}

			for i, want := range tt.wantRows {
				if i >= len(rows) {
					t.Errorf("fila %d ausente", i)
					continue
				}
				for j, cell := range want {
					if rows[i][j] != cell {
						t.Errorf("fila %d, columna %d = %q, want %q", i, j, rows[i][j], cell)
					}
				}
			}
		})
	}
}

// TestBuildTableStyles verifica que los estilos de tabla se construyen sin panic
// y que las propiedades clave están configuradas.
func TestBuildTableStyles(t *testing.T) {
	ts := buildTableStyles()

	// Verificar que el header tiene borde inferior
	if !ts.Header.GetBorderBottom() {
		t.Error("se esperaba BorderBottom(true) en header")
	}

	// Verificar que el header es bold
	if !ts.Header.GetBold() {
		t.Error("se esperaba Bold(true) en header")
	}

	// Verificar que Selected es bold
	if !ts.Selected.GetBold() {
		t.Error("se esperaba Bold(true) en Selected")
	}

	// Verificar que no panikea al acceder a propiedades
	_ = ts.Header.Render("test")
	_ = ts.Selected.Render("test")
}

// TestBuildTicketTable verifica que la construcción de la tabla de tickets
// inicializa correctamente columnas y filas.
func TestBuildTicketTable(t *testing.T) {
	snap := &app.CockpitSnapshot{
		ActiveIssues: []issue.WorkItem{
			{Key: "A-1", State: "in_progress", Priority: "high", Title: "Primera"},
			{Key: "A-2", State: "started", Priority: "medium", Title: "Segunda"},
		},
	}

	tbl := buildTicketTable(snap, 8)

	// Verificar columnas
	columns := tbl.Columns()
	if len(columns) != 4 {
		t.Errorf("columnas = %d, want 4", len(columns))
	}

	wantTitles := []string{"TICKET", "ESTADO", "PRIORIDAD", "TÍTULO"}
	for i, col := range columns {
		if col.Title != wantTitles[i] {
			t.Errorf("columna %d título = %q, want %q", i, col.Title, wantTitles[i])
		}
	}

	// Verificar filas
	rows := tbl.Rows()
	if len(rows) != 2 {
		t.Errorf("filas = %d, want 2", len(rows))
	}

	// Verificar que la tabla está enfocada para permitir navegación con flechas
	if !tbl.Focused() {
		t.Error("la tabla debería estar enfocada para navegación con flechas")
	}
}

// TestBuildTicketTable_NilSnapshot verifica que buildTicketTable no panikea con snapshot nil.
func TestBuildTicketTable_NilSnapshot(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("buildTicketTable(nil) panikeó: %v", r)
		}
	}()

	tbl := buildTicketTable(nil, 4)
	rows := tbl.Rows()
	if len(rows) != 0 {
		t.Errorf("filas con snapshot nil = %d, want 0", len(rows))
	}
}

// TestRenderMetricsCard_TableDriven verifica que la tarjeta de métricas alostáticas
// se renderiza correctamente en diversos escenarios de carga cognitiva.
func TestRenderMetricsCard_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		snap           *app.CockpitSnapshot
		wantContains   []string // cadenas que deben aparecer en el output
		wantNotContain []string // cadenas que NO deben aparecer
	}{
		{
			name: "snapshot nil devuelve mensaje de cargando",
			snap: nil,
			wantContains: []string{"Cargando"},
		},
		{
			name: "estado nominal muestra STATUS sin ALERTA",
			snap: &app.CockpitSnapshot{
				KleeMeasureMinutes: 120,
				AllostaticLoad: ledger.AllostaticLoad{
					TotalTime:    120 * time.Minute,
					DeepWorkTime: 90 * time.Minute,
					ToilTime:     30 * time.Minute,
					WIPCount:     2,
				},
				CycleTimeDays: 4.0,
				StatusSummary: "Estado nominal óptimo",
			},
			wantContains:   []string{"STATUS:", "Estado nominal óptimo", "Tiempo Neto Klee", "WIP:", "Toil Ratio", "Deep Work", "Little's Law"},
			wantNotContain: []string{"ALERTA:"},
		},
		{
			name: "toil ratio alto muestra ALERTA",
			snap: &app.CockpitSnapshot{
				KleeMeasureMinutes: 60,
				AllostaticLoad: ledger.AllostaticLoad{
					TotalTime:    60 * time.Minute,
					DeepWorkTime: 20 * time.Minute,
					ToilTime:     40 * time.Minute, // >40% → ALERTA
					WIPCount:     1,
				},
				CycleTimeDays: 2.0,
				StatusSummary: "Sobrecarga de trabajo invisible (Toil crítico)",
			},
			wantContains:   []string{"ALERTA:", "Sobrecarga de trabajo invisible"},
			wantNotContain: []string{"STATUS:"},
		},
		{
			name: "cero minutos no divide por cero",
			snap: &app.CockpitSnapshot{
				KleeMeasureMinutes: 0,
				AllostaticLoad: ledger.AllostaticLoad{
					TotalTime:    0,
					DeepWorkTime: 0,
					ToilTime:     0,
					WIPCount:     0,
				},
				CycleTimeDays: 0.0,
				StatusSummary: "Estado nominal óptimo",
			},
			wantContains:   []string{"STATUS:", "0.00 hrs", "WIP: 0", "0.0%"},
			wantNotContain: []string{"ALERTA:"},
		},
		{
			name: "WIP elevado muestra congestión",
			snap: &app.CockpitSnapshot{
				KleeMeasureMinutes: 180,
				AllostaticLoad: ledger.AllostaticLoad{
					TotalTime:    180 * time.Minute,
					DeepWorkTime: 150 * time.Minute,
					ToilTime:     30 * time.Minute, // 16.7% — bajo
					WIPCount:     5,                // >3 → congestión
				},
				CycleTimeDays: 10.0,
				StatusSummary: "Congestión por WIP elevado (Riesgo de cambio de contexto)",
			},
			wantContains:   []string{"STATUS:", "Congestión por WIP elevado", "WIP: 5"},
			wantNotContain: []string{"ALERTA:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := renderMetricsCard(tt.snap)

			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("renderMetricsCard() debería contener %q, pero no lo encontró.\nOutput:\n%s", want, output)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if strings.Contains(output, notWant) {
					t.Errorf("renderMetricsCard() NO debería contener %q, pero lo encontró.\nOutput:\n%s", notWant, output)
				}
			}
		})
	}
}

// mockCockpitReader implementa app.CockpitReader para pruebas unitarias.
type mockCockpitReader struct {
	snapshot *app.CockpitSnapshot
	err      error
}

func (m *mockCockpitReader) GetSnapshot(ctx context.Context, worktreeRoot string) (*app.CockpitSnapshot, error) {
	return m.snapshot, m.err
}

// TestRenderWorktreesList_TableDriven verifica que la lista de worktrees
// se renderiza correctamente en diversos escenarios.
func TestRenderWorktreesList_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		snap           *app.CockpitSnapshot
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "snapshot nil devuelve mensaje vacío",
			snap: nil,
			wantContains: []string{"No hay contenedores"},
		},
		{
			name: "sin worktrees activos devuelve mensaje vacío",
			snap: &app.CockpitSnapshot{
				ActiveWorktrees: []workspace.Container{},
			},
			wantContains: []string{"No hay contenedores"},
		},
		{
			name: "un worktree limpio se muestra correctamente",
			snap: &app.CockpitSnapshot{
				ActiveWorktrees: []workspace.Container{
					{
						TicketID: "OMNIFY-175",
						Slug:     "campaign-validations",
						Bindings: []workspace.WorktreeBinding{
							{RepoName: "omnify-web", Branch: "feat/OMNIFY-175", IsClean: true},
							{RepoName: "omnify-api", Branch: "feat/OMNIFY-175", IsClean: true},
						},
					},
				},
			},
			wantContains: []string{
				"campaign-validations",
				"OMNIFY-175",
				"2 repos",
				"omnify-web",
				"feat/OMNIFY-175",
				"✓ Limpio",
			},
		},
		{
			name: "worktree con cambios pendientes muestra advertencia",
			snap: &app.CockpitSnapshot{
				ActiveWorktrees: []workspace.Container{
					{
						TicketID: "OPS-4",
						Slug:     "whatsapp-webhooks",
						Bindings: []workspace.WorktreeBinding{
							{RepoName: "omnify-web", Branch: "infra/OPS-4", IsClean: false},
						},
					},
				},
			},
			wantContains: []string{
				"whatsapp-webhooks",
				"OPS-4",
				"⚠ Cambios pendientes",
			},
		},
		{
			name: "múltiples worktrees preservan orden",
			snap: &app.CockpitSnapshot{
				ActiveWorktrees: []workspace.Container{
					{
						TicketID: "A-1",
						Slug:     "primero",
						Bindings: []workspace.WorktreeBinding{
							{RepoName: "repo-a", Branch: "main", IsClean: true},
						},
					},
					{
						TicketID: "B-2",
						Slug:     "segundo",
						Bindings: []workspace.WorktreeBinding{
							{RepoName: "repo-b", Branch: "dev", IsClean: false},
						},
					},
				},
			},
			wantContains: []string{
				"primero",
				"segundo",
				"✓ Limpio",
				"⚠ Cambios pendientes",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := renderWorktreesList(tt.snap)

			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("renderWorktreesList() debería contener %q, pero no lo encontró.\nOutput:\n%s", want, output)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if strings.Contains(output, notWant) {
					t.Errorf("renderWorktreesList() NO debería contener %q, pero lo encontró.\nOutput:\n%s", notWant, output)
				}
			}
		})
	}
}

// TestNewFetchSnapshotCmd verifica que newFetchSnapshotCmd devuelve un tea.Cmd
// que ejecuta GetSnapshot y retorna un fetchSnapshotResult.
func TestNewFetchSnapshotCmd(t *testing.T) {
	tests := []struct {
		name     string
		snapshot *app.CockpitSnapshot
		err      error
	}{
		{
			name: "snapshot exitoso devuelve mensaje sin error",
			snapshot: &app.CockpitSnapshot{
				Timestamp:     time.Date(2026, 11, 22, 15, 0, 0, 0, time.UTC),
				StatusSummary: "Estado nominal óptimo",
			},
			err: nil,
		},
		{
			name:     "error de servicio devuelve mensaje con error",
			snapshot: nil,
			err:      fmt.Errorf("conexión fallida"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockCockpitReader{snapshot: tt.snapshot, err: tt.err}

			cmd := newFetchSnapshotCmd(mock)
			msg := cmd()

			result, ok := msg.(fetchSnapshotResult)
			if !ok {
				t.Fatalf("cmd() debería retornar fetchSnapshotResult, got %T", msg)
			}
			if result.snap != tt.snapshot {
				t.Errorf("snap = %v, want %v", result.snap, tt.snapshot)
			}
			if tt.err == nil && result.err != nil {
				t.Errorf("err debería ser nil, got: %v", result.err)
			}
			if tt.err != nil && result.err == nil {
				t.Error("err debería no ser nil")
			}
		})
	}
}

// TestNewFetchSnapshotCmd_ReturnsTeaCmd verifica que newFetchSnapshotCmd
// devuelve un tea.Cmd válido (no nil).
func TestNewFetchSnapshotCmd_ReturnsTeaCmd(t *testing.T) {
	mock := &mockCockpitReader{}
	cmd := newFetchSnapshotCmd(mock)
	if cmd == nil {
		t.Error("newFetchSnapshotCmd no debería retornar nil")
	}
}

// TestBuildTicketTable_Height verifica que la altura de la tabla se ajusta
// dinámicamente según el parámetro height.
func TestBuildTicketTable_Height(t *testing.T) {
	tests := []struct {
		name       string
		snap       *app.CockpitSnapshot
		height     int
		wantHeight int
	}{
		{
			name:       "altura mínima con snapshot nil",
			snap:       nil,
			height:     4,
			wantHeight: 3, // bubbles/table resta 1 para el header
		},
		{
			name: "altura personalizada con snapshot",
			snap: &app.CockpitSnapshot{
				ActiveIssues: []issue.WorkItem{
					{Key: "OMNIFY-175", State: "in_progress", Priority: "high", Title: "Test"},
				},
			},
			height:     10,
			wantHeight: 9, // bubbles/table resta 1 para el header
		},
		{
			name: "altura muy grande se respeta",
			snap: &app.CockpitSnapshot{},
			height: 20,
			wantHeight: 19, // bubbles/table resta 1 para el header
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tbl := buildTicketTable(tt.snap, tt.height)
			if tbl.Height() != tt.wantHeight {
				t.Errorf("tabla Height() = %d, want %d", tbl.Height(), tt.wantHeight)
			}
		})
	}
}

// TestRenderLog_Structure verifica que renderLog tiene una estructura visual
// clara: header de sección, separador, métricas con badges, lista de tickets
// y ayuda. Similar a renderWorkspaces y renderTickets.
func TestRenderLog_Structure(t *testing.T) {
	snap := &app.CockpitSnapshot{
		TodayMinutes:       120,
		KleeMeasureMinutes: 90,
		ActiveIssues: []issue.WorkItem{
			{Key: "OMNIFY-175", State: "in_progress", Priority: "high", Title: "Test ticket"},
		},
	}
	output := renderLog(snap)

	// 1. Header de sección al inicio
	if !strings.Contains(output, "Registro de Tiempo") {
		t.Error("renderLog() debería contener el header 'Registro de Tiempo'")
	}

	// 2. Métricas con badges
	if !strings.Contains(output, "120 min") {
		t.Error("renderLog() debería mostrar '120 min'")
	}
	if !strings.Contains(output, "1.5 hrs") {
		t.Error("renderLog() debería mostrar '1.5 hrs'")
	}

	// 3. Lista de tickets
	if !strings.Contains(output, "OMNIFY-175") {
		t.Error("renderLog() debería listar el ticket 'OMNIFY-175'")
	}

	// 4. Referencia a comandos
	if !strings.Contains(output, "alo log") {
		t.Error("renderLog() debería referenciar 'alo log'")
	}

	// 5. No debe usar CardStyle (bordes redondeados)
	for _, corner := range []string{"╭", "╰", "╮", "╯"} {
		if strings.Contains(output, corner) {
			t.Errorf("renderLog() no debería usar CardStyle (detectada esquina %q)", corner)
			break
		}
	}
}

// TestRenderLog_HeaderFirst verifica que el header de sección aparece
// en la primera línea del output, sin líneas vacías previas.
func TestRenderLog_HeaderFirst(t *testing.T) {
	snap := &app.CockpitSnapshot{
		TodayMinutes:       30,
		KleeMeasureMinutes: 20,
		ActiveIssues:       []issue.WorkItem{},
	}
	output := renderLog(snap)

	// El header debe estar en la primera línea
	firstLine := strings.Split(output, "\n")[0]
	if !strings.Contains(firstLine, "Registro de Tiempo") {
		t.Errorf("la primera línea no contiene el header: %q", firstLine)
	}
}

// TestRenderLog_LineCount verifica que renderLog produce un número de líneas
// razonable: al menos 5 (header + separador + métricas + tickets + ayuda)
// y como máximo 12 (para no desplazar el header global de la TUI).
func TestRenderLog_LineCount(t *testing.T) {
	snap := &app.CockpitSnapshot{
		TodayMinutes:       120,
		KleeMeasureMinutes: 90,
		ActiveIssues: []issue.WorkItem{
			{Key: "OMNIFY-175", State: "in_progress", Priority: "high", Title: "Test ticket"},
		},
	}
	output := renderLog(snap)
	lines := strings.Count(output, "\n")

	if lines < 5 {
		t.Errorf("renderLog() produce solo %d líneas, debería ser ≥ 5 para tener estructura visual", lines)
	}
	if lines > 12 {
		t.Errorf("renderLog() produce %d líneas, debería ser ≤ 12 para no desplazar el header global", lines)
	}
}

// TestRenderLog_TableDriven verifica que la pestaña Log muestra información
// útil del snapshot (tiempo registrado hoy, tickets activos) con diseño visual
// estructurado (header, separadores, badges, lista con bullets).
func TestRenderLog_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		snap           *app.CockpitSnapshot
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "snapshot nil muestra mensaje de no hay datos",
			snap: nil,
			wantContains: []string{"Registro de Tiempo", "No hay datos"},
		},
		{
			name: "sin minutos registrados muestra mensaje adecuado",
			snap: &app.CockpitSnapshot{
				TodayMinutes:       0,
				KleeMeasureMinutes: 0,
				ActiveIssues:       []issue.WorkItem{},
			},
			wantContains: []string{"Registro de Tiempo", "0 min registrados"},
		},
		{
			name: "con minutos registrados muestra el tiempo",
			snap: &app.CockpitSnapshot{
				TodayMinutes:       120,
				KleeMeasureMinutes: 90,
				ActiveIssues: []issue.WorkItem{
					{Key: "OMNIFY-175", State: "in_progress", Priority: "high", Title: "Test ticket"},
				},
			},
			wantContains: []string{
				"Registro de Tiempo",
				"120 min",
				"1.5 hrs", // KleeMeasureMinutes=90 se muestra como 1.5 hrs
				"OMNIFY-175",
				"Tickets disponibles:",
			},
		},
		{
			name: "múltiples tickets activos se listan",
			snap: &app.CockpitSnapshot{
				TodayMinutes:       60,
				KleeMeasureMinutes: 45,
				ActiveIssues: []issue.WorkItem{
					{Key: "A-1", State: "in_progress", Priority: "high", Title: "Primero"},
					{Key: "B-2", State: "started", Priority: "medium", Title: "Segundo"},
				},
			},
			wantContains: []string{
				"Registro de Tiempo",
				"A-1",
				"Primero",
				"B-2",
				"Segundo",
			},
		},
		{
			name: "muestra referencia a comandos headless",
			snap: &app.CockpitSnapshot{
				TodayMinutes:       30,
				KleeMeasureMinutes: 20,
				ActiveIssues: []issue.WorkItem{
					{Key: "X-1", State: "in_progress", Priority: "low", Title: "Demo"},
				},
			},
			wantContains: []string{
				"Registro de Tiempo",
				"alo log -i",
				"alo log",
			},
		},
		{
			name: "sin tickets activos no muestra lista",
			snap: &app.CockpitSnapshot{
				TodayMinutes:       45,
				KleeMeasureMinutes: 30,
				ActiveIssues:       []issue.WorkItem{},
			},
			wantContains:   []string{"Registro de Tiempo", "45 min"},
			wantNotContain: []string{"Tickets disponibles:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := renderLog(tt.snap)

			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("renderLog() debería contener %q, pero no lo encontró.\nOutput:\n%s", want, output)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if strings.Contains(output, notWant) {
					t.Errorf("renderLog() NO debería contener %q, pero lo encontró.\nOutput:\n%s", notWant, output)
				}
			}
		})
	}
}
