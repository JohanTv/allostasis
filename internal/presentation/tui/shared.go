package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/JohanTv/allostasis/internal/app"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fetchSnapshotResult contiene el resultado de una llamada a GetSnapshot.
// Es el tipo compartido que unifica fetchSnapshotCmd entre cockpit.go y browse.go.
type fetchSnapshotResult struct {
	snap *app.CockpitSnapshot
	err  error
}

// newFetchSnapshotCmd crea un tea.Cmd que ejecuta GetSnapshot y retorna
// un fetchSnapshotResult. Los modelos (cockpit y browse) convierten este
// resultado a su tipo específico de mensaje en su Update().
// Esto elimina la duplicación entre cockpit.go y browse.go.
func newFetchSnapshotCmd(svc app.CockpitReader) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snap, err := svc.GetSnapshot(ctx, ".worktrees")
		return fetchSnapshotResult{snap: snap, err: err}
	}
}

// snapshotToRows convierte los issues activos de un snapshot a filas de tabla.
// Devuelve un slice vacío si el snapshot es nil o no tiene issues activos.
func snapshotToRows(snap *app.CockpitSnapshot) []table.Row {
	var rows []table.Row
	if snap == nil {
		return rows
	}
	for _, it := range snap.ActiveIssues {
		rows = append(rows, table.Row{
			it.Key,
			string(it.State),
			it.Priority,
			it.Title,
		})
	}
	return rows
}

// buildTableStyles construye los estilos compartidos para las tablas de tickets.
func buildTableStyles() table.Styles {
	ts := table.DefaultStyles()
	ts.Header = ts.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorSecondary).
		BorderBottom(true).
		Bold(true).
		Foreground(ColorPrimary)
	ts.Selected = ts.Selected.
		Foreground(ColorText).
		Background(ColorSecondary).
		Bold(true)
	return ts
}

// buildTicketTable crea una nueva tabla de tickets a partir de un snapshot.
// El parámetro height determina la altura máxima de la tabla (mínimo 4).
// Las columnas son TICKET, ESTADO, PRIORIDAD y TÍTULO.
func buildTicketTable(snap *app.CockpitSnapshot, height int) table.Model {
	columns := []table.Column{
		{Title: "TICKET", Width: 14},
		{Title: "ESTADO", Width: 14},
		{Title: "PRIORIDAD", Width: 10},
		{Title: "TÍTULO", Width: 50},
	}

	rows := snapshotToRows(snap)

	if height < 4 {
		height = 4
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(height),
	)
	t.SetStyles(buildTableStyles())
	return t
}

// renderLog genera el contenido de la pestaña Log con información útil
// del snapshot: tiempo registrado hoy, tiempo neto Klee y tickets activos.
// Sigue el mismo patrón compacto que renderWorkspaces: HeaderStyle para el
// título de sección, luego contenido sin líneas vacías innecesarias.
func renderLog(snap *app.CockpitSnapshot) string {
	var b strings.Builder

	// Header de sección — consistente con renderTickets() y renderWorkspaces()
	b.WriteString(HeaderStyle.Render("📅 Registro de Tiempo"))
	b.WriteString("\n")

	if snap == nil {
		b.WriteString(MutedStyle.Render("   (No hay datos disponibles)\n\n"))
		return b.String()
	}

	kleeHours := float64(snap.KleeMeasureMinutes) / 60.0

	// Métricas en una línea compacta
	b.WriteString(fmt.Sprintf("   %s registrado  │  %s neto (Klee)\n",
		BadgeStyle.Render(fmt.Sprintf("%d min", snap.TodayMinutes)),
		BadgeStyle.Render(fmt.Sprintf("%.1f hrs", kleeHours)),
	))

	if snap.TodayMinutes == 0 {
		b.WriteString(MutedStyle.Render("   (0 min registrados hoy)\n"))
	}

	if len(snap.ActiveIssues) > 0 {
		b.WriteString("\n")
		b.WriteString(MutedStyle.Render("   Tickets disponibles:\n"))
		for _, it := range snap.ActiveIssues {
			badge := BadgeStyle.Render(it.Key)
			b.WriteString(fmt.Sprintf("   • %s — %s\n", badge, it.Title))
		}
	}

	b.WriteString("\n")
	b.WriteString(MutedStyle.Render("   alo log -i (interactivo)  │  alo log <TICKET> <MIN> (headless)\n\n"))

	return b.String()
}

// renderMetricsCard genera la tarjeta de métricas alostáticas a partir de un snapshot.
// Si el snapshot es nil, devuelve un mensaje de "Cargando datos...".
// Incluye el badge de estado (STATUS o ALERTA cuando ToilRatio > 0.4),
// las métricas Klee, WIP, Toil/Deep Work ratios y el tiempo de ciclo proyectado.
func renderMetricsCard(snap *app.CockpitSnapshot) string {
	var b strings.Builder

	if snap == nil {
		b.WriteString(MutedStyle.Render("  (Cargando datos...)\n"))
		return b.String()
	}

	kleeHours := float64(snap.KleeMeasureMinutes) / 60.0
	toilRatio := snap.AllostaticLoad.ToilRatio() * 100.0
	deepRatio := snap.AllostaticLoad.DeepWorkRatio() * 100.0

	statusBadge := BadgeStyle.Render("STATUS: " + snap.StatusSummary)
	if snap.AllostaticLoad.ToilRatio() > 0.4 {
		statusBadge = WarningBadgeStyle.Render("ALERTA: " + snap.StatusSummary)
	}

	metricsText := fmt.Sprintf(
		"Tiempo Neto Klee: %.2f hrs  │  WIP: %d  │  Toil Ratio: %.1f%%  │  Deep Work: %.1f%%\nTiempo de Ciclo Proyectado (Little's Law): %.1f días",
		kleeHours,
		snap.AllostaticLoad.WIPCount,
		toilRatio,
		deepRatio,
		snap.CycleTimeDays,
	)

	cardContent := lipgloss.JoinVertical(lipgloss.Left, statusBadge, "\n", metricsText)
	b.WriteString(CardStyle.Render(cardContent))
	b.WriteString("\n")

	return b.String()
}

// renderWorktreesList genera la sección de worktrees activos a partir de un snapshot.
// Si el snapshot es nil o no tiene worktrees, devuelve un mensaje indicando que no hay contenedores.
func renderWorktreesList(snap *app.CockpitSnapshot) string {
	var b strings.Builder

	if snap == nil || len(snap.ActiveWorktrees) == 0 {
		b.WriteString(MutedStyle.Render("   (No hay contenedores de worktree activos)\n\n"))
		return b.String()
	}

	for _, wt := range snap.ActiveWorktrees {
		b.WriteString(fmt.Sprintf("   • %s (%s — %d repos)\n", wt.Slug, wt.TicketID, len(wt.Bindings)))
		for _, bnd := range wt.Bindings {
			cleanStr := "✓ Limpio"
			if !bnd.IsClean {
				cleanStr = "⚠ Cambios pendientes"
			}
			b.WriteString(fmt.Sprintf("       - %-12s [%s] %s\n", bnd.RepoName, bnd.Branch, cleanStr))
		}
	}
	b.WriteString("\n")

	return b.String()
}
