package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allostasis/alo/internal/app"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type snapshotMsg struct {
	snap *app.CockpitSnapshot
	err  error
}

func fetchSnapshotCmd(svc *app.CockpitService) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snap, err := svc.GetSnapshot(ctx, ".worktrees")
		return snapshotMsg{snap: snap, err: err}
	}
}

type model struct {
	service    *app.CockpitService
	snapshot   *app.CockpitSnapshot
	issueTable table.Model
	err        error
	quitting   bool
	width      int
	height     int
}

func initialModel(svc *app.CockpitService, snap *app.CockpitSnapshot) model {
	columns := []table.Column{
		{Title: "TICKET", Width: 14},
		{Title: "ESTADO", Width: 12},
		{Title: "PRIORIDAD", Width: 10},
		{Title: "TÍTULO", Width: 40},
	}

	var rows []table.Row
	for _, it := range snap.ActiveIssues {
		rows = append(rows, table.Row{
			it.Key,
			string(it.State),
			it.Priority,
			it.Title,
		})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(6),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorSecondary).
		BorderBottom(true).
		Bold(true).
		Foreground(ColorPrimary)
	s.Selected = s.Selected.
		Foreground(ColorText).
		Background(ColorSecondary).
		Bold(true)
	t.SetStyles(s)

	return model{
		service:    svc,
		snapshot:   snap,
		issueTable: t,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "r":
			// Despachar comando asíncrono para no bloquear el loop de renderizado
			return m, fetchSnapshotCmd(m.service)
		}
	case snapshotMsg:
		if msg.err == nil && msg.snap != nil {
			m.snapshot = msg.snap
			var rows []table.Row
			for _, it := range msg.snap.ActiveIssues {
				rows = append(rows, table.Row{
					it.Key,
					string(it.State),
					it.Priority,
					it.Title,
				})
			}
			m.issueTable.SetRows(rows)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.issueTable.SetWidth(msg.Width - 6)
	}

	m.issueTable, cmd = m.issueTable.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.quitting {
		return "\n  ✨ Cockpit de Allostasis cerrado. ¡Mantén tu foco soberano!\n\n"
	}

	var b strings.Builder

	// 1. Header
	title := TitleStyle.Render("🧬 ALLOSTASIS // Developer Cockpit v0.1.0")
	clockStr := MutedStyle.Render(m.snapshot.Timestamp.Format("2006-01-02 15:04:05 MST"))
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", clockStr))
	b.WriteString("\n\n")

	// 2. Indicador de Carga Alostática y Ley de Little
	kleeHours := float64(m.snapshot.KleeMeasureMinutes) / 60.0
	toilRatio := m.snapshot.AllostaticLoad.ToilRatio() * 100.0
	deepRatio := m.snapshot.AllostaticLoad.DeepWorkRatio() * 100.0

	statusBadge := BadgeStyle.Render("STATUS: " + m.snapshot.StatusSummary)
	if m.snapshot.AllostaticLoad.ToilRatio() > 0.4 {
		statusBadge = WarningBadgeStyle.Render("ALERTA: " + m.snapshot.StatusSummary)
	}

	metricsText := fmt.Sprintf(
		"Tiempo Neto Klee: %.2f hrs  │  WIP: %d  │  Toil Ratio: %.1f%%  │  Deep Work: %.1f%%\nTiempo de Ciclo Proyectado (Little's Law): %.1f días",
		kleeHours,
		m.snapshot.AllostaticLoad.WIPCount,
		toilRatio,
		deepRatio,
		m.snapshot.CycleTimeDays,
	)

	cardContent := lipgloss.JoinVertical(lipgloss.Left, statusBadge, "\n", metricsText)
	b.WriteString(CardStyle.Render(cardContent))
	b.WriteString("\n")

	// 3. Tabla de Tareas Activas
	b.WriteString(HeaderStyle.Render("📋 Tareas Activas en Progreso (WIP Limit Guard)"))
	b.WriteString("\n")
	if len(m.snapshot.ActiveIssues) == 0 {
		b.WriteString(MutedStyle.Render("   (No hay tareas activas en progreso en este momento)\n\n"))
	} else {
		b.WriteString(m.issueTable.View())
		b.WriteString("\n\n")
	}

	// 4. Sección Worktrees
	b.WriteString(HeaderStyle.Render("🌳 Worktrees Multi-Repo Aislados (.worktrees/)"))
	b.WriteString("\n")
	if len(m.snapshot.ActiveWorktrees) == 0 {
		b.WriteString(MutedStyle.Render("   (No hay contenedores multi-repo activos)\n\n"))
	} else {
		for _, wt := range m.snapshot.ActiveWorktrees {
			b.WriteString(fmt.Sprintf("   • %s (%d repos vinculados)\n", wt.Slug, len(wt.Bindings)))
			for _, bnd := range wt.Bindings {
				cleanStr := "✓ Limpio"
				if !bnd.IsClean {
					cleanStr = "⚠ Cambios pendientes"
				}
				b.WriteString(fmt.Sprintf("       - %-12s [%s] %s\n", bnd.RepoName, bnd.Branch, cleanStr))
			}
		}
		b.WriteString("\n")
	}

	// 5. Barra de Ayuda
	help := HelpStyle.Render("Atajos: [↑/↓] Navegar tabla  │  [r] Refrescar datos  │  [q / esc] Salir")
	b.WriteString(help)
	b.WriteString("\n")

	return b.String()
}

// RunCockpit inicia el loop de Bubble Tea en la terminal interactiva.
func RunCockpit(svc *app.CockpitService, snap *app.CockpitSnapshot) error {
	p := tea.NewProgram(initialModel(svc, snap), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
