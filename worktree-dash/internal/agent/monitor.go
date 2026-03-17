// Package agent monitors Claude Code terminal sessions to detect idle state.
// A Monitor watches tmux panes running Claude and detects when the agent
// is waiting for user input (idle prompt visible, content stopped changing).
package agent

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Status represents the detected state of a Claude session.
type Status int

const (
	StatusUnknown  Status = iota
	StatusWorking         // content is changing — agent is active
	StatusIdle            // prompt detected + content stable — waiting for input
	StatusFinished        // empty prompt — task complete, ready for next
)

// IdleEvent is sent when a Claude session transitions state.
type IdleEvent struct {
	Label    string // tab label (e.g., "claude:e2e")
	Alias    string // worktree alias
	Idle     bool   // true = became idle/finished, false = resumed working
	Finished bool   // true = task complete (empty prompt), false = waiting for input
}

// CaptureFunc captures the content of a tmux pane. Injected by the caller
// so the monitor doesn't depend on the terminal package directly.
type CaptureFunc func(pane_id string) (string, error)

// watcher tracks one Claude session.
type watcher struct {
	label        string
	alias        string
	pane_id      string
	last_hash    string
	idle_hash    string    // hash when we last notified idle — skip if same
	stable_at    time.Time // when content last changed
	change_count int       // consecutive polls with different content
	status       Status
	stop         chan struct{}
}

// Monitor manages watchers for Claude terminal sessions.
type Monitor struct {
	capture  CaptureFunc
	watchers map[string]*watcher // keyed by tab label
	events   chan IdleEvent
	mu       sync.Mutex

	// How long content must be unchanged before checking for idle prompt.
	stable_threshold time.Duration
	// How often to poll pane content.
	poll_interval time.Duration
}

// NewMonitor creates a Monitor that uses the given capture function.
func NewMonitor(capture CaptureFunc) *Monitor {
	return &Monitor{
		capture:          capture,
		watchers:         make(map[string]*watcher),
		events:           make(chan IdleEvent, 16),
		stable_threshold: 5 * time.Second,
		poll_interval:    3 * time.Second,
	}
}

// Events returns the channel that receives idle/resume events.
func (m *Monitor) Events() <-chan IdleEvent {
	return m.events
}

// Watch starts monitoring a Claude session. If already watching this label,
// the pane_id is updated (in case the session was recreated).
func (m *Monitor) Watch(label, alias, pane_id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if w, ok := m.watchers[label]; ok {
		// Update pane ID if session was recreated
		w.pane_id = pane_id
		return
	}

	w := &watcher{
		label:   label,
		alias:   alias,
		pane_id: pane_id,
		status:  StatusUnknown,
		stop:    make(chan struct{}),
	}
	m.watchers[label] = w
	go m.poll_loop(w)
}

// Unwatch stops monitoring a Claude session.
func (m *Monitor) Unwatch(label string) {
	m.mu.Lock()
	w, ok := m.watchers[label]
	if ok {
		delete(m.watchers, label)
	}
	m.mu.Unlock()

	if ok {
		close(w.stop)
	}
}

// UnwatchAll stops all watchers.
func (m *Monitor) UnwatchAll() {
	m.mu.Lock()
	watchers := make([]*watcher, 0, len(m.watchers))
	for _, w := range m.watchers {
		watchers = append(watchers, w)
	}
	m.watchers = make(map[string]*watcher)
	m.mu.Unlock()

	for _, w := range watchers {
		close(w.stop)
	}
}

// poll_loop periodically captures pane content and detects idle state.
func (m *Monitor) poll_loop(w *watcher) {
	ticker := time.NewTicker(m.poll_interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			m.check(w)
		}
	}
}

// check captures the pane and evaluates the agent's state.
func (m *Monitor) check(w *watcher) {
	content, err := m.capture(w.pane_id)
	if err != nil {
		return // pane might be gone
	}

	hash := content_hash(content)
	now := time.Now()

	if hash != w.last_hash {
		// Content changed
		w.last_hash = hash
		w.stable_at = now
		w.change_count++
		// Only transition to "working" after 3+ consecutive meaningful
		// content changes to avoid flapping on tab switches / cursor blinks
		if w.status != StatusWorking && w.change_count >= 3 {
			w.status = StatusWorking
			m.emit_resumed(w)
		}
		return
	}

	// Content unchanged — reset change counter
	w.change_count = 0

	if now.Sub(w.stable_at) < m.stable_threshold {
		return
	}

	// Content stable — check for idle prompt.
	// Only notify if the agent was previously working AND the content
	// is different from the last idle state (prevents re-notification
	// when tab switching causes brief "working" → back to same idle).
	result := detect_idle_prompt(content)
	if result != not_idle && w.status == StatusWorking {
		if hash == w.idle_hash {
			// Same content as last idle — just a tab switch, not real work
			w.status = StatusIdle
			return
		}
		w.idle_hash = hash
		finished := result == idle_finished
		if finished {
			w.status = StatusFinished
		} else {
			w.status = StatusIdle
		}
		m.emit_idle(w, finished)
	}
}

func (m *Monitor) emit_idle(w *watcher, finished bool) {
	select {
	case m.events <- IdleEvent{Label: w.label, Alias: w.alias, Idle: true, Finished: finished}:
	default:
	}
}

func (m *Monitor) emit_resumed(w *watcher) {
	select {
	case m.events <- IdleEvent{Label: w.label, Alias: w.alias, Idle: false}:
	default:
	}
}

// content_hash returns a hash of the meaningful content, stripping cursor
// artifacts and normalizing whitespace so tab switching / cursor blinks
// don't register as content changes.
func content_hash(s string) string {
	// Strip trailing whitespace per line and collapse empty lines.
	// This removes cursor position differences and formatting noise.
	lines := strings.Split(s, "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	h := sha256.Sum256([]byte(strings.Join(normalized, "\n")))
	return fmt.Sprintf("%x", h[:8])
}

// idle_result describes the type of idle state detected.
type idle_result int

const (
	not_idle idle_result = iota
	idle_finished        // empty ❯ prompt — task complete
	idle_waiting         // tool approval, question, or other input needed
)

// detect_idle_prompt checks if the captured content indicates Claude Code is
// waiting for user input. Checks for the ❯ prompt character (empty = finished,
// with text = selector/approval) and the welcome screen indicator.
func detect_idle_prompt(content string) idle_result {
	lines := strings.Split(content, "\n")
	checked := 0

	has_empty_prompt := false // ❯ alone = task finished, ready for next
	has_selector := false     // ❯ with text = approval/question, needs input
	has_welcome := false      // welcome screen = no active task

	for i := len(lines) - 1; i >= 0 && checked < 15; i-- {
		line := strings.TrimRight(lines[i], " \t")
		if line == "" {
			continue
		}
		checked++
		trimmed := strings.TrimSpace(line)

		if trimmed == "❯" {
			has_empty_prompt = true
		} else if strings.Contains(line, "❯") {
			has_selector = true
		}
		if strings.Contains(line, "Press ? for all keybindings") {
			has_welcome = true
		}
	}

	if has_selector {
		return idle_waiting
	}
	if has_empty_prompt || has_welcome {
		return idle_finished
	}
	return not_idle
}
