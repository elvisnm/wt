package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type HintPair struct {
	Key  string
	Desc string
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func RenderStatusBar(width int, panel_name string, hints []HintPair) string {
	hint_style := lipgloss.NewStyle().Foreground(HintColor)
	sep_style := lipgloss.NewStyle().Foreground(DimTextColor)

	var parts []string
	for _, h := range hints {
		parts = append(parts, hint_style.Render(h.Desc+": "+h.Key))
	}

	help := hint_style.Render("?: help")
	content := strings.Join(parts, sep_style.Render(" | ")) + sep_style.Render(" | ") + help

	return "\n" + lipgloss.NewStyle().
		Width(width).
		Render(" "+content) + "\n"
}

// RenderStatusBarWithActivity renders the status bar with an activity spinner
// when there's an active operation (e.g., "Restarting...").
func RenderStatusBarWithActivity(width int, activity string, spin_frame int) string {
	frame := spinFrames[spin_frame%len(spinFrames)]
	icon := lipgloss.NewStyle().Foreground(StartingColor).Render(frame)
	text := lipgloss.NewStyle().Foreground(StartingColor).Render(" " + activity)

	return "\n" + lipgloss.NewStyle().
		Width(width).
		Render(" "+icon+text) + "\n"
}

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
