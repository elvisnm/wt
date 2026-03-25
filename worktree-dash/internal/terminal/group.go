package terminal

// MaxGroupPanes is the maximum number of panes allowed in a single tab group.
const MaxGroupPanes = 4

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

// LayoutMap renders a compact box-drawing representation of the split layout.
// Returns 3 lines. Only meaningful for groups with 2+ sessions.
func (g *TabGroup) LayoutMap() []string {
	if !g.IsSplit() {
		return nil
	}
	return render_layout_map(g.tree, -1)
}

// LayoutMapHighlighted renders the layout map with one session highlighted.
// The highlighted session's cells are filled with orange blocks.
func (g *TabGroup) LayoutMapHighlighted(highlight_id int) []string {
	if !g.IsSplit() {
		return nil
	}
	return render_layout_map(g.tree, highlight_id)
}

// render_layout_map generates a 2D grid from the split tree, then draws box chars.
// If highlight_id >= 0, that session's cells are filled with orange blocks.
func render_layout_map(root *SplitNode, highlight_id int) []string {
	type rect struct{ x, y, w, h int }
	grid := [2][4]int{}

	var assign func(n *SplitNode, r rect)
	assign = func(n *SplitNode, r rect) {
		if n == nil || r.w <= 0 || r.h <= 0 {
			return
		}
		if n.is_leaf() {
			for row := r.y; row < r.y+r.h && row < 2; row++ {
				for col := r.x; col < r.x+r.w && col < 4; col++ {
					grid[row][col] = n.SessionID
				}
			}
			return
		}
		if n.Dir == SplitH {
			mid := r.w / 2
			if mid < 1 {
				mid = 1
			}
			assign(n.Left, rect{r.x, r.y, mid, r.h})
			assign(n.Right, rect{r.x + mid, r.y, r.w - mid, r.h})
		} else {
			mid := r.h / 2
			if mid < 1 {
				mid = 1
			}
			assign(n.Left, rect{r.x, r.y, r.w, mid})
			assign(n.Right, rect{r.x, r.y + mid, r.w, r.h - mid})
		}
	}
	assign(root, rect{0, 0, 4, 2})

	return grid_to_box(grid, highlight_id)
}

// ANSI color codes for layout map highlighting
const (
	mapOrange = "\033[38;5;214m"
	mapDim    = "\033[38;5;240m"
	mapReset  = "\033[0m"
)

// grid_to_box converts a 2×4 grid into 3 lines of box-drawing characters.
// Borders appear where adjacent cells have different session IDs.
// If highlight_id >= 0, borders touching that session's cells are drawn in orange.
func grid_to_box(grid [2][4]int, highlight_id int) []string {
	cols := 4

	v_div := [2][3]bool{}
	for r := 0; r < 2; r++ {
		for c := 0; c < cols-1; c++ {
			v_div[r][c] = grid[r][c] != grid[r][c+1]
		}
	}
	h_div := [4]bool{}
	for c := 0; c < cols; c++ {
		h_div[c] = grid[0][c] != grid[1][c]
	}

	hi := func(id int) bool { return highlight_id >= 0 && id == highlight_id }

	// Color a border char: orange if it touches a highlighted cell, dim grey otherwise
	bdr := func(ch string, touches_highlight bool) string {
		if touches_highlight {
			return mapOrange + ch + mapReset
		}
		return mapDim + ch + mapReset
	}

	// Top line
	top := bdr("┌", hi(grid[0][0]))
	for c := 0; c < cols-1; c++ {
		top += bdr("─", hi(grid[0][c]))
		if v_div[0][c] {
			top += bdr("┬", hi(grid[0][c]) || hi(grid[0][c+1]))
		} else {
			top += bdr("─", hi(grid[0][c]))
		}
	}
	top += bdr("─", hi(grid[0][cols-1]))
	top += bdr("┐", hi(grid[0][cols-1]))

	// Middle line
	mid := ""
	any_h := h_div[0] || h_div[1] || h_div[2] || h_div[3]
	if any_h {
		if h_div[0] {
			mid += bdr("├", hi(grid[0][0]) || hi(grid[1][0]))
		} else {
			mid += bdr("│", hi(grid[0][0]))
		}
		for c := 0; c < cols-1; c++ {
			if h_div[c] {
				mid += bdr("─", hi(grid[0][c]) || hi(grid[1][c]))
			} else {
				mid += " "
			}
			hl := h_div[c]
			hr := h_div[c+1]
			vt := v_div[0][c]
			vb := v_div[1][c]
			jn := box_junction(hl, hr, vt, vb)
			touches := hi(grid[0][c]) || hi(grid[0][c+1]) || hi(grid[1][c]) || hi(grid[1][c+1])
			mid += bdr(jn, touches)
		}
		if h_div[cols-1] {
			mid += bdr("─", hi(grid[0][cols-1]) || hi(grid[1][cols-1]))
			mid += bdr("┤", hi(grid[0][cols-1]) || hi(grid[1][cols-1]))
		} else {
			mid += " "
			mid += bdr("│", hi(grid[0][cols-1]))
		}
	} else {
		mid += bdr("│", hi(grid[0][0]))
		for c := 0; c < cols-1; c++ {
			mid += " "
			if v_div[0][c] || v_div[1][c] {
				mid += bdr("│", hi(grid[0][c]) || hi(grid[0][c+1]))
			} else {
				mid += " "
			}
		}
		mid += " "
		mid += bdr("│", hi(grid[0][cols-1]))
	}

	// Bottom line
	bot := bdr("└", hi(grid[1][0]))
	for c := 0; c < cols-1; c++ {
		bot += bdr("─", hi(grid[1][c]))
		if v_div[1][c] {
			bot += bdr("┴", hi(grid[1][c]) || hi(grid[1][c+1]))
		} else {
			bot += bdr("─", hi(grid[1][c]))
		}
	}
	bot += bdr("─", hi(grid[1][cols-1]))
	bot += bdr("┘", hi(grid[1][cols-1]))

	return []string{top, mid, bot}
}

// box_junction returns the correct box-drawing character for a junction point.
// h_left/h_right: horizontal line extends left/right.
// v_top/v_bot: vertical line extends up/down.
func box_junction(h_left, h_right, v_top, v_bot bool) string {
	switch {
	case h_left && h_right && v_top && v_bot:
		return "┼"
	case h_left && h_right && v_top:
		return "┴"
	case h_left && h_right && v_bot:
		return "┬"
	case h_left && h_right:
		return "─"
	case v_top && v_bot && h_right:
		return "├"
	case v_top && v_bot && h_left:
		return "┤"
	case v_top && v_bot:
		return "│"
	case h_right && v_bot:
		return "┌"
	case h_left && v_bot:
		return "┐"
	case h_right && v_top:
		return "└"
	case h_left && v_top:
		return "┘"
	case h_left || h_right:
		return "─"
	case v_top || v_bot:
		return "│"
	default:
		return " "
	}
}
