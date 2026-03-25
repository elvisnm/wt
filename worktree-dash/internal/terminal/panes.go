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
	server     *TmuxServer
	active_win string // which session window is currently displayed in the right pane

	// Stable tmux pane IDs (e.g. "%0", "%1", "%5") — survive splits and swaps
	left_pane_id string // bubbletea control app

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
	}

	return pl, nil
}

// NewPaneLayout creates a PaneLayout connected to an existing tmux server.
// Used by the inner-mode bubbletea app to manage pane operations.
// Layout: left=idx0, right=idx1 (no notify pane).
func NewPaneLayout(server *TmuxServer) *PaneLayout {
	left_id := capture_pane_id(server, "wt:0.0")

	return &PaneLayout{
		server:       server,
		left_pane_id: left_id,
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

// ShowSession swaps a session's tmux window pane into the right viewport.
// If another session is currently visible, it gets returned to its background window first.
// Focus stays on the left pane.
//
// Note: swap-pane moves pane IDs between windows, so we always resolve the
// right viewport position dynamically rather than using a stored pane ID.
func (pl *PaneLayout) ShowSession(window string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	// Return current session to its background window
	if pl.active_win != "" {
		right := pl.resolve_right_viewport()
		pl.server.Run("swap-pane", "-s", right, "-t", fmt.Sprintf("wt:%s.0", pl.active_win))
	}

	if window == "" {
		pl.active_win = ""
		return
	}

	// Re-resolve after swap-out — the right viewport pane changed
	right := pl.resolve_right_viewport()
	pl.server.Run("swap-pane", "-s", fmt.Sprintf("wt:%s.0", window), "-t", right)
	pl.active_win = window

	pl.server.Run("select-pane", "-t", pl.left_pane_id)
}

// ReturnSession returns the currently visible session to its background window.
// The right pane reverts to showing the placeholder.
func (pl *PaneLayout) ReturnSession() {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	if pl.active_win == "" {
		return
	}

	right := pl.resolve_right_viewport()
	pl.server.Run("swap-pane", "-s", right, "-t", fmt.Sprintf("wt:%s.0", pl.active_win))
	pl.active_win = ""
	pl.server.Run("select-pane", "-t", pl.left_pane_id)
}

// SwitchTab swaps the visible session with a different one.
// Focus stays on the left pane so the user can keep browsing tabs.
func (pl *PaneLayout) SwitchTab(from_window, to_window string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	if from_window == to_window {
		return
	}

	// Return current session to its background window
	if from_window != "" {
		right := pl.resolve_right_viewport()
		pl.server.Run("swap-pane", "-s", right, "-t", fmt.Sprintf("wt:%s.0", from_window))
	}

	// Re-resolve after swap-out — the right viewport pane changed
	if to_window != "" {
		right := pl.resolve_right_viewport()
		pl.server.Run("swap-pane", "-s", fmt.Sprintf("wt:%s.0", to_window), "-t", right)
	}

	pl.active_win = to_window

	// Ensure focus stays on the left pane
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
