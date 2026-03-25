package terminal

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/elvisnm/wt/internal/labels"
)

// Manager handles multiple tmux-backed terminal sessions organized in tab groups.
// Each tab group contains 1+ sessions arranged in a split layout.
// Integrates with PaneLayout to swap the active group into the right viewport.
type Manager struct {
	groups        []*TabGroup
	active_tab    int
	next_id       int // session IDs
	next_group_id int
	server        *TmuxServer
	panes         *PaneLayout
	max_panes     int // max panes per group (from settings, default 4)
	mu            sync.Mutex
}

// NewManager creates a Manager that owns its own TmuxServer.
func NewManager() *Manager {
	return &Manager{
		server:  NewTmuxServer(),
		next_id: 1, // Start at 1; id 0 is reserved for preview sessions
	}
}

// NewManagerWithServer creates a Manager using an existing TmuxServer.
func NewManagerWithServer(server *TmuxServer) *Manager {
	return &Manager{
		server:  server,
		next_id: 1,
	}
}

// SetSplitLimits sets the max panes per group.
func (mgr *Manager) SetSplitLimits(max_panes int) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	mgr.max_panes = max_panes
}

// MaxPanes returns the configured max panes per group.
func (mgr *Manager) MaxPanes() int {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.max_panes <= 0 {
		return 4 // default
	}
	return mgr.max_panes
}

// SetPaneLayout attaches a PaneLayout for swap-pane integration.
func (mgr *Manager) SetPaneLayout(pl *PaneLayout) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	mgr.panes = pl
}

// PaneLayout returns the attached PaneLayout, or nil.
func (mgr *Manager) PaneLayout() *PaneLayout {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return mgr.panes
}

// Server returns the underlying TmuxServer.
func (mgr *Manager) Server() *TmuxServer {
	return mgr.server
}

// group_extras builds the GroupPane slice for a group's non-primary sessions.
// The join sequence reconstructs the layout by walking the split tree breadth-first:
// - The primary (leftmost leaf) is swap-paned into the viewport first
// - Joins are sorted by depth, with H-splits before V-splits at each level
// This ensures H-splits establish the column skeleton before V-splits fill in rows.
// Without this ordering, V-splits nested under H-splits produce wrong tmux layouts.
func group_extras(g *TabGroup) []GroupPane {
	if g == nil || g.Count() <= 1 {
		return nil
	}
	tree := g.Tree()
	if tree == nil {
		return nil
	}

	type joinOp struct {
		pane_id   string
		window    string
		dir       SplitDir
		target_id string
		depth     int
	}

	var ops []joinOp

	// Walk tree to collect all join operations with their depth
	var walk func(n *SplitNode, depth int)
	walk = func(n *SplitNode, depth int) {
		if n == nil || n.is_leaf() {
			return
		}
		left_first := first_leaf(n.Left)
		right_first := first_leaf(n.Right)
		if left_first >= 0 && right_first >= 0 {
			s := g.SessionByID(right_first)
			target_s := g.SessionByID(left_first)
			if s != nil && target_s != nil {
				ops = append(ops, joinOp{
					pane_id:   s.PaneID(),
					window:    s.Window(),
					dir:       n.Dir,
					target_id: target_s.PaneID(),
					depth:     depth,
				})
			}
		}
		walk(n.Left, depth+1)
		walk(n.Right, depth+1)
	}
	walk(tree, 0)

	// Sort: depth ascending, then H-splits before V-splits at same depth.
	// This ensures columns are established before rows are filled in.
	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].depth != ops[j].depth {
			return ops[i].depth < ops[j].depth
		}
		return ops[i].dir == SplitH && ops[j].dir == SplitV
	})

	extras := make([]GroupPane, len(ops))
	for i, op := range ops {
		extras[i] = GroupPane{
			PaneID:   op.pane_id,
			Window:   op.window,
			Dir:      op.dir,
			TargetID: op.target_id,
		}
	}
	return extras
}

// first_leaf returns the session ID of the leftmost leaf in a subtree.
func first_leaf(n *SplitNode) int {
	if n == nil {
		return -1
	}
	if n.is_leaf() {
		return n.SessionID
	}
	return first_leaf(n.Left)
}

// ── Open / Create ───────────────────────────────────────────────────────

// Open creates a new session in its own tab group and makes it active.
// If a live session with the same label already exists, focuses that tab instead.
func (mgr *Manager) Open(label string, cmd_name string, args []string, width, height int, dir string) (*Session, error) {
	if s := mgr.FocusByLabel(label); s != nil {
		return s, nil
	}

	mgr.mu.Lock()
	id := mgr.next_id
	mgr.next_id++
	gid := mgr.next_group_id
	mgr.next_group_id++
	mgr.mu.Unlock()

	s, err := NewSession(id, label, cmd_name, args, width, height, dir, mgr.server)
	if err != nil {
		return nil, err
	}

	g := NewTabGroup(gid, s)

	mgr.mu.Lock()
	mgr.groups = append(mgr.groups, g)
	mgr.active_tab = len(mgr.groups) - 1
	pl := mgr.panes
	mgr.mu.Unlock()

	if pl != nil {
		pl.ShowSession(s.Window())
	}

	return s, nil
}

// OpenNew always creates a new session in its own tab group.
// Appends a number (#2, #3, ...) if a live session with the same base label exists.
func (mgr *Manager) OpenNew(label string, cmd_name string, args []string, width, height int, dir string) (*Session, error) {
	mgr.mu.Lock()
	count := 0
	for _, g := range mgr.groups {
		for _, s := range g.sessions {
			if s.Label == label || (len(s.Label) > len(label) && s.Label[:len(label)] == label && s.Label[len(label):len(label)+2] == " #") {
				count++
			}
		}
	}
	final_label := label
	if count > 0 {
		final_label = fmt.Sprintf("%s #%d", label, count+1)
	}
	id := mgr.next_id
	mgr.next_id++
	gid := mgr.next_group_id
	mgr.next_group_id++
	mgr.mu.Unlock()

	s, err := NewSession(id, final_label, cmd_name, args, width, height, dir, mgr.server)
	if err != nil {
		return nil, err
	}

	g := NewTabGroup(gid, s)

	mgr.mu.Lock()
	mgr.groups = append(mgr.groups, g)
	mgr.active_tab = len(mgr.groups) - 1
	pl := mgr.panes
	mgr.mu.Unlock()

	if pl != nil {
		pl.ShowSession(s.Window())
	}

	return s, nil
}

// OpenNewSendKeys is like OpenNew but uses send-keys to type the command
// into an interactive shell instead of passing it via tmux new-window.
func (mgr *Manager) OpenNewSendKeys(label string, cmd_name string, args []string, width, height int, dir string) (*Session, error) {
	mgr.mu.Lock()
	count := 0
	for _, g := range mgr.groups {
		for _, s := range g.sessions {
			if s.Label == label || (len(s.Label) > len(label) && s.Label[:len(label)] == label && s.Label[len(label):len(label)+2] == " #") {
				count++
			}
		}
	}
	final_label := label
	if count > 0 {
		final_label = fmt.Sprintf("%s #%d", label, count+1)
	}
	id := mgr.next_id
	mgr.next_id++
	gid := mgr.next_group_id
	mgr.next_group_id++
	mgr.mu.Unlock()

	s, err := NewSessionSendKeys(id, final_label, cmd_name, args, width, height, dir, mgr.server)
	if err != nil {
		return nil, err
	}

	g := NewTabGroup(gid, s)

	mgr.mu.Lock()
	mgr.groups = append(mgr.groups, g)
	mgr.active_tab = len(mgr.groups) - 1
	pl := mgr.panes
	mgr.mu.Unlock()

	if pl != nil {
		pl.ShowSession(s.Window())
	}

	return s, nil
}

// equalize_active_locked equalizes the right-side panes if the active group
// is a split group. Caller must hold mgr.mu.
func (mgr *Manager) equalize_active_locked() {
	if mgr.panes == nil {
		return
	}
	if mgr.active_tab >= 0 && mgr.active_tab < len(mgr.groups) {
		g := mgr.groups[mgr.active_tab]
		if g.IsSplit() {
			mgr.panes.equalize_right_panes(g)
		}
	}
}

// ── Focus / Navigation ──────────────────────────────────────────────────

// FocusByLabel finds a live session by label across all groups and focuses its group.
func (mgr *Manager) FocusByLabel(label string) *Session {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for i, g := range mgr.groups {
		for _, s := range g.sessions {
			if s.Label == label && s.IsAlive() {
				old_idx := mgr.active_tab
				mgr.active_tab = i

				if mgr.panes != nil && old_idx != i {
					var from_win string
					var from_extras []GroupPane
					if old_idx >= 0 && old_idx < len(mgr.groups) {
						from_win = mgr.groups[old_idx].Primary().Window()
						from_extras = group_extras(mgr.groups[old_idx])
					}
					mgr.panes.SwitchGroup(from_win, from_extras, g.Primary().Window(), group_extras(g))
					mgr.equalize_active_locked()
				}

				return s
			}
		}
	}
	return nil
}

// FocusByIndex makes the group at the given index active.
func (mgr *Manager) FocusByIndex(idx int) *Session {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if idx < 0 || idx >= len(mgr.groups) {
		return nil
	}

	old_idx := mgr.active_tab
	mgr.active_tab = idx

	if mgr.panes != nil && old_idx != idx {
		var from_win string
		var from_extras []GroupPane
		if old_idx >= 0 && old_idx < len(mgr.groups) {
			from_win = mgr.groups[old_idx].Primary().Window()
			from_extras = group_extras(mgr.groups[old_idx])
		}
		mgr.panes.SwitchGroup(from_win, from_extras, mgr.groups[idx].Primary().Window(), group_extras(mgr.groups[idx]))
		mgr.equalize_active_locked()
	}

	return mgr.groups[idx].Primary()
}

// HasLabel returns true if any session (alive or dead) exists with this label.
func (mgr *Manager) HasLabel(label string) bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	for _, g := range mgr.groups {
		for _, s := range g.sessions {
			if s.Label == label {
				return true
			}
		}
	}
	return false
}

// IsLabelAlive returns true if a session with this label exists and is running.
func (mgr *Manager) IsLabelAlive(label string) bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	for _, g := range mgr.groups {
		for _, s := range g.sessions {
			if s.Label == label {
				return s.IsAlive()
			}
		}
	}
	return false
}

// Active returns the primary session of the active group, or nil.
func (mgr *Manager) Active() *Session {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.groups) == 0 || mgr.active_tab < 0 || mgr.active_tab >= len(mgr.groups) {
		return nil
	}
	return mgr.groups[mgr.active_tab].Primary()
}

// ActiveGroup returns the currently active tab group, or nil.
func (mgr *Manager) ActiveGroup() *TabGroup {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.groups) == 0 || mgr.active_tab < 0 || mgr.active_tab >= len(mgr.groups) {
		return nil
	}
	return mgr.groups[mgr.active_tab]
}

// ActiveIndex returns the active tab (group) index.
func (mgr *Manager) ActiveIndex() int {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return mgr.active_tab
}

// ActiveLabel returns the label of the active group's primary session.
func (mgr *Manager) ActiveLabel() string {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.active_tab >= 0 && mgr.active_tab < len(mgr.groups) {
		return mgr.groups[mgr.active_tab].Label()
	}
	return ""
}

// Sessions returns all sessions across all groups (flat list).
func (mgr *Manager) Sessions() []*Session {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	var result []*Session
	for _, g := range mgr.groups {
		result = append(result, g.sessions...)
	}
	return result
}

// Groups returns all tab groups.
func (mgr *Manager) Groups() []*TabGroup {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	result := make([]*TabGroup, len(mgr.groups))
	copy(result, mgr.groups)
	return result
}

// Count returns the number of tab groups (tabs).
func (mgr *Manager) Count() int {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return len(mgr.groups)
}

// ── Split ───────────────────────────────────────────────────────────────

// SplitInto creates a new session and adds it to the target session's group,
// splitting the target pane in the given direction. If the group is currently
// visible in the viewport, the new pane is joined immediately.
// Returns the new session, or an error if the group is at max capacity or
// the target session is not found.
func (mgr *Manager) SplitInto(target_session_id int, label, cmd_name string, args []string, width, height int, dir string, split_dir SplitDir) (*Session, error) {
	mgr.mu.Lock()

	// Find the group containing the target session
	var target_group *TabGroup
	var target_group_idx int
	for i, g := range mgr.groups {
		if g.Contains(target_session_id) {
			target_group = g
			target_group_idx = i
			break
		}
	}
	if target_group == nil {
		mgr.mu.Unlock()
		return nil, fmt.Errorf("session %d not found in any group", target_session_id)
	}
	max := mgr.max_panes
	if max <= 0 {
		max = 4
	}
	if target_group.Count() >= max {
		mgr.mu.Unlock()
		return nil, fmt.Errorf("max %d panes per group", max)
	}

	id := mgr.next_id
	mgr.next_id++
	is_active := (target_group_idx == mgr.active_tab)
	pl := mgr.panes

	mgr.mu.Unlock()

	s, err := NewSession(id, label, cmd_name, args, width, height, dir, mgr.server)
	if err != nil {
		return nil, err
	}

	mgr.mu.Lock()
	target_group.Add(s, target_session_id, split_dir)
	mgr.mu.Unlock()

	// If this group is currently visible, join the new pane into the viewport.
	// join-pane -s <new> -t <existing> places the new pane after (right/below) the existing one.
	if is_active && pl != nil {
		flag := "-v"
		if split_dir == SplitH {
			flag = "-h"
		}
		target_s := target_group.SessionByID(target_session_id)
		target_pane := pl.resolve_right_viewport()
		if target_s != nil && target_s.PaneID() != "" {
			target_pane = target_s.PaneID()
		}

		pl.server.Run("join-pane", flag,
			"-s", s.PaneID(),
			"-t", target_pane,
		)

		// Update stored extras so ReturnSession can break them back correctly
		pl.mu.Lock()
		pl.active_extras = group_extras(target_group)
		pl.mu.Unlock()

		// Equalize right-side panes by resizing each to equal share
		pl.equalize_right_panes(target_group)
		pl.restore_left_width()
		pl.server.Run("select-pane", "-t", pl.left_pane_id)
	}

	return s, nil
}

// MoveInto moves an existing standalone session into a target group.
// The source group (which must have exactly 1 session) is removed.
// Returns an error if source isn't standalone or target is at max capacity.
func (mgr *Manager) MoveInto(session_id, target_session_id int, split_dir SplitDir) error {
	mgr.mu.Lock()

	var source_group *TabGroup
	var source_idx int
	var target_group *TabGroup
	var target_idx int
	var session *Session

	for i, g := range mgr.groups {
		if g.Contains(session_id) {
			source_group = g
			source_idx = i
			session = g.SessionByID(session_id)
		}
		if g.Contains(target_session_id) {
			target_group = g
			target_idx = i
		}
	}

	if source_group == nil || target_group == nil || session == nil {
		mgr.mu.Unlock()
		return fmt.Errorf("session not found")
	}
	if source_group == target_group {
		mgr.mu.Unlock()
		return fmt.Errorf("session already in target group")
	}
	if source_group.Count() != 1 {
		mgr.mu.Unlock()
		return fmt.Errorf("can only move standalone sessions")
	}
	max := mgr.max_panes
	if max <= 0 {
		max = 4
	}
	if target_group.Count() >= max {
		mgr.mu.Unlock()
		return fmt.Errorf("max %d panes per group", max)
	}

	pl := mgr.panes
	was_source_active := (source_idx == mgr.active_tab)
	is_target_active := (target_idx == mgr.active_tab)

	// Return viewport if source is active
	if was_source_active && pl != nil {
		pl.ReturnSession()
	}

	// Remove source group
	mgr.groups = append(mgr.groups[:source_idx], mgr.groups[source_idx+1:]...)

	// Adjust active_tab and target_idx after removal
	if source_idx < mgr.active_tab {
		mgr.active_tab--
	}
	// Recalculate target_idx since we removed a group
	target_idx = -1
	for i, g := range mgr.groups {
		if g == target_group {
			target_idx = i
			break
		}
	}

	// Add session to target group
	target_group.Add(session, target_session_id, split_dir)

	// If target group is visible, rejoin with new pane
	// Recalculate is_target_active after index shift
	is_target_active = (target_idx == mgr.active_tab)
	if is_target_active && pl != nil {
		// Return current viewport and re-show with updated group
		pl.ReturnSession()
		pl.ShowGroup(target_group.Primary().Window(), group_extras(target_group))
		mgr.equalize_active_locked()
	}

	// If source was active and target is not, show target
	if was_source_active && !is_target_active && pl != nil {
		mgr.active_tab = target_idx
		pl.ShowGroup(target_group.Primary().Window(), group_extras(target_group))
		mgr.equalize_active_locked()
	}

	mgr.mu.Unlock()
	return nil
}

// ── Tab Switching ───────────────────────────────────────────────────────

// NextTab switches to the next tab group.
func (mgr *Manager) NextTab() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.groups) <= 1 {
		return
	}
	old_tab := mgr.active_tab
	mgr.active_tab = (mgr.active_tab + 1) % len(mgr.groups)

	if mgr.panes != nil {
		mgr.panes.SwitchGroup(
			mgr.groups[old_tab].Primary().Window(),
			group_extras(mgr.groups[old_tab]),
			mgr.groups[mgr.active_tab].Primary().Window(),
			group_extras(mgr.groups[mgr.active_tab]),
		)
		mgr.equalize_active_locked()
	}
}

// PrevTab switches to the previous tab group.
func (mgr *Manager) PrevTab() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.groups) <= 1 {
		return
	}
	old_tab := mgr.active_tab
	mgr.active_tab = (mgr.active_tab - 1 + len(mgr.groups)) % len(mgr.groups)

	if mgr.panes != nil {
		mgr.panes.SwitchGroup(
			mgr.groups[old_tab].Primary().Window(),
			group_extras(mgr.groups[old_tab]),
			mgr.groups[mgr.active_tab].Primary().Window(),
			group_extras(mgr.groups[mgr.active_tab]),
		)
		mgr.equalize_active_locked()
	}
}

// ── Close ───────────────────────────────────────────────────────────────

// CloseActive closes the current tab group and returns whether any tabs remain.
func (mgr *Manager) CloseActive() bool {
	mgr.mu.Lock()

	if len(mgr.groups) == 0 {
		mgr.mu.Unlock()
		return false
	}

	g := mgr.groups[mgr.active_tab]
	pl := mgr.panes

	if pl != nil {
		pl.ReturnSession()
	}

	for _, s := range g.sessions {
		go s.Close()
	}

	mgr.groups = append(mgr.groups[:mgr.active_tab], mgr.groups[mgr.active_tab+1:]...)

	if mgr.active_tab >= len(mgr.groups) && mgr.active_tab > 0 {
		mgr.active_tab--
	}

	has_tabs := len(mgr.groups) > 0

	if has_tabs && pl != nil {
		next := mgr.groups[mgr.active_tab]
		pl.ShowGroup(next.Primary().Window(), group_extras(next))
		mgr.equalize_active_locked()
	}

	mgr.mu.Unlock()
	return has_tabs
}

// CloseByLabel closes the first session matching the label.
// If the session is the only one in its group, the group is removed.
// If the session is part of a multi-session group, only that session is removed.
func (mgr *Manager) CloseByLabel(label string) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for i, g := range mgr.groups {
		for _, s := range g.sessions {
			if s.Label != label {
				continue
			}

			pl := mgr.panes
			was_active := (i == mgr.active_tab)

			if g.Count() == 1 {
				// Only session in group — remove the group
				if pl != nil && was_active {
					pl.ReturnSession()
				}

				mgr.groups = append(mgr.groups[:i], mgr.groups[i+1:]...)
				if mgr.active_tab >= len(mgr.groups) && mgr.active_tab > 0 {
					mgr.active_tab--
				}

				if len(mgr.groups) > 0 && pl != nil && was_active {
					next := mgr.groups[mgr.active_tab]
					pl.ShowGroup(next.Primary().Window(), group_extras(next))
					mgr.equalize_active_locked()
				}

				go s.Close()
			} else {
				// Multi-session group — kill just this pane in place
				if was_active && pl != nil && s.PaneID() != "" {
					pl.server.Run("kill-pane", "-t", s.PaneID())
				}

				g.Remove(s.ID)

				if was_active && pl != nil {
					pl.mu.Lock()
					pl.active_win = g.Primary().Window()
					pl.active_extras = group_extras(g)
					pl.mu.Unlock()
					pl.equalize_right_panes(g)
					pl.server.Run("select-pane", "-t", pl.left_pane_id)
				}

				if !was_active {
					go s.Close()
				}
			}

			return
		}
	}
}

// CloseBySessionID closes a specific session by its ID.
// If it's the only session in its group, the group is removed.
// If it's part of a multi-session group, only that pane is killed in place.
func (mgr *Manager) CloseBySessionID(session_id int) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for i, g := range mgr.groups {
		s := g.SessionByID(session_id)
		if s == nil {
			continue
		}

		pl := mgr.panes
		was_active := (i == mgr.active_tab)

		if g.Count() == 1 {
			// Last pane in group — remove the whole group.
			// Use ReturnSession to swap the pane back (preserves the right viewport placeholder).
			if pl != nil && was_active {
				pl.ReturnSession()
			}

			mgr.groups = append(mgr.groups[:i], mgr.groups[i+1:]...)
			if mgr.active_tab >= len(mgr.groups) && mgr.active_tab > 0 {
				mgr.active_tab--
			}

			if len(mgr.groups) > 0 && pl != nil && was_active {
				next := mgr.groups[mgr.active_tab]
				pl.ShowGroup(next.Primary().Window(), group_extras(next))
				mgr.equalize_active_locked()
			}

			go s.Close()
		} else {
			// Multi-pane group — kill just this pane directly in the viewport.
			// Tmux automatically resizes remaining panes to fill the space.
			if was_active && pl != nil && s.PaneID() != "" {
				pl.server.Run("kill-pane", "-t", s.PaneID())
			}

			g.Remove(s.ID)

			// Update PaneLayout state to reflect the new group composition
			if was_active && pl != nil {
				pl.mu.Lock()
				pl.active_win = g.Primary().Window()
				pl.active_extras = group_extras(g)
				pl.mu.Unlock()
				pl.equalize_right_panes(g)
				pl.server.Run("select-pane", "-t", pl.left_pane_id)
			}

			if !was_active {
				go s.Close()
			}
		}

		return
	}
}

// CloseByPrefix closes all sessions whose labels start with the given prefix.
func (mgr *Manager) CloseByPrefix(prefix string) {
	mgr.mu.Lock()
	var matched []string
	for _, g := range mgr.groups {
		for _, s := range g.sessions {
			if strings.HasPrefix(s.Label, prefix) {
				matched = append(matched, s.Label)
			}
		}
	}
	mgr.mu.Unlock()

	for _, label := range matched {
		mgr.CloseByLabel(label)
	}
}

// CloseDeadByPrefix closes any dead sessions whose labels match the prefix.
func (mgr *Manager) CloseDeadByPrefix(prefix string) bool {
	mgr.mu.Lock()
	var dead_labels []string
	for _, g := range mgr.groups {
		for _, s := range g.sessions {
			if !s.IsAlive() && strings.HasPrefix(s.Label, prefix) {
				dead_labels = append(dead_labels, s.Label)
			}
		}
	}
	mgr.mu.Unlock()

	for _, label := range dead_labels {
		mgr.CloseByLabel(label)
	}
	return len(dead_labels) > 0
}

// CloseDeadByPrefixIfClean closes dead sessions matching the prefix only if
// they exited with code 0. Returns true if any were closed.
func (mgr *Manager) CloseDeadByPrefixIfClean(prefix string) bool {
	mgr.mu.Lock()
	var clean_labels []string
	for _, g := range mgr.groups {
		for _, s := range g.sessions {
			if !s.IsAlive() && strings.HasPrefix(s.Label, prefix) {
				s.mu.Lock()
				code := s.ExitCode
				s.mu.Unlock()
				if code == 0 {
					clean_labels = append(clean_labels, s.Label)
				}
			}
		}
	}
	mgr.mu.Unlock()

	for _, label := range clean_labels {
		mgr.CloseByLabel(label)
	}
	return len(clean_labels) > 0
}

// CloseDeadLogs closes any dead sessions whose labels start with "Logs".
func (mgr *Manager) CloseDeadLogs() bool {
	return mgr.CloseDeadByPrefix(labels.Logs)
}

// CloseDeadByLabel closes a dead session with the exact label.
func (mgr *Manager) CloseDeadByLabel(label string) bool {
	mgr.mu.Lock()
	found := false
	for _, g := range mgr.groups {
		for _, s := range g.sessions {
			if s.Label == label && !s.IsAlive() {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	mgr.mu.Unlock()

	if found {
		mgr.CloseByLabel(label)
	}
	return found
}

// CloseAll closes all sessions and kills the tmux server.
func (mgr *Manager) CloseAll() {
	mgr.mu.Lock()
	var all_sessions []*Session
	for _, g := range mgr.groups {
		all_sessions = append(all_sessions, g.sessions...)
	}
	mgr.groups = nil
	mgr.active_tab = 0
	mgr.mu.Unlock()

	for _, s := range all_sessions {
		s.Close()
	}

	mgr.server.Kill()
}

// ── Query ───────────────────────────────────────────────────────────────

// HasLiveSessions checks if any sessions are still running.
func (mgr *Manager) HasLiveSessions() bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	for _, g := range mgr.groups {
		for _, s := range g.sessions {
			if s.IsAlive() {
				return true
			}
		}
	}
	return false
}

// TabLabels returns the labels for the tab bar, including group hierarchy.
// TabLabelsWithCursor returns tab labels with the layout map highlighting the
// session at flat_cursor position.
func (mgr *Manager) TabLabelsWithCursor(flat_cursor int) []TabLabel {
	return mgr.tab_labels_internal(flat_cursor)
}

func (mgr *Manager) TabLabels() []TabLabel {
	return mgr.tab_labels_internal(-1)
}

func (mgr *Manager) tab_labels_internal(flat_cursor int) []TabLabel {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// First pass: build flat list and find cursor's session ID + group
	var result []TabLabel
	var cursor_session_id int
	var cursor_group_id int

	flat_idx := 0
	for i, g := range mgr.groups {
		is_active := i == mgr.active_tab
		sessions := g.Sessions()

		if len(sessions) == 1 {
			s := sessions[0]
			if flat_idx == flat_cursor {
				cursor_session_id = s.ID
				cursor_group_id = g.ID
			}
			result = append(result, TabLabel{
				Index:     i + 1,
				Label:     s.Label,
				Active:    is_active,
				Alive:     s.IsAlive(),
				SessionID: s.ID,
				GroupID:   g.ID,
				GroupSize: 1,
			})
			flat_idx++
		} else {
			// Group header
			if flat_idx == flat_cursor {
				cursor_group_id = g.ID
			}
			result = append(result, TabLabel{
				Index:       i + 1,
				Label:       fmt.Sprintf("Group (%d panes)", len(sessions)),
				Active:      is_active,
				Alive:       true,
				GroupID:     g.ID,
				IsGroupHead: true,
				GroupSize:   len(sessions),
			})
			flat_idx++

			for _, s := range sessions {
				if flat_idx == flat_cursor {
					cursor_session_id = s.ID
					cursor_group_id = g.ID
				}
				result = append(result, TabLabel{
					Index:        i + 1,
					Label:        s.Label,
					Active:       is_active,
					Alive:        s.IsAlive(),
					SessionID:    s.ID,
					GroupID:      g.ID,
					IsGroupChild: true,
					GroupSize:    len(sessions),
				})
				flat_idx++
			}
		}
	}

	// Second pass: attach highlighted layout maps to the last child of each group
	for i, g := range mgr.groups {
		if !g.IsSplit() {
			continue
		}
		// Generate layout map — highlighted if cursor is in this group
		var layout_map []string
		if cursor_group_id == g.ID && flat_cursor >= 0 {
			layout_map = g.LayoutMapHighlighted(cursor_session_id)
		} else {
			layout_map = g.LayoutMap()
		}

		// Find the last entry for this group and attach the map
		_ = i
		for j := len(result) - 1; j >= 0; j-- {
			if result[j].GroupID == g.ID && result[j].IsGroupChild {
				result[j].LayoutMap = layout_map
				break
			}
		}
	}

	return result
}

// TabLabel holds display info for a tab entry.
type TabLabel struct {
	Index        int
	Label        string
	Active       bool
	Alive        bool
	SessionID    int
	GroupID      int
	IsGroupHead  bool     // true for the group header line (multi-session groups)
	IsGroupChild bool     // true for session entries within a group
	GroupSize    int      // total sessions in the group
	LayoutMap    []string // 3-line mini layout map (only on last child)
}

func (t TabLabel) String() string {
	marker := ""
	if !t.Alive {
		marker = " (exited)"
	}
	return fmt.Sprintf("%d: %s%s", t.Index, t.Label, marker)
}
