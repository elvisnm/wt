package terminal

import (
	"fmt"
	"strings"
	"sync"
)

// Manager handles multiple tmux-backed terminal sessions (tabs).
// Integrates with PaneLayout to swap the active session into the right viewport.
type Manager struct {
	sessions   []*Session
	active_tab int
	next_id    int
	server     *TmuxServer
	panes      *PaneLayout
	mu         sync.Mutex
}

// NewManager creates a Manager that owns its own TmuxServer.
// Used when running as a standalone process (not inner mode).
func NewManager() *Manager {
	return &Manager{
		server:  NewTmuxServer(),
		next_id: 1, // Start at 1; id 0 is reserved for preview sessions
	}
}

// NewManagerWithServer creates a Manager using an existing TmuxServer.
// Used in inner mode where the outer process owns the server.
func NewManagerWithServer(server *TmuxServer) *Manager {
	return &Manager{
		server:  server,
		next_id: 1, // Start at 1; id 0 is reserved for preview sessions
	}
}

// SetPaneLayout attaches a PaneLayout for swap-pane integration.
// Must be called before opening sessions.
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

// Server returns the underlying TmuxServer for direct use.
func (mgr *Manager) Server() *TmuxServer {
	return mgr.server
}

// Open creates a new session and makes it active.
// If a live session with the same label already exists, it focuses that tab instead.
// When a PaneLayout is attached, swaps the session into the right viewport.
func (mgr *Manager) Open(label string, cmd_name string, args []string, width, height int, dir string) (*Session, error) {
	if s := mgr.FocusByLabel(label); s != nil {
		return s, nil
	}

	mgr.mu.Lock()
	id := mgr.next_id
	mgr.next_id++
	mgr.mu.Unlock()

	s, err := NewSession(id, label, cmd_name, args, width, height, dir, mgr.server)
	if err != nil {
		return nil, err
	}

	mgr.mu.Lock()
	mgr.sessions = append(mgr.sessions, s)
	mgr.active_tab = len(mgr.sessions) - 1
	pl := mgr.panes
	mgr.mu.Unlock()

	if pl != nil {
		pl.ShowSession(s.Window())
	}

	return s, nil
}

// OpenNew always creates a new session, even if one with a similar label exists.
// Appends a number (#2, #3, ...) if a live session with the same base label exists.
// When a PaneLayout is attached, swaps the session into the right viewport.
func (mgr *Manager) OpenNew(label string, cmd_name string, args []string, width, height int, dir string) (*Session, error) {
	mgr.mu.Lock()
	count := 0
	for _, s := range mgr.sessions {
		if s.Label == label || (len(s.Label) > len(label) && s.Label[:len(label)] == label && s.Label[len(label):len(label)+2] == " #") {
			count++
		}
	}
	final_label := label
	if count > 0 {
		final_label = fmt.Sprintf("%s #%d", label, count+1)
	}
	id := mgr.next_id
	mgr.next_id++
	mgr.mu.Unlock()

	s, err := NewSession(id, final_label, cmd_name, args, width, height, dir, mgr.server)
	if err != nil {
		return nil, err
	}

	mgr.mu.Lock()
	mgr.sessions = append(mgr.sessions, s)
	mgr.active_tab = len(mgr.sessions) - 1
	pl := mgr.panes
	mgr.mu.Unlock()

	if pl != nil {
		pl.ShowSession(s.Window())
	}

	return s, nil
}

// FocusByLabel finds a live session by label and makes it active.
// Returns the session if found, nil otherwise.
// When a PaneLayout is attached, swaps the session into the right viewport.
func (mgr *Manager) FocusByLabel(label string) *Session {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for i, s := range mgr.sessions {
		if s.Label == label && s.IsAlive() {
			old_idx := mgr.active_tab
			mgr.active_tab = i

			if mgr.panes != nil && old_idx != i {
				var old_win string
				if old_idx >= 0 && old_idx < len(mgr.sessions) {
					old_win = mgr.sessions[old_idx].Window()
				}
				mgr.panes.SwitchTab(old_win, s.Window())
			}

			return s
		}
	}
	return nil
}

// FocusByIndex makes the session at the given index active.
// Returns the session if found, nil otherwise.
// When a PaneLayout is attached, swaps the session into the right viewport.
func (mgr *Manager) FocusByIndex(idx int) *Session {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if idx < 0 || idx >= len(mgr.sessions) {
		return nil
	}

	old_idx := mgr.active_tab
	mgr.active_tab = idx

	if mgr.panes != nil && old_idx != idx {
		var old_win string
		if old_idx >= 0 && old_idx < len(mgr.sessions) {
			old_win = mgr.sessions[old_idx].Window()
		}
		mgr.panes.SwitchTab(old_win, mgr.sessions[idx].Window())
	}

	return mgr.sessions[idx]
}

// HasLabel returns true if any session (alive or dead) exists with this label
func (mgr *Manager) HasLabel(label string) bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for _, s := range mgr.sessions {
		if s.Label == label {
			return true
		}
	}
	return false
}

// IsLabelAlive returns true if a session with this label exists and is still running
func (mgr *Manager) IsLabelAlive(label string) bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for _, s := range mgr.sessions {
		if s.Label == label {
			return s.IsAlive()
		}
	}
	return false
}

// ExitCodeForLabel returns the exit code of a finished session, or -1 if not found/still alive
func (mgr *Manager) ExitCodeForLabel(label string) int {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for _, s := range mgr.sessions {
		if s.Label == label {
			s.mu.Lock()
			code := s.ExitCode
			s.mu.Unlock()
			return code
		}
	}
	return -1
}

// Active returns the currently active session, or nil if none
func (mgr *Manager) Active() *Session {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if len(mgr.sessions) == 0 {
		return nil
	}
	if mgr.active_tab < 0 || mgr.active_tab >= len(mgr.sessions) {
		return nil
	}
	return mgr.sessions[mgr.active_tab]
}

// ActiveIndex returns the active tab index
func (mgr *Manager) ActiveIndex() int {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return mgr.active_tab
}

// Sessions returns all current sessions
func (mgr *Manager) Sessions() []*Session {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	result := make([]*Session, len(mgr.sessions))
	copy(result, mgr.sessions)
	return result
}

// Count returns the number of sessions
func (mgr *Manager) Count() int {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return len(mgr.sessions)
}

// NextTab switches to the next tab.
// When a PaneLayout is attached, swaps the visible session.
func (mgr *Manager) NextTab() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.sessions) <= 1 {
		return
	}
	old_tab := mgr.active_tab
	mgr.active_tab = (mgr.active_tab + 1) % len(mgr.sessions)

	if mgr.panes != nil {
		mgr.panes.SwitchTab(
			mgr.sessions[old_tab].Window(),
			mgr.sessions[mgr.active_tab].Window(),
		)
	}
}

// PrevTab switches to the previous tab.
// When a PaneLayout is attached, swaps the visible session.
func (mgr *Manager) PrevTab() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.sessions) <= 1 {
		return
	}
	old_tab := mgr.active_tab
	mgr.active_tab = (mgr.active_tab - 1 + len(mgr.sessions)) % len(mgr.sessions)

	if mgr.panes != nil {
		mgr.panes.SwitchTab(
			mgr.sessions[old_tab].Window(),
			mgr.sessions[mgr.active_tab].Window(),
		)
	}
}

// CloseActive closes the current tab and returns whether any tabs remain.
// When a PaneLayout is attached, returns the pane and shows the next session.
func (mgr *Manager) CloseActive() bool {
	mgr.mu.Lock()

	if len(mgr.sessions) == 0 {
		mgr.mu.Unlock()
		return false
	}

	s := mgr.sessions[mgr.active_tab]
	pl := mgr.panes

	// Return the session pane from the viewport before closing
	if pl != nil {
		pl.ReturnSession()
	}

	go s.Close()

	mgr.sessions = append(mgr.sessions[:mgr.active_tab], mgr.sessions[mgr.active_tab+1:]...)

	if mgr.active_tab >= len(mgr.sessions) && mgr.active_tab > 0 {
		mgr.active_tab--
	}

	has_tabs := len(mgr.sessions) > 0

	// Show the new active session in the viewport
	if has_tabs && pl != nil {
		pl.ShowSession(mgr.sessions[mgr.active_tab].Window())
	}

	mgr.mu.Unlock()
	return has_tabs
}

// CloseByLabel closes the first session matching the given label.
// When a PaneLayout is attached, handles viewport swapping.
func (mgr *Manager) CloseByLabel(label string) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for i, s := range mgr.sessions {
		if s.Label == label {
			pl := mgr.panes
			was_active := (i == mgr.active_tab)

			// If this session is currently displayed, return it first
			if pl != nil && was_active {
				pl.ReturnSession()
			}

			mgr.sessions = append(mgr.sessions[:i], mgr.sessions[i+1:]...)
			if mgr.active_tab >= len(mgr.sessions) && mgr.active_tab > 0 {
				mgr.active_tab--
			}

			// Show the new active session if the closed one was visible
			if len(mgr.sessions) > 0 && pl != nil && was_active {
				pl.ShowSession(mgr.sessions[mgr.active_tab].Window())
			}

			// Kill the tmux window after swap-pane completes to avoid races
			go s.Close()

			return
		}
	}
}

// CloseDeadLogs closes any dead sessions whose labels start with "Logs".
// Returns true if any sessions were closed.
func (mgr *Manager) CloseDeadLogs() bool {
	// Collect dead log labels outside the lock, then use CloseByLabel.
	mgr.mu.Lock()
	var dead_labels []string
	for _, s := range mgr.sessions {
		if !s.IsAlive() && strings.HasPrefix(s.Label, "Logs") {
			dead_labels = append(dead_labels, s.Label)
		}
	}
	mgr.mu.Unlock()

	for _, label := range dead_labels {
		mgr.CloseByLabel(label)
	}
	return len(dead_labels) > 0
}

// CloseAll closes all sessions and kills the tmux server.
func (mgr *Manager) CloseAll() {
	mgr.mu.Lock()
	sessions := make([]*Session, len(mgr.sessions))
	copy(sessions, mgr.sessions)
	mgr.sessions = nil
	mgr.active_tab = 0
	mgr.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}

	mgr.server.Kill()
}

// HasLiveSessions checks if any sessions are still running
func (mgr *Manager) HasLiveSessions() bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	for _, s := range mgr.sessions {
		if s.IsAlive() {
			return true
		}
	}
	return false
}

// TabLabels returns the labels for the tab bar
func (mgr *Manager) TabLabels() []TabLabel {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	labels := make([]TabLabel, len(mgr.sessions))
	for i, s := range mgr.sessions {
		labels[i] = TabLabel{
			Index:  i + 1,
			Label:  s.Label,
			Active: i == mgr.active_tab,
			Alive:  s.IsAlive(),
		}
	}
	return labels
}

// TabLabel holds display info for a tab
type TabLabel struct {
	Index  int
	Label  string
	Active bool
	Alive  bool
}

func (t TabLabel) String() string {
	marker := ""
	if !t.Alive {
		marker = " (exited)"
	}
	return fmt.Sprintf("%d: %s%s", t.Index, t.Label, marker)
}
