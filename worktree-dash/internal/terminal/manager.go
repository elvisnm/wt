package terminal

import (
	"fmt"
	"strings"
	"sync"

	"github.com/elvisnm/wt/internal/labels"
)

// Manager handles multiple tmux-backed terminal sessions organized in tab groups.
// Each tab group contains 1+ sessions arranged in a split layout.
// Integrates with PaneLayout to swap the active group into the right viewport.
type Manager struct {
	groups       []*TabGroup
	active_tab   int
	next_id      int // session IDs
	next_group_id int
	server       *TmuxServer
	panes        *PaneLayout
	mu           sync.Mutex
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
					var old_win string
					if old_idx >= 0 && old_idx < len(mgr.groups) {
						old_win = mgr.groups[old_idx].Primary().Window()
					}
					mgr.panes.SwitchTab(old_win, g.Primary().Window())
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
		var old_win string
		if old_idx >= 0 && old_idx < len(mgr.groups) {
			old_win = mgr.groups[old_idx].Primary().Window()
		}
		mgr.panes.SwitchTab(old_win, mgr.groups[idx].Primary().Window())
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
		mgr.panes.SwitchTab(
			mgr.groups[old_tab].Primary().Window(),
			mgr.groups[mgr.active_tab].Primary().Window(),
		)
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
		mgr.panes.SwitchTab(
			mgr.groups[old_tab].Primary().Window(),
			mgr.groups[mgr.active_tab].Primary().Window(),
		)
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

	// Close all sessions in the group
	for _, s := range g.sessions {
		go s.Close()
	}

	mgr.groups = append(mgr.groups[:mgr.active_tab], mgr.groups[mgr.active_tab+1:]...)

	if mgr.active_tab >= len(mgr.groups) && mgr.active_tab > 0 {
		mgr.active_tab--
	}

	has_tabs := len(mgr.groups) > 0

	if has_tabs && pl != nil {
		pl.ShowSession(mgr.groups[mgr.active_tab].Primary().Window())
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
					pl.ShowSession(mgr.groups[mgr.active_tab].Primary().Window())
				}

				go s.Close()
			} else {
				// Multi-session group — remove just this session
				g.Remove(s.ID)
				go s.Close()
			}

			return
		}
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
func (mgr *Manager) TabLabels() []TabLabel {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	var result []TabLabel
	for i, g := range mgr.groups {
		is_active := i == mgr.active_tab
		sessions := g.Sessions()
		layout_map := g.LayoutMap()

		for j, s := range sessions {
			tl := TabLabel{
				Index:        i + 1,
				Label:        s.Label,
				Active:       is_active,
				Alive:        s.IsAlive(),
				SessionID:    s.ID,
				GroupID:      g.ID,
				IsGroupChild: j > 0,
				GroupSize:    len(sessions),
			}
			// Attach layout map to the last session in the group
			if layout_map != nil && j == len(sessions)-1 {
				tl.LayoutMap = layout_map
			}
			result = append(result, tl)
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
	IsGroupChild bool     // true for 2nd+ session in a group
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
