package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// TabInfo holds display data for a tab entry.
type TabInfo struct {
	Index        int
	Label        string
	Active       bool
	Alive        bool
	Idle         bool     // agent is waiting for input
	IsGroupHead  bool     // group header line (multi-session groups)
	IsGroupChild bool     // session entry within a group
	GroupSize    int      // total sessions in this group
	LayoutMap    []string // 3-line mini layout map (only on last child)
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

	// Build lines with layout map overlay
	var lines []string
	for i, tab := range tabs {
		line := format_tab_line(tab, inner_w, i, i == cursor, focused)
		lines = append(lines, line)
	}

	// Overlay layout maps on the right side of group lines
	lines = overlay_layout_maps(tabs, lines, inner_w)

	start, end := visible_window(len(lines), cursor, inner_h)
	lines = lines[start:end]

	content := strings.Join(lines, "\n")
	styled := style.Render(content)
	return inject_title(styled, title)
}

func format_tab_line(tab TabInfo, width int, pos int, selected bool, panel_focused bool) string {
	name := tab.Label

	var right string
	if !tab.Alive && !tab.IsGroupHead {
		right = "dead"
	}

	right_w := len(right)

	// Determine prefix based on entry type
	var prefix string
	if tab.IsGroupHead {
		// Group header: " N ▸ " with tab number
		shortcut := ""
		if tab.Index > 0 && tab.Index <= 9 {
			shortcut = fmt.Sprintf("%d", tab.Index)
		}
		prefix = fmt.Sprintf(" %s ", shortcut)
	} else if tab.IsGroupChild {
		prefix = "   ├ " // 5 chars: 3 indent + tree connector + space
	} else {
		// Standalone tab
		shortcut := ""
		if tab.Index > 0 && tab.Index <= 9 {
			shortcut = fmt.Sprintf("%d", tab.Index)
		}
		prefix = fmt.Sprintf(" %s ", shortcut)
	}

	status := tab_status_indicator_plain(tab)
	prefix_w := lipgloss.Width(prefix) + 2 // + status + space
	max_name := width - right_w - prefix_w - 2
	if max_name < 4 {
		max_name = 4
	}
	if utf8.RuneCountInString(name) > max_name {
		runes := []rune(name)
		name = string(runes[:max_name-1]) + "~"
	}

	label := prefix + status + " " + name
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
			MaxHeight(1).
			Render(line)
	}

	if selected {
		return lipgloss.NewStyle().
			Bold(true).
			Width(width).
			MaxHeight(1).
			Render(line)
	}

	// Non-selected: use colored status indicator
	colored_status := tab_status_indicator(tab)
	label = prefix + colored_status + " " + name
	pad = width - lipgloss.Width(label) - right_w - 1
	if pad < 1 {
		pad = 1
	}
	line = label + strings.Repeat(" ", pad) + right + " "

	// Dim style for group children
	if tab.IsGroupChild {
		return lipgloss.NewStyle().
			Foreground(DimTextColor).
			Width(width).
			MaxHeight(1).
			Render(line)
	}

	return lipgloss.NewStyle().Width(width).MaxHeight(1).Render(line)
}

// overlay_layout_maps renders mini layout maps on the right side of group lines.
// The map is aligned right, starting at the group header line.
func overlay_layout_maps(tabs []TabInfo, lines []string, width int) []string {
	dim := lipgloss.NewStyle().Foreground(BorderColor)

	for i, tab := range tabs {
		if tab.LayoutMap == nil || len(tab.LayoutMap) != 3 {
			continue
		}
		// Find the first line of this group (the header)
		// The LayoutMap is attached to the last child — walk back to find header
		header_idx := i
		for header_idx > 0 && tabs[header_idx].IsGroupChild {
			header_idx--
		}

		// Overlay the 3 map lines starting at header_idx
		for j, map_line := range tab.LayoutMap {
			line_idx := header_idx + j
			if line_idx >= len(lines) {
				break
			}
			styled_map := dim.Render(map_line)
			map_w := lipgloss.Width(map_line)
			// Place it at the right edge with 1 char margin
			insert_pos := width - map_w - 1
			if insert_pos < 0 {
				continue
			}
			lines[line_idx] = splice_visual_line(lines[line_idx], styled_map, insert_pos, map_w)
		}
	}
	return lines
}

// splice_visual_line replaces a visual range in a line with new content.
func splice_visual_line(bg, fg string, start_x, fg_w int) string {
	left_end := visual_offset_to_byte(bg, start_x)
	right_start := visual_offset_to_byte(bg, start_x+fg_w)

	if left_end < 0 {
		left_end = len(bg)
	}
	if right_start < 0 {
		right_start = len(bg)
	}

	return bg[:left_end] + "\x1b[0m" + fg + "\x1b[0m" + bg[right_start:]
}

func tab_status_indicator(tab TabInfo) string {
	if !tab.Alive {
		return lipgloss.NewStyle().Foreground(StoppedColor).Render("○")
	}
	if tab.Idle {
		return lipgloss.NewStyle().Foreground(StartingColor).Render("◉")
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
