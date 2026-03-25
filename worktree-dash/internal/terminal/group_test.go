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
	// MaxGroupPanes is now 6 (absolute ceiling)
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 2, SplitV)
	g.Add(mock_session(4, "S4"), 1, SplitV)
	g.Add(mock_session(5, "S5"), 3, SplitH)
	g.Add(mock_session(6, "S6"), 5, SplitV)

	if g.Count() != 6 {
		t.Errorf("Count = %d, want 6", g.Count())
	}

	// 7th should fail (absolute ceiling)
	if g.Add(mock_session(7, "S7"), 4, SplitH) {
		t.Error("Add should fail at absolute ceiling (6)")
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

// ── Dot Map Tests ────────────────────────────────────────────────────────

// strip_ansi removes ANSI escape codes for test assertions.
func strip_ansi(s string) string {
	result := ""
	in_escape := false
	for _, r := range s {
		if r == '\033' {
			in_escape = true
			continue
		}
		if in_escape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				in_escape = false
			}
			continue
		}
		result += string(r)
	}
	return result
}

// count_dots counts the ● characters in a string (ignoring ANSI codes).
func count_dots(s string) int {
	return strings.Count(strip_ansi(s), "\u25cf")
}

func TestDotMap2H(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)

	lines := g.LayoutMap()
	if len(lines) != 1 {
		t.Fatalf("2H: want 1 line, got %d", len(lines))
	}
	if count_dots(lines[0]) != 2 {
		t.Errorf("2H: want 2 dots, got %d in %q", count_dots(lines[0]), strip_ansi(lines[0]))
	}
	t.Logf("2H: %s", strip_ansi(lines[0]))
}

func TestDotMap2V(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitV)

	lines := g.LayoutMap()
	if len(lines) != 2 {
		t.Fatalf("2V: want 2 lines, got %d", len(lines))
	}
	for i, l := range lines {
		if count_dots(l) != 1 {
			t.Errorf("2V line %d: want 1 dot, got %d", i, count_dots(l))
		}
	}
	t.Logf("2V:\n%s", strip_ansi(strings.Join(lines, "\n")))
}

func TestDotMap3H(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 2, SplitH)

	lines := g.LayoutMap()
	if len(lines) != 1 {
		t.Fatalf("3H: want 1 line, got %d", len(lines))
	}
	if count_dots(lines[0]) != 3 {
		t.Errorf("3H: want 3 dots, got %d", count_dots(lines[0]))
	}
	t.Logf("3H: %s", strip_ansi(lines[0]))
}

func TestDotMap3V(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitV)
	g.Add(mock_session(3, "S3"), 2, SplitV)

	lines := g.LayoutMap()
	if len(lines) != 3 {
		t.Fatalf("3V: want 3 lines, got %d", len(lines))
	}
	t.Logf("3V:\n%s", strip_ansi(strings.Join(lines, "\n")))
}

func TestDotMap6H(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 2, SplitH)
	g.Add(mock_session(4, "S4"), 3, SplitH)
	g.Add(mock_session(5, "S5"), 4, SplitH)
	g.Add(mock_session(6, "S6"), 5, SplitH)

	lines := g.LayoutMap()
	if len(lines) != 1 {
		t.Fatalf("6H: want 1 line, got %d", len(lines))
	}
	if count_dots(lines[0]) != 6 {
		t.Errorf("6H: want 6 dots, got %d", count_dots(lines[0]))
	}
	t.Logf("6H: %s", strip_ansi(lines[0]))
}

func TestDotMapMixed_HV(t *testing.T) {
	// H(1, V(2, 3)): 2 columns, right has 2 rows → 2 lines
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 2, SplitV)

	lines := g.LayoutMap()
	if len(lines) != 2 {
		t.Fatalf("H(1,V(2,3)): want 2 lines, got %d", len(lines))
	}
	if count_dots(lines[0]) != 2 {
		t.Errorf("line 0: want 2 dots, got %d", count_dots(lines[0]))
	}
	if count_dots(lines[1]) != 1 {
		t.Errorf("line 1: want 1 dot, got %d", count_dots(lines[1]))
	}
	t.Logf("H(1,V(2,3)):\n%s", strip_ansi(strings.Join(lines, "\n")))
}

func TestDotMapMixed_VH(t *testing.T) {
	// V(H(1, 3), 2): top row has 2 columns, bottom has 1 → 2 lines
	// This is the nested H-within-V case that used to lose session 3.
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitV)
	g.Add(mock_session(3, "S3"), 1, SplitH)

	lines := g.LayoutMap()
	if len(lines) != 2 {
		t.Fatalf("V(H(1,3),2): want 2 lines, got %d", len(lines))
	}
	// Row 0: 2 dots (pane 1, pane 3). Row 1: 1 dot (pane 2)
	if count_dots(lines[0]) != 2 {
		t.Errorf("line 0: want 2 dots, got %d", count_dots(lines[0]))
	}
	if count_dots(lines[1]) != 1 {
		t.Errorf("line 1: want 1 dot, got %d", count_dots(lines[1]))
	}
	t.Logf("V(H(1,3),2):\n%s", strip_ansi(strings.Join(lines, "\n")))
}

func TestDotMap3x2Grid(t *testing.T) {
	// 3 columns × 2 rows
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 2, SplitH)
	g.Add(mock_session(4, "S4"), 1, SplitV)
	g.Add(mock_session(5, "S5"), 2, SplitV)
	g.Add(mock_session(6, "S6"), 3, SplitV)

	lines := g.LayoutMap()
	if len(lines) != 2 {
		t.Fatalf("3x2: want 2 lines, got %d", len(lines))
	}
	for i, l := range lines {
		if count_dots(l) != 3 {
			t.Errorf("3x2 line %d: want 3 dots, got %d", i, count_dots(l))
		}
	}
	t.Logf("3x2:\n%s", strip_ansi(strings.Join(lines, "\n")))
}

func TestDotMapHighlight(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 2, SplitV)

	// Highlight session 1 — appears only in row 0 (spans full height, dot in first row)
	lines := g.LayoutMapHighlighted(1)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], dotOrange) {
		t.Error("line 0 should have orange highlight for session 1")
	}
	t.Logf("Highlighted(1):\n%s", strings.Join(lines, "\n"))

	// Highlight session 3 — appears only in row 1
	lines = g.LayoutMapHighlighted(3)
	if !strings.Contains(lines[1], dotOrange) {
		t.Error("line 1 should have orange highlight for session 3")
	}
	t.Logf("Highlighted(3):\n%s", strings.Join(lines, "\n"))
}

func TestDotMap2x2Grid(t *testing.T) {
	g := NewTabGroup(1, mock_session(1, "S1"))
	g.Add(mock_session(2, "S2"), 1, SplitH)
	g.Add(mock_session(3, "S3"), 1, SplitV)
	g.Add(mock_session(4, "S4"), 2, SplitV)

	lines := g.LayoutMap()
	if len(lines) != 2 {
		t.Fatalf("2x2: want 2 lines, got %d", len(lines))
	}
	for i, l := range lines {
		if count_dots(l) != 2 {
			t.Errorf("2x2 line %d: want 2 dots, got %d", i, count_dots(l))
		}
	}
	t.Logf("2x2:\n%s", strip_ansi(strings.Join(lines, "\n")))
}

func TestCountVLeaves(t *testing.T) {
	// Single leaf
	if v := count_v_leaves(&SplitNode{SessionID: 1}); v != 1 {
		t.Errorf("single leaf: got %d, want 1", v)
	}
	// V(1, 2) = 2 rows
	tree := &SplitNode{SessionID: -1, Dir: SplitV,
		Left: &SplitNode{SessionID: 1}, Right: &SplitNode{SessionID: 2}}
	if v := count_v_leaves(tree); v != 2 {
		t.Errorf("V(1,2): got %d, want 2", v)
	}
	// H(V(1,2), 3) = still 1 (H doesn't add rows)
	htree := &SplitNode{SessionID: -1, Dir: SplitH, Left: tree, Right: &SplitNode{SessionID: 3}}
	if v := count_v_leaves(htree); v != 1 {
		t.Errorf("H(V(1,2), 3): got %d, want 1", v)
	}
}
