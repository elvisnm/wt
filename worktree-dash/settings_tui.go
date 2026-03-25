package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/elvisnm/wt/internal/settings"
	"golang.org/x/term"
)

type settingsItem int

const (
	itemDetails settingsItem = iota
	itemUsage
	itemTasks
	itemLeftPane
	itemMaxPanes
	itemSave
	itemExit
	itemCount // sentinel
)

// runSettings runs the interactive settings TUI in the right pane.
// Renders centered with bordered boxes matching guide/help style.
// Exits with code 0 on save, code 1 on exit without save.
func runSettings() {
	original := settings.Load()
	s := original // working copy

	// Clear any stale draft from a previous session
	settings.ClearDraft()

	old_state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "wt _settings: failed to set raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(int(os.Stdin.Fd()), old_state)

	// Handle resize
	sig_ch := make(chan os.Signal, 1)
	signal.Notify(sig_ch, syscall.SIGWINCH)
	go func() {
		for range sig_ch {
			draw_settings(s, itemDetails, false)
		}
	}()

	cursor := itemDetails
	saved := false

	draw_settings(s, cursor, saved)

	exit := func(code int) {
		term.Restore(int(os.Stdin.Fd()), old_state)
		fmt.Print("\033[?25h")
		os.Exit(code)
	}

	buf := make([]byte, 4)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			break
		}

		saved = false

		switch {
		// Arrow up / k
		case (n == 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'A') ||
			(n == 1 && buf[0] == 'k'):
			if cursor > 0 {
				cursor--
			}

		// Arrow down / j
		case (n == 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'B') ||
			(n == 1 && buf[0] == 'j'):
			if cursor < itemCount-1 {
				cursor++
			}

		// Arrow left — decrease range value
		case n == 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'D':
			switch cursor {
			case itemLeftPane:
				if s.LeftPanePct > settings.MinLeftPanePct {
					s.LeftPanePct--
				}
			case itemMaxPanes:
				if s.MaxPanesPerGroup > settings.MinMaxPanesPerGroup {
					s.MaxPanesPerGroup--
				}
			}

		// Arrow right — increase range value
		case n == 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'C':
			switch cursor {
			case itemLeftPane:
				if s.LeftPanePct < settings.MaxLeftPanePct {
					s.LeftPanePct++
				}
			case itemMaxPanes:
				if s.MaxPanesPerGroup < settings.MaxMaxPanesPerGroup {
					s.MaxPanesPerGroup++
				}
			}

		// Space / Enter — toggle or action
		case n == 1 && (buf[0] == ' ' || buf[0] == '\r' || buf[0] == '\n'):
			switch cursor {
			case itemDetails:
				s.DefaultPanels.Details = !s.DefaultPanels.Details
			case itemUsage:
				s.DefaultPanels.Usage = !s.DefaultPanels.Usage
			case itemTasks:
				s.DefaultPanels.Tasks = !s.DefaultPanels.Tasks
			case itemLeftPane:
				// no-op, use arrows
			case itemSave:
				settings.Save(s)
				settings.ClearDraft()
				exit(0)
				return
			case itemExit:
				if settings_changed(original, s) {
					settings.SaveDraft(s)
				}
				exit(0)
				return
			}

		// q / Escape / Ctrl+C — exit
		case n == 1 && (buf[0] == 'q' || buf[0] == 0x1b || buf[0] == 0x03):
			if settings_changed(original, s) {
				settings.SaveDraft(s)
			}
			exit(0)
			return
		}

		draw_settings(s, cursor, saved)
	}
}

func settings_changed(original, current settings.Settings) bool {
	return original.DefaultPanels != current.DefaultPanels ||
		original.LeftPanePct != current.LeftPanePct ||
		original.MaxPanesPerGroup != current.MaxPanesPerGroup
}

func draw_settings(s settings.Settings, cursor settingsItem, saved bool) {
	tw, th := termSize()

	col_w := 46
	if tw < col_w+4 {
		col_w = tw - 4
	}
	if col_w < 30 {
		col_w = 30
	}

	var lines []string

	// Title (same style as guide/help)
	lines = append(lines,
		guideCenterLine(ansiBold+ansiCyan+"wt — Settings"+ansiReset, col_w),
		"",
	)

	// Default Panels box
	var panel_lines []string
	panel_lines = append(panel_lines, settings_toggle(cursor == itemDetails, "Details panel", "Shift+D", s.DefaultPanels.Details))
	panel_lines = append(panel_lines, settings_toggle(cursor == itemUsage, "Usage panel", "Shift+U", s.DefaultPanels.Usage))
	panel_lines = append(panel_lines, settings_toggle(cursor == itemTasks, "Tasks panel", "Shift+T", s.DefaultPanels.Tasks))
	lines = append(lines, guideBox("Default Panels", panel_lines, col_w)...)

	lines = append(lines, "")

	// Left Pane Width box
	var width_lines []string
	width_lines = append(width_lines, settings_range(cursor == itemLeftPane, s.LeftPanePct, settings.MinLeftPanePct, settings.MaxLeftPanePct, col_w-6))
	width_lines = append(width_lines, ansiDim+"Use "+ansiReset+guideKey("←")+ansiDim+" / "+ansiReset+guideKey("→")+ansiDim+" to adjust"+ansiReset)
	lines = append(lines, guideBox("Left Pane Width", width_lines, col_w)...)

	lines = append(lines, "")

	// Split Panes box
	var split_lines []string
	split_lines = append(split_lines, settings_range_label(cursor == itemMaxPanes, "Max   ", s.MaxPanesPerGroup, settings.MinMaxPanesPerGroup, settings.MaxMaxPanesPerGroup, col_w-6))
	split_lines = append(split_lines, ansiDim+"Sessions per group"+ansiReset)
	lines = append(lines, guideBox("Split Panes", split_lines, col_w)...)

	lines = append(lines, "")

	// Actions
	var action_lines []string
	if cursor == itemSave {
		action_lines = append(action_lines, ansiCyan+"▸ "+ansiBold+"Save & Close"+ansiReset)
	} else {
		action_lines = append(action_lines, ansiDim+"  Save & Close"+ansiReset)
	}
	if cursor == itemExit {
		action_lines = append(action_lines, ansiCyan+"▸ "+ansiBold+"Exit"+ansiReset)
	} else {
		action_lines = append(action_lines, ansiDim+"  Exit"+ansiReset)
	}
	lines = append(lines, helpBox("Actions", action_lines, col_w)...)

	// Footer hints
	lines = append(lines,
		"",
		guideCenterLine(ansiDim+"↑↓ navigate  ␣/Enter select  ←→ adjust  q exit"+ansiReset, col_w),
	)

	// Center vertically and horizontally
	content_h := len(lines)
	top_pad := (th - content_h) / 2
	if top_pad < 1 {
		top_pad = 1
	}

	left_pad := (tw - col_w) / 2
	if left_pad < 0 {
		left_pad = 0
	}
	prefix := strings.Repeat(" ", left_pad)

	fmt.Print("\033[2J\033[H\033[?25l")
	for i := 0; i < top_pad; i++ {
		fmt.Print("\r\n")
	}
	for _, l := range lines {
		fmt.Printf("%s%s\r\n", prefix, l)
	}
}

func settings_toggle(focused bool, label, shortcut string, on bool) string {
	prefix := "  "
	if focused {
		prefix = ansiCyan + "▸ " + ansiReset
	}

	checkbox := ansiDim + "[ ]" + ansiReset
	if on {
		checkbox = ansiDim + "[" + ansiCyan + "✓" + ansiDim + "]" + ansiReset
	}

	hint := ansiDim + "(" + shortcut + ")" + ansiReset

	return fmt.Sprintf("%s%s %s  %s", prefix, checkbox, label, hint)
}

func settings_range(focused bool, value, min_val, max_val, max_w int) string {
	prefix := "  "
	if focused {
		prefix = ansiCyan + "▸ " + ansiReset
	}

	track_w := max_w - 12
	if track_w < 10 {
		track_w = 10
	}
	if track_w > 40 {
		track_w = 40
	}

	filled := (value - min_val) * track_w / (max_val - min_val)
	if filled < 0 {
		filled = 0
	}
	if filled > track_w {
		filled = track_w
	}
	empty := track_w - filled

	bar := ansiCyan + strings.Repeat("█", filled) +
		ansiDim + strings.Repeat("░", empty) + ansiReset

	label := fmt.Sprintf("%s%d%%%s", ansiBold, value, ansiReset)

	return fmt.Sprintf("%s%s  %s", prefix, bar, label)
}

func settings_range_label(focused bool, name string, value, min_val, max_val, max_w int) string {
	prefix := "  "
	if focused {
		prefix = ansiCyan + "▸ " + ansiReset
	}

	track_w := max_w - 18
	if track_w < 6 {
		track_w = 6
	}
	if track_w > 30 {
		track_w = 30
	}

	filled := 0
	if max_val > min_val {
		filled = (value - min_val) * track_w / (max_val - min_val)
	}
	if filled < 0 {
		filled = 0
	}
	if filled > track_w {
		filled = track_w
	}
	empty := track_w - filled

	bar := ansiCyan + strings.Repeat("█", filled) +
		ansiDim + strings.Repeat("░", empty) + ansiReset

	label := fmt.Sprintf("%s%d%s", ansiBold, value, ansiReset)
	name_styled := ansiDim + name + ": " + ansiReset

	return fmt.Sprintf("%s%s%s  %s", prefix, name_styled, bar, label)
}
