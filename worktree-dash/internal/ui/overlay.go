package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// OverlayCentered composites fg on top of bg, centered horizontally and vertically.
func OverlayCentered(bg, fg string, width, height int) string {
	bg_lines := strings.Split(bg, "\n")
	fg_lines := strings.Split(fg, "\n")

	for len(bg_lines) < height {
		bg_lines = append(bg_lines, strings.Repeat(" ", width))
	}

	fg_h := len(fg_lines)
	fg_w := 0
	for _, l := range fg_lines {
		if w := lipgloss.Width(l); w > fg_w {
			fg_w = w
		}
	}

	start_y := (height - fg_h) / 2
	start_x := (width - fg_w) / 2
	if start_x < 0 {
		start_x = 0
	}
	if start_y < 0 {
		start_y = 0
	}

	for i, fg_line := range fg_lines {
		y := start_y + i
		if y >= 0 && y < len(bg_lines) {
			bg_lines[y] = splice_visual(bg_lines[y], fg_line, start_x, fg_w)
		}
	}

	return strings.Join(bg_lines, "\n")
}

// splice_visual replaces a visual range [start_x, start_x+fg_w) in bg with fg.
func splice_visual(bg, fg string, start_x, fg_w int) string {
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

// RenderConfirmModal renders a centered confirmation box.
func RenderConfirmModal(prompt string, width, height int) string {
	prompt_style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")).
		Bold(true)

	dim_style := lipgloss.NewStyle().Foreground(DimTextColor)
	key_style := lipgloss.NewStyle().Foreground(HintColor)
	hint_line := key_style.Render("Enter") + dim_style.Render(": confirm  ") + key_style.Render("Esc") + dim_style.Render(": cancel")

	content := lipgloss.JoinVertical(lipgloss.Center,
		prompt_style.Render(prompt),
		"",
		hint_line,
	)

	prompt_w := lipgloss.Width(prompt)
	hint_w := lipgloss.Width("Enter: confirm  Esc: cancel")
	inner_w := prompt_w
	if hint_w > inner_w {
		inner_w = hint_w
	}
	box_w := inner_w + 8 // padding (4) + border (2) + margin (2)
	if box_w < 36 {
		box_w = 36
	}
	if box_w > width-4 {
		box_w = width - 4
	}

	box := lipgloss.NewStyle().
		Width(box_w).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(FocusBorderColor).
		Padding(1, 3).
		Align(lipgloss.Center)

	return box.Render(content)
}
