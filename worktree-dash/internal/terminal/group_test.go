package terminal

import (
	"strings"
	"testing"
)

func mock_session(id int, label string) *Session {
	return &Session{ID: id, Label: label, Alive: true}
}

func TestNewTabGroup(t *testing.T) {
	s := mock_session(1, "Shell")
	g := NewTabGroup(1, s)

	if g.Count() != 1 {
		t.Errorf("Count = %d, want 1", g.Count())
	}
	if g.IsSplit() {
		t.Error("single-session group should not be split")
	}
	if g.Primary() != s {
		t.Error("Primary should be the original session")
	}
	if g.LayoutMap() != nil {
		t.Error("single-session group should have nil LayoutMap")
	}
}

func TestAddSplitH(t *testing.T) {
	s1 := mock_session(1, "Shell")
	s2 := mock_session(2, "Claude")
	g := NewTabGroup(1, s1)

	if !g.Add(s2, 1, SplitH) {
		t.Fatal("Add should succeed")
	}
	if g.Count() != 2 {
		t.Errorf("Count = %d, want 2", g.Count())
	}
	if !g.IsSplit() {
		t.Error("group should be split after Add")
	}
	if !g.Contains(2) {
		t.Error("group should contain session 2")
	}
}

func TestAddSplitV(t *testing.T) {
	s1 := mock_session(1, "Shell")
	s2 := mock_session(2, "Logs")
	g := NewTabGroup(1, s1)

	if !g.Add(s2, 1, SplitV) {
		t.Fatal("Add should succeed")
	}
	if g.Count() != 2 {
		t.Errorf("Count = %d, want 2", g.Count())
	}
}

func TestAddMaxCapacity(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 2, SplitV)
	g.Add(mock_session(4, "S4"), 1, SplitV)

	if g.Count() != 4 {
		t.Errorf("Count = %d, want 4", g.Count())
	}

	// 5th should fail
	if g.Add(mock_session(5, "S5"), 3, SplitH) {
		t.Error("Add should fail at max capacity")
	}
}

func TestAddInvalidTarget(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "Shell"))
	if g.Add(mock_session(2, "Claude"), 999, SplitH) {
		t.Error("Add should fail with invalid target")
	}
}

func TestRemove(t *testing.T) {
	s1 := mock_session(1, "Shell")
	s2 := mock_session(2, "Claude")
	g := NewTabGroup(1, s1)
	g.Add(s2, 1, SplitH)

	removed := g.Remove(2)
	if removed != s2 {
		t.Error("should return removed session")
	}
	if g.Count() != 1 {
		t.Errorf("Count = %d, want 1", g.Count())
	}
	if g.IsSplit() {
		t.Error("should not be split after removing to 1")
	}
}

func TestRemoveNotFound(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "Shell"))
	if g.Remove(999) != nil {
		t.Error("should return nil for unknown ID")
	}
}

func TestSessionIDs(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 2, SplitV)

	ids := g.tree.session_ids()
	if len(ids) != 3 {
		t.Errorf("session_ids count = %d, want 3", len(ids))
	}
}

// ── Layout Map Tests ────────────────────────────────────────────────────

func TestLayoutMap2H(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)

	lines := g.LayoutMap()
	if len(lines) != 3 {
		t.Fatalf("LayoutMap should return 3 lines, got %d", len(lines))
	}
	// Should have vertical divider
	if !strings.Contains(lines[0], "┬") {
		t.Errorf("top line should have ┬: %q", lines[0])
	}
	if !strings.Contains(lines[2], "┴") {
		t.Errorf("bottom line should have ┴: %q", lines[2])
	}
	t.Logf("2H:\n%s", strings.Join(lines, "\n"))
}

func TestLayoutMap2V(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitV)

	lines := g.LayoutMap()
	if len(lines) != 3 {
		t.Fatalf("LayoutMap should return 3 lines, got %d", len(lines))
	}
	// Should have horizontal divider
	if !strings.Contains(lines[1], "├") || !strings.Contains(lines[1], "┤") {
		t.Errorf("middle line should have ├ and ┤: %q", lines[1])
	}
	t.Logf("2V:\n%s", strings.Join(lines, "\n"))
}

func TestLayoutMapMixed3(t *testing.T) {
	// S1 | S2 with S2 split into S2/S3 vertically
	// Result: S1 full left, S2 top-right, S3 bottom-right
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 2, SplitV)

	lines := g.LayoutMap()
	if len(lines) != 3 {
		t.Fatalf("LayoutMap should return 3 lines, got %d", len(lines))
	}
	t.Logf("Mixed 3 (1 left + 2 right stacked):\n%s", strings.Join(lines, "\n"))
}

func TestLayoutMap4Grid(t *testing.T) {
	// S1 | S2, then S1 splits to S1/S3, S2 splits to S2/S4
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 1, SplitV)
	g.Add(mock_session(4, "S4"), 2, SplitV)

	lines := g.LayoutMap()
	if len(lines) != 3 {
		t.Fatalf("LayoutMap should return 3 lines, got %d", len(lines))
	}
	// Should have cross junction
	if !strings.Contains(lines[1], "┼") {
		t.Errorf("middle line should have ┼ for 2x2 grid: %q", lines[1])
	}
	t.Logf("4-pane 2x2 grid:\n%s", strings.Join(lines, "\n"))
}
