package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type HelpSection struct {
	Title string
	Items []HintPair
}

func HelpSections() []HelpSection {
	return []HelpSection{
		{
			Title: "Navigation",
			Items: []HintPair{
				{Key: "j/k", Desc: "Navigate list"},
				{Key: "</>", Desc: "Switch panel"},
				{Key: "a/w/s/d", Desc: "Jump to panel"},
			{Key: "1-9", Desc: "Jump to tab N"},
				{Key: "Tab", Desc: "Next panel"},
				{Key: "Esc", Desc: "Back / close"},
			},
		},
		{
			Title: "a - Active Tabs",
			Items: []HintPair{
				{Key: "j/k", Desc: "Navigate tabs"},
				{Key: "Enter", Desc: "Focus terminal"},
				{Key: "f", Desc: "Fullscreen"},
				{Key: "x", Desc: "Close tab"},
			},
		},
		{
			Title: "w - Worktrees",
			Items: []HintPair{
				{Key: "Enter", Desc: "Action menu"},
				{Key: "b", Desc: "Shell"},
				{Key: "c", Desc: "Claude Code"},
				{Key: "l", Desc: "Logs"},
				{Key: "n", Desc: "Create worktree"},
				{Key: "r", Desc: "Restart container"},
				{Key: "t", Desc: "Stop container"},
				{Key: "u", Desc: "Start container"},
				{Key: "i", Desc: "Info"},
			{Key: "x", Desc: "Remove worktree"},
			},
		},
		{
			Title: "s - Services",
			Items: []HintPair{
				{Key: "Enter", Desc: "Preview logs"},
				{Key: "l", Desc: "Pin logs (tab)"},
				{Key: "r", Desc: "Restart service"},
			},
		},
		{
			Title: "Operations",
			Items: []HintPair{
				{Key: "A", Desc: "AWS keys"},
				{Key: "D", Desc: "Database ops"},
				{Key: "L", Desc: "LAN toggle"},
				{Key: "X", Desc: "Admin toggle"},
				{Key: "M", Desc: "Maintenance"},
			},
		},
		{
			Title: "General",
			Items: []HintPair{
				{Key: "?", Desc: "This help"},
				{Key: "q", Desc: "Quit"},
				{Key: "Ctrl+C", Desc: "Quit"},
			},
		},
	}
}

// RenderHelpModal returns the help box (to be composited via OverlayCentered).
func RenderHelpModal(max_w, max_h int) string {
	sections := HelpSections()

	key_style := lipgloss.NewStyle().
		Bold(true).
		Foreground(FocusBorderColor).
		Width(8).
		Align(lipgloss.Right)

	desc_style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	section_title_style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255")).
		MarginTop(1)

	var all_lines []string
	for i, sec := range sections {
		if i == 0 {
			all_lines = append(all_lines, section_title_style.Copy().MarginTop(0).Render(sec.Title))
		} else {
			all_lines = append(all_lines, section_title_style.Render(sec.Title))
		}
		for _, item := range sec.Items {
			line := fmt.Sprintf(" %s  %s",
				key_style.Render(item.Key),
				desc_style.Render(item.Desc),
			)
			all_lines = append(all_lines, line)
		}
	}

	content := strings.Join(all_lines, "\n")

	overlay_w := 40
	if overlay_w > max_w-4 {
		overlay_w = max_w - 4
	}
	overlay_h := len(all_lines) + 2
	if overlay_h > max_h-4 {
		overlay_h = max_h - 4
	}

	box := lipgloss.NewStyle().
		Width(overlay_w).
		Height(overlay_h).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(FocusBorderColor).
		Padding(0, 1)

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(FocusBorderColor).
		Render(" Keybindings ")

	styled := box.Render(content)
	return inject_title(styled, title)
}
