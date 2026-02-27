package terminal

import (
	"testing"
)

func setupPaneLayout(t *testing.T) (*TmuxServer, *PaneLayout) {
	t.Helper()
	ts := newTestServer(t)
	if err := ts.EnsureStarted(); err != nil {
		t.Fatalf("EnsureStarted failed: %v", err)
	}

	pl, err := SetupPaneLayout(ts, 28, "")
	if err != nil {
		t.Fatalf("SetupPaneLayout failed: %v", err)
	}
	return ts, pl
}

func TestSetupPaneLayout(t *testing.T) {
	_, pl := setupPaneLayout(t)

	// Should have no active session initially
	if pl.HasActiveSession() {
		t.Fatal("expected no active session after setup")
	}
	if pl.ActiveWindow() != "" {
		t.Errorf("ActiveWindow = %q, want empty", pl.ActiveWindow())
	}

	// Right pane should exist and have valid dimensions
	w, h := pl.RightPaneDimensions()
	if w <= 0 || h <= 0 {
		t.Errorf("RightPaneDimensions = (%d, %d), want positive", w, h)
	}
}

func TestShowAndReturnSession(t *testing.T) {
	ts, pl := setupPaneLayout(t)

	// Create a session
	s, err := NewSession(1, "test-show", "bash", []string{"-c", "sleep 30"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	// Show session in right pane
	pl.ShowSession(s.Window())
	if !pl.HasActiveSession() {
		t.Fatal("expected active session after ShowSession")
	}
	if pl.ActiveWindow() != s.Window() {
		t.Errorf("ActiveWindow = %q, want %q", pl.ActiveWindow(), s.Window())
	}

	// Return session to background
	pl.ReturnSession()
	if pl.HasActiveSession() {
		t.Fatal("expected no active session after ReturnSession")
	}
}

func TestSwitchTab(t *testing.T) {
	ts, pl := setupPaneLayout(t)

	// Create two sessions
	s1, err := NewSession(1, "tab1", "bash", []string{"-c", "sleep 30"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession 1 failed: %v", err)
	}
	defer s1.Close()

	s2, err := NewSession(2, "tab2", "bash", []string{"-c", "sleep 30"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession 2 failed: %v", err)
	}
	defer s2.Close()

	// Show first session
	pl.ShowSession(s1.Window())
	if pl.ActiveWindow() != s1.Window() {
		t.Errorf("ActiveWindow = %q, want %q", pl.ActiveWindow(), s1.Window())
	}

	// Switch to second session
	pl.SwitchTab(s1.Window(), s2.Window())
	if pl.ActiveWindow() != s2.Window() {
		t.Errorf("after SwitchTab: ActiveWindow = %q, want %q", pl.ActiveWindow(), s2.Window())
	}

	// Switch back to first
	pl.SwitchTab(s2.Window(), s1.Window())
	if pl.ActiveWindow() != s1.Window() {
		t.Errorf("after SwitchTab back: ActiveWindow = %q, want %q", pl.ActiveWindow(), s1.Window())
	}
}

func TestSwitchTabSameWindow(t *testing.T) {
	ts, pl := setupPaneLayout(t)

	s, err := NewSession(1, "tab", "bash", []string{"-c", "sleep 30"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	pl.ShowSession(s.Window())

	// Switching to the same window should be a no-op
	pl.SwitchTab(s.Window(), s.Window())
	if pl.ActiveWindow() != s.Window() {
		t.Errorf("ActiveWindow = %q, want %q", pl.ActiveWindow(), s.Window())
	}
}

func TestZoomUnzoom(t *testing.T) {
	_, pl := setupPaneLayout(t)

	// Should not be zoomed initially
	if pl.IsZoomed() {
		t.Fatal("expected not zoomed initially")
	}

	// Zoom the right pane
	pl.ZoomRight()
	if !pl.IsZoomed() {
		t.Fatal("expected zoomed after ZoomRight")
	}

	// Unzoom
	pl.UnzoomRight()
	if pl.IsZoomed() {
		t.Fatal("expected not zoomed after UnzoomRight")
	}
}

func TestUnzoomWhenNotZoomed(t *testing.T) {
	_, pl := setupPaneLayout(t)

	// Should be a no-op (not zoomed)
	pl.UnzoomRight()
	if pl.IsZoomed() {
		t.Fatal("should not be zoomed")
	}
}

func TestConfigureBindings(t *testing.T) {
	_, pl := setupPaneLayout(t)

	// Should not panic
	pl.ConfigureBindings()
}

func TestNewPaneLayout(t *testing.T) {
	requireTmux(t)
	ts := NewTmuxServer()
	defer ts.Kill()

	if err := ts.EnsureStarted(); err != nil {
		t.Fatalf("EnsureStarted failed: %v", err)
	}

	// ConnectTmuxServer creates a "connected" server (inner mode)
	connected := ConnectTmuxServer(ts.Socket())
	pl := NewPaneLayout(connected)

	if pl.server != connected {
		t.Fatal("server not set correctly")
	}
}

func TestUpdateStatusBar(t *testing.T) {
	_, pl := setupPaneLayout(t)

	// Should not panic
	pl.UpdateStatusBar("left text", "right text")
}

func TestShowSessionReplacesActive(t *testing.T) {
	ts, pl := setupPaneLayout(t)

	s1, err := NewSession(1, "first", "bash", []string{"-c", "sleep 30"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession 1 failed: %v", err)
	}
	defer s1.Close()

	s2, err := NewSession(2, "second", "bash", []string{"-c", "sleep 30"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession 2 failed: %v", err)
	}
	defer s2.Close()

	// Show first session
	pl.ShowSession(s1.Window())
	if pl.ActiveWindow() != s1.Window() {
		t.Fatalf("ActiveWindow = %q, want %q", pl.ActiveWindow(), s1.Window())
	}

	// Show second session (should auto-return first)
	pl.ShowSession(s2.Window())
	if pl.ActiveWindow() != s2.Window() {
		t.Errorf("ActiveWindow = %q, want %q", pl.ActiveWindow(), s2.Window())
	}
}

func TestShowEmptyWindowClearsActive(t *testing.T) {
	ts, pl := setupPaneLayout(t)

	s, err := NewSession(1, "test", "bash", []string{"-c", "sleep 30"}, 80, 24, "", ts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	pl.ShowSession(s.Window())
	if !pl.HasActiveSession() {
		t.Fatal("expected active session")
	}

	// Show empty window should clear
	pl.ShowSession("")
	if pl.HasActiveSession() {
		t.Fatal("expected no active session after ShowSession empty")
	}
}
