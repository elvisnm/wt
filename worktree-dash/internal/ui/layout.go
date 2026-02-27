package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Layout holds computed dimensions for the left-column panels.
// The right pane is a native tmux pane managed by terminal.PaneLayout.
type Layout struct {
	Width  int
	Height int

	TabsHeight     int
	WorktreeHeight int
	ServicesHeight int
	DetailsHeight  int
	StatusHeight   int
}

func (l Layout) Resize(w, h int) Layout {
	l.Width = w
	l.Height = h
	l.StatusHeight = 0

	panels_h := h - l.StatusHeight
	if panels_h < 8 {
		panels_h = 8
	}

	// 4 panels: tabs 20%, worktrees 30%, services 25%, details 25%
	l.TabsHeight = panels_h * 20 / 100
	if l.TabsHeight < 4 {
		l.TabsHeight = 4
	}
	l.WorktreeHeight = panels_h * 30 / 100
	l.ServicesHeight = panels_h * 25 / 100
	l.DetailsHeight = panels_h - l.TabsHeight - l.WorktreeHeight - l.ServicesHeight

	return l
}

// Style helpers — lazygit-inspired palette
var (
	BorderColor      = lipgloss.Color("240")
	FocusBorderColor = lipgloss.Color("34")
	DimTextColor     = lipgloss.Color("250")
	HighlightColor   = lipgloss.Color("34")
	SelectedBgColor  = lipgloss.Color("25")
	RunningColor     = lipgloss.Color("34")
	StoppedColor     = lipgloss.Color("160")
	StartingColor    = lipgloss.Color("214")
	HeaderColor      = lipgloss.Color("240")
	HintColor        = lipgloss.Color("214")
)

func PanelStyle(width, height int, focused bool) lipgloss.Style {
	border_color := BorderColor
	if focused {
		border_color = FocusBorderColor
	}

	s := lipgloss.NewStyle().
		Width(width - 2).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border_color)

	if focused {
		s = s.BorderBackground(lipgloss.NoColor{})
	}

	return s
}

// visible_window returns the slice of lines that fits within max_lines,
// scrolling to keep cursor visible. Returns (start, end) indices.
func visible_window(total, cursor, max_lines int) (int, int) {
	if total <= max_lines {
		return 0, total
	}
	start := 0
	if cursor >= max_lines {
		start = cursor - max_lines + 1
	}
	end := start + max_lines
	if end > total {
		end = total
		start = end - max_lines
	}
	if start < 0 {
		start = 0
	}
	return start, end
}

// OverlayScrollbar adds a scrollbar indicator on the right border of a rendered panel.
// total = total content lines, visible = visible lines, offset = scroll offset,
// track_h = inner panel height (height - 2 for borders).
// Only shows the scrollbar when content overflows.
func OverlayScrollbar(rendered string, total, visible, offset int, focused bool) string {
	track_h := visible
	if total <= track_h || track_h <= 0 {
		return rendered // no scrollbar needed
	}

	// Calculate thumb size and position
	thumb_h := track_h * track_h / total
	if thumb_h < 1 {
		thumb_h = 1
	}
	max_offset := total - track_h
	if offset > max_offset {
		offset = max_offset
	}
	if offset < 0 {
		offset = 0
	}
	thumb_pos := 0
	if max_offset > 0 {
		thumb_pos = offset * (track_h - thumb_h) / max_offset
	}

	// Style for thumb
	thumb_color := BorderColor
	if focused {
		thumb_color = FocusBorderColor
	}
	thumb_style := lipgloss.NewStyle().Foreground(thumb_color)

	lines := strings.Split(rendered, "\n")
	// Content lines are lines[1] through lines[len-2] (skip top/bottom border)
	for i := 0; i < track_h && (i+1) < len(lines)-1; i++ {
		line := lines[i+1] // +1 to skip top border

		var indicator string
		if i >= thumb_pos && i < thumb_pos+thumb_h {
			indicator = thumb_style.Render("█")
		} else {
			indicator = thumb_style.Render("│")
		}

		// Replace the last visible character (right border │) with the scrollbar
		// Find the last │ in the line and replace it
		last_border := strings.LastIndex(line, "│")
		if last_border >= 0 {
			lines[i+1] = line[:last_border] + indicator + line[last_border+len("│"):]
		}
	}

	return strings.Join(lines, "\n")
}

func TitleStyle(focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(FocusBorderColor)
	}
	return lipgloss.NewStyle().
		Foreground(DimTextColor)
}
