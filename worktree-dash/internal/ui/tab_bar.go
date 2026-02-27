package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// TabInfo holds display data for a tab
type TabInfo struct {
	Index  int
	Label  string
	Active bool
	Alive  bool
}

// RenderTabsPanel renders the active tabs as a bordered panel matching
// the worktree/services style. Each tab is a line with a status indicator.
func RenderTabsPanel(tabs []TabInfo, cursor int, width, height int, focused bool) string {
	title := TitleStyle(focused).Render(" a - Active Tabs ")
	style := PanelStyle(width, height, focused)

	inner_w := width - 4
	inner_h := height - 2

	if len(tabs) == 0 {
		placeholder := lipgloss.NewStyle().
			Foreground(DimTextColor).
			Render("No sessions open")
		styled := style.Render(placeholder)
		return inject_title(styled, title)
	}

	var lines []string
	for i, tab := range tabs {
		line := format_tab_line(tab, inner_w, i, i == cursor, focused)
		lines = append(lines, line)
	}

	start, end := visible_window(len(lines), cursor, inner_h)
	lines = lines[start:end]

	content := strings.Join(lines, "\n")
	styled := style.Render(content)
	return inject_title(styled, title)
}

func format_tab_line(tab TabInfo, width int, pos int, selected bool, panel_focused bool) string {
	// Show 1-based shortcut number (1-9) before the label
	shortcut := ""
	if pos < 9 {
		shortcut = fmt.Sprintf("%d", pos+1)
	}

	name := tab.Label

	var right string
	if !tab.Alive {
		right = "dead"
	}

	status := tab_status_indicator_plain(tab)
	right_w := len(right)
	// " {shortcut} {status} {name} ... {right} "
	prefix_w := 2 + len(shortcut) // " {shortcut} "
	max_name := width - right_w - prefix_w - 3
	if max_name < 4 {
		max_name = 4
	}
	if utf8.RuneCountInString(name) > max_name {
		runes := []rune(name)
		name = string(runes[:max_name-1]) + "~"
	}
	label := fmt.Sprintf(" %s %s %s", shortcut, status, name)
	pad := width - lipgloss.Width(label) - right_w - 1
	if pad < 1 {
		pad = 1
	}

	line := label + strings.Repeat(" ", pad) + right + " "

	if selected && panel_focused {
		return lipgloss.NewStyle().
			Bold(true).
			Background(SelectedBgColor).
			Foreground(lipgloss.Color("255")).
			Width(width).
			Render(line)
	}

	if selected {
		return lipgloss.NewStyle().
			Bold(true).
			Width(width).
			Render(line)
	}

	colored_status := tab_status_indicator(tab)
	label = fmt.Sprintf(" %s %s %s", shortcut, colored_status, name)
	pad = width - lipgloss.Width(label) - right_w - 1
	if pad < 1 {
		pad = 1
	}
	line = label + strings.Repeat(" ", pad) + right + " "
	return lipgloss.NewStyle().Width(width).Render(line)
}

func tab_status_indicator(tab TabInfo) string {
	if !tab.Alive {
		return lipgloss.NewStyle().Foreground(StoppedColor).Render("○")
	}
	if tab.Active {
		return lipgloss.NewStyle().Foreground(RunningColor).Render("●")
	}
	return lipgloss.NewStyle().Foreground(DimTextColor).Render("●")
}

func tab_status_indicator_plain(tab TabInfo) string {
	if !tab.Alive {
		return "○"
	}
	return "●"
}
