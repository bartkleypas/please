package tui

import "github.com/charmbracelet/lipgloss"

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇"}

var (
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e0e7e3")).Background(lipgloss.Color("#1e3a2f")).Padding(0, 1)
	userStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))
	botStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa"))
	markStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#facc15"))
	warningStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#fb7185"))
	helpStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#4b7a63")).Italic(true)
	highlightStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#0d1b15")).Background(lipgloss.Color("#4ade80")).Bold(true)
	thoughtStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b")).Italic(true)
	thoughtBadgeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#818cf8")).Italic(true)
	historyBoxStyle   = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#2d5a45")).
			Padding(0, 1)
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#2d5a45")).
			Padding(0, 1)
)
