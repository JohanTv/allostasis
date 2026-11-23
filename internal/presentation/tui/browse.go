package tui

import (
	"fmt"
	"strings"

	"github.com/JohanTv/allostasis/internal/app"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// tabConfig describe una pestaña de la TUI: etiqueta, función de renderizado y atajo numérico.
type tabConfig struct {
	Label   string
	Render  func(browseModel) string
	KeyHint string
}

// tabs es la configuración table-driven de todas las pestañas navegables.
// Agregar una nueva pestaña requiere solo añadir un elemento a este slice.
var tabs = []tabConfig{
	{Label: "📊 Cockpit", Render: func(m browseModel) string { return m.renderCockpit() }, KeyHint: "1"},
	{Label: "📋 Tickets", Render: func(m browseModel) string { return m.renderTickets() }, KeyHint: "2"},
	{Label: "🌳 Workspaces", Render: func(m browseModel) string { return m.renderWorkspaces() }, KeyHint: "3"},
	{Label: "📅 Log", Render: func(m browseModel) string { return m.renderLog() }, KeyHint: "4"},
}

type browseModel struct {
	width        int
	height       int
	currentTab   int
	quitting     bool
	cockpitSvc   app.CockpitReader
	snapshot     *app.CockpitSnapshot
	issueTable   table.Model
	ticketRows   []table.Row
	widthChanged bool
	errorMsg     string
}

func initialBrowseModel(
	cockpitSvc app.CockpitReader,
	snap *app.CockpitSnapshot,
) browseModel {
	ticketRows := snapshotToRows(snap)
	ticketTable := buildTicketTable(snap, 8)

	return browseModel{
		currentTab: 0,
		cockpitSvc: cockpitSvc,
		snapshot:   snap,
		issueTable: ticketTable,
		ticketRows: ticketRows,
	}
}

func (m browseModel) Init() tea.Cmd {
	return nil
}

func (m browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		s := msg.String()
		switch s {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			m.currentTab = (m.currentTab + 1) % len(tabs)
		case "shift+tab":
			m.currentTab = (m.currentTab + len(tabs) - 1) % len(tabs)
		case "r":
			return m, newFetchSnapshotCmd(m.cockpitSvc)
		default:
			// Cuando la pestaña activa es Tickets, delegar las teclas a la tabla
			// para permitir navegación con flechas y Enter.
			if m.currentTab == 1 { // TabTickets
				var cmd2 tea.Cmd
				m.issueTable, cmd2 = m.issueTable.Update(msg)
				if cmd2 != nil {
					cmd = cmd2
				}
				// También permitir atajos numéricos mientras se navega la tabla
				if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
					idx := int(s[0] - '1')
					if idx < len(tabs) {
						m.currentTab = idx
					}
				}
				return m, cmd
			}
			// Atajos numéricos: "1", "2", "3", "4", ... → ir a la pestaña correspondiente
			if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
				idx := int(s[0] - '1') // "1" → índice 0
				if idx < len(tabs) {
					m.currentTab = idx
				}
			}
		}

	case fetchSnapshotResult:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("⚠ Error al refrescar: %s", msg.err.Error())
		} else if msg.snap != nil {
			m.snapshot = msg.snap
			m.errorMsg = ""
			rows := snapshotToRows(msg.snap)
			m.ticketRows = rows
			m.issueTable.SetRows(rows)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.widthChanged = true
	}

	if m.widthChanged {
		w := m.width - 6
		if w < 40 {
			w = 40
		}
		m.issueTable.SetWidth(w)
		m.widthChanged = false
	}

	return m, cmd
}

func (m browseModel) View() string {
	if m.quitting {
		return "\n  ✨ Allostasis TUI cerrada. ¡Mantén tu foco soberano!\n\n"
	}

	var b strings.Builder

	// 1. Header
	title := TitleStyle.Render("🧬 ALLOSTASIS // TUI v0.2.0")
	b.WriteString(title)
	b.WriteString("\n")

	// 2. Barra de pestañas
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	// 3. Contenido de la pestaña activa (table-driven)
	b.WriteString(tabs[m.currentTab].Render(m))

	// 4. Barra de ayuda
	b.WriteString(m.renderHelp())
	b.WriteString("\n")

	return b.String()
}

func (m browseModel) renderTabBar() string {
	var parts []string
	for i, tab := range tabs {
		if i == m.currentTab {
			parts = append(parts, BadgeStyle.Render(" "+tab.Label+" "))
		} else {
			parts = append(parts, MutedStyle.Render(" "+tab.Label+" "))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (m browseModel) renderCockpit() string {
	return renderMetricsCard(m.snapshot)
}

func (m browseModel) renderTickets() string {
	var b strings.Builder

	b.WriteString(HeaderStyle.Render("📋 Tareas Activas"))
	b.WriteString("\n")

	if m.errorMsg != "" {
		b.WriteString(WarningBadgeStyle.Render(m.errorMsg))
		b.WriteString("\n")
	}

	if len(m.ticketRows) == 0 {
		b.WriteString(MutedStyle.Render("   (No hay tareas activas en progreso)\n\n"))
	} else {
		b.WriteString(m.issueTable.View())
		b.WriteString("\n\n")
	}

	return b.String()
}

func (m browseModel) renderWorkspaces() string {
	var b strings.Builder

	b.WriteString(HeaderStyle.Render("🌳 Workspaces (Worktrees)"))
	b.WriteString("\n")

	b.WriteString(renderWorktreesList(m.snapshot))

	return b.String()
}

func (m browseModel) renderLog() string {
	return renderLog(m.snapshot)
}

func (m browseModel) renderHelp() string {
	return HelpStyle.Render("Tab / Shift+Tab: cambiar pestaña  │  1-4: ir a pestaña  │  r: refrescar  │  q / esc: salir")
}

// RunBrowse inicia la TUI unificada con pestañas navegables.
func RunBrowse(
	cockpitSvc app.CockpitReader,
	snap *app.CockpitSnapshot,
) error {
	m := initialBrowseModel(cockpitSvc, snap)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error al ejecutar TUI: %w", err)
	}
	return nil
}
