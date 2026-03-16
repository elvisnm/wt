package terminal

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Session represents a terminal session backed by a tmux window.
// The session's pane is displayed natively via swap-pane in the right viewport.
type Session struct {
	ID    int
	Label string
	Alive bool

	server  *TmuxServer
	window  string // tmux window name "w{id}"
	target  string // "{window}.0" — the pane target
	pane_id string // tmux pane ID (e.g. "%5") — stable across swap-pane

	ExitCode int // process exit code (-1 if unknown)

	done chan struct{}
	mu   sync.Mutex
}

// build_shell_cmd builds a shell command string with proper quoting.
// Uses exec so the shell replaces itself with the target process.
func build_shell_cmd(cmd_name string, args []string) string {
	shell_cmd := "exec " + cmd_name
	if len(args) > 0 {
		quoted := make([]string, len(args))
		for i, a := range args {
			if strings.ContainsAny(a, " \t\"'\\$") {
				quoted[i] = "'" + strings.ReplaceAll(a, "'", "'\\''") + "'"
			} else {
				quoted[i] = a
			}
		}
		shell_cmd += " " + strings.Join(quoted, " ")
	}
	return shell_cmd
}

// NewSession creates a tmux window running the given command.
// If the tmux session doesn't have any windows yet (first call after EnsureStarted),
// it reuses the initial window. Otherwise it creates a new window.
func NewSession(id int, label string, cmd_name string, args []string, width, height int, dir string, server *TmuxServer) (*Session, error) {
	if err := server.EnsureStarted(0, 0); err != nil {
		return nil, err
	}

	window := fmt.Sprintf("w%d", id)
	target := fmt.Sprintf("%s.0", window)

	shell_cmd := build_shell_cmd(cmd_name, args)

	// Build new-window command (no -x/-y: those require tmux 3.2+)
	tmux_args := []string{
		"new-window", "-d",
		"-n", window,
	}
	if dir != "" {
		tmux_args = append(tmux_args, "-c", dir)
	}
	tmux_args = append(tmux_args, shell_cmd)

	out, err := server.Run(tmux_args...)
	if err != nil {
		if strings.Contains(out, "no current session") || strings.Contains(out, "session not found") {
			return nil, fmt.Errorf("tmux window creation failed: %w\n%s", err, out)
		}
		return nil, fmt.Errorf("tmux new-window failed: %w\n%s", err, out)
	}

	// Override remain-on-exit for this window so we can detect exit code
	server.Run("set-option", "-t", window, "remain-on-exit", "on")

	// Capture the pane ID (e.g. "%5") — stable across swap-pane operations
	pane_id := ""
	if pid_out, err := server.Run("display-message", "-t", target, "-p", "#{pane_id}"); err == nil {
		pane_id = strings.TrimSpace(pid_out)
	}

	// Resize the pane to the requested dimensions (works on all tmux versions)
	server.Run("resize-pane", "-t", target,
		"-x", fmt.Sprintf("%d", width),
		"-y", fmt.Sprintf("%d", height),
	)

	s := &Session{
		ID:       id,
		Label:    label,
		Alive:    true,
		server:   server,
		window:   window,
		target:   target,
		pane_id:  pane_id,
		ExitCode: -1,
		done:     make(chan struct{}),
	}

	go s.monitor_loop()

	return s, nil
}

// monitor_loop polls tmux to detect when the pane's process exits.
// Uses the pane ID (e.g. "%5") to track the pane across swap-pane operations,
// since swap-pane moves panes between windows and the window name alone would
// query the wrong pane after a swap.
func (s *Session) monitor_loop() {
	defer close(s.done)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		if !s.Alive {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		// Query pane status using the pane ID which is stable across swap-pane.
		// Fall back to window name if pane ID wasn't captured.
		target := s.window
		if s.pane_id != "" {
			target = s.pane_id
		}
		out, err := s.server.Run(
			"display-message", "-t", target,
			"-p", "#{pane_dead} #{pane_dead_status}",
		)
		if err != nil {
			// Pane/window was killed externally
			s.mu.Lock()
			s.Alive = false
			s.mu.Unlock()
			return
		}

		fields := strings.Fields(strings.TrimSpace(out))
		if len(fields) >= 1 && fields[0] == "1" {
			// Pane is dead
			exit_code := -1
			if len(fields) >= 2 {
				fmt.Sscanf(fields[1], "%d", &exit_code)
			}
			s.mu.Lock()
			s.Alive = false
			s.ExitCode = exit_code
			s.mu.Unlock()
			return
		}
	}
}

// Resize changes the tmux pane dimensions.
func (s *Session) Resize(width, height int) {
	s.server.Run(
		"resize-pane", "-t", s.target,
		"-x", fmt.Sprintf("%d", width),
		"-y", fmt.Sprintf("%d", height),
	)
}

// IsAlive checks if the process is still running.
func (s *Session) IsAlive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Alive
}

// Window returns the tmux window name for this session.
func (s *Session) Window() string {
	return s.window
}

// PaneID returns the stable tmux pane ID (e.g. "%5") for this session.
func (s *Session) PaneID() string {
	return s.pane_id
}

// Respawn kills the running process and starts a new command in the same pane.
// The tmux window and pane stay in place — no pane swapping or window recreation.
func (s *Session) Respawn(cmd_name string, args []string, dir string) {
	s.mu.Lock()
	s.Alive = true
	s.ExitCode = -1
	s.mu.Unlock()

	shell_cmd := build_shell_cmd(cmd_name, args)
	// Use pane_id (e.g. "%5") instead of window target — the pane may have
	// been swapped into a different window position by ShowSession.
	target := s.pane_id
	if target == "" {
		target = s.target
	}
	respawn_args := []string{"respawn-pane", "-k", "-t", target}
	if dir != "" {
		respawn_args = append(respawn_args, "-c", dir)
	}
	respawn_args = append(respawn_args, shell_cmd)
	s.server.Run(respawn_args...)
}

// Close terminates the tmux window.
func (s *Session) Close() {
	s.mu.Lock()
	already_dead := !s.Alive
	s.Alive = false
	s.mu.Unlock()

	if !already_dead {
		s.server.Run("kill-window", "-t", s.window)
	}

	// Wait for monitor to finish (with timeout)
	select {
	case <-s.done:
	case <-time.After(500 * time.Millisecond):
	}
}
