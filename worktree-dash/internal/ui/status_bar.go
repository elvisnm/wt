package ui

import (
	"github.com/charmbracelet/lipgloss"
)

type HintPair struct {
	Key  string
	Desc string
}

var SpinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func RenderInputBar(width int, prompt string, value string) string {
	prompt_style := lipgloss.NewStyle().
		Bold(true).
		Foreground(FocusBorderColor)

	cursor := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")).
		Background(FocusBorderColor).
		Render(" ")

	esc_hint := lipgloss.NewStyle().
		Foreground(DimTextColor).
		Render("  (Esc to cancel)")

	content := prompt_style.Render(prompt+": ") + value + cursor + esc_hint

	return lipgloss.NewStyle().
		Width(width).
		Render(" " + content)
}

func RenderResultBar(width int, result string) string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	return lipgloss.NewStyle().
		Width(width).
		Render(" " + style.Render(result))
}
