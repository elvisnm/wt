package terminal

import (
	"fmt"
	"strings"
	"sync"
)

// PaneLayout manages the 2-pane tmux layout for the dashboard.
// Pane 0 (left): bubbletea control app
// Pane 1 (right): native terminal session
type PaneLayout struct {
	server     *TmuxServer
	active_win string // which session window is currently displayed in the right pane
	mu         sync.Mutex
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

	// Focus back to the left pane so bubbletea gets input first
	ts.Run("select-pane", "-t", "wt:0.0")

	return &PaneLayout{
		server: ts,
	}, nil
}

// NewPaneLayout creates a PaneLayout connected to an existing tmux server.
// Used by the inner-mode bubbletea app to manage pane operations.
func NewPaneLayout(server *TmuxServer) *PaneLayout {
	return &PaneLayout{
		server: server,
	}
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
	ts.Run("bind-key", "q", "select-pane", "-t", "wt:0.0")

	// prefix+f: toggle zoom on the content pane
	ts.Run("bind-key", "f", "resize-pane", "-t", "wt:0.1", "-Z")

	// prefix+1-9: jump to tab N — sends Alt+N to bubbletea (pane 0)
	// Bubbletea's alt_tab_number handler picks this up and calls FocusByIndex + FocusRight
	for i := 1; i <= 9; i++ {
		key := fmt.Sprintf("%d", i)
		ts.Run("bind-key", key, "send-keys", "-t", "wt:0.0", fmt.Sprintf("M-%d", i))
	}
}

// ShowSession swaps a session's tmux window pane into the right viewport (pane 1).
// If another session is currently visible, it gets returned to its background window first.
// Focus stays on the left pane (pane 0).
func (pl *PaneLayout) ShowSession(window string) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	// Return current session to its background window
	if pl.active_win != "" {
		pl.server.Run("swap-pane", "-s", "wt:0.1", "-t", fmt.Sprintf("wt:%s.0", pl.active_win))
	}

	if window == "" {
		pl.active_win = ""
		return
	}

	// Bring new session into the right viewport
	pl.server.Run("swap-pane", "-s", fmt.Sprintf("wt:%s.0", window), "-t", "wt:0.1")
	pl.active_win = window

	// Ensure focus stays on the left pane (swap-pane can move focus)
	pl.server.Run("select-pane", "-t", "wt:0.0")
}

// ReturnSession returns the currently visible session to its background window.
// The right pane reverts to showing the placeholder.
func (pl *PaneLayout) ReturnSession() {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	if pl.active_win == "" {
		return
	}

	pl.server.Run("swap-pane", "-s", "wt:0.1", "-t", fmt.Sprintf("wt:%s.0", pl.active_win))
	pl.active_win = ""
	pl.server.Run("select-pane", "-t", "wt:0.0")
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
		pl.server.Run("swap-pane", "-s", "wt:0.1", "-t", fmt.Sprintf("wt:%s.0", from_window))
	}

	// Bring new session into the viewport
	if to_window != "" {
		pl.server.Run("swap-pane", "-s", fmt.Sprintf("wt:%s.0", to_window), "-t", "wt:0.1")
	}

	pl.active_win = to_window

	// Ensure focus stays on the left pane
	pl.server.Run("select-pane", "-t", "wt:0.0")
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

// ZoomRight zooms the right content pane to fill the entire tmux window.
func (pl *PaneLayout) ZoomRight() {
	pl.server.Run("resize-pane", "-t", "wt:0.1", "-Z")
}

// UnzoomRight unzooms the right content pane (restores the split layout).
// If not zoomed, this is a no-op (zoom toggles).
func (pl *PaneLayout) UnzoomRight() {
	// Check if currently zoomed before toggling
	out, err := pl.server.Run("display-message", "-t", "wt:0", "-p", "#{window_zoomed_flag}")
	if err != nil {
		return
	}
	if strings.TrimSpace(out) == "1" {
		pl.server.Run("resize-pane", "-t", "wt:0.1", "-Z")
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
	pl.server.Run("select-pane", "-t", "wt:0.1")
}

// FocusLeft switches tmux focus to the left pane (bubbletea control app).
func (pl *PaneLayout) FocusLeft() {
	pl.server.Run("select-pane", "-t", "wt:0.0")
}

// RightPaneDimensions returns the current width and height of the right content pane.
func (pl *PaneLayout) RightPaneDimensions() (int, int) {
	out, err := pl.server.Run(
		"display-message", "-t", "wt:0.1",
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
	pl.server.Run("resize-pane", "-t", "wt:0.0", "-x", fmt.Sprintf("%d", left_cols))
}

// Server returns the underlying TmuxServer.
func (pl *PaneLayout) Server() *TmuxServer {
	return pl.server
}
