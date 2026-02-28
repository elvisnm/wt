package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/worktree"

	"github.com/charmbracelet/lipgloss"
)

func RenderWorktreePanel(worktrees []worktree.Worktree, cursor int, width, height int, focused bool, cfg *config.Config) string {
	title := TitleStyle(focused).Render(" w - Worktrees ")
	style := PanelStyle(width, height, focused)

	inner_w := width - 4
	inner_h := height - 2 // border

	var lines []string
	for i, wt := range worktrees {
		line := format_worktree_line(wt, inner_w, i == cursor, focused, cfg)
		lines = append(lines, line)
	}

	total := len(lines)
	start, end := visible_window(total, cursor, inner_h)
	lines = lines[start:end]

	content := strings.Join(lines, "\n")

	styled := style.Render(content)
	styled = OverlayScrollbar(styled, total, inner_h, start, focused)
	styled = inject_title(styled, title)

	return styled
}

func format_worktree_line(wt worktree.Worktree, width int, selected bool, panel_focused bool, cfg *config.Config) string {
	name := wt.Alias
	if name == "" {
		name = wt.Name
	}

	var right string
	if strings.HasSuffix(wt.Health, "...") && !wt.Running {
		right = strings.TrimSuffix(wt.Health, "...")
	} else if wt.Running && wt.Type == worktree.TypeLocal {
		right = "dev"
		if cfg != nil && cfg.ServiceManager() == "pm2" {
			right = "pm2"
		}
	} else if wt.Running && wt.HostBuild {
		right = fmt.Sprintf("hb %s %s", wt.CPU, wt.Mem)
	} else if wt.Running {
		right = fmt.Sprintf("%s %s", wt.CPU, wt.Mem)
	} else if wt.ContainerExists {
		right = "docker"
	} else {
		right = "local"
	}

	status := status_indicator_plain(wt)
	right_w := len(right)
	// 4 = " " + status + " " + min 1 pad; +1 trailing space
	max_name := width - right_w - 5
	if max_name < 4 {
		max_name = 4
	}
	if utf8.RuneCountInString(name) > max_name {
		runes := []rune(name)
		name = string(runes[:max_name-1]) + "~"
	}
	label := fmt.Sprintf(" %s %s", status, name)
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

	// Color the status indicator for non-selected lines
	colored_status := status_indicator(wt)
	label = fmt.Sprintf(" %s %s", colored_status, name)
	pad = width - lipgloss.Width(label) - right_w - 1
	if pad < 1 {
		pad = 1
	}
	line = label + strings.Repeat(" ", pad) + right + " "
	return lipgloss.NewStyle().Width(width).Render(line)
}

func status_indicator(wt worktree.Worktree) string {
	switch {
	case strings.HasSuffix(wt.Health, "..."):
		return lipgloss.NewStyle().Foreground(StartingColor).Render("◐")
	case wt.Running && wt.Health == "healthy":
		return lipgloss.NewStyle().Foreground(RunningColor).Render("●")
	case wt.Running && wt.Health == "starting":
		return lipgloss.NewStyle().Foreground(StartingColor).Render("◐")
	case wt.Running:
		return lipgloss.NewStyle().Foreground(RunningColor).Render("●")
	case wt.ContainerExists:
		return lipgloss.NewStyle().Foreground(StoppedColor).Render("○")
	case wt.Type == worktree.TypeLocal:
		return lipgloss.NewStyle().Foreground(DimTextColor).Render("◇")
	default:
		return lipgloss.NewStyle().Foreground(StoppedColor).Render("○")
	}
}

func status_indicator_plain(wt worktree.Worktree) string {
	switch {
	case strings.HasSuffix(wt.Health, "..."):
		return "◐"
	case wt.Running && wt.Health == "healthy":
		return "●"
	case wt.Running && wt.Health == "starting":
		return "◐"
	case wt.Running:
		return "●"
	case wt.ContainerExists:
		return "○"
	case wt.Type == worktree.TypeLocal:
		return "◇"
	default:
		return "○"
	}
}

// inject_title replaces part of the top border with a title string.
// It uses lipgloss.Width for visual width calculations and operates
// on raw bytes to avoid corrupting ANSI escape sequences.
func inject_title(rendered, title string) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}

	top := lines[0]
	title_w := lipgloss.Width(title)
	top_w := lipgloss.Width(top)

	if title_w+4 > top_w {
		return rendered
	}

	// Skip the first visual character (border corner) plus one border segment,
	// then splice in the title string. We find the byte position of the 2nd
	// visible character by scanning through ANSI sequences.
	insert_byte := visual_offset_to_byte(top, 2)
	end_byte := visual_offset_to_byte(top, 2+title_w)

	if insert_byte < 0 || end_byte < 0 || end_byte > len(top) {
		return rendered
	}

	// Extract the ANSI color sequence from the start of the border line
	// so we can re-apply it after the title (which ends with a reset).
	border_color := extract_ansi_prefix(top)

	lines[0] = top[:insert_byte] + title + border_color + top[end_byte:]
	return strings.Join(lines, "\n")
}

// extract_ansi_prefix returns the leading ANSI escape sequence(s) from a string.
func extract_ansi_prefix(s string) string {
	var result string
	i := 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			// Find end of escape sequence
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				result += s[i : j+1]
				i = j + 1
				continue
			}
		}
		break // stop at first non-ANSI character
	}
	return result
}

// visual_offset_to_byte finds the byte index corresponding to a visual column offset,
// skipping over ANSI escape sequences that don't consume visual width.
func visual_offset_to_byte(s string, target_col int) int {
	col := 0
	i := 0
	for i < len(s) && col < target_col {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Skip CSI sequence: ESC [ ... final_byte
			j := i + 2
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3F {
				j++
			}
			if j < len(s) {
				j++ // skip final byte
			}
			i = j
			continue
		}
		// Decode one UTF-8 rune and advance
		_, size := decodeRune(s[i:])
		i += size
		col++
	}
	if col == target_col {
		return i
	}
	return -1
}

func decodeRune(s string) (rune, int) {
	return utf8.DecodeRuneInString(s)
}
