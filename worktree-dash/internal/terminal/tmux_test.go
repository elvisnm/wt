package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("requires tmux")
	}
}

func TestCheckTmux(t *testing.T) {
	// Can only truly test the happy path since tmux may or may not be installed
	err := CheckTmux()
	if _, lookErr := exec.LookPath("tmux"); lookErr != nil {
		if err == nil {
			t.Fatal("expected error when tmux is not found")
		}
	} else {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestNewTmuxServer(t *testing.T) {
	requireTmux(t)

	ts := NewTmuxServer()
	if ts == nil {
		t.Fatal("NewTmuxServer returned nil")
	}
	if ts.Socket() == "" {
		t.Fatal("socket name is empty")
	}

	// Verify socket dir was created
	home, _ := os.UserHomeDir()
	socket_dir := filepath.Join(home, ".wt", "tmux")
	if _, err := os.Stat(socket_dir); err != nil {
		t.Fatalf("socket dir not created: %v", err)
	}
}

func TestServerLifecycle(t *testing.T) {
	requireTmux(t)

	ts := NewTmuxServer()
	defer ts.Kill()

	// Server should not be started yet
	if ts.started {
		t.Fatal("server should not be started before EnsureStarted")
	}

	// Start server
	if err := ts.EnsureStarted(); err != nil {
		t.Fatalf("EnsureStarted failed: %v", err)
	}

	if !ts.started {
		t.Fatal("server should be started after EnsureStarted")
	}

	// Verify we can run commands
	out, err := ts.Run("list-sessions")
	if err != nil {
		t.Fatalf("list-sessions failed: %v", err)
	}
	if out == "" {
		t.Fatal("expected session list output")
	}

	// Second call should be a no-op
	if err := ts.EnsureStarted(); err != nil {
		t.Fatalf("second EnsureStarted failed: %v", err)
	}

	// Kill server
	ts.Kill()

	if ts.started {
		t.Fatal("server should not be started after Kill")
	}
}

func TestStaleSocketCleanup(t *testing.T) {
	requireTmux(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}

	socket_dir := filepath.Join(home, ".wt", "tmux")
	os.MkdirAll(socket_dir, 0755)

	// Create a marker file for a non-existent PID
	stale_path := filepath.Join(socket_dir, "wt-999999999")
	os.WriteFile(stale_path, []byte{}, 0644)

	// Creating a new server should clean it up
	ts := NewTmuxServer()
	defer ts.Kill()

	if _, err := os.Stat(stale_path); err == nil {
		t.Fatal("stale socket marker should have been removed")
	}
}
