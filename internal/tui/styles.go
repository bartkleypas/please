package tui

import "github.com/charmbracelet/lipgloss"

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇"}

var (
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA")).Background(lipgloss.Color("#7D56F4")).Padding(0, 1)
	userStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
	botStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF"))
	markStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFCC00"))
	warningStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF4500")) // Orange-Red
	historyBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#555555")).
			Padding(0, 1)
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#555555")).
			Padding(0, 1)
)
