package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/elvisnm/wt/internal/aws"
	"github.com/elvisnm/wt/internal/labels"
	"github.com/elvisnm/wt/internal/sentinel"
	"github.com/elvisnm/wt/internal/worktree"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handle_create_sentinel processes the dc-create completion sentinel.
// On success, defers dev server start (local) or esbuild watch (host-build)
// until after discovery refreshes the worktree list.
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

	if exit_code == 0 && created_alias != "" {
		if env_type == "local" {
			m.pending_dev_alias = created_alias
		} else {
			m.pending_esbuild_alias = created_alias
		}
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

// handle_aws_keys_sentinel processes the AWS keys script completion sentinel.
// On success, refreshes credentials, propagates to tmux, and restarts all running services.
func (m Model) handle_aws_keys_sentinel(sr *sentinel.Result) (Model, tea.Cmd) {
	debug_log("[aws] sentinel found: raw=%q exit_code=%d", sr.Raw, sr.ExitCode)
	m.aws_keys_running = false
	m.term_mgr.CloseByLabel(labels.AWSKeys)
	if m.pane_layout != nil {
		m.pane_layout.FocusLeft()
	}
	if sr.ExitCode != 0 {
		debug_log("[aws] FAILED: exit_code=%d", sr.ExitCode)
		m.activity = "AWS keys update failed"
		m.focus_worktrees_if_empty()
		return m, tick_after(100*time.Millisecond, "render")
	}
	profile := ""
	if m.cfg != nil {
		profile = m.cfg.AwsSsoProfile()
	}
	debug_log("[aws] SUCCESS: refreshing credentials (profile=%q)", profile)
	if err := aws.Refresh(profile); err != nil {
		debug_log("[aws] Refresh FAILED: %v", err)
		m.activity = fmt.Sprintf("AWS credential refresh failed: %v", err)
		m.focus_worktrees_if_empty()
		return m, tick_after(100*time.Millisecond, "render")
	}
	aws.PropagateToTmux(m.term_mgr.Server())
	debug_log("[aws] propagated credentials to tmux server")

	// If a deferred action was pending, execute it
	if m.pending_sso_action != "" {
		debug_log("[aws] SSO login done, resolving pending action=%s", m.pending_sso_action)
		return m.resolve_sso_action()
	}

	m.activity = "AWS credentials updated — restarting services..."
	cmds := []tea.Cmd{
		tick_after(100*time.Millisecond, "render"),
		tick_after(3*time.Second, "status"),
	}
	debug_log("[aws] worktrees to check: %d", len(m.worktrees))
	for _, wt := range m.worktrees {
		if !wt.Running {
			debug_log("[aws]   skip %s (not running)", wt.Alias)
			continue
		}
		switch wt.Type {
		case worktree.TypeLocal:
			debug_log("[aws]   restart local: %s", wt.Alias)
			var cmd tea.Cmd
			m, cmd = m.restart_local_services(wt)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		case worktree.TypeDocker:
			debug_log("[aws]   restart docker: %s (container=%s host_build=%v)", wt.Alias, wt.Container, wt.HostBuild)
			wt := wt
			if wt.HostBuild {
				build_label := labels.Tab(labels.Build, wt.Alias)
				m.term_mgr.CloseByLabel(build_label)
				debug_log("[aws]   closed esbuild tab %q for restart", build_label)
			}
			cmds = append(cmds, tea.Sequence(
				func() tea.Msg {
					return MsgActionStarted{WtName: wt.Name, Status: "refreshing..."}
				},
				func() tea.Msg {
					run_docker("stop", wt.Container)
					out, err := run_worktree_up(wt, m.repo_root, m.cfg)
					if err != nil {
						return MsgActionOutput{Output: out, Err: err}
					}
					if wt.HostBuild {
						return MsgOpenBuildAfterStart{WtName: wt.Name}
					}
					return MsgActionOutput{Output: out}
				},
			))
		}
	}
	m.focus_worktrees_if_empty()
	return m, tea.Batch(cmds...)
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

// resolve_sso_action executes the deferred action after SSO session is confirmed valid.
func (m Model) resolve_sso_action() (Model, tea.Cmd) {
	action := m.pending_sso_action
	m.pending_sso_action = ""
	m.activity = ""

	switch action {
	case "create":
		return m.open_create(m.selected_worktree())
	case "start":
		if m.pending_sso_start != nil {
			wt := *m.pending_sso_start
			m.pending_sso_start = nil
			return m.start_worktree(wt)
		}
	case "restart":
		if m.pending_sso_start != nil {
			wt := *m.pending_sso_start
			m.pending_sso_start = nil
			if wt.Type == worktree.TypeLocal {
				return m.restart_local_services(wt)
			}
			if wt.HostBuild {
				return m.restart_host_build(wt)
			}
			return m, cmd_docker_action("restart", wt, m.repo_root, m.cfg)
		}
	}
	// No pending action (manual Shift+A) — show timed alert
	white := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("34")).Bold(true)
	m.alert_open = true
	m.alert_message = white.Render("AWS session is already ") + green.Render("VALID")
	m.alert_countdown = 3
	return m, tick_after(1*time.Second, "alert")
}
