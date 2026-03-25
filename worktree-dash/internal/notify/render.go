package notify

import (
	"fmt"
	"regexp"
	"strings"
)

// ANSI color codes — consistent with panes.go and pick.go
const (
	ansiDim    = "\033[38;5;240m" // grey for borders
	ansiBold   = "\033[1;37m"     // bold white for title
	ansiOrange = "\033[38;5;214m" // orange for active notifications
	ansiGreen  = "\033[1;32m"     // bold green for success
	ansiReset  = "\033[0m"

	// Cursor control
	ansiClearScreen = "\033[2J\033[H"
	ansiHideCursor  = "\033[?25l"
	ansiShowCursor  = "\033[?25h"
)

var ansiPattern = regexp.MustCompile(`\033\[[0-9;]*m`)

// visual_len returns the display width of a string, stripping ANSI escapes.
func visual_len(s string) int {
	return len(ansiPattern.ReplaceAllString(s, ""))
}

// render_top_border renders: ╭─ Title ─────╮
func render_top_border(title, border_color string, inner int) string {
	title_seg := fmt.Sprintf(" %s%s%s ", ansiBold, title, border_color)
	fill_len := inner - 1 - (len(title) + 2)
	if fill_len < 1 {
		fill_len = 1
	}
	return fmt.Sprintf("%s╭─%s%s╮%s", border_color, title_seg, strings.Repeat("─", fill_len), ansiReset)
}

// render_bottom_border renders: ╰──────────╯
func render_bottom_border(border_color string, inner int) string {
	return fmt.Sprintf("%s╰%s╯%s", border_color, strings.Repeat("─", inner), ansiReset)
}

// render_bordered_line renders: │ message │
func render_bordered_line(message string, msg_vis_len int, border_color string, inner int) string {
	pad := inner - msg_vis_len - 2
	if pad < 0 {
		pad = 0
	}
	return fmt.Sprintf("%s│%s %s%s%s │%s", border_color, ansiReset, message, strings.Repeat(" ", pad), border_color, ansiReset)
}

// RenderClear renders a compact 2-row empty box.
func RenderClear(width int) string {
	if width < 10 {
		width = 10
	}
	inner := width - 2
	top := render_top_border("No notifications", ansiDim, inner)
	bottom := render_bottom_border(ansiDim, inner)
	return top + "\r\n" + bottom
}

// RenderNotify renders a 3-row box with title and message.
func RenderNotify(title, message string, width int) string {
	if width < 10 {
		width = 10
	}
	inner := width - 2
	top := render_top_border(title, ansiOrange, inner)
	mid := render_bordered_line(message, visual_len(message), ansiOrange, inner)
	bottom := render_bottom_border(ansiOrange, inner)
	return top + "\r\n" + mid + "\r\n" + bottom
}

// RenderPicker renders the picker box (title + options with cursor).
func RenderPicker(title string, options []string, cursor int, width int) string {
	if width < 10 {
		width = 10
	}
	inner := width - 2

	var b strings.Builder
	b.WriteString(render_top_border(title, ansiOrange, inner))

	for i, opt := range options {
		b.WriteString("\r\n")
		if i == cursor {
			line := fmt.Sprintf("%s▸ %s%s%s", ansiOrange, ansiBold, opt, ansiReset)
			b.WriteString(render_bordered_line(line, len(opt)+4, ansiOrange, inner))
		} else {
			line := fmt.Sprintf("  %s%s%s", ansiDim, opt, ansiReset)
			// visual: "  " + opt = 2 + len(opt)
			b.WriteString(render_bordered_line(line, len(opt)+2, ansiOrange, inner))
		}
	}

	b.WriteString("\r\n")
	b.WriteString(render_bottom_border(ansiOrange, inner))
	return b.String()
}

// RenderConfirm renders the confirm box (title + prompt + Yes/No options).
func RenderConfirm(title, prompt string, cursor int, width int) string {
	if width < 10 {
		width = 10
	}
	inner := width - 2

	var b strings.Builder
	b.WriteString(render_top_border(title, ansiOrange, inner))

	// Prompt line
	b.WriteString("\r\n")
	prompt_line := fmt.Sprintf("%s%s%s", ansiBold, prompt, ansiReset)
	b.WriteString(render_bordered_line(prompt_line, len(prompt), ansiOrange, inner))

	// Options line: ▸ Yes    No  (or vice versa)
	b.WriteString("\r\n")
	options := []string{"Yes", "No"}
	var parts []string
	for i, opt := range options {
		if i == cursor {
			parts = append(parts, fmt.Sprintf("%s▸ %s%s", ansiOrange, opt, ansiReset))
		} else {
			parts = append(parts, fmt.Sprintf("  %s%s%s", ansiDim, opt, ansiReset))
		}
	}
	opt_line := strings.Join(parts, "    ")
	opt_vis := 0
	for j, opt := range options {
		opt_vis += len(opt) + 2
		if j < len(options)-1 {
			opt_vis += 4
		}
	}
	b.WriteString(render_bordered_line(opt_line, opt_vis, ansiOrange, inner))

	b.WriteString("\r\n")
	b.WriteString(render_bottom_border(ansiOrange, inner))
	return b.String()
}

// RenderInput renders the input box (title + prompt + value with cursor block).
func RenderInput(title, prompt, value string, width int) string {
	if width < 10 {
		width = 10
	}
	inner := width - 2

	var b strings.Builder
	b.WriteString(render_top_border(title, ansiOrange, inner))

	// Prompt + value line with block cursor
	b.WriteString("\r\n")
	display := fmt.Sprintf("%s %s%s█%s", prompt, ansiBold, value, ansiReset)
	display_vis := len(prompt) + 1 + len(value) + 1
	b.WriteString(render_bordered_line(display, display_vis, ansiOrange, inner))

	b.WriteString("\r\n")
	b.WriteString(render_bottom_border(ansiOrange, inner))
	return b.String()
}
