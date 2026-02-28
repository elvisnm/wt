package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// TmuxServer manages a dedicated tmux server instance for the dashboard.
// Each dashboard process gets its own tmux server via a unique socket.
type TmuxServer struct {
	socket     string // socket name (not path), e.g. "wt-12345"
	socket_dir string // directory for socket files
	ctrl_cmd   *exec.Cmd
	started    bool
	mu         sync.Mutex
}

// CheckTmux verifies that tmux is installed and available on PATH.
// Returns nil if found, or an error with platform-specific install instructions.
func CheckTmux() error {
	_, err := exec.LookPath("tmux")
	if err != nil {
		switch runtime.GOOS {
		case "darwin":
			return fmt.Errorf("tmux is required but not found.\n\nInstall with:\n  brew install tmux")
		case "linux":
			return fmt.Errorf("tmux is required but not found.\n\nInstall with:\n  sudo apt install tmux    # Debian/Ubuntu\n  sudo dnf install tmux    # Fedora\n  sudo pacman -S tmux      # Arch")
		default:
			return fmt.Errorf("tmux is required but not found.\n\nPlease install tmux and ensure it is on your PATH.")
		}
	}
	return nil
}

// NewTmuxServer creates a new TmuxServer with a unique socket for this process.
// Cleans up stale sockets from previous dashboard instances that didn't exit cleanly.
func NewTmuxServer() *TmuxServer {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	socket_dir := filepath.Join(home, ".wt", "tmux")
	os.MkdirAll(socket_dir, 0755)

	socket := fmt.Sprintf("wt-%d", os.Getpid())

	ts := &TmuxServer{
		socket:     socket,
		socket_dir: socket_dir,
	}

	ts.cleanup_stale_sockets()

	return ts
}

// ConnectTmuxServer returns a TmuxServer that connects to an existing socket.
// Used by the inner-mode bubbletea app to share the tmux server created by the outer process.
func ConnectTmuxServer(socket string) *TmuxServer {
	return &TmuxServer{
		socket:  socket,
		started: true,
	}
}

// Socket returns the tmux socket name.
func (ts *TmuxServer) Socket() string {
	return ts.socket
}

// IsStarted returns whether the server has been started.
func (ts *TmuxServer) IsStarted() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.started
}

// EnsureStarted creates the tmux server on first call.
// Creates a detached session with sensible defaults and starts a control-mode
// client to keep the server alive for resize operations.
// Pass the real terminal width/height so the initial pane layout matches the
// final size after tmux attach (avoids a visible re-layout flicker).
func (ts *TmuxServer) EnsureStarted(width, height int) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.started {
		return nil
	}

	if width <= 0 {
		width = 200
	}
	if height <= 0 {
		height = 50
	}

	// Create a new tmux session (detached) sized to match the real terminal
	out, err := ts.run_locked("new-session", "-d", "-s", "wt",
		"-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
	if err != nil {
		// Retry without -x/-y for older tmux versions
		out, err = ts.run_locked("new-session", "-d", "-s", "wt")
		if err != nil {
			return fmt.Errorf("tmux new-session failed: %w\n%s", err, out)
		}
	}

	// Configure the server
	configs := [][]string{
		{"set-option", "-g", "remain-on-exit", "on"},
		{"set-option", "-g", "status", "off"},
		{"set-option", "-g", "window-size", "largest"},
		{"set-option", "-g", "aggressive-resize", "on"},
		{"set-option", "-g", "escape-time", "200"},
		{"set-option", "-g", "mouse", "on"},
		// Pane border color — matches left panel border (colour240)
		{"set-option", "-g", "pane-border-style", "fg=colour240"},
		{"set-option", "-g", "pane-active-border-style", "fg=colour240"},
		// Set an obscure prefix key to avoid conflicts
		{"set-option", "-g", "prefix", "C-b"},
		{"set-option", "-g", "prefix2", "None"},
	}
	for _, args := range configs {
		ts.run_locked(args...)
	}

	// Start control-mode client in background to keep server alive.
	// This invisible client allows resize-pane to work even when no
	// tmux attach is active (i.e., when viewing capture-pane in panel mode).
	ts.ctrl_cmd = exec.Command("tmux", "-L", ts.socket, "-CC", "attach-session", "-t", "wt")
	ts.ctrl_cmd.Stdin = nil
	ts.ctrl_cmd.Stdout = nil
	ts.ctrl_cmd.Stderr = nil
	if err := ts.ctrl_cmd.Start(); err != nil {
		// Non-fatal: resize may not work in panel mode but sessions still work
		ts.ctrl_cmd = nil
	}

	ts.started = true
	return nil
}

// Run executes a tmux command on this server's socket.
// Prepends the socket flag automatically.
func (ts *TmuxServer) Run(args ...string) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.run_locked(args...)
}

// run_locked executes a tmux command (caller must hold ts.mu).
func (ts *TmuxServer) run_locked(args ...string) (string, error) {
	full := make([]string, 0, len(args)+2)
	full = append(full, "-L", ts.socket)
	full = append(full, args...)

	cmd := exec.Command("tmux", full...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// KillControlClient terminates the control-mode client without killing the server.
// Called before tmux attach so the real client is the only one — prevents a resize
// when the real terminal's dimensions differ from the control client's.
func (ts *TmuxServer) KillControlClient() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.ctrl_cmd != nil && ts.ctrl_cmd.Process != nil {
		ts.ctrl_cmd.Process.Kill()
		ts.ctrl_cmd.Wait()
		ts.ctrl_cmd = nil
	}
}

// Kill terminates the tmux server and cleans up the control client.
func (ts *TmuxServer) Kill() {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if !ts.started {
		return
	}

	// Kill the control-mode client
	if ts.ctrl_cmd != nil && ts.ctrl_cmd.Process != nil {
		ts.ctrl_cmd.Process.Kill()
		ts.ctrl_cmd.Wait()
		ts.ctrl_cmd = nil
	}

	// Kill the tmux server
	ts.run_locked("kill-server")
	ts.started = false
}

// cleanup_stale_sockets removes socket files from previous dashboard
// instances that didn't exit cleanly. Checks if the PID is still alive.
func (ts *TmuxServer) cleanup_stale_sockets() {
	entries, err := os.ReadDir(ts.socket_dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "wt-") {
			continue
		}

		pid_str := strings.TrimPrefix(name, "wt-")
		pid, err := strconv.Atoi(pid_str)
		if err != nil {
			continue
		}

		// Skip our own socket
		if pid == os.Getpid() {
			continue
		}

		// Check if the process is still running
		if process_alive(pid) {
			continue
		}

		// Stale socket — kill the tmux server and remove the file
		kill_cmd := exec.Command("tmux", "-L", name, "kill-server")
		kill_cmd.Run()

		// tmux stores sockets in its own tmp dir, but clean our marker too
		os.Remove(filepath.Join(ts.socket_dir, name))
	}
}

// process_alive checks if a process with the given PID exists.
func process_alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to test existence.
	return p.Signal(syscall.Signal(0)) == nil
}
