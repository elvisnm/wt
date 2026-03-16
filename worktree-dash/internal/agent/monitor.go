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
	StatusUnknown Status = iota
	StatusWorking        // content is changing — agent is active
	StatusIdle           // prompt detected + content stable — waiting for input
)

// IdleEvent is sent when a Claude session transitions to/from idle.
type IdleEvent struct {
	Label string // tab label (e.g., "claude:e2e")
	Alias string // worktree alias
	Idle  bool   // true = became idle, false = resumed working
}

// CaptureFunc captures the content of a tmux pane. Injected by the caller
// so the monitor doesn't depend on the terminal package directly.
type CaptureFunc func(pane_id string) (string, error)

// watcher tracks one Claude session.
type watcher struct {
	label      string
	alias      string
	pane_id    string
	last_hash  string
	stable_at  time.Time // when content last changed
	status     Status
	stop       chan struct{}
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
		stable_threshold: 10 * time.Second,
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
		// Content changed — agent is working
		w.last_hash = hash
		w.stable_at = now
		if w.status != StatusWorking {
			w.status = StatusWorking
			m.emit(w, false)
		}
		return
	}

	// Content unchanged — check if stable long enough
	if now.Sub(w.stable_at) < m.stable_threshold {
		return // not stable long enough yet
	}

	// Content stable — check for idle prompt
	if w.status != StatusIdle && detect_idle_prompt(content) {
		w.status = StatusIdle
		m.emit(w, true)
	}
}

func (m *Monitor) emit(w *watcher, idle bool) {
	select {
	case m.events <- IdleEvent{Label: w.label, Alias: w.alias, Idle: idle}:
	default:
		// Drop event if channel is full
	}
}

// content_hash returns a fast hash of the content for change detection.
func content_hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}

// detect_idle_prompt checks if the captured content looks like Claude Code
// waiting for input. Claude's idle prompt looks like:
//
//	─────────────────────────
//	❯
//	─────────────────────────
//	  PR #10396 · esc to interrupt
//
// We detect: the ❯ prompt character between two horizontal line separators.
func detect_idle_prompt(content string) bool {
	lines := strings.Split(content, "\n")

	// Scan last 10 non-empty lines
	has_prompt := false
	has_separator := false
	has_hint := false

	checked := 0
	for i := len(lines) - 1; i >= 0 && checked < 10; i-- {
		line := strings.TrimRight(lines[i], " \t")
		if line == "" {
			continue
		}
		checked++

		// The ❯ prompt (U+276F) or > prompt
		trimmed := strings.TrimSpace(line)
		if trimmed == "❯" || trimmed == ">" || trimmed == "❯ " || trimmed == "> " {
			has_prompt = true
		}

		// Horizontal separator line (all ─ characters)
		stripped := strings.TrimSpace(line)
		if len(stripped) > 10 && strings.Count(stripped, "─") == len([]rune(stripped)) {
			has_separator = true
		}

		// "esc to interrupt" hint line
		if strings.Contains(line, "esc to interrupt") {
			has_hint = true
		}
	}

	// Prompt + separator is enough. Hint is optional (confirms Claude Code).
	return has_prompt && (has_separator || has_hint)
}
