package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Paleta cromática Allostasis
	ColorPrimary   = lipgloss.Color("#00F5D4") // Cyan neón
	ColorWarning   = lipgloss.Color("#FFB703") // Ámbar cálido
	ColorDanger    = lipgloss.Color("#F72585") // Carmesí sobrecarga
	ColorSecondary = lipgloss.Color("#7209B7") // Púrpura cósmico
	ColorBg        = lipgloss.Color("#080B10") // Dark Abyss
	ColorSurface   = lipgloss.Color("#161B22") // Dark Surface
	ColorText      = lipgloss.Color("#ECEFF4") // Blanco nítido
	ColorMuted     = lipgloss.Color("#6C7A89") // Gris tenue

	// Estilos de componentes
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Background(ColorSurface).
			Padding(0, 1).
			MarginBottom(1)

	BadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(ColorPrimary).
			Padding(0, 1)

	WarningBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#000000")).
				Background(ColorWarning).
				Padding(0, 1)

	CardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorSecondary).
			Padding(1, 2).
			MarginBottom(1)

	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText).
			MarginBottom(1)

	MutedStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			MarginTop(1)
)
