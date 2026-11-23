package tui

import (
	"fmt"
	"strings"

	"github.com/JohanTv/allostasis/internal/app"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	service    app.CockpitReader
	snapshot   *app.CockpitSnapshot
	issueTable table.Model
	err        error
	quitting   bool
	errorMsg   string
	width      int
	height     int
}

func initialModel(svc app.CockpitReader, snap *app.CockpitSnapshot) model {
	columns := []table.Column{
		{Title: "TICKET", Width: 14},
		{Title: "ESTADO", Width: 12},
		{Title: "PRIORIDAD", Width: 10},
		{Title: "TÍTULO", Width: 40},
	}

	rows := snapshotToRows(snap)

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(6),
	)
	t.SetStyles(buildTableStyles())
	t.SetWidth(40) // ancho por defecto, se ajusta con WindowSizeMsg

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
			return m, newFetchSnapshotCmd(m.service)
		}
	case fetchSnapshotResult:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("⚠ Error al refrescar: %s", msg.err.Error())
		} else if msg.snap != nil {
			m.snapshot = msg.snap
			m.errorMsg = ""
			rows := snapshotToRows(msg.snap)
			m.issueTable.SetRows(rows)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		w := msg.Width - 6
		if w < 40 {
			w = 40
		}
		m.issueTable.SetWidth(w)
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
	if m.snapshot != nil {
		clockStr := MutedStyle.Render(m.snapshot.Timestamp.Format("2006-01-02 15:04:05 MST"))
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", clockStr))
	} else {
		b.WriteString(title)
	}
	b.WriteString("\n\n")

	if m.snapshot == nil {
		b.WriteString(MutedStyle.Render("  (Cargando datos...)\n"))
		b.WriteString(HelpStyle.Render("Atajos: [r] Refrescar datos  │  [q / esc] Salir"))
		b.WriteString("\n")
		return b.String()
	}

	// 2. Indicador de Carga Alostática y Ley de Little
	b.WriteString(renderMetricsCard(m.snapshot))

	// 2.5. Mensaje de error (si el refresh falló)
	if m.errorMsg != "" {
		b.WriteString(WarningBadgeStyle.Render(m.errorMsg))
		b.WriteString("\n")
	}

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
	b.WriteString(renderWorktreesList(m.snapshot))

	// 5. Barra de Ayuda
	help := HelpStyle.Render("Atajos: [↑/↓] Navegar tabla  │  [r] Refrescar datos  │  [q / esc] Salir")
	b.WriteString(help)
	b.WriteString("\n")

	return b.String()
}

// RunCockpit inicia el loop de Bubble Tea en la terminal interactiva.
func RunCockpit(svc app.CockpitReader, snap *app.CockpitSnapshot) error {
	p := tea.NewProgram(initialModel(svc, snap), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
