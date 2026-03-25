package terminal

import (
	"fmt"
	"strings"
	"sync"
)

// PaneLayout manages the tmux pane layout for the dashboard.
//
// Base layout (2 panes):
//
//	pane 0 (left): bubbletea control app
//	pane 1 (right): native terminal sessions via swap-pane
//
// With notification panel (3 panes):
//
//	pane 0 (left): bubbletea control app
//	pane N (top-right): notification/menu panel — dynamic height
//	pane 1 (right): terminal sessions — fills remaining space
//
// All pane operations use stable pane IDs (%N) instead of positional
// indices, because splitting/destroying panes shifts indices.
type PaneLayout struct {
	server        *TmuxServer
	active_win    string      // primary session window displayed in the right pane
	active_extras []GroupPane // extra panes joined into the viewport (for split groups)

	// Stable tmux pane IDs (e.g. "%0", "%1", "%5") — survive splits and swaps
	left_pane_id string // bubbletea control app
	left_pct     int    // configured left pane width percentage

	mu sync.Mutex
}

// SetupPaneLayout creates the 2-pane split in the tmux session.
// Must be called after EnsureStarted(). The initial window (0) gets split
// horizontally: left 28%, right 72%. Returns the layout manager.
// If exe_path is provided, the right pane runs "wt _guide; exec cat" to show
// the welcome guide. Otherwise, falls back to "clear && exec cat".
func SetupPaneLayout(ts *TmuxServer, left_pct int, exe_path string) (*PaneLayout, error) {
	if left_pct <= 0 || left_pct >= 100 {
		left_pct = 28
	}
	right_pct := 100 - left_pct

	// Right pane command: show guide if exe_path available, otherwise blank placeholder
	pane_cmd := "clear && exec cat"
	if exe_path != "" {
		pane_cmd = fmt.Sprintf("%s _guide; exec cat", exe_path)
	}

	// Split window 0 horizontally: the new pane (right) gets right_pct%
	out, err := ts.Run("split-window", "-h", "-t", "wt:0",
		"-l", fmt.Sprintf("%d%%", right_pct),
		pane_cmd)
	if err != nil {
		return nil, fmt.Errorf("tmux split-window failed: %w\n%s", err, out)
	}

	// Capture stable pane IDs
	// Layout: left=idx0, right=idx1. No notification pane —
	// notifications render inline in the bubbletea left pane.
	left_id := capture_pane_id(ts, "wt:0.0")

	// Focus back to the left pane so bubbletea gets input first
	ts.Run("select-pane", "-t", left_id)

	pl := &PaneLayout{
		server:       ts,
		left_pane_id: left_id,
		left_pct:     left_pct,
	}

	return pl, nil
}

// NewPaneLayout creates a PaneLayout connected to an existing tmux server.
// Used by the inner-mode bubbletea app to manage pane operations.
// Layout: left=idx0, right=idx1 (no notify pane).
func NewPaneLayout(server *TmuxServer) *PaneLayout {
	left_id := capture_pane_id(server, "wt:0.0")

	// Read current left pane width percentage
	left_pct := 20
	if w_str, err := server.Run("display-message", "-t", left_id, "-p", "#{pane_width}"); err == nil {
		var pw int
		fmt.Sscanf(strings.TrimSpace(w_str), "%d", &pw)
		if tw_str, err2 := server.Run("display-message", "-t", "wt:0", "-p", "#{window_width}"); err2 == nil {
			var tw int
			fmt.Sscanf(strings.TrimSpace(tw_str), "%d", &tw)
			if tw > 0 {
				left_pct = pw * 100 / tw
			}
		}
	}

	return &PaneLayout{
		server:       server,
		left_pane_id: left_id,
		left_pct:     left_pct,
	}
}

// capture_pane_id returns the stable pane ID (e.g. "%5") for a tmux target.
func capture_pane_id(ts *TmuxServer, target string) string {
	out, err := ts.Run("display-message", "-t", target, "-p", "#{pane_id}")
	if err != nil {
		return target // fallback to positional target
	}
	return strings.TrimSpace(out)
}

// discover_pane_excluding lists panes in window 0 and returns the first ID
// that isn't in the exclude set.
func discover_pane_excluding(ts *TmuxServer, exclude ...string) string {
	out, err := ts.Run("list-panes", "-t", "wt:0", "-F", "#{pane_id}")
	if err != nil {
		return ""
	}
	known := make(map[string]bool, len(exclude))
	for _, id := range exclude {
		known[id] = true
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		id := strings.TrimSpace(line)
		if id != "" && !known[id] {
			return id
		}
	}
	return ""
}

// query_pane_width returns the current width of a tmux pane, or 80 as fallback.
func query_pane_width(ts *TmuxServer, pane_id string) int {
	if pane_id == "" {
		return 80
	}
	out, err := ts.Run("display-message", "-t", pane_id, "-p", "#{pane_width}")
	if err != nil {
		return 80
	}
	var w int
	fmt.Sscanf(strings.TrimSpace(out), "%d", &w)
	if w <= 0 {
		return 80
	}
	return w
}

// parse_pane_ids parses the output of list-panes -F "#{pane_id}" into a slice.
func parse_pane_ids(out string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		id := strings.TrimSpace(line)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// ConfigureBindings sets up tmux key bindings for pane navigation.
// prefix (Ctrl+]) then q = return focus to left pane (auto-unzooms if zoomed)
// prefix then f = toggle fullscreen (zoom right pane)
func (pl *PaneLayout) ConfigureBindings() {
	ts := pl.server

	// Strip all default bindings first
	ts.Run("unbind-key", "-a")

	// Use Ctrl+] as prefix — rarely used by terminal apps, works in Claude Code
	ts.Run("set-option", "-g", "prefix", "C-]")
	ts.Run("set-option", "-g", "prefix2", "None")

	// prefix+q: return to dashboard — select-pane auto-unzooms if zoomed
	ts.Run("bind-key", "q", "select-pane", "-t", pl.left_pane_id)

	// prefix+f: toggle zoom on the right viewport (last pane in window 0)
	right_target := pl.resolve_right_viewport()
	ts.Run("bind-key", "f", "resize-pane", "-t", right_target, "-Z")

	// prefix+1-9: jump to tab N — sends Alt+N to bubbletea (pane 0)
	// Bubbletea's alt_tab_number handler picks this up and calls FocusByIndex + FocusRight
	for i := 1; i <= 9; i++ {
		key := fmt.Sprintf("%d", i)
		ts.Run("bind-key", key, "send-keys", "-t", pl.left_pane_id, fmt.Sprintf("M-%d", i))
	}

	// Focus indicator: green divider when right pane (terminal) is active,
	// dim gray when left pane (dashboard) is active.
	// after-select-pane hook fires on every pane focus change.
	// #{pane_index}: "0" = falsy (dashboard), "1" = truthy (terminal)
	// Set BOTH border styles so the entire divider changes color uniformly.
	ts.Run("set-hook", "-g", "after-select-pane",
		`if-shell -F "#{pane_index}" "set -g pane-border-style fg=colour34 ; set -g pane-active-border-style fg=colour34" "set -g pane-border-style fg=colour240 ; set -g pane-active-border-style fg=colour240"`)

	// Block C-k on the dashboard pane (pane 0). iTerm2 Cmd+K sends C-k to the
	// terminal process. Without this, it clears the bubbletea display.
	// Allow C-k through only on pane 1 (terminal sessions like zsh).
	// -n = root table (no prefix needed). if-shell -F evaluates #{pane_index}:
	// "0" is falsy (pane 0 → no-op), "1" is truthy (pane 1 → forward C-k).
	ts.Run("bind-key", "-n", "C-k",
		"if-shell", "-F", "#{pane_index}",
		"send-keys C-k", "")

	// Prevent mouse clicks from switching pane focus. tmux "mouse on" enables
	// click-to-select-pane, which causes unwanted focus changes when the user
	// clicks on the terminal to return from another app. Keep MouseDrag1Pane
	// so text selection still works in terminal sessions.
	ts.Run("unbind-key", "-n", "MouseDown1Pane")
	ts.Run("unbind-key", "-n", "MouseDown1Status")
	ts.Run("unbind-key", "-n", "MouseDrag1Border")
}

// ── Notification / Menu Panel ──────────────────────────────────────────

const (
	rightViewportTarget = "wt:0.1" // Layout: left=0, right=1 (no notify pane)
)

// ── Session Management ─────────────────────────────────────────────────

// GroupPane describes a session pane for group show/return operations.
type GroupPane struct {
	PaneID   string   // stable tmux pane ID (e.g. "%5")
	Window   string   // background tmux window (e.g. "w3")
	Dir      SplitDir // split direction relative to the target
	TargetID string   // pane ID to join onto (e.g. "%3") — empty = right viewport
}

// ShowSession swaps a single session's tmux window pane into the right viewport.
// If another session is currently visible, it gets returned to its background window first.
// Focus stays on the left pane.
func (pl *PaneLayout) ShowSession(window string) {
	pl.ShowGroup(window, nil)
}

// ShowGroup swaps the primary session into the right viewport, then joins
// extra panes into splits. For single-session groups (extras=nil), this is
// identical to the old ShowSession behavior.
func (pl *PaneLayout) ShowGroup(primary_window string, extras []GroupPane) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	// Return current group first
	pl.return_active_locked()

	if primary_window == "" {
		pl.active_win = ""
		pl.active_extras = nil
		return
	}

	// Swap primary into viewport
	right := pl.resolve_right_viewport()
	pl.server.Run("swap-pane", "-s", fmt.Sprintf("wt:%s.0", primary_window), "-t", right)
	pl.active_win = primary_window

	// Join extra panes into the viewport as splits
	for _, ep := range extras {
		flag := "-v"
		if ep.Dir == SplitH {
			flag = "-h"
		}
		target := pl.resolve_right_viewport()
		if ep.TargetID != "" {
			target = ep.TargetID
		}
		pl.server.Run("join-pane", flag,
			"-s", ep.PaneID,
			"-t", target,
		)
	}

	pl.active_extras = extras
	if len(extras) > 0 {
		pl.restore_left_width()
	}
	pl.server.Run("select-pane", "-t", pl.left_pane_id)
}

// ReturnSession returns the currently visible session to its background window.
func (pl *PaneLayout) ReturnSession() {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.return_active_locked()
}

// return_active_locked returns the active group (primary + extras) to background.
// Caller must hold pl.mu.
func (pl *PaneLayout) return_active_locked() {
	if pl.active_win == "" {
		return
	}

	// Break extra panes back to their background windows (reverse order)
	for i := len(pl.active_extras) - 1; i >= 0; i-- {
		ep := pl.active_extras[i]
		pl.server.Run("break-pane", "-s", ep.PaneID, "-t", ep.Window)
	}

	// Swap primary back to its background window
	right := pl.resolve_right_viewport()
	swap_target := fmt.Sprintf("wt:%s.0", pl.active_win)
	out, swap_err := pl.server.Run("swap-pane", "-s", right, "-t", swap_target)
	if swap_err != nil {
		// swap failed — the background window may not have a pane to swap with.
		// This happens when the session was a survivor of kill-pane in a split group.
		// Respawn the right viewport pane with the guide placeholder instead.
		pl.server.Run("respawn-pane", "-k", "-t", right, "exec cat")
	}
	_ = out

	had_extras := len(pl.active_extras) > 0
	pl.active_win = ""
	pl.active_extras = nil
	if had_extras {
		pl.restore_left_width()
	}
	pl.server.Run("select-pane", "-t", pl.left_pane_id)
}

// SwitchTab swaps the visible group with a different one.
// Focus stays on the left pane so the user can keep browsing tabs.
func (pl *PaneLayout) SwitchTab(from_window, to_window string) {
	pl.SwitchGroup(from_window, nil, to_window, nil)
}

// SwitchGroup transitions from one group to another.
// Handles single→single, single→group, group→single, group→group transitions.
func (pl *PaneLayout) SwitchGroup(from_window string, from_extras []GroupPane, to_window string, to_extras []GroupPane) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	if from_window == to_window {
		return
	}

	// Return current group
	pl.return_active_locked()

	// Show new group
	if to_window != "" {
		right := pl.resolve_right_viewport()
		pl.server.Run("swap-pane", "-s", fmt.Sprintf("wt:%s.0", to_window), "-t", right)
		pl.active_win = to_window

		for _, ep := range to_extras {
			flag := "-v"
			if ep.Dir == SplitH {
				flag = "-h"
			}
			target := pl.resolve_right_viewport()
			if ep.TargetID != "" {
				target = ep.TargetID
			}
			pl.server.Run("join-pane", flag,
				"-s", ep.PaneID,
				"-t", target,
			)
		}
		pl.active_extras = to_extras
		if len(to_extras) > 0 {
			pl.restore_left_width()
		}
	}

	pl.server.Run("select-pane", "-t", pl.left_pane_id)
}

// ActiveWindow returns the window name currently displayed in the right pane.
func (pl *PaneLayout) ActiveWindow() string {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return pl.active_win
}

// HasActiveSession returns true if a session is currently displayed in the right pane.
func (pl *PaneLayout) HasActiveSession() bool {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return pl.active_win != ""
}

// ── Zoom & Focus ───────────────────────────────────────────────────────

// is_pane_in_viewport checks if a pane ID is currently in window 0 (the viewport).
func (pl *PaneLayout) is_pane_in_viewport(pane_id string) bool {
	out, err := pl.server.Run("list-panes", "-t", "wt:0", "-F", "#{pane_id}")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == pane_id {
			return true
		}
	}
	return false
}

// restore_left_width resizes the left pane back to its configured percentage.
// Called after kill-pane/join-pane operations that may redistribute pane widths.
func (pl *PaneLayout) restore_left_width() {
	if pl.left_pct > 0 {
		pl.server.Run("resize-pane", "-t", pl.left_pane_id,
			"-x", fmt.Sprintf("%d%%", pl.left_pct))
	}
}

// resolve_right_viewport returns a positional tmux target for the right viewport.
// Layout: left=idx0, notify=idx1, right=idx2. Right is always wt:0.2.
// Positional targets always refer to whatever pane occupies that position,
// even after swap-pane moves pane IDs between windows.
func (pl *PaneLayout) resolve_right_viewport() string {
	return rightViewportTarget
}

// ZoomRight zooms the right content pane to fill the entire tmux window.
func (pl *PaneLayout) ZoomRight() {
	pl.server.Run("resize-pane", "-t", pl.resolve_right_viewport(), "-Z")
}

// UnzoomRight unzooms the right content pane (restores the split layout).
// If not zoomed, this is a no-op (zoom toggles).
func (pl *PaneLayout) UnzoomRight() {
	out, err := pl.server.Run("display-message", "-t", "wt:0", "-p", "#{window_zoomed_flag}")
	if err != nil {
		return
	}
	if strings.TrimSpace(out) == "1" {
		pl.server.Run("resize-pane", "-t", pl.resolve_right_viewport(), "-Z")
	}
}

// IsZoomed returns true if the right pane is currently zoomed (fullscreen).
func (pl *PaneLayout) IsZoomed() bool {
	out, err := pl.server.Run("display-message", "-t", "wt:0", "-p", "#{window_zoomed_flag}")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "1"
}

// FocusRight switches tmux focus to the right content pane (terminal session).
func (pl *PaneLayout) FocusRight() {
	pl.server.Run("select-pane", "-t", pl.resolve_right_viewport())
}

// FocusLeft switches tmux focus to the left pane (bubbletea control app).
func (pl *PaneLayout) FocusLeft() {
	pl.server.Run("select-pane", "-t", pl.left_pane_id)
}

// RightPaneDimensions returns the current width and height of the right content pane.
func (pl *PaneLayout) RightPaneDimensions() (int, int) {
	out, err := pl.server.Run(
		"display-message", "-t", pl.resolve_right_viewport(),
		"-p", "#{pane_width} #{pane_height}",
	)
	if err != nil {
		return 80, 24
	}

	var w, h int
	n, _ := fmt.Sscanf(strings.TrimSpace(out), "%d %d", &w, &h)
	if n != 2 || w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

// ── Misc ───────────────────────────────────────────────────────────────

// UpdateStatusBar updates the tmux status bar content.
// Left side shows the panel/mode name, right side shows key hints.
func (pl *PaneLayout) UpdateStatusBar(left, right string) {
	ts := pl.server
	ts.Run("set-option", "-g", "status", "on")
	ts.Run("set-option", "-g", "status-style", "bg=default,fg=colour240")
	ts.Run("set-option", "-g", "status-left-length", "50")
	ts.Run("set-option", "-g", "status-right-length", "120")
	ts.Run("set-option", "-g", "status-left", fmt.Sprintf(" %s", left))
	ts.Run("set-option", "-g", "status-right", fmt.Sprintf("%s ", right))
}

// ShowPopup displays a tmux popup overlay (requires tmux 3.3+).
// The popup covers both panes and runs the given shell command.
func (pl *PaneLayout) ShowPopup(content string, width_pct, height_pct int) {
	if width_pct <= 0 {
		width_pct = 60
	}
	if height_pct <= 0 {
		height_pct = 80
	}

	pl.server.Run(
		"display-popup",
		"-w", fmt.Sprintf("%d%%", width_pct),
		"-h", fmt.Sprintf("%d%%", height_pct),
		"-E",
		fmt.Sprintf("echo '%s' | less -R", strings.ReplaceAll(content, "'", "'\\''")),
	)
}

// ResizeSplit adjusts the left/right pane split ratio.
func (pl *PaneLayout) ResizeSplit(left_cols int) {
	pl.server.Run("resize-pane", "-t", pl.left_pane_id, "-x", fmt.Sprintf("%d", left_cols))
}

// IsRightPaneFocused returns true if the right terminal pane has tmux focus.
func (pl *PaneLayout) IsRightPaneFocused() bool {
	out, err := pl.server.Run("display-message", "-t", "wt:0", "-p", "#{pane_index}")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "1"
}

// IsClientFocused returns true if the tmux client (terminal window) has focus.
// When the user switches to another app or terminal tab, this returns false.
func (pl *PaneLayout) IsClientFocused() bool {
	out, err := pl.server.Run("list-clients", "-F", "#{client_flags}")
	if err != nil {
		return false
	}
	return strings.Contains(out, "focused")
}

// Server returns the underlying TmuxServer.
func (pl *PaneLayout) Server() *TmuxServer {
	return pl.server
}
