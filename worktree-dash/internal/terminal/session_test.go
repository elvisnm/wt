package terminal

import (
	"testing"
	"time"
)

func newTestServer(t *testing.T) *TmuxServer {
	t.Helper()
	requireTmux(t)

	ts := NewTmuxServer()
	t.Cleanup(func() { ts.Kill() })
	return ts
}

func TestNewSession(t *testing.T) {
	ts := newTestServer(t)

	s, err := NewSession(1, "test-echo", "echo", []string{"hello"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	if s.ID != 1 {
		t.Errorf("ID = %d, want 1", s.ID)
	}
	if s.Label != "test-echo" {
		t.Errorf("Label = %q, want %q", s.Label, "test-echo")
	}
	if s.Window() != "w1" {
		t.Errorf("Window = %q, want %q", s.Window(), "w1")
	}
}

func TestSessionResize(t *testing.T) {
	ts := newTestServer(t)

	s, err := NewSession(3, "test-resize", "bash", []string{"-c", "sleep 10"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	// Resize should not error
	s.Resize(120, 40)
}

func TestSessionExitCode(t *testing.T) {
	ts := newTestServer(t)

	// Command that exits with code 42
	s, err := NewSession(4, "test-exit", "bash", []string{"-c", "exit 42"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	// Wait for the process to die and monitor to detect it
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !s.IsAlive() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if s.IsAlive() {
		t.Fatal("session should have died")
	}

	s.mu.Lock()
	code := s.ExitCode
	s.mu.Unlock()

	if code != 42 {
		t.Errorf("ExitCode = %d, want 42", code)
	}
}

func TestSessionClose(t *testing.T) {
	ts := newTestServer(t)

	s, err := NewSession(6, "test-close", "bash", []string{"-c", "sleep 60"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	if !s.IsAlive() {
		t.Fatal("session should be alive before Close")
	}

	s.Close()

	if s.IsAlive() {
		t.Fatal("session should not be alive after Close")
	}
}
