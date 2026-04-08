package app

import (
	"strings"
	"time"

	"github.com/elvisnm/wt/internal/labels"
	"github.com/elvisnm/wt/internal/sentinel"

	tea "github.com/charmbracelet/bubbletea"
)

// handle_create_sentinel processes the dc-create completion sentinel.
// On success, defers dev server start (local) until after discovery
// refreshes the worktree list.
func (m Model) handle_create_sentinel(sr *sentinel.Result) (Model, tea.Cmd) {
	lines := strings.SplitN(sr.Raw, "\n", 3)
	exit_code := sr.ExitCode
	created_alias := ""
	env_type := ""
	if len(lines) > 1 {
		created_alias = strings.TrimSpace(lines[1])
	}
	if len(lines) > 2 {
		env_type = strings.TrimSpace(lines[2])
	}
	debug_log("[create] sentinel found: exit_code=%d alias=%q env=%q", exit_code, created_alias, env_type)

	// Close all Create tabs
	m.term_mgr.CloseByLabel(labels.Create)
	for _, wt := range m.worktrees {
		m.term_mgr.CloseByLabel(labels.Tab(labels.Create, wt.Alias))
	}

	if exit_code == 0 && created_alias != "" && env_type == "local" {
		m.pending_dev_alias = created_alias
	}

	m.focus_worktrees_if_empty()
	return m, tea.Batch(
		tick_after(100*time.Millisecond, "render"),
		m.cmd_discover(),
	)
}

// handle_skip_worktree_sentinel processes the skip-worktree script completion sentinel.
func (m Model) handle_skip_worktree_sentinel(sr *sentinel.Result) (Model, tea.Cmd) {
	m.skip_worktree_running = false
	// Close the "Skip —" tab
	for _, s := range m.term_mgr.Sessions() {
		if strings.HasPrefix(s.Label, labels.Skip+labels.Sep) {
			m.term_mgr.CloseByLabel(s.Label)
			break
		}
	}
	if sr.ExitCode == 0 {
		m.activity = "Skip-worktree updated"
	} else {
		m.activity = "Skip-worktree failed"
	}
	m.focus_worktrees_if_empty()
	return m, tick_after(100*time.Millisecond, "render")
}

// handle_heihei_sentinel processes the HeiHei scream completion sentinel.
func (m Model) handle_heihei_sentinel() (Model, tea.Cmd) {
	m.heihei_playing = false
	m.term_mgr.CloseByLabel(labels.HeiHei)
	if m.pane_layout != nil {
		m.pane_layout.FocusLeft()
	}
	m.focus_worktrees_if_empty()
	return m, nil
}

