package terminal

// MaxGroupPanes is the absolute ceiling for panes per group.
// The runtime limit comes from settings (2-6).
const MaxGroupPanes = 6

// SplitDir represents the direction of a split.
type SplitDir int

const (
	SplitH SplitDir = iota // side by side (vertical divider)
	SplitV                 // stacked (horizontal divider)
)

// SplitNode is a binary tree representing the pane layout within a TabGroup.
// Internal nodes have a direction (H or V) and two children.
// Leaf nodes hold a session ID.
type SplitNode struct {
	// Leaf: session ID (>= 0). Internal: -1.
	SessionID int
	Dir       SplitDir
	Left      *SplitNode
	Right     *SplitNode
}

// is_leaf returns true if this node represents a single pane (session).
func (n *SplitNode) is_leaf() bool {
	return n.Left == nil && n.Right == nil
}

// session_ids returns all session IDs in the tree (in-order traversal).
func (n *SplitNode) session_ids() []int {
	if n == nil {
		return nil
	}
	if n.is_leaf() {
		return []int{n.SessionID}
	}
	return append(n.Left.session_ids(), n.Right.session_ids()...)
}

// find_and_split replaces the leaf with target_id by a split node containing
// the target and the new session. Returns true if the target was found.
func (n *SplitNode) find_and_split(target_id, new_id int, dir SplitDir) bool {
	if n == nil {
		return false
	}
	if n.is_leaf() && n.SessionID == target_id {
		n.Left = &SplitNode{SessionID: target_id}
		n.Right = &SplitNode{SessionID: new_id}
		n.Dir = dir
		n.SessionID = -1
		return true
	}
	return n.Left.find_and_split(target_id, new_id, dir) ||
		n.Right.find_and_split(target_id, new_id, dir)
}

// remove_session removes a session from the tree by replacing its parent split
// with the sibling. Returns the new root.
func remove_session(root *SplitNode, session_id int) *SplitNode {
	if root == nil {
		return nil
	}
	if root.is_leaf() {
		if root.SessionID == session_id {
			return nil
		}
		return root
	}
	if root.Left != nil && root.Left.is_leaf() && root.Left.SessionID == session_id {
		return root.Right
	}
	if root.Right != nil && root.Right.is_leaf() && root.Right.SessionID == session_id {
		return root.Left
	}
	root.Left = remove_session(root.Left, session_id)
	root.Right = remove_session(root.Right, session_id)
	// Collapse degenerate internal nodes: if one child became nil after
	// deep removal, replace this node with the surviving child.
	if root.Left == nil {
		return root.Right
	}
	if root.Right == nil {
		return root.Left
	}
	return root
}

// ── TabGroup ────────────────────────────────────────────────────────────

// TabGroup represents a tab that may contain 1 or more split panes.
// Single-pane groups behave identically to the current session model.
type TabGroup struct {
	ID       int
	sessions []*Session
	tree     *SplitNode
}

// NewTabGroup creates a group with a single session.
func NewTabGroup(id int, s *Session) *TabGroup {
	return &TabGroup{
		ID:       id,
		sessions: []*Session{s},
		tree:     &SplitNode{SessionID: s.ID},
	}
}

// Primary returns the first session in the group (anchor for swap-pane).
func (g *TabGroup) Primary() *Session {
	if len(g.sessions) == 0 {
		return nil
	}
	return g.sessions[0]
}

// Sessions returns all sessions in the group.
func (g *TabGroup) Sessions() []*Session {
	return g.sessions
}

// Count returns the number of sessions in the group.
func (g *TabGroup) Count() int {
	return len(g.sessions)
}

// IsSplit returns true if the group has more than one pane.
func (g *TabGroup) IsSplit() bool {
	return len(g.sessions) > 1
}

// Tree returns the split tree root.
func (g *TabGroup) Tree() *SplitNode {
	return g.tree
}

// SessionByID finds a session in the group by its ID.
func (g *TabGroup) SessionByID(id int) *Session {
	for _, s := range g.sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// CountColumns returns the number of top-level columns in the layout.
// A single pane = 1 column. Each H split at the root level adds a column.
func (g *TabGroup) CountColumns() int {
	return count_h_leaves(g.tree)
}

// MaxRowsInAnyColumn returns the max V-depth (rows) in any column.
func (g *TabGroup) MaxRowsInAnyColumn() int {
	return max_v_depth(g.tree)
}

// count_h_leaves counts leaf columns: at H splits, sum left+right; at V splits or leaves, count as 1.
func count_h_leaves(n *SplitNode) int {
	if n == nil || n.is_leaf() {
		return 1
	}
	if n.Dir == SplitH {
		return count_h_leaves(n.Left) + count_h_leaves(n.Right)
	}
	// V split: still one column
	return 1
}

// max_v_depth returns the max number of rows in any column.
func max_v_depth(n *SplitNode) int {
	if n == nil || n.is_leaf() {
		return 1
	}
	if n.Dir == SplitV {
		return max_v_depth(n.Left) + max_v_depth(n.Right)
	}
	// H split: max of both sides
	l := max_v_depth(n.Left)
	r := max_v_depth(n.Right)
	if l > r {
		return l
	}
	return r
}

// count_v_leaves counts leaf rows: at V splits, sum left+right; at H splits or leaves, count as 1.
// This is the vertical analogue of count_h_leaves.
func count_v_leaves(n *SplitNode) int {
	if n == nil || n.is_leaf() {
		return 1
	}
	if n.Dir == SplitV {
		return count_v_leaves(n.Left) + count_v_leaves(n.Right)
	}
	// H split: still one row
	return 1
}

// Add adds a session to the group, splitting the target pane.
// Returns false if the group is at max capacity or the target is not found.
func (g *TabGroup) Add(s *Session, target_session_id int, dir SplitDir) bool {
	if len(g.sessions) >= MaxGroupPanes {
		return false
	}
	if !g.tree.find_and_split(target_session_id, s.ID, dir) {
		return false
	}
	g.sessions = append(g.sessions, s)
	return true
}

// Remove removes a session from the group by ID.
// Returns the removed session, or nil if not found.
func (g *TabGroup) Remove(session_id int) *Session {
	var removed *Session
	var remaining []*Session
	for _, s := range g.sessions {
		if s.ID == session_id {
			removed = s
		} else {
			remaining = append(remaining, s)
		}
	}
	if removed == nil {
		return nil
	}
	g.sessions = remaining
	g.tree = remove_session(g.tree, session_id)
	return removed
}

// Contains returns true if the group has a session with the given ID.
func (g *TabGroup) Contains(session_id int) bool {
	for _, s := range g.sessions {
		if s.ID == session_id {
			return true
		}
	}
	return false
}

// Label returns the display label for the group (primary session's label).
func (g *TabGroup) Label() string {
	if p := g.Primary(); p != nil {
		return p.Label
	}
	return ""
}

// ── Mini Layout Map ─────────────────────────────────────────────────────
//
// Compact dot grid showing the pane layout topology.
// Each pane = one dot. Gray ● for inactive, orange ● for the highlighted pane.
// Positioned at the bottom-right corner of the group's child list.
//
// Examples:
//   2 columns:        ● ●
//   3 rows:           ●  (3 lines)
//                     ●
//                     ●
//   3×2 grid:         ● ● ●
//                     ● ● ●
//   Mixed H(1,V(2,3)):  ● ●
//                       ● ●  (pane 1 spans both rows)

// LayoutMap renders a compact dot representation of the split layout.
func (g *TabGroup) LayoutMap() []string {
	if !g.IsSplit() {
		return nil
	}
	return render_dot_map(g.tree, -1)
}

// LayoutMapHighlighted renders the dot map with one session highlighted in orange.
func (g *TabGroup) LayoutMapHighlighted(highlight_id int) []string {
	if !g.IsSplit() {
		return nil
	}
	return render_dot_map(g.tree, highlight_id)
}

// ANSI color codes for dot map
const (
	dotOrange = "\033[38;5;214m"
	dotDim    = "\033[38;5;240m"
	dotReset  = "\033[0m"
)

// render_dot_map produces a compact dot grid from the split tree.
// Uses position-based layout: walks the tree recursively, assigning each leaf
// a (col, row) position based on count_h_leaves and count_v_leaves offsets.
// This correctly handles nested splits (e.g., V(H(1,2), 3) → 2 cols × 2 rows).
func render_dot_map(root *SplitNode, highlight_id int) []string {
	if root == nil || root.is_leaf() {
		return nil
	}

	// Assign (col, row) positions to each leaf session
	type dot_pos struct {
		session_id int
		col, row   int
	}
	var positions []dot_pos
	var assign func(n *SplitNode, col, row int)
	assign = func(n *SplitNode, col, row int) {
		if n == nil {
			return
		}
		if n.is_leaf() {
			positions = append(positions, dot_pos{n.SessionID, col, row})
			return
		}
		if n.Dir == SplitH {
			left_cols := count_h_leaves(n.Left)
			assign(n.Left, col, row)
			assign(n.Right, col+left_cols, row)
		} else {
			left_rows := count_v_leaves(n.Left)
			assign(n.Left, col, row)
			assign(n.Right, col, row+left_rows)
		}
	}
	assign(root, 0, 0)

	if len(positions) == 0 {
		return nil
	}

	// Find grid bounds
	max_col, max_row := 0, 0
	for _, p := range positions {
		if p.col > max_col {
			max_col = p.col
		}
		if p.row > max_row {
			max_row = p.row
		}
	}
	num_cols := max_col + 1
	num_rows := max_row + 1

	// Build sparse grid: grid[row][col] = session_id (0 = empty)
	grid := make([][]int, num_rows)
	for r := range grid {
		grid[r] = make([]int, num_cols)
	}
	for _, p := range positions {
		grid[p.row][p.col] = p.session_id
	}

	// Render dot grid
	dot := func(sid int) string {
		if highlight_id >= 0 && sid == highlight_id {
			return dotOrange + "\u25cf" + dotReset
		}
		return dotDim + "\u25cf" + dotReset
	}

	var lines []string
	for r := 0; r < num_rows; r++ {
		line := ""
		for c := 0; c < num_cols; c++ {
			if c > 0 {
				line += " "
			}
			if grid[r][c] > 0 {
				line += dot(grid[r][c])
			} else {
				line += " "
			}
		}
		lines = append(lines, line)
	}
	return lines
}

// flatten_h_children collects all children of consecutive H-splits into a flat list.
// H(A, H(B, H(C, D))) → [A, B, C, D]
func flatten_h_children(n *SplitNode) []*SplitNode {
	if n == nil || n.is_leaf() || n.Dir != SplitH {
		return []*SplitNode{n}
	}
	return append(flatten_h_children(n.Left), flatten_h_children(n.Right)...)
}

// flatten_v_children collects all children of consecutive V-splits into a flat list.
func flatten_v_children(n *SplitNode) []*SplitNode {
	if n == nil || n.is_leaf() || n.Dir != SplitV {
		return []*SplitNode{n}
	}
	return append(flatten_v_children(n.Left), flatten_v_children(n.Right)...)
}
