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

	server *TmuxServer
	window string // tmux window name "w{id}"
	target string // "{window}.0" — the pane target

	ExitCode int // process exit code (-1 if unknown)

	done chan struct{}
	mu   sync.Mutex
}

// NewSession creates a tmux window running the given command.
// If the tmux session doesn't have any windows yet (first call after EnsureStarted),
// it reuses the initial window. Otherwise it creates a new window.
func NewSession(id int, label string, cmd_name string, args []string, width, height int, dir string, server *TmuxServer) (*Session, error) {
	if err := server.EnsureStarted(); err != nil {
		return nil, err
	}

	window := fmt.Sprintf("w%d", id)
	target := fmt.Sprintf("%s.0", window)

	// Build the shell command to run inside tmux
	shell_cmd := cmd_name
	if len(args) > 0 {
		// Quote args that contain spaces
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
		ExitCode: -1,
		done:     make(chan struct{}),
	}

	go s.monitor_loop()

	return s, nil
}

// monitor_loop polls tmux to detect when the pane's process exits.
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

		// Query pane status: #{pane_dead} #{pane_dead_status}
		out, err := s.server.Run(
			"list-panes", "-t", s.window,
			"-F", "#{pane_dead} #{pane_dead_status}",
		)
		if err != nil {
			// Window was killed externally
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

// Write sends input bytes to the tmux pane via send-keys -H (hex-encoded).
func (s *Session) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	// Convert bytes to hex format for send-keys -H
	hex_parts := make([]string, len(data))
	for i, b := range data {
		hex_parts[i] = fmt.Sprintf("%02x", b)
	}

	_, err := s.server.Run(
		"send-keys", "-t", s.target, "-H",
		strings.Join(hex_parts, " "),
	)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// WriteString sends a string to the tmux pane.
func (s *Session) WriteString(str string) (int, error) {
	return s.Write([]byte(str))
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
