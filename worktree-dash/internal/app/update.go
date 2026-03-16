package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/elvisnm/wt/internal/beads"
	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/esbuild"
	"github.com/elvisnm/wt/internal/labels"
	"github.com/elvisnm/wt/internal/pm2"
	"github.com/elvisnm/wt/internal/sentinel"
	"github.com/elvisnm/wt/internal/terminal"
	"github.com/elvisnm/wt/internal/ui"
	"github.com/elvisnm/wt/internal/worktree"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalc_layout()
		m.ready = true
		// In pane layout mode, tmux handles right pane resize natively.
		// Resize background session windows to match the new right pane dimensions.
		if m.pane_layout != nil {
			rw, rh := m.pane_layout.RightPaneDimensions()
			for _, s := range m.term_mgr.Sessions() {
				s.Resize(rw, rh)
			}
			if m.preview_session != nil {
				m.preview_session.Resize(rw, rh)
			}
		}
		return m, nil

	case MsgDiscovered:
		first_load := !m.discovered
		m.discovered = true
		debug_log("[discovery] MsgDiscovered: count=%d first_load=%v", len(msg.Worktrees), first_load)
		m.update_worktrees(msg.Worktrees)

		// Open deferred esbuild watch for host-build worktrees created via dc-create
		if m.pending_esbuild_alias != "" {
			for _, wt := range m.worktrees {
				if wt.Alias == m.pending_esbuild_alias && wt.HostBuild && wt.Running {
					debug_log("[create] deferred esbuild open for %s", wt.Alias)
					m, _ = m.open_esbuild_watch(wt)
					break
				}
			}
			m.pending_esbuild_alias = ""
		}

		// Start deferred dev server for local worktrees created via dc-create
		if m.pending_dev_alias != "" {
			for _, wt := range m.worktrees {
				if wt.Alias == m.pending_dev_alias && wt.Type == worktree.TypeLocal {
					debug_log("[create] deferred dev server start for %s", wt.Alias)
					m, _ = m.start_dev_server(wt)
					break
				}
			}
			m.pending_dev_alias = ""
		}

		// Signal the outer process that we're ready (unblocks tmux attach).
		if first_load && m.pane_layout != nil {
			m.pane_layout.Server().Run("wait-for", "-S", "wt-ready")
		}
		cmds := []tea.Cmd{
			tick_after(5*time.Second, "status"),
			tick_after(3*time.Second, "stats"),
			tick_after(100*time.Millisecond, "render"),
		}
		wt := m.selected_worktree()
		if wt != nil && wt.Running {
			cmds = append(cmds, m.refresh_services())
		} else if len(m.services) > 0 {
			m.services = nil
			m.service_cursor = 0
			m.close_preview()
		}
		return m, tea.Batch(cmds...)

	case MsgStatusUpdated:
		debug_log("[tick] MsgStatusUpdated: count=%d", len(msg.Worktrees))
		m.update_worktrees(msg.Worktrees)
		cmds := []tea.Cmd{tick_after(5*time.Second, "status")}
		wt := m.selected_worktree()
		if wt != nil {
			debug_log("[tick] selected: %s type=%v running=%v svcs=%d cursor=%d", wt.Alias, wt.Type, wt.Running, len(m.services), m.cursor)
		} else {
			debug_log("[tick] selected: nil cursor=%d len=%d", m.cursor, len(m.worktrees))
		}
		if wt != nil && wt.Running && len(m.services) == 0 {
			cmds = append(cmds, m.refresh_services())
			if m.activity != "" {
				m.activity = ""
			}
		}
		if wt != nil && !wt.Running && len(m.services) > 0 {
			m.services = nil
			m.service_cursor = 0
			m.close_preview()
		}
		return m, tea.Batch(cmds...)

	case MsgStatsUpdated:
		debug_log("[tick] MsgStatsUpdated: count=%d", len(msg.Worktrees))
		// Merge stats (CPU, Mem, MemPct) into existing worktrees.
		// Do NOT replace the list — the stats snapshot may be stale
		// (captured before new worktrees were discovered).
		stats_map := make(map[string]*worktree.Worktree)
		for i := range msg.Worktrees {
			stats_map[msg.Worktrees[i].Path] = &msg.Worktrees[i]
		}
		for i := range m.worktrees {
			if s, ok := stats_map[m.worktrees[i].Path]; ok {
				m.worktrees[i].CPU = s.CPU
				m.worktrees[i].Mem = s.Mem
				m.worktrees[i].MemPct = s.MemPct
			}
		}
		return m, tick_after(3*time.Second, "stats")

	case MsgUsageUpdated:
		m.usage_data = msg.Usage
		m.usage_err = msg.Err
		if msg.Token != "" {
			m.usage_token = msg.Token
		}
		if m.usage_visible {
			return m, tick_after(60*time.Second, "usage")
		}
		return m, nil

	case MsgTasksLoaded:
		m.tasks_list = msg.Tasks
		m.tasks_err = msg.Err
		if m.tasks_cursor >= len(m.tasks_list) {
			m.tasks_cursor = len(m.tasks_list) - 1
			if m.tasks_cursor < 0 {
				m.tasks_cursor = 0
			}
		}
		if m.tasks_visible {
			m.recalc_layout()
			return m, tick_after(3*time.Second, "tasks")
		}
		return m, nil

	case MsgTaskDetailLoaded:
		if msg.Err != nil {
			m.tasks_err = msg.Err
			return m, nil
		}
		m.tasks_detail = msg.Task
		m.tasks_detail_scroll = 0
		if m.tasks_visible {
			m.recalc_layout()
		}
		return m, nil

	case MsgTaskActionDone:
		if msg.Err != nil {
			m.tasks_err = msg.Err
			return m, nil
		}
		return m, cmd_fetch_tasks()

	case MsgServicesUpdated:
		sel := m.selected_worktree()
		sel_name := "<nil>"
		if sel != nil {
			sel_name = sel.Alias
		}
		debug_log("[services] MsgServicesUpdated: count=%d for=%s svc_cursor=%d", len(msg.Services), sel_name, m.service_cursor)
		m.services = msg.Services
		if m.service_cursor >= len(m.services) {
			m.service_cursor = 0
		}
		if m.preview_session != nil {
			found := false
			for _, svc := range m.services {
				if svc.Name == m.preview_svc_name {
					found = true
					break
				}
			}
			if !found {
				m.close_preview()
			}
		}
		return m, tick_after(5*time.Second, "services")

	case MsgSessionOpened:
		if msg.Err != nil {
			m.terminal_output = fmt.Sprintf("Error opening session: %v", msg.Err)
		} else {
			m.terminal_output = ""
			m.prev_focus = m.focus; m.focus = PanelTerminal
		}
		return m, nil

	case MsgSsoSessionCheck:
		if msg.Valid {
			debug_log("[aws] SSO session valid, pending_action=%s", m.pending_sso_action)
			return m.resolve_sso_action()
		}
		debug_log("[aws] SSO session expired, pending_action=%s, opening login", m.pending_sso_action)
		m.activity = "AWS SSO session expired — logging in..."
		return m.open_sso_login()

	case MsgActionStarted:
		if m.actions_pending == nil {
			m.actions_pending = make(map[string]bool)
		}
		m.actions_pending[msg.WtName] = true
		for i := range m.worktrees {
			if m.worktrees[i].Name == msg.WtName {
				m.worktrees[i].Health = msg.Status
				break
			}
		}
		m.activity = fmt.Sprintf("%s %s", msg.Status, msg.WtName)
		m.spin_frame = 0
		return m, tick_after(80*time.Millisecond, "spin")

	case MsgActionOutput:
		m.actions_pending = nil
		m.activity = ""
		if msg.Err != nil {
			if msg.Output != "" {
				m.activity = fmt.Sprintf("Error: %s", last_line(msg.Output))
			} else {
				m.activity = fmt.Sprintf("Error: %v", msg.Err)
			}
		}
		return m, tea.Batch(m.cmd_discover(), m.refresh_services())

	case MsgOpenBuildAfterStart:
		m.actions_pending = nil
		m.activity = ""
		for _, wt := range m.worktrees {
			if wt.Name == msg.WtName {
				mdl, cmd := m.open_esbuild_watch(wt)
				m = mdl
				return m, tea.Batch(cmd, m.cmd_discover())
			}
		}
		return m, m.cmd_discover()

	case MsgTick:
		switch msg.Kind {
		case "status":
			wts := make([]worktree.Worktree, len(m.worktrees))
			copy(wts, m.worktrees)
			return m, cmd_fetch_status(m.worktrees_dir, wts, m.cfg, m.term_mgr)
		case "stats":
			wts := make([]worktree.Worktree, len(m.worktrees))
			copy(wts, m.worktrees)
			return m, cmd_fetch_stats(wts, m.cfg)
		case "services":
			if wt := m.selected_worktree(); wt != nil && wt.Running {
				return m, m.refresh_services()
			}
			return m, tick_after(5*time.Second, "services")
		case "usage":
			if m.usage_visible {
				return m, cmd_fetch_usage(m.usage_token)
			}
			return m, nil
		case "tasks":
			if m.tasks_visible {
				return m, cmd_fetch_tasks()
			}
			return m, nil
		case "spin":
			spinning := m.activity != "" ||
				(m.usage_visible && m.usage_data == nil && m.usage_err == nil)
			if spinning {
				m.spin_frame++
				return m, tick_after(80*time.Millisecond, "spin")
			}
			return m, nil
		case "clear-activity":
			m.activity = ""
			return m, nil
		case "alert":
			if !m.alert_open {
				return m, nil
			}
			m.alert_countdown--
			if m.alert_countdown <= 0 {
				m.alert_open = false
				return m, nil
			}
			return m, tick_after(1*time.Second, "alert")
		case "render":
			// Check if dc-create finished (via sentinel file).
			// Always check the sentinel first — the pane may have already
			// been reaped as dead before the file becomes visible on disk.
			if sr := sentinel.Read(sentinel.Create); sr != nil {
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
						// Defer dev server start for local worktrees until after discovery
						m.pending_dev_alias = created_alias
					} else {
						// Defer esbuild watch for host-build worktrees until after discovery
						// refreshes the worktree list (the new worktree may not be in
						// m.worktrees yet, or may not have HostBuild=true yet).
						m.pending_esbuild_alias = created_alias
					}
				}

				m.focus_worktrees_if_empty()
				return m, tea.Batch(
					tick_after(100*time.Millisecond, "render"),
					m.cmd_discover(),
				)
			} else if m.term_mgr.HasLabel(labels.Create) || m.has_create_alias_tab() {
				if m.term_mgr.CloseDeadByPrefixIfClean(labels.Create) {
					// Create process exited cleanly without sentinel (e.g. Ctrl+C)
					m.focus_worktrees_if_empty()
				}
				// If process crashed (non-zero exit), the tab stays open so
				// the user can read the error output.
			}
			// Check if skip-worktree script finished (via sentinel file)
			if m.skip_worktree_running {
				if sr := sentinel.Read(sentinel.SkipWorktree); sr != nil {
					exit_code := sr.ExitCode
					m.skip_worktree_running = false
					// Close the "Skip —" tab
					for _, s := range m.term_mgr.Sessions() {
						if strings.HasPrefix(s.Label, labels.Skip+labels.Sep) {
							m.term_mgr.CloseByLabel(s.Label)
							break
						}
					}
					if exit_code == 0 {
						m.activity = "Skip-worktree updated"
					} else {
						m.activity = "Skip-worktree failed"
					}
					m.focus_worktrees_if_empty()
					// If tabs remain, CloseByLabel already showed the next one
					return m, tick_after(100*time.Millisecond, "render")
				}
			}
			// Check if the AWS Keys script finished (via sentinel file)
			if m.aws_keys_running {
				if sr := sentinel.Read(sentinel.AWSKeys); sr != nil {
					debug_log("[aws] sentinel found: raw=%q exit_code=%d", sr.Raw, sr.ExitCode)
					exit_code := sr.ExitCode
					m.aws_keys_running = false
					m.term_mgr.CloseByLabel(labels.AWSKeys)
					if m.pane_layout != nil {
						m.pane_layout.FocusLeft()
					}
					if exit_code != 0 {
						debug_log("[aws] FAILED: exit_code=%d", exit_code)
						m.activity = "AWS keys update failed"
						m.focus_worktrees_if_empty()
						return m, tick_after(100*time.Millisecond, "render")
					}
					is_sso := m.cfg != nil && m.cfg.AwsSsoProfile() != ""
					if is_sso {
						debug_log("[aws] SUCCESS (SSO): exporting credentials from SSO session")
						if err := export_sso_credentials(m.cfg.AwsSsoProfile()); err != nil {
							debug_log("[aws] export_sso_credentials FAILED: %v", err)
							m.activity = fmt.Sprintf("SSO login OK but credential export failed: %v", err)
							m.focus_worktrees_if_empty()
							return m, tick_after(100*time.Millisecond, "render")
						}
						// Propagate SSO-exported keys to tmux server
						if server := m.term_mgr.Server(); server != nil {
							for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
								if val := os.Getenv(key); val != "" {
									server.SetEnv(key, val)
								}
							}
							debug_log("[aws] propagated SSO-exported keys to tmux server")
						}
					} else {
						debug_log("[aws] SUCCESS (paste): reloading credentials and restarting services")
						reload_aws_credentials()
						// Propagate AWS env vars to tmux server so new windows inherit them
						if server := m.term_mgr.Server(); server != nil {
							for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
								if val := os.Getenv(key); val != "" {
									server.SetEnv(key, val)
								}
							}
							debug_log("[aws] propagated AWS keys to tmux server")
						}
					}
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
								// Close existing esbuild tab — it has stale env
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
			}
			// Check if HeiHei scream finished (via sentinel file)
			if m.heihei_playing {
				if sentinel.Read(sentinel.HeiHei) != nil {
					m.heihei_playing = false
					m.term_mgr.CloseByLabel(labels.HeiHei)
					if m.pane_layout != nil {
						m.pane_layout.FocusLeft()
					}
					m.focus_worktrees_if_empty()
				}
			}
			// Auto-close dead Logs tabs
			if m.term_mgr != nil && m.term_mgr.CloseDeadLogs() {
				m.focus_worktrees_if_empty()
			}
			// Re-render tick for PTY output updates
			if m.term_mgr.Count() > 0 || m.preview_session != nil {
				return m, tick_after(100*time.Millisecond, "render")
			}
			return m, nil
		}
		return m, nil

	case MsgResultClear:
		m.result_text = ""
		return m, nil

	case tea.MouseMsg:
		return m.handle_mouse(msg)

	case tea.KeyMsg:
		// In pane layout mode, the right pane gets native input via tmux focus.
		// Bubbletea only receives keys when the left pane (pane 0) has focus.
		if m.alert_open {
			m.alert_open = false
			return m, tick_after(100*time.Millisecond, "render")
		}
		if m.help_open {
			return m.handle_help_key(msg)
		}
		if m.confirm_open {
			return m.handle_confirm_key(msg)
		}
		if m.input_active {
			return m.handle_input_key(msg)
		}
		if m.picker_open {
			return m.handle_picker_key(msg)
		}
		return m.handle_key(msg)
	}

	return m, nil
}

func (m Model) handle_mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		switch {
		case m.focus == PanelTerminal:
			// In pane layout mode, mouse scroll is handled natively by tmux
		case m.focus == PanelDetails:
			m.details_scroll -= 3
			if m.details_scroll < 0 {
				m.details_scroll = 0
			}
		case m.focus == PanelWorktrees:
			if m.cursor > 0 {
				m.cursor--
				m.details_scroll = 0
				m.close_preview()
				m.services = nil
				m.service_cursor = 0
				return m, m.refresh_services()
			}
		case m.focus == PanelServices:
			if m.service_cursor > 0 {
				m.service_cursor--
			}
		}
	case tea.MouseButtonWheelDown:
		switch {
		case m.focus == PanelTerminal:
			// In pane layout mode, mouse scroll is handled natively by tmux
		case m.focus == PanelDetails:
			wt := m.selected_worktree()
			if wt != nil {
				inner_h := m.layout.DetailsHeight - 2
				total := ui.DetailLineCount(wt, m.cfg)
				max_scroll := total - inner_h
				if max_scroll < 0 {
					max_scroll = 0
				}
				m.details_scroll += 3
				if m.details_scroll > max_scroll {
					m.details_scroll = max_scroll
				}
			}
		case m.focus == PanelWorktrees:
			if m.cursor < len(m.worktrees)-1 {
				m.cursor++
				m.details_scroll = 0
				m.close_preview()
				m.services = nil
				m.service_cursor = 0
				return m, m.refresh_services()
			}
		case m.focus == PanelServices:
			if m.service_cursor < len(m.services)-1 {
				m.service_cursor++
			}
		}
	}
	return m, nil
}

func (m *Model) clamp_cursor() {
	if len(m.worktrees) == 0 {
		m.cursor = 0
	} else if m.cursor >= len(m.worktrees) {
		m.cursor = len(m.worktrees) - 1
	}
}

// update_worktrees replaces the worktree list while preserving cursor selection
func (m *Model) update_worktrees(wts []worktree.Worktree) {
	var selected_name string
	if m.cursor >= 0 && m.cursor < len(m.worktrees) {
		selected_name = m.worktrees[m.cursor].Name
	}

	// Worktrees with pending actions (removing, starting, etc.) are kept
	// from the current state. Periodic discovery can re-find a directory
	// before it's fully deleted — filtering it out prevents flicker.
	if len(m.actions_pending) > 0 {
		filtered := make([]worktree.Worktree, 0, len(wts))
		for _, wt := range wts {
			if !m.actions_pending[wt.Name] {
				filtered = append(filtered, wt)
			}
		}
		for _, wt := range m.worktrees {
			if m.actions_pending[wt.Name] {
				filtered = append(filtered, wt)
			}
		}
		wts = filtered
	}

	// Mark worktrees as "creating..." when a Create tab exists and hasn't
	// finished yet (no sentinel file). This handles the gap between dc-create
	// writing the env file (worktree discovered) and docker compose up finishing.
	if m.term_mgr != nil && (m.term_mgr.HasLabel(labels.Create) || m.has_create_alias_tab()) {
		if !sentinel.Exists(sentinel.Create) {
			// Sentinel doesn't exist — creation still in progress
			for i := range wts {
				if wts[i].Type == worktree.TypeDocker && !wts[i].ContainerExists {
					wts[i].Health = "creating..."
				}
			}
		}
	}

	m.worktrees = wts

	if selected_name != "" {
		for i, wt := range m.worktrees {
			if wt.Name == selected_name {
				m.cursor = i
				debug_log("[update_wt] stored %d worktrees, cursor=%d (%s)", len(wts), m.cursor, selected_name)
				for j, w := range m.worktrees {
					debug_log("[update_wt]   [%d] %s type=%v running=%v", j, w.Alias, w.Type, w.Running)
				}
				return
			}
		}
	}

	m.clamp_cursor()
	debug_log("[update_wt] stored %d worktrees, cursor=%d (clamped, prev=%q)", len(wts), m.cursor, selected_name)
	for j, w := range m.worktrees {
		debug_log("[update_wt]   [%d] %s type=%v running=%v", j, w.Alias, w.Type, w.Running)
	}
}

func (m Model) selected_worktree() *worktree.Worktree {
	if m.cursor >= 0 && m.cursor < len(m.worktrees) {
		wt := m.worktrees[m.cursor]
		return &wt
	}
	return nil
}

func (m Model) handle_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	debug_log("[keys] key=%q focus=%d", msg.String(), m.focus)
	switch {
	case key.Matches(msg, Keys.Quit), key.Matches(msg, Keys.CtrlC):
		m.confirm_open = true
		m.confirm_prompt = "Quit worktree?"
		m.confirm_action = quit_action
		return m, nil

	case key.Matches(msg, Keys.Tab):
		m.close_preview()
		m.next_panel()
		return m, nil

	case key.Matches(msg, Keys.ShiftTab):
		m.close_preview()
		m.prev_panel()
		return m, nil

	case key.Matches(msg, Keys.Escape):
		if m.focus == PanelTasks && m.tasks_detail != nil {
			m.tasks_detail = nil
			m.recalc_layout()
			return m, nil
		}
		if m.focus == PanelTerminal {
			m.focus = m.prev_focus
		} else if m.focus != PanelWorktrees {
			m.focus = PanelWorktrees
		}
		return m, nil

	case key.Matches(msg, Keys.TabPrev):
		m.close_preview()
		m.prev_panel()
		return m, nil

	case key.Matches(msg, Keys.TabNext):
		m.close_preview()
		m.next_panel()
		return m, nil

	case key.Matches(msg, Keys.PanelLeft):
		m.close_preview()
		m.prev_panel()
		return m, nil

	case key.Matches(msg, Keys.PanelRight):
		m.close_preview()
		m.next_panel()
		return m, nil
	}

	// Help — open keybindings page in right pane
	if key.Matches(msg, Keys.Help) {
		return m.open_help()
	}

	// Panel jump shortcuts: a(ctive tabs), w(orktrees), s(ervices)
	switch msg.String() {
	case "a":
		m.close_preview()
		m.prev_focus = m.focus
		m.focus = PanelTerminal
		return m, nil
	case "w":
		m.close_preview()
		m.focus = PanelWorktrees
		return m, nil
	case "s":
		m.focus = PanelServices
		return m, nil
	}

	// 1-9 or Alt+1-9: jump directly to tab N and focus right pane
	// Alt+N is sent by tmux prefix+N bindings; plain N works from bubbletea directly
	if n := tab_number(msg); n > 0 && n <= m.term_mgr.Count() {
		m.close_preview()
		m.term_mgr.FocusByIndex(n - 1)
		m.prev_focus = m.focus
		m.focus = PanelTerminal
		if m.pane_layout != nil {
			m.pane_layout.FocusRight()
		}
		return m, nil
	}

	// Global operations (Shift+key) — gated by feature flags when config is available
	switch msg.String() {
	case "A":
		if m.cfg == nil || m.cfg.FeatureEnabled("awsCredentials") {
			debug_log("[aws] Shift+A pressed")
			// SSO mode: check session first
			if profile := m.cfg.AwsSsoProfile(); profile != "" {
				return m.check_sso_then_login()
			}
			return m.open_aws_keys()
		}
	case "B":
		return m.open_db_picker()
	case "D":
		return m.toggle_details()
	case "X":
		if m.cfg == nil || m.cfg.FeatureEnabled("admin") {
			return m.toggle_admin()
		}
	case "L":
		if m.cfg == nil || m.cfg.FeatureEnabled("lan") {
			return m.toggle_lan()
		}
	case "M":
		return m.open_maintenance_picker()
	case "K":
		return m.toggle_skip_worktree()
	case "H":
		return m.play_heihei()
	case "U":
		return m.toggle_usage()
	case "T":
		return m.toggle_tasks()
	}

	switch m.focus {
	case PanelWorktrees:
		return m.handle_worktree_key(msg)
	case PanelDetails:
		return m.handle_details_key(msg)
	case PanelServices:
		return m.handle_services_key(msg)
	case PanelTerminal:
		return m.handle_terminal_key(msg)
	case PanelTasks:
		return m.handle_tasks_key(msg)
	}

	return m, nil
}

func (m Model) handle_worktree_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Up):
		if m.cursor > 0 {
			prev := m.cursor
			m.cursor--
			m.details_scroll = 0
			m.close_preview()
			m.services = nil
			m.service_cursor = 0
			wt := m.selected_worktree()
			if wt != nil {
				debug_log("[keys] worktree up: cursor %d->%d now=%s running=%v", prev, m.cursor, wt.Alias, wt.Running)
			}
			return m, m.refresh_services()
		}
		return m, nil

	case key.Matches(msg, Keys.Down):
		if m.cursor < len(m.worktrees)-1 {
			m.cursor++
			m.details_scroll = 0
			m.close_preview()
			m.services = nil
			m.service_cursor = 0
			return m, m.refresh_services()
		}
		return m, nil

	case key.Matches(msg, Keys.PageUp):
		page := m.layout.WorktreeHeight - 4
		if page < 1 {
			page = 1
		}
		prev := m.cursor
		m.cursor -= page
		if m.cursor < 0 {
			m.cursor = 0
		}
		if m.cursor != prev {
			m.details_scroll = 0
			m.close_preview()
			m.services = nil
			m.service_cursor = 0
			return m, m.refresh_services()
		}
		return m, nil

	case key.Matches(msg, Keys.PageDown):
		page := m.layout.WorktreeHeight - 4
		if page < 1 {
			page = 1
		}
		prev := m.cursor
		m.cursor += page
		if m.cursor >= len(m.worktrees) {
			m.cursor = len(m.worktrees) - 1
		}
		if m.cursor != prev {
			m.details_scroll = 0
			m.close_preview()
			m.services = nil
			m.service_cursor = 0
			return m, m.refresh_services()
		}
		return m, nil

	case key.Matches(msg, Keys.Enter):
		wt := m.selected_worktree()
		if wt != nil {
			m.picker_actions = m.actions_for_worktree(*wt)
			m.picker_cursor = 0
			m.picker_open = true
			m.picker_context = pickerWorktree
		}
		return m, nil
	}

	// "n" works even with an empty worktree list
	if msg.String() == "n" {
		debug_log("[create] 'n' pressed: opening create wizard")
		// SSO check before opening wizard
		if profile := m.sso_profile(); profile != "" {
			m.pending_sso_action = "create"
			m.activity = "Checking AWS SSO session..."
			return m, func() tea.Msg {
				valid := check_sso_session(profile)
				return MsgSsoSessionCheck{Valid: valid}
			}
		}
		return m.open_create(m.selected_worktree())
	}

	// Quick action keys
	wt := m.selected_worktree()
	if wt == nil {
		return m, nil
	}

	switch msg.String() {
	case "b":
		return m.open_bash(*wt)
	case "c":
		return m.open_claude(*wt)
	case "z":
		return m.open_local_shell(*wt)
	case "d":
		return m.toggle_details()
	case "l":
		return m.open_logs(*wt)
	case "i":
		return m.open_worktree_info()
	case "g":
		return m.open_pull(*wt)
	case "e":
		if wt.HostBuild && wt.Running {
			return m.open_esbuild_watch(*wt)
		}
	case "r":
		if wt.Running {
			if wt.Type == worktree.TypeLocal {
				return m.restart_local_services(*wt)
			}
			if wt.HostBuild {
				return m.restart_host_build(*wt)
			}
			return m, cmd_docker_action("restart", *wt, m.repo_root, m.cfg)
		}
	case "t":
		if wt.Running {
			if wt.Type == worktree.TypeLocal {
				return m.stop_dev_server(*wt)
			}
			if wt.HostBuild {
				return m.stop_host_build(*wt)
			}
			return m, cmd_docker_action("stop", *wt, m.repo_root, m.cfg)
		}
	case "u":
		if !wt.Running {
			// SSO check before starting
			if profile := m.sso_profile(); profile != "" {
				wtCopy := *wt
				m.pending_sso_action = "start"
				m.pending_sso_start = &wtCopy
				m.activity = "Checking AWS SSO session..."
				return m, func() tea.Msg {
					valid := check_sso_session(profile)
					return MsgSsoSessionCheck{Valid: valid}
				}
			}
			return m.start_worktree(*wt)
		}
	case "x":
		return m.remove_worktree(*wt)
	}

	return m, nil
}

func (m Model) handle_details_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	wt := m.selected_worktree()
	max_scroll := 0
	if wt != nil {
		inner_h := m.layout.DetailsHeight - 2
		total := ui.DetailLineCount(wt, m.cfg)
		max_scroll = total - inner_h
		if max_scroll < 0 {
			max_scroll = 0
		}
	}

	switch {
	case key.Matches(msg, Keys.Up), msg.String() == "k":
		if m.details_scroll > 0 {
			m.details_scroll--
		}
		return m, nil
	case key.Matches(msg, Keys.Down), msg.String() == "j":
		if m.details_scroll < max_scroll {
			m.details_scroll++
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handle_services_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	wt := m.selected_worktree()

	switch {
	case key.Matches(msg, Keys.Up):
		if m.service_cursor > 0 {
			m.service_cursor--
			if m.preview_session != nil && wt != nil && wt.Running {
				if m.service_cursor >= 0 && m.service_cursor < len(m.services) {
					return m, m.open_preview_logs(*wt, m.services[m.service_cursor])
				}
			}
		}
		return m, nil

	case key.Matches(msg, Keys.Down):
		if m.service_cursor < len(m.services)-1 {
			m.service_cursor++
			if m.preview_session != nil && wt != nil && wt.Running {
				if m.service_cursor >= 0 && m.service_cursor < len(m.services) {
					return m, m.open_preview_logs(*wt, m.services[m.service_cursor])
				}
			}
		}
		return m, nil

	case key.Matches(msg, Keys.PageUp):
		page := m.layout.ServicesHeight - 4
		if page < 1 {
			page = 1
		}
		m.service_cursor -= page
		if m.service_cursor < 0 {
			m.service_cursor = 0
		}
		return m, nil

	case key.Matches(msg, Keys.PageDown):
		page := m.layout.ServicesHeight - 4
		if page < 1 {
			page = 1
		}
		m.service_cursor += page
		if m.service_cursor >= len(m.services) {
			m.service_cursor = len(m.services) - 1
		}
		if m.service_cursor < 0 {
			m.service_cursor = 0
		}
		return m, nil

	case key.Matches(msg, Keys.Escape):
		if m.preview_session != nil {
			m.close_preview()
			return m, nil
		}
		m.focus = PanelWorktrees
		return m, nil

	case key.Matches(msg, Keys.Enter):
		if wt != nil && wt.Running && m.service_cursor >= 0 && m.service_cursor < len(m.services) {
			svc := m.services[m.service_cursor]
			// Static manager: Enter focuses the dev tab (no per-service preview)
			if m.is_static_local(*wt) {
				return m.open_service_logs(*wt, svc)
			}
			if m.preview_session != nil && m.preview_svc_name == svc.Name {
				// Already previewing this service — promote to full log tab
				m.close_preview()
				return m.open_service_logs(*wt, svc)
			}
			return m, m.open_preview_logs(*wt, svc)
		}
		return m, nil
	}

	if wt == nil || !wt.Running {
		return m, nil
	}

	switch msg.String() {
	case "l":
		if m.service_cursor >= 0 && m.service_cursor < len(m.services) {
			m.close_preview()
			svc := m.services[m.service_cursor]
			return m.open_service_logs(*wt, svc)
		}
	case "r":
		if m.is_static_local(*wt) {
			return m, m.show_result("Per-service restart not available")
		}
		if m.service_cursor >= 0 && m.service_cursor < len(m.services) {
			svc := m.services[m.service_cursor]
			m.activity = fmt.Sprintf("Restarting %s...", svc.DisplayName)
			return m, cmd_service_action("restart", *wt, svc, m.cfg)
		}
	case "t":
		if m.is_static_local(*wt) {
			return m, m.show_result("Per-service stop not available")
		}
		if m.service_cursor >= 0 && m.service_cursor < len(m.services) {
			svc := m.services[m.service_cursor]
			m.activity = fmt.Sprintf("Stopping %s...", svc.DisplayName)
			return m, cmd_service_action("stop", *wt, svc, m.cfg)
		}
	}

	return m, nil
}

func (m Model) handle_terminal_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.term_mgr.Active()

	switch {
	case key.Matches(msg, Keys.Up):
		m.term_mgr.PrevTab()
		return m, nil

	case key.Matches(msg, Keys.Down):
		m.term_mgr.NextTab()
		return m, nil

	case key.Matches(msg, Keys.Enter):
		// Focus the right pane — user types natively in the terminal
		if s != nil && s.IsAlive() && m.pane_layout != nil {
			m.pane_layout.FocusRight()
		}
		return m, nil

	case msg.String() == "f":
		// Fullscreen — zoom the right pane and focus it
		if s != nil && s.IsAlive() && m.pane_layout != nil {
			m.pane_layout.ZoomRight()
			m.pane_layout.FocusRight()
		}
		return m, nil

	case msg.String() == "x":
		m.term_mgr.CloseActive()
		if m.term_mgr.Count() == 0 {
			m.focus = PanelWorktrees
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handle_picker_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Quit), key.Matches(msg, Keys.CtrlC):
		m.picker_open = false
		m.confirm_open = true
		m.confirm_prompt = "Quit worktree?"
		m.confirm_action = quit_action
		return m, nil

	case key.Matches(msg, Keys.Escape):
		m.picker_open = false
		return m, nil

	case key.Matches(msg, Keys.Tab):
		m.picker_open = false
		m.next_panel()
		return m, nil

	case key.Matches(msg, Keys.Up):
		if m.picker_cursor > 0 {
			m.picker_cursor--
		}
		return m, nil

	case key.Matches(msg, Keys.Down):
		if m.picker_cursor < len(m.picker_actions)-1 {
			m.picker_cursor++
		}
		return m, nil

	case key.Matches(msg, Keys.Enter):
		if m.picker_cursor >= 0 && m.picker_cursor < len(m.picker_actions) {
			action := m.picker_actions[m.picker_cursor]
			m.picker_open = false
			return m.dispatch_picker(action)
		}
		return m, nil
	}

	// Handle direct key presses in picker
	for _, a := range m.picker_actions {
		if msg.String() == a.Key {
			m.picker_open = false
			return m.dispatch_picker(a)
		}
	}

	return m, nil
}

func (m Model) dispatch_picker(action ui.PickerAction) (Model, tea.Cmd) {
	switch m.picker_context {
	case pickerDB:
		return m.execute_db_action(action)
	case pickerMaintenance:
		return m.execute_maintenance_action(action)
	case pickerRemove:
		return m.execute_remove_action(action)
	case pickerStartService:
		return m.execute_start_service_action(action)
	case pickerStopService:
		return m.execute_stop_service_action(action)
	default:
		return m.execute_picker_action(action)
	}
}

func (m Model) execute_picker_action(action ui.PickerAction) (Model, tea.Cmd) {
	wt := m.selected_worktree()
	if wt == nil {
		return m, nil
	}

	switch action.Key {
	case "b":
		return m.open_bash(*wt)
	case "c":
		return m.open_claude(*wt)
	case "z":
		return m.open_local_shell(*wt)
	case "l":
		return m.open_logs(*wt)
	case "n":
		return m.open_create(wt)
	case "g":
		return m.open_pull(*wt)
	case "e":
		if wt.HostBuild {
			return m.open_esbuild_watch(*wt)
		}
	case "r":
		if wt.Type == worktree.TypeLocal {
			return m.restart_local_services(*wt)
		}
		if wt.HostBuild {
			return m.restart_host_build(*wt)
		}
		return m, cmd_docker_action("restart", *wt, m.repo_root, m.cfg)
	case "t":
		if wt.Type == worktree.TypeLocal {
			return m.stop_dev_server(*wt)
		}
		if wt.HostBuild {
			return m.stop_host_build(*wt)
		}
		return m, cmd_docker_action("stop", *wt, m.repo_root, m.cfg)
	case "u":
		if wt.Type == worktree.TypeLocal {
			return m.start_dev_server(*wt)
		}
		if wt.HostBuild {
			return m.start_host_build(*wt)
		}
		return m, cmd_docker_action("start", *wt, m.repo_root, m.cfg)
	case "o":
		return m.open_start_service_picker(*wt)
	case "p":
		return m.open_stop_service_picker(*wt)
	case "i":
		return m.open_worktree_info()
	case "x":
		return m.remove_worktree(*wt)
	}

	return m, nil
}

// open_shell opens a shell session in the container or worktree dir
func (m Model) open_bash(wt worktree.Worktree) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()

	var cmd_name string
	var args []string
	var dir string

	if wt.Type == worktree.TypeDocker && wt.Running {
		cmd_name = "docker"
		args = []string{"exec", "-it", wt.Container, "bash"}
	} else {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "bash"
		}
		cmd_name = shell
		dir = wt.Path
	}

	label := labels.Tab(labels.Shell, wt.Alias)
	_, err := m.term_mgr.OpenNew(label, cmd_name, args, w, h, dir)
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to open bash: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal
	// Focus the right pane for native terminal interaction
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}
	return m, tick_after(100*time.Millisecond, "render")
}

// open_pull asks for confirmation then runs dc-pull.js to safely pull latest changes.
func (m Model) open_pull(wt worktree.Worktree) (Model, tea.Cmd) {
	m.confirm_open = true
	m.confirm_prompt = fmt.Sprintf("Pull latest changes on %s?", wt.Alias)
	m.confirm_action = func(mdl *Model) (Model, tea.Cmd) {
		return mdl.run_pull(wt)
	}
	return m, nil
}

func (m Model) run_pull(wt worktree.Worktree) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()

	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-pull.js")
	shell_cmd := fmt.Sprintf("node %q --repo %q --worktree %q", script, m.repo_root, wt.Path)

	label := labels.Tab(labels.Pull, wt.Alias)
	_, err := m.term_mgr.Open(label, "bash", []string{"-c", shell_cmd}, w, h, wt.Path)
	if err != nil {
		m.activity = fmt.Sprintf("Failed to pull: %v", err)
		return m, nil
	}

	m.activity = fmt.Sprintf("Pulling latest changes for %s...", wt.Alias)
	m.terminal_output = ""
	m.prev_focus = m.focus
	m.focus = PanelTerminal
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}
	return m, tick_after(100*time.Millisecond, "render")
}

// open_claude opens Claude Code in the worktree
func (m Model) open_claude(wt worktree.Worktree) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()

	var cmd_name string
	var args []string
	var dir string

	// Read claude path from config (set by wt init), fallback to PATH
	cmd_name = "claude"
	if m.cfg != nil {
		if c, ok := m.cfg.Dash.Commands["claude"]; ok && c.Cmd != "" {
			cmd_name = c.Cmd
		}
	}
	dir = wt.Path

	label := labels.Tab(labels.Claude, wt.Alias)
	_, err := m.term_mgr.OpenNew(label, cmd_name, args, w, h, dir)
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to open Claude: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal
	// Focus the right pane for native terminal interaction
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}
	return m, tick_after(100*time.Millisecond, "render")
}

// open_local_shell opens a host shell (zsh/bash) in the worktree directory
func (m Model) open_local_shell(wt worktree.Worktree) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "zsh"
	}

	label := labels.Tab(labels.Zsh, wt.Alias)
	_, err := m.term_mgr.OpenNew(label, shell, nil, w, h, wt.Path)
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to open shell: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal
	// Focus the right pane for native terminal interaction
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}
	return m, tick_after(100*time.Millisecond, "render")
}

// open_logs opens logs for the container or local worktree.
// For static manager on local worktrees, focuses the Dev tab instead.
func (m Model) open_logs(wt worktree.Worktree) (Model, tea.Cmd) {
	if !wt.Running {
		m.terminal_output = "Logs only available for running worktrees"
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}

	// For static manager on local worktrees, focus the Dev tab
	manager := "pm2"
	if m.cfg != nil {
		if wt.Type == worktree.TypeDocker {
			manager = m.cfg.DockerServiceManager()
		} else {
			manager = m.cfg.ServiceManager()
		}
	}
	if manager == "static" && wt.Type == worktree.TypeLocal {
		if label := find_dev_tab(m, wt); label != "" {
			m.term_mgr.FocusByLabel(label)
			m.prev_focus = m.focus; m.focus = PanelTerminal
			return m, nil
		}
		return m, m.show_result("No dev tab open")
	}

	w, h := m.right_pane_dimensions()
	label := labels.Tab(labels.Logs, wt.Alias)

	var cmd_name string
	var args []string
	var dir string

	if wt.Type == worktree.TypeDocker {
		cmd_name = "docker"
		args = []string{"exec", "-it", wt.Container, "pm2", "logs", "--lines", "100"}
	} else {
		cmd_name = "pm2"
		args = []string{"logs", "--lines", "100"}
		dir = wt.Path
	}

	_, err := m.term_mgr.Open(label, cmd_name, args, w, h, dir)
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to open logs: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal

	return m, tick_after(100*time.Millisecond, "render")
}

// open_create runs the interactive dc-create.js script to create a new container
func (m Model) open_create(wt *worktree.Worktree) (Model, tea.Cmd) {
	// Reload AWS credentials so the spawned process inherits the latest keys
	reload_aws_credentials()

	// Propagate AWS env vars to the tmux server so new windows inherit them
	if server := m.term_mgr.Server(); server != nil {
		for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
			if val := os.Getenv(key); val != "" {
				server.SetEnv(key, val)
			}
		}
	}

	w, h := m.right_pane_dimensions()

	// Remove stale sentinel before opening
	sentinel.Clear(sentinel.Create)

	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-create.js")
	debug_log("[create] open_create: script=%s", script)
	// Always use "Create" — the selected worktree's alias doesn't match the
	// NEW worktree being created, which breaks mark_local_running's devTab check.
	label := labels.Create

	_, err := m.term_mgr.Open(label, "node", []string{script}, w, h, m.repo_root)
	if err != nil {
		debug_log("[create] open_create: FAILED to open terminal: %v", err)
		m.terminal_output = fmt.Sprintf("Failed to open create: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}
	debug_log("[create] open_create: terminal opened label=%q", label)

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal
	// Focus the right pane for native terminal interaction
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}
	return m, tick_after(100*time.Millisecond, "render")
}

func (m Model) open_service_logs(wt worktree.Worktree, svc worktree.Service) (Model, tea.Cmd) {
	// For static manager, focus the Dev tab (local) or use docker logs (Docker)
	manager := "pm2"
	if m.cfg != nil {
		if wt.Type == worktree.TypeDocker {
			manager = m.cfg.DockerServiceManager()
		} else {
			manager = m.cfg.ServiceManager()
		}
	}
	if manager == "static" && wt.Type == worktree.TypeLocal {
		if label := find_dev_tab(m, wt); label != "" {
			m.term_mgr.FocusByLabel(label)
			m.prev_focus = m.focus; m.focus = PanelTerminal
			return m, nil
		}
		return m, m.show_result("No dev tab open")
	}

	w, h := m.right_pane_dimensions()

	var cmd_name string
	var args []string
	var label string
	var dir string

	svc_label := wt.Alias + "/" + svc.DisplayName

	// Esbuild service — tail the log file
	if svc.Name == "esbuild" && wt.Type == worktree.TypeLocal {
		log_path := esbuild.LogPath(wt.PM2Home())
		label = labels.Tab(labels.Logs, svc_label)
		cmd_name = "tail"
		args = []string{"-f", "-n", "80", log_path}
		dir = wt.Path
		_, err := m.term_mgr.Open(label, cmd_name, args, w, h, dir)
		if err != nil {
			return m, m.show_result(fmt.Sprintf("Failed to open esbuild logs: %v", err))
		}
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}

	if wt.Type == worktree.TypeDocker && manager == "static" {
		// Static Docker: use docker logs (no pm2 inside containers)
		cmd_name = "docker"
		if svc.Name == "__all" {
			args = []string{"logs", "-f", "--tail", "80", wt.Container}
			label = labels.Tab(labels.Logs, wt.Alias)
		} else {
			container := container_for_service(wt, svc.Name, m.cfg)
			args = []string{"logs", "-f", "--tail", "80", container}
			label = labels.Tab(labels.Logs, svc_label)
		}
	} else if wt.Type == worktree.TypeDocker {
		cmd_name = "docker"
		if svc.Name == "__all" {
			args = []string{"exec", "-it", wt.Container, "pm2", "logs", "--lines", "80"}
			label = labels.Tab(labels.Logs, wt.Alias)
		} else {
			args = []string{"exec", "-it", wt.Container, "pm2", "logs", svc.Name, "--lines", "80"}
			label = labels.Tab(labels.Logs, svc_label)
		}
	} else {
		dir = wt.Path
		if wt.IsolatedPM2 {
			// Isolated PM2: wrap with PM2_HOME so pm2 finds the right daemon
			pm2_home := wt.PM2Home()
			cmd_name = "bash"
			if svc.Name == "__all" {
				args = []string{"-c", fmt.Sprintf("PM2_HOME=%s exec pm2 logs --lines 80", pm2_home)}
				label = labels.Tab(labels.Logs, wt.Alias)
			} else {
				target := m.pm2_log_target(svc, wt)
				args = []string{"-c", fmt.Sprintf("PM2_HOME=%s exec pm2 logs %s --lines 80", pm2_home, target)}
				label = labels.Tab(labels.Logs, svc_label)
			}
		} else {
			cmd_name = "pm2"
			if svc.Name == "__all" {
				args = []string{"logs", "--lines", "80"}
				label = labels.Tab(labels.Logs, wt.Alias)
			} else {
				args = []string{"logs", svc.Name, "--lines", "80"}
				label = labels.Tab(labels.Logs, svc_label)
			}
		}
	}

	_, err := m.term_mgr.Open(label, cmd_name, args, w, h, dir)
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to open logs: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal

	return m, tick_after(100*time.Millisecond, "render")
}

func cmd_service_action(action string, wt worktree.Worktree, svc worktree.Service, cfg *config.Config) tea.Cmd {
	// Handle esbuild watcher actions
	if svc.Name == "esbuild" && wt.Type == worktree.TypeLocal {
		return func() tea.Msg {
			state_dir := wt.PM2Home()
			if action == "stop" {
				esbuild.Stop(state_dir)
				return MsgActionOutput{Output: "esbuild stopped"}
			}
			// start and restart both launch esbuild; restart stops first
			if action == "restart" {
				esbuild.Stop(state_dir)
			}
			build_script := ""
			if cfg != nil && cfg.Paths.BuildScript != "" {
				build_script = filepath.Join(wt.Path, cfg.Paths.BuildScript)
			}
			if build_script == "" {
				return MsgActionOutput{Output: "No build script configured"}
			}
			if err := esbuild.Start(build_script, wt.Path, state_dir, build_esbuild_env(wt, cfg)); err != nil {
				return MsgActionOutput{Err: err}
			}
			return MsgActionOutput{Output: "esbuild " + action + "ed"}
		}
	}

	// Determine the effective manager for this worktree type
	manager := "pm2"
	if cfg != nil {
		if wt.Type == worktree.TypeDocker {
			manager = cfg.DockerServiceManager()
		} else {
			manager = cfg.ServiceManager()
		}
	}

	if manager != "pm2" {
		// Static manager doesn't support per-service actions
		return func() tea.Msg {
			return MsgActionOutput{Output: "Per-service actions not available for static services"}
		}
	}

	return func() tea.Msg {
		pm2_target := svc.Name
		if pm2_target == "__all" {
			pm2_target = "all"
		}

		var out string
		var err error
		if wt.Type == worktree.TypeDocker {
			out, err = run_docker_cmd("exec", wt.Container, "pm2", action, pm2_target)
		} else if wt.IsolatedPM2 {
			env := pm2.HomeEnv(wt.PM2Home())
			if action == "start" {
				// Use the project's own ecosystem config (same one pnpm dev uses)
				ecosystem := ""
				if cfg != nil {
					ecosystem = cfg.PM2EcosystemConfig()
				}
				if ecosystem == "" {
					ecosystem = "ecosystem.dev.config.js"
				}
				eco_path := filepath.Join(wt.Path, ecosystem)
				debug_log("[service_action] start via ecosystem: %s --only %s", eco_path, pm2_target)
				out, err = run_host_cmd_env_dir(wt.Path, env, "pm2", "start", eco_path, "--only", pm2_target, "--update-env")
			} else {
				out, err = run_host_cmd_env_dir(wt.Path, env, "pm2", action, pm2_target)
			}
		} else {
			out, err = run_host_cmd("pm2", action, pm2_target)
		}
		debug_log("[service_action] %s %s: out=%q err=%v", action, pm2_target, out, err)
		return MsgActionOutput{Output: out, Err: err}
	}
}

func run_docker_cmd(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func run_host_cmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func run_host_cmd_env(env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func run_host_cmd_env_dir(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func last_line(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

func (m Model) refresh_services() tea.Cmd {
	wt := m.selected_worktree()
	if wt == nil {
		debug_log("[services] refresh_services: no selected worktree (cursor=%d, len=%d)", m.cursor, len(m.worktrees))
		return nil
	}
	if !wt.Running {
		debug_log("[services] refresh_services: %s not running (type=%v)", wt.Alias, wt.Type)
		return nil
	}
	debug_log("[services] refresh_services: %s type=%v running=%v", wt.Alias, wt.Type, wt.Running)
	if wt.Type == worktree.TypeDocker {
		return cmd_fetch_services(*wt, m.cfg)
	}
	return cmd_fetch_local_services(*wt, m.cfg)
}

// focus_worktrees_if_empty switches focus back to the worktrees panel and
// returns tmux focus to the left pane when no terminal tabs remain open.
func (m *Model) focus_worktrees_if_empty() {
	if m.term_mgr.Count() == 0 {
		m.focus = PanelWorktrees
		if m.pane_layout != nil {
			m.pane_layout.FocusLeft()
		}
	}
}

// close_dev_tabs closes the dev server and create wizard tabs for a worktree.
func (m *Model) close_dev_tabs(alias string) {
	m.term_mgr.CloseByLabel(labels.Tab(labels.Dev, alias))
	m.term_mgr.CloseByLabel(labels.Tab(labels.Create, alias))
	m.term_mgr.CloseByLabel(labels.Create)
}

// close_worktree_logs closes all log tabs scoped to a worktree.
// Per-service labels are "Logs — alias/svc", all-logs label is "Logs — alias".
func (m *Model) close_worktree_logs(wt worktree.Worktree) {
	m.term_mgr.CloseByLabel(labels.Tab(labels.Logs, wt.Alias))
	m.term_mgr.CloseByPrefix(labels.Tab(labels.Logs, wt.Alias+"/"))
}

func tick_after(d time.Duration, kind string) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return MsgTick{Kind: kind}
	})
}

// close_preview closes the preview session and restores the right pane.
func (m *Model) close_preview() {
	if m.preview_session == nil {
		return
	}
	// Restore the manager's active session in the right pane.
	// ReturnSession/ShowSession swaps the preview pane back to its background
	// window, then brings the guide or active managed session into the viewport.
	if m.pane_layout != nil {
		active := m.term_mgr.Active()
		if active != nil {
			m.pane_layout.ShowSession(active.Window())
		} else {
			m.pane_layout.ReturnSession()
		}
	}
	go m.preview_session.Close()
	m.preview_session = nil
	m.preview_svc_name = ""
}

func (m *Model) open_preview_logs(wt worktree.Worktree, svc worktree.Service) tea.Cmd {
	if m.preview_svc_name == svc.Name {
		return nil
	}

	// For static manager on local worktrees, preview is not available
	// (all output goes to the Dev tab)
	manager := "pm2"
	if m.cfg != nil {
		if wt.Type == worktree.TypeDocker {
			manager = m.cfg.DockerServiceManager()
		} else {
			manager = m.cfg.ServiceManager()
		}
	}
	if manager == "static" && wt.Type == worktree.TypeLocal {
		return nil
	}

	var cmd_name string
	var args []string
	var dir string

	if wt.Type == worktree.TypeDocker && manager == "static" {
		cmd_name = "docker"
		if svc.Name == "__all" {
			args = []string{"logs", "-f", "--tail", "80", wt.Container}
		} else {
			container := container_for_service(wt, svc.Name, m.cfg)
			args = []string{"logs", "-f", "--tail", "80", container}
		}
	} else if wt.Type == worktree.TypeDocker {
		cmd_name = "docker"
		if svc.Name == "__all" {
			args = []string{"exec", "-it", wt.Container, "pm2", "logs", "--lines", "80"}
		} else {
			args = []string{"exec", "-it", wt.Container, "pm2", "logs", svc.Name, "--lines", "80"}
		}
	} else {
		dir = wt.Path
		if svc.Name == "esbuild" {
			cmd_name = "tail"
			args = []string{"-f", "-n", "80", esbuild.LogPath(wt.PM2Home())}
		} else if wt.IsolatedPM2 {
			pm2_home := wt.PM2Home()
			cmd_name = "bash"
			if svc.Name == "__all" {
				args = []string{"-c", fmt.Sprintf("PM2_HOME=%s exec pm2 logs --lines 80", pm2_home)}
			} else {
				target := m.pm2_log_target(svc, wt)
				args = []string{"-c", fmt.Sprintf("PM2_HOME=%s exec pm2 logs %s --lines 80", pm2_home, target)}
			}
		} else {
			cmd_name = "pm2"
			if svc.Name == "__all" {
				args = []string{"logs", "--lines", "80"}
			} else {
				args = []string{"logs", svc.Name, "--lines", "80"}
			}
		}
	}

	// If a preview is already open, respawn the command in the same pane.
	// This avoids pane swapping and the guide screen flashing between transitions.
	if m.preview_session != nil {
		m.preview_session.Respawn(cmd_name, args, dir)
		m.preview_svc_name = svc.Name
		return tick_after(100*time.Millisecond, "render")
	}

	// First preview: no existing session to clean up.
	w, h := m.right_pane_dimensions()
	s, err := terminal.NewSession(0, "preview", cmd_name, args, w, h, dir, m.term_mgr.Server())
	if err != nil {
		m.activity = fmt.Sprintf("Preview failed: %v", err)
		return nil
	}
	m.preview_session = s
	m.preview_svc_name = svc.Name

	if m.pane_layout != nil {
		m.pane_layout.ShowSession(s.Window())
	}

	return tick_after(100*time.Millisecond, "render")
}

// alt_tab_number returns the tab number (1-9) for an Alt+N key press, or 0 if not a tab shortcut.
// tab_number extracts a 1-9 number from a key message (plain or Alt+N).
func tab_number(msg tea.KeyMsg) int {
	for _, r := range msg.Runes {
		if r >= '1' && r <= '9' {
			return int(r - '0')
		}
	}
	return 0
}

func (m Model) handle_help_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Escape), key.Matches(msg, Keys.Help),
		key.Matches(msg, Keys.Quit), key.Matches(msg, Keys.CtrlC):
		m.help_open = false
		m.term_mgr.CloseByLabel(labels.Help)
	}
	return m, nil
}

func (m Model) open_help() (Model, tea.Cmd) {
	// If help is already open, close it (toggle)
	if m.help_open {
		m.help_open = false
		m.term_mgr.CloseByLabel(labels.Help)
		return m, nil
	}

	w, h := m.right_pane_dimensions()

	exe, err := os.Executable()
	if err != nil {
		return m, nil
	}
	exe, _ = filepath.EvalSymlinks(exe)

	_, err = m.term_mgr.Open(labels.Help, exe, []string{"_help"}, w, h, "")
	if err != nil {
		return m, nil
	}

	m.help_open = true
	m.prev_focus = m.focus
	m.focus = PanelTerminal
	return m, nil
}

func (m Model) handle_confirm_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Escape), key.Matches(msg, Keys.CtrlC):
		m.confirm_open = false
		m.confirm_prompt = ""
		m.confirm_action = nil
		return m, nil

	case key.Matches(msg, Keys.Enter):
		if m.confirm_action != nil {
			cb := m.confirm_action
			m.confirm_open = false
			m.confirm_prompt = ""
			m.confirm_action = nil
			return cb(&m)
		}
		m.confirm_open = false
		return m, nil
	}

	return m, nil
}

func (m Model) handle_input_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Escape):
		m.input_active = false
		m.input_prompt = ""
		m.input_value = ""
		m.input_callback = nil
		return m, nil

	case key.Matches(msg, Keys.Enter):
		if m.input_callback != nil {
			cb := m.input_callback
			val := m.input_value
			m.input_active = false
			m.input_prompt = ""
			m.input_value = ""
			m.input_callback = nil
			return m, cb(val)
		}
		m.input_active = false
		return m, nil

	case msg.Type == tea.KeyBackspace:
		if len(m.input_value) > 0 {
			m.input_value = m.input_value[:len(m.input_value)-1]
		}
		return m, nil

	case msg.Type == tea.KeyRunes:
		m.input_value += string(msg.Runes)
		return m, nil
	}

	return m, nil
}

func (m *Model) start_input(prompt string, callback func(string) tea.Cmd) {
	m.input_active = true
	m.input_prompt = prompt
	m.input_value = ""
	m.input_callback = callback
	m.result_text = ""
}

func (m *Model) show_result(text string) tea.Cmd {
	m.result_text = text
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return MsgResultClear{}
	})
}

// open_worktree_info focuses the Details panel which shows all info.
func (m Model) open_worktree_info() (Model, tea.Cmd) {
	m.prev_focus = m.focus
	m.focus = PanelDetails
	m.details_scroll = 0
	return m, nil
}

// start_dev_server starts PM2 and esbuild as independent daemons for a local worktree.
// Both survive dashboard close. On reopen, discovery detects them via PM2 status and PID files.
func (m Model) start_dev_server(wt worktree.Worktree) (Model, tea.Cmd) {
	debug_log("[services] start_dev_server: alias=%s path=%s isolated=%v", wt.Alias, wt.Path, wt.IsolatedPM2)

	// Reload AWS credentials so PM2 processes get the latest keys
	reload_aws_credentials()

	ecosystem_config := filepath.Join(wt.Path, "ecosystem.worktree.config.js")
	if _, err := os.Stat(ecosystem_config); os.IsNotExist(err) {
		debug_log("[services] start_dev_server: ecosystem config not found at %s", ecosystem_config)
		m.activity = fmt.Sprintf("error: ecosystem.worktree.config.js not found for %s", wt.Alias)
		return m, nil
	}

	// Build env vars for PM2
	var pm2_home string
	if wt.IsolatedPM2 {
		pm2_home = wt.PM2Home()
	}

	// Start PM2 daemon
	out, err := pm2.Start(pm2_home, ecosystem_config, wt.Path, nil)
	if err != nil {
		debug_log("[services] start_dev_server: PM2 start failed: %v (output: %s)", err, out)
		m.activity = fmt.Sprintf("PM2 start failed: %v", err)
		return m, nil
	}
	debug_log("[services] start_dev_server: PM2 started")

	// Start esbuild watcher as daemon
	build_script := ""
	if m.cfg != nil && m.cfg.Paths.BuildScript != "" {
		build_script = filepath.Join(wt.Path, m.cfg.Paths.BuildScript)
	}
	if build_script != "" {
		state_dir := wt.PM2Home()
		extra_env := build_esbuild_env(wt, m.cfg)
		if err := esbuild.Start(build_script, wt.Path, state_dir, extra_env); err != nil {
			debug_log("[services] start_dev_server: esbuild start failed: %v", err)
			// Non-fatal — PM2 is running, esbuild can be started manually
			m.activity = fmt.Sprintf("started %s (esbuild failed: %v)", wt.Alias, err)
		} else {
			debug_log("[services] start_dev_server: esbuild started")
		}
	}

	m.terminal_output = ""
	m.activity = fmt.Sprintf("started %s", wt.Alias)

	return m, tea.Batch(
		tick_after(100*time.Millisecond, "render"),
		tick_after(3*time.Second, "status"),
	)
}

// build_esbuild_env returns env vars needed to run the esbuild watcher.
func build_esbuild_env(wt worktree.Worktree, cfg *config.Config) []string {
	env := []string{"NODE_ENV=development"}
	if cfg != nil {
		if env_var := cfg.EnvVar("environment"); env_var != "" {
			env = append(env, fmt.Sprintf("%s=development", env_var))
		}
		if path_var := cfg.EnvVar("projectPath"); path_var != "" {
			env = append(env, fmt.Sprintf("%s=%s", path_var, wt.Path))
		}
	}
	env = append(env, fmt.Sprintf("WORKTREE_PORT_OFFSET=%d", wt.Offset))
	env = append(env, fmt.Sprintf("WORKTREE_NAME=%s", wt.Name))
	return env
}

// stop_dev_server stops PM2 services for a local worktree (with confirmation)
func (m Model) stop_dev_server(wt worktree.Worktree) (Model, tea.Cmd) {
	m.confirm_open = true
	m.confirm_prompt = fmt.Sprintf("Stop dev server on %s?", wt.Alias)
	m.confirm_action = func(mdl *Model) (Model, tea.Cmd) {
		return mdl.run_stop_dev_server(wt)
	}
	return m, nil
}

func (m Model) run_stop_dev_server(wt worktree.Worktree) (Model, tea.Cmd) {
	manager := "pm2"
	if m.cfg != nil {
		manager = m.cfg.ServiceManager()
	}
	debug_log("[services] run_stop_dev_server: alias=%s manager=%s", wt.Alias, manager)

	// Close the dev server terminal session if it exists
	m.close_dev_tabs(wt.Alias)
	// Close any open service log tabs for this worktree
	m.close_worktree_logs(wt)
	m.close_preview()
	if m.term_mgr.Count() == 0 && m.focus == PanelTerminal {
		m.focus = PanelWorktrees
	}

	if manager != "pm2" {
		// For non-pm2 managers, closing the dev tab + killing processes is sufficient
		return m, tea.Sequence(
			func() tea.Msg {
				return MsgActionStarted{WtName: wt.Name, Status: "stopping..."}
			},
			func() tea.Msg {
				kill_local_dev_processes(wt.Path)
				return MsgActionOutput{}
			},
		)
	}

	// Stop esbuild watcher daemon if running
	esbuild.Stop(wt.PM2Home())

	// Check for isolated PM2_HOME
	if wt.IsolatedPM2 {
		return m, tea.Sequence(
			func() tea.Msg {
				return MsgActionStarted{WtName: wt.Name, Status: "stopping..."}
			},
			func() tea.Msg {
				out, err := run_host_cmd_env(pm2.HomeEnv(wt.PM2Home()), "pm2", "kill")
				return MsgActionOutput{Output: out, Err: err}
			},
		)
	}

	svc_names := make([]string, 0, len(m.services))
	for _, svc := range m.services {
		if svc.Name != "__all" {
			svc_names = append(svc_names, svc.Name)
		}
	}

	return m, tea.Sequence(
		func() tea.Msg {
			return MsgActionStarted{WtName: wt.Name, Status: "stopping..."}
		},
		func() tea.Msg {
			var last_err error
			var last_out string
			for _, name := range svc_names {
				out, err := run_host_cmd("pm2", "delete", name)
				if err != nil {
					last_err = err
					last_out = out
				}
			}
			return MsgActionOutput{Output: last_out, Err: last_err}
		},
	)
}

// restart_local_services kills and restarts a local worktree's dev server
// so it picks up fresh environment (e.g. updated AWS keys).
func (m Model) restart_local_services(wt worktree.Worktree) (Model, tea.Cmd) {
	debug_log("[aws] restart_local_services: %s (path=%s)", wt.Alias, wt.Path)
	m.activity = fmt.Sprintf("Restarting %s...", wt.Alias)

	// Kill OS-level node processes for this worktree
	debug_log("[aws] restart_local_services: killing dev processes")
	kill_local_dev_processes(wt.Path)

	// Close any existing terminal tabs for this worktree
	m.close_dev_tabs(wt.Alias)

	// Start a fresh dev server (reload_aws_credentials is called inside)
	return m.start_dev_server(wt)
}

// is_static_local returns true if the worktree uses the static service manager
// and is a local worktree (not Docker). Used to gate per-service actions.
func (m Model) is_static_local(wt worktree.Worktree) bool {
	if m.cfg == nil || wt.Type != worktree.TypeLocal {
		return false
	}
	return m.cfg.ServiceManager() == "static"
}

// find_dev_tab returns the label of the active dev/create tab for a worktree,
// or "" if none is found. The dev server may run under "Dev — alias",
// "Create — alias", or just "Create" (when dc-create starts the dev server inline).
func find_dev_tab(m Model, wt worktree.Worktree) string {
	for _, label := range []string{
		labels.Tab(labels.Dev, wt.Alias),
		labels.Tab(labels.Create, wt.Alias),
		labels.Create,
	} {
		if m.term_mgr.HasLabel(label) {
			return label
		}
	}
	return ""
}

// has_create_alias_tab checks if any "Create — {alias}" tab exists
func (m Model) has_create_alias_tab() bool {
	for _, s := range m.term_mgr.Sessions() {
		if strings.HasPrefix(s.Label, labels.Create+labels.Sep) {
			return true
		}
	}
	return false
}

// container_for_service returns the Docker container name for a specific service.
// For shared compose, each service runs in its own container: {name}-{slug}-{service}.
// The worktree's Container field stores the primary service container; we swap the suffix.
func container_for_service(wt worktree.Worktree, svc_name string, cfg *config.Config) string {
	if cfg == nil || cfg.Services.Primary == "" {
		return wt.Container
	}
	primary := cfg.Services.Primary
	if strings.HasSuffix(wt.Container, "-"+primary) {
		return strings.TrimSuffix(wt.Container, primary) + svc_name
	}
	return wt.Container
}

// toggle_admin toggles admin access for the selected worktree (with confirmation)
func (m Model) toggle_admin() (tea.Model, tea.Cmd) {
	wt := m.selected_worktree()
	if wt == nil || !wt.Running || wt.Type != worktree.TypeDocker {
		m.activity = "Admin toggle requires a running Docker worktree"
		return m, nil
	}

	env_filename := ".env.worktree"
	if m.cfg != nil && m.cfg.Env.Filename != "" {
		env_filename = m.cfg.Env.Filename
	}
	admin_var := "ADMIN_ACCOUNTS"
	if m.cfg != nil {
		if v := m.cfg.EnvVar("adminAccounts"); v != "" {
			admin_var = v
		}
	}
	env_path := filepath.Join(wt.Path, env_filename)
	action := "set"
	env_data, _ := os.ReadFile(env_path)
	if strings.Contains(string(env_data), admin_var) {
		action = "unset"
	}

	m.confirm_open = true
	m.confirm_prompt = fmt.Sprintf("Admin %s on %s?", action, wt.Alias)
	m.confirm_action = func(mdl *Model) (Model, tea.Cmd) {
		return mdl.run_admin_toggle(*wt, action)
	}
	return m, nil
}

func (m Model) run_admin_toggle(wt worktree.Worktree, action string) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()
	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-admin.js")
	label := labels.Tab(labels.Admin+" "+strings.ToUpper(action[:1])+action[1:], wt.Alias)

	_, err := m.term_mgr.Open(label, "node", []string{script, action, "--name=" + wt.Name}, w, h, m.repo_root)
	if err != nil {
		m.activity = fmt.Sprintf("Failed: %v", err)
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus
	m.focus = PanelTerminal
	return m, tick_after(100*time.Millisecond, "render")
}

// toggle_lan toggles LAN access for the selected worktree (with confirmation)
func (m Model) toggle_lan() (tea.Model, tea.Cmd) {
	wt := m.selected_worktree()
	if wt == nil || !wt.Running || wt.Type != worktree.TypeDocker {
		m.activity = "LAN toggle requires a running Docker worktree"
		return m, nil
	}

	env_filename := ".env.worktree"
	if m.cfg != nil && m.cfg.Env.Filename != "" {
		env_filename = m.cfg.Env.Filename
	}
	lan_var := "LAN_DOMAIN"
	if m.cfg != nil {
		if v := m.cfg.EnvVar("lanDomain"); v != "" {
			lan_var = v
		}
	}
	env_path := filepath.Join(wt.Path, env_filename)
	action := "enable"
	env_data, _ := os.ReadFile(env_path)
	if strings.Contains(string(env_data), lan_var) {
		action = "disable"
	}

	m.confirm_open = true
	m.confirm_prompt = fmt.Sprintf("LAN %s on %s?", action, wt.Alias)
	m.confirm_action = func(mdl *Model) (Model, tea.Cmd) {
		return mdl.run_lan_toggle(*wt, action)
	}
	return m, nil
}

func (m Model) run_lan_toggle(wt worktree.Worktree, action string) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()
	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-lan.js")

	args := []string{script, wt.Name}
	label := labels.Tab(labels.LANOn, wt.Alias)
	if action == "disable" {
		args = append(args, "--off")
		label = labels.Tab(labels.LANOff, wt.Alias)
	}

	_, err := m.term_mgr.Open(label, "node", args, w, h, m.repo_root)
	if err != nil {
		m.activity = fmt.Sprintf("Failed: %v", err)
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus
	m.focus = PanelTerminal
	return m, tick_after(100*time.Millisecond, "render")
}

// toggle_skip_worktree toggles skip-worktree flags for the selected worktree (with confirmation).
// The gate check is done here (not in the key handler) so we can show activity messages.
func (m Model) toggle_skip_worktree() (tea.Model, tea.Cmd) {
	wt := m.selected_worktree()
	if wt == nil {
		m.activity = "Skip-worktree: no worktree selected"
		return m, nil
	}

	// Check config — the Go config gets the raw JS export, so Git.SkipWorktree
	// may be empty even when defaults exist. We still allow the toggle because
	// the Node script reads the merged config with defaults.
	has_config_paths := m.cfg != nil && len(m.cfg.Git.SkipWorktree) > 0
	if !has_config_paths {
		// Try detecting if the Node config has skip paths by checking the script exists
		script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-skip-worktree.js")
		if _, err := os.Stat(script); err != nil {
			m.activity = "Skip-worktree: dc-skip-worktree.js not found"
			return m, nil
		}
	}

	// Detect current state: check if any files have skip-worktree set
	action := "apply"
	cmd := exec.Command("git", "-C", wt.Path, "ls-files", "-v")
	out, err := cmd.Output()
	if err != nil {
		m.activity = fmt.Sprintf("Skip-worktree: git ls-files failed: %v", err)
		return m, nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "S ") {
			action = "remove"
			break
		}
	}

	m.confirm_open = true
	verb := "Apply"
	if action == "remove" {
		verb = "Remove"
	}
	m.confirm_prompt = fmt.Sprintf("%s skip-worktree on %s?", verb, wt.Alias)
	m.confirm_action = func(mdl *Model) (Model, tea.Cmd) {
		return mdl.run_skip_worktree_toggle(*wt, action)
	}
	return m, nil
}

func (m Model) run_skip_worktree_toggle(wt worktree.Worktree, action string) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()
	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-skip-worktree.js")

	if _, err := os.Stat(script); err != nil {
		m.activity = fmt.Sprintf("Skip-worktree: script not found: %s", script)
		return m, nil
	}

	args := []string{script, action, wt.Name}
	label := labels.Tab(labels.Skip, wt.Alias)

	m.activity = fmt.Sprintf("Running skip-worktree %s on %s...", action, wt.Alias)

	_, err := m.term_mgr.Open(label, "node", args, w, h, m.repo_root)
	if err != nil {
		m.activity = fmt.Sprintf("Skip-worktree failed: %v", err)
		return m, nil
	}

	m.skip_worktree_running = true
	m.terminal_output = ""
	m.prev_focus = m.focus
	m.focus = PanelTerminal
	return m, tick_after(100*time.Millisecond, "render")
}

// open_maintenance_picker shows the maintenance operations picker
func (m Model) open_maintenance_picker() (tea.Model, tea.Cmd) {
	m.picker_actions = ui.FilterMaintenanceActions(m.cfg)
	m.picker_cursor = 0
	m.picker_open = true
	m.picker_context = pickerMaintenance
	return m, nil
}

// execute_maintenance_action runs the selected maintenance operation
func (m Model) execute_maintenance_action(action ui.PickerAction) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()

	var args []string
	var label string

	switch action.Key {
	case "p":
		script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-prune.js")
		args = []string{script}
		label = labels.Prune
	case "s":
		script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-autostop.js")
		args = []string{script}
		label = labels.Autostop
	case "r":
		script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-rebuild-base.js")
		args = []string{script}
		label = labels.RebuildBase
	default:
		return m, nil
	}

	_, err := m.term_mgr.Open(label, "node", args, w, h, m.repo_root)
	if err != nil {
		m.activity = fmt.Sprintf("Failed: %v", err)
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus
	m.focus = PanelTerminal

	return m, tick_after(100*time.Millisecond, "render")
}

// resolve_sso_action executes the deferred action after SSO session is confirmed valid.
func (m Model) resolve_sso_action() (tea.Model, tea.Cmd) {
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
	}
	// No pending action (manual Shift+A) — show timed alert
	white := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("34")).Bold(true)
	m.alert_open = true
	m.alert_message = white.Render("AWS session is already ") + green.Render("VALID")
	m.alert_countdown = 3
	return m, tick_after(1*time.Second, "alert")
}

// sso_profile returns the configured SSO profile, or empty string.
func (m Model) sso_profile() string {
	if m.cfg == nil {
		return ""
	}
	return m.cfg.AwsSsoProfile()
}

// start_worktree dispatches to the appropriate start method based on worktree type.
func (m Model) start_worktree(wt worktree.Worktree) (Model, tea.Cmd) {
	if wt.Type == worktree.TypeLocal {
		return m.start_dev_server(wt)
	}
	if wt.HostBuild {
		return m.start_host_build(wt)
	}
	if wt.Type == worktree.TypeDocker {
		return m, cmd_docker_action("start", wt, m.repo_root, m.cfg)
	}
	return m, nil
}

// check_sso_session runs `aws sts get-caller-identity` to verify the SSO session.
// Returns true if the session is valid, false if expired or error.
func check_sso_session(profile string) bool {
	cmd := exec.Command("aws", "sts", "get-caller-identity", "--profile", profile, "--output", "json")
	cmd.Env = os.Environ()
	err := cmd.Run()
	return err == nil
}

// MsgSsoSessionCheck is sent after an async SSO session check completes.
type MsgSsoSessionCheck struct {
	Valid bool
}

// check_sso_then_login checks the SSO session async, then opens login if expired.
func (m Model) check_sso_then_login() (tea.Model, tea.Cmd) {
	profile := m.cfg.AwsSsoProfile()
	debug_log("[aws] checking SSO session for profile=%s", profile)
	m.activity = "Checking AWS SSO session..."
	return m, func() tea.Msg {
		valid := check_sso_session(profile)
		return MsgSsoSessionCheck{Valid: valid}
	}
}

// open_sso_login opens `aws sso login --profile X` in a terminal tab.
func (m Model) open_sso_login() (tea.Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()
	profile := m.cfg.AwsSsoProfile()
	sentinel_path := sentinel.Path(sentinel.AWSKeys)

	sentinel.Clear(sentinel.AWSKeys)
	debug_log("[aws] open_sso_login: profile=%s", profile)

	shell_cmd := fmt.Sprintf("aws sso login --profile %s; echo $? > %s", profile, sentinel_path)
	label := labels.AWSKeys
	_, err := m.term_mgr.Open(label, "bash", []string{"-c", shell_cmd}, w, h, m.repo_root)
	if err != nil {
		debug_log("[aws] open_sso_login: FAILED: %v", err)
		m.activity = fmt.Sprintf("Failed to open SSO login: %v", err)
		return m, nil
	}

	debug_log("[aws] open_sso_login: terminal opened")
	m.aws_keys_running = true
	m.terminal_output = ""
	m.prev_focus = m.focus
	m.focus = PanelTerminal
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}

	return m, tick_after(100*time.Millisecond, "render")
}

// open_aws_keys runs the aws-keys.js paste script in a terminal session.
// The render tick detects when the session exits and triggers service restarts.
func (m Model) open_aws_keys() (tea.Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()
	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "aws-keys.js")

	sentinel.Clear(sentinel.AWSKeys)
	debug_log("[aws] open_aws_keys: paste mode, script=%s", script)

	label := labels.AWSKeys
	_, err := m.term_mgr.Open(label, "node", []string{script}, w, h, m.repo_root)
	if err != nil {
		debug_log("[aws] open_aws_keys: FAILED: %v", err)
		m.activity = fmt.Sprintf("Failed to open AWS keys: %v", err)
		return m, nil
	}

	debug_log("[aws] open_aws_keys: terminal opened")
	m.aws_keys_running = true
	m.terminal_output = ""
	m.prev_focus = m.focus
	m.focus = PanelTerminal
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}

	return m, tick_after(100*time.Millisecond, "render")
}

func (m Model) play_heihei() (tea.Model, tea.Cmd) {
	if len(m.heihei_audio) == 0 || m.heihei_playing {
		return m, nil
	}

	// Write embedded audio to a temp file (once, reuse on subsequent calls)
	if m.heihei_tmpfile == "" {
		tmp, err := os.CreateTemp("", "wt-heihei-*.mp3")
		if err != nil {
			return m, nil
		}
		if _, err := tmp.Write(m.heihei_audio); err != nil {
			tmp.Close()
			_ = os.Remove(tmp.Name())
			return m, nil
		}
		tmp.Close()
		m.heihei_tmpfile = tmp.Name()
	}

	// Remove stale sentinel before opening
	sentinel.Clear(sentinel.HeiHei)

	exe, err := os.Executable()
	if err != nil {
		return m, nil
	}
	exe, _ = filepath.EvalSymlinks(exe)

	w, h := m.right_pane_dimensions()
	_, err = m.term_mgr.Open(labels.HeiHei, exe, []string{"_heihei", m.heihei_tmpfile}, w, h, "")
	if err != nil {
		return m, nil
	}

	m.heihei_playing = true
	m.terminal_output = ""
	m.prev_focus = m.focus
	m.focus = PanelTerminal
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}

	return m, tick_after(100*time.Millisecond, "render")
}

func (m Model) toggle_details() (tea.Model, tea.Cmd) {
	m.details_visible = !m.details_visible
	m.recalc_layout()

	// If details was hidden and focus was on it, move to services
	if !m.details_visible && m.focus == PanelDetails {
		m.focus = PanelServices
	}
	return m, nil
}

func (m Model) toggle_usage() (tea.Model, tea.Cmd) {
	m.usage_visible = !m.usage_visible
	m.recalc_layout()

	if !m.usage_visible {
		return m, nil
	}

	// Fire async fetch — cmd_fetch_usage handles token acquisition if needed.
	// The MsgUsageUpdated handler schedules the next 60s tick, so no tick here
	// (avoids duplicate tick chains on rapid toggle).
	// Start spinner while loading.
	return m, tea.Batch(cmd_fetch_usage(m.usage_token), tick_after(80*time.Millisecond, "spin"))
}

// panel_visible returns whether a panel should be included in cycling.
func (m *Model) panel_visible(p Panel) bool {
	switch p {
	case PanelDetails:
		return m.details_visible
	case PanelTasks:
		return m.tasks_visible
	default:
		return true
	}
}

// next_panel cycles focus forward, skipping hidden panels.
func (m *Model) next_panel() {
	for i := 0; i < PanelCount; i++ {
		m.focus = (m.focus + 1) % PanelCount
		if m.panel_visible(m.focus) {
			return
		}
	}
}

// prev_panel cycles focus backward, skipping hidden panels.
func (m *Model) prev_panel() {
	for i := 0; i < PanelCount; i++ {
		m.focus = (m.focus - 1 + PanelCount) % PanelCount
		if m.panel_visible(m.focus) {
			return
		}
	}
}

// running_base_names returns a set of base service names (without worktree suffix)
// from the currently fetched m.services. PM2 names like "app-feat-test" become "app".
func (m *Model) running_base_names(alias string) map[string]bool {
	running := make(map[string]bool)
	suffix := ""
	if alias != "" {
		suffix = "-" + alias
	}
	for _, svc := range m.services {
		if svc.Status != "online" {
			continue
		}
		name := svc.Name
		if suffix != "" && strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
		}
		running[name] = true
	}
	return running
}

// pm2_log_target returns the PM2 process name(s) to pass to `pm2 logs` for a service.
// For isolated PM2, process names are suffixed with the worktree alias.
// For multi-process services (e.g. sync -> combined_sync, listings_sync), returns a
// regex pattern so pm2 logs shows all matching processes.
func (m Model) pm2_log_target(svc worktree.Service, wt worktree.Worktree) string {
	names := []string{svc.Name}
	if m.cfg != nil {
		for _, entry := range m.cfg.Dash.Services.List {
			if entry.Name == svc.Name {
				names = pm2_process_names(entry, wt.Name)
				break
			}
		}
	} else if wt.IsolatedPM2 && wt.Name != "" {
		names = []string{svc.Name + "-" + wt.Name}
	}

	if len(names) == 1 {
		return names[0]
	}
	// Multiple processes: use regex pattern for pm2 logs
	return "/" + strings.Join(names, "|") + "/"
}

// pm2_process_names returns the PM2 process names for a config service entry,
// namespaced with the worktree alias (e.g. "app" -> "app-feat-test").
func pm2_process_names(entry config.DashServiceEntry, alias string) []string {
	bases := entry.BaseProcesses()
	if alias == "" {
		return bases
	}
	names := make([]string, len(bases))
	for i, b := range bases {
		names[i] = b + "-" + alias
	}
	return names
}

func (m Model) open_start_service_picker(wt worktree.Worktree) (Model, tea.Cmd) {
	return m.open_service_picker(wt, pickerStartService)
}

func (m Model) open_stop_service_picker(wt worktree.Worktree) (Model, tea.Cmd) {
	return m.open_service_picker(wt, pickerStopService)
}

// open_service_picker builds a picker of services filtered by state.
// For pickerStartService: shows services with any stopped process.
// For pickerStopService: shows services with any running process.
func (m Model) open_service_picker(wt worktree.Worktree, mode string) (Model, tea.Cmd) {
	debug_log("[svc_picker] open: mode=%s alias=%s services=%d cfg=%v", mode, wt.Alias, len(m.services), m.cfg != nil)
	if m.cfg == nil || len(m.cfg.Dash.Services.List) == 0 {
		m.activity = "No services configured"
		return m, nil
	}

	running := m.running_base_names(wt.Alias)
	want_running := mode == pickerStopService

	var actions []ui.PickerAction
	idx := 0
	for _, entry := range m.cfg.Dash.Services.List {
		match := false
		for _, b := range entry.BaseProcesses() {
			if running[b] == want_running {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		if idx >= 26 {
			break
		}
		key := string(rune('a' + idx))
		actions = append(actions, ui.PickerAction{
			Key:   key,
			Label: entry.Name,
			Desc:  fmt.Sprintf("port %d", entry.Port),
		})
		idx++
	}

	debug_log("[svc_picker] %s: %d services to offer", mode, len(actions))
	if len(actions) == 0 {
		if want_running {
			m.activity = "No services are running"
		} else {
			m.activity = "All services are already running"
		}
		return m, nil
	}

	m.picker_actions = actions
	m.picker_cursor = 0
	m.picker_open = true
	m.picker_context = mode
	return m, nil
}

func (m Model) execute_start_service_action(action ui.PickerAction) (Model, tea.Cmd) {
	debug_log("[start_svc] execute: label=%s key=%s", action.Label, action.Key)
	wt := m.selected_worktree()
	if wt == nil {
		return m, nil
	}

	// Find the config entry to get the actual PM2 process names
	var pm2_names []string
	for _, entry := range m.cfg.Dash.Services.List {
		if entry.Name == action.Label {
			pm2_names = pm2_process_names(entry, wt.Name)
			break
		}
	}

	if len(pm2_names) == 0 {
		return m, nil
	}

	m.activity = fmt.Sprintf("Starting %s...", action.Label)

	// For isolated PM2, regenerate ecosystem once then start all processes
	if wt.IsolatedPM2 {
		return m, cmd_start_isolated_services(*wt, pm2_names, m.cfg)
	}

	// Non-isolated: start each process individually
	var cmds []tea.Cmd
	for _, name := range pm2_names {
		svc := worktree.Service{Name: name, DisplayName: name}
		cmds = append(cmds, cmd_service_action("start", *wt, svc, m.cfg))
	}
	return m, tea.Batch(cmds...)
}

// cmd_start_isolated_services regenerates the ecosystem config once, then
// starts all named processes via pm2 --only.
func cmd_start_isolated_services(wt worktree.Worktree, pm2_names []string, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		// Use the project's own ecosystem config (same one pnpm dev uses)
		eco_name := ""
		if cfg != nil {
			eco_name = cfg.PM2EcosystemConfig()
		}
		if eco_name == "" {
			eco_name = "ecosystem.dev.config.js"
		}
		ecosystem := filepath.Join(wt.Path, eco_name)
		env := pm2.HomeEnv(wt.PM2Home())

		var last_out string
		var last_err error
		for _, name := range pm2_names {
			debug_log("[start_svc] start via ecosystem: %s --only %s", ecosystem, name)
			last_out, last_err = run_host_cmd_env_dir(wt.Path, env, "pm2", "start", ecosystem, "--only", name, "--update-env")
			debug_log("[start_svc] start %s: out=%q err=%v", name, last_out, last_err)
		}
		return MsgActionOutput{Output: last_out, Err: last_err}
	}
}

func (m Model) execute_stop_service_action(action ui.PickerAction) (Model, tea.Cmd) {
	debug_log("[stop_svc] execute: label=%s key=%s", action.Label, action.Key)
	wt := m.selected_worktree()
	if wt == nil {
		return m, nil
	}

	var pm2_names []string
	for _, entry := range m.cfg.Dash.Services.List {
		if entry.Name == action.Label {
			pm2_names = pm2_process_names(entry, wt.Name)
			break
		}
	}

	if len(pm2_names) == 0 {
		return m, nil
	}

	m.activity = fmt.Sprintf("Stopping %s...", action.Label)

	var cmds []tea.Cmd
	for _, name := range pm2_names {
		svc := worktree.Service{Name: name, DisplayName: name}
		cmds = append(cmds, cmd_service_action("stop", *wt, svc, m.cfg))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) open_db_picker() (tea.Model, tea.Cmd) {
	wt := m.selected_worktree()
	if wt == nil || !wt.Running || wt.Type != worktree.TypeDocker {
		m.activity = "Database ops require a running Docker worktree"
		return m, nil
	}

	m.picker_actions = ui.FilterDatabaseActions(m.cfg)
	m.picker_cursor = 0
	m.picker_open = true
	m.picker_context = pickerDB
	return m, nil
}

// execute_db_action runs the selected database operation
func (m Model) execute_db_action(action ui.PickerAction) (Model, tea.Cmd) {
	wt := m.selected_worktree()
	if wt == nil {
		return m, nil
	}

	w, h := m.right_pane_dimensions()
	scripts_dir := flow_scripts_dir(m.repo_root, m.cfg)
	seed_script := filepath.Join(scripts_dir, "dc-seed.js")
	fix_script := filepath.Join(scripts_dir, "dc-images-fix.js")

	var cmd_name string
	var args []string
	var label string

	switch action.Key {
	case "s":
		cmd_name = "node"
		args = []string{seed_script, wt.Name}
		label = labels.Tab(labels.DBSeed, wt.Alias)
	case "d":
		cmd_name = "node"
		args = []string{seed_script, wt.Name, "--drop"}
		label = labels.Tab(labels.DBDrop, wt.Alias)
	case "r":
		cmd_name = "node"
		args = []string{seed_script, wt.Name, "--reset"}
		label = labels.Tab(labels.DBReset, wt.Alias)
	case "f":
		db_name := ""
		if m.cfg != nil {
			db_name = m.cfg.DbName(wt.Alias)
		}
		if db_name == "" {
			db_name = "db_" + wt.Alias
		}
		cmd_name = "node"
		args = []string{fix_script, "--db=" + db_name}
		label = labels.Tab(labels.FixImages, wt.Alias)
	default:
		return m, nil
	}

	_, err := m.term_mgr.Open(label, cmd_name, args, w, h, m.repo_root)
	if err != nil {
		m.activity = fmt.Sprintf("Failed: %v", err)
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus
	m.focus = PanelTerminal

	return m, tick_after(100*time.Millisecond, "render")
}

// open_esbuild_watch opens a terminal tab running dc-build.js for a host-build worktree
func (m Model) open_esbuild_watch(wt worktree.Worktree) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()

	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-build.js")
	label := labels.Tab(labels.Build, wt.Alias)
	shell_cmd := fmt.Sprintf("node %q %s", script, wt.Name)

	_, err := m.term_mgr.Open(label, "bash", []string{"-c", shell_cmd}, w, h, m.repo_root)
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to start esbuild watch: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal
	// Focus the right pane for native terminal interaction
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}
	return m, tick_after(100*time.Millisecond, "render")
}

// start_host_build starts the Docker container and then opens esbuild watch on the host
func (m Model) start_host_build(wt worktree.Worktree) (Model, tea.Cmd) {
	m.activity = fmt.Sprintf("starting... %s", wt.Alias)

	return m, tea.Sequence(
		func() tea.Msg {
			return MsgActionStarted{WtName: wt.Name, Status: "starting..."}
		},
		func() tea.Msg {
			out, err := run_worktree_up(wt, m.repo_root, m.cfg)
			if err != nil {
				return MsgActionOutput{Output: out, Err: err}
			}
			return MsgOpenBuildAfterStart{WtName: wt.Name}
		},
	)
}

// restart_host_build restarts the Docker container and reopens the esbuild watch tab
func (m Model) restart_host_build(wt worktree.Worktree) (Model, tea.Cmd) {
	build_label := labels.Tab(labels.Build, wt.Alias)
	m.term_mgr.CloseByLabel(build_label)
	debug_log("[restart] host-build %s: closed esbuild tab, restarting docker", wt.Alias)

	return m, tea.Sequence(
		func() tea.Msg {
			return MsgActionStarted{WtName: wt.Name, Status: "restarting..."}
		},
		func() tea.Msg {
			out, err := run_docker("restart", wt.Container)
			if err != nil {
				return MsgActionOutput{Output: out, Err: err}
			}
			return MsgOpenBuildAfterStart{WtName: wt.Name}
		},
	)
}

// stop_host_build closes the esbuild watch session and stops the Docker container
func (m Model) stop_host_build(wt worktree.Worktree) (Model, tea.Cmd) {
	build_label := labels.Tab(labels.Build, wt.Alias)
	m.term_mgr.CloseByLabel(build_label)
	if m.term_mgr.Count() == 0 && m.focus == PanelTerminal {
		m.focus = PanelWorktrees
	}

	return m, cmd_docker_action("stop", wt, m.repo_root, m.cfg)
}

// remove_worktree opens a picker to choose removal mode
func (m Model) remove_worktree(wt worktree.Worktree) (Model, tea.Cmd) {
	m.picker_open = true
	m.picker_cursor = 0
	m.picker_actions = ui.RemoveActions
	m.picker_context = pickerRemove
	return m, nil
}

func (m Model) execute_remove_action(action ui.PickerAction) (Model, tea.Cmd) {
	wt := m.selected_worktree()
	if wt == nil {
		return m, nil
	}

	switch action.Key {
	case "n":
		return m.run_remove_worktree(*wt, false)
	case "f":
		return m.run_remove_worktree(*wt, true)
	}

	return m, nil
}

func (m Model) run_remove_worktree(wt worktree.Worktree, force bool) (Model, tea.Cmd) {
	// Close any terminal sessions for this worktree
	for _, prefix := range []string{labels.Shell, labels.Claude, labels.Logs, labels.Dev, labels.Build} {
		m.term_mgr.CloseByLabel(labels.Tab(prefix, wt.Alias))
	}
	if m.term_mgr.Count() == 0 && m.focus == PanelTerminal {
		m.focus = PanelWorktrees
	}

	m.services = nil
	m.service_cursor = 0
	m.close_preview()

	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-worktree-down.js")

	args := []string{script, wt.Name, "--remove"}
	if force {
		args = append(args, "--force")
	}

	return m, tea.Sequence(
		func() tea.Msg {
			return MsgActionStarted{WtName: wt.Name, Status: "removing..."}
		},
		func() tea.Msg {
			out, err := run_host_cmd("node", args...)
			return MsgActionOutput{Output: out, Err: err}
		},
	)
}

// --- Beads tasks panel ---

func (m Model) toggle_tasks() (tea.Model, tea.Cmd) {
	m.tasks_visible = !m.tasks_visible
	m.recalc_layout()

	if !m.tasks_visible {
		if m.focus == PanelTasks {
			m.focus = PanelServices
		}
		return m, nil
	}

	// Reset state, focus panel, and fetch
	m.tasks_cursor = 0
	m.tasks_detail = nil
	m.tasks_detail_scroll = 0
	m.tasks_err = nil
	m.focus = PanelTasks
	return m, cmd_fetch_tasks()
}

func (m Model) handle_tasks_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.tasks_detail != nil {
		return m.handle_tasks_detail_key(msg)
	}
	return m.handle_tasks_list_key(msg)
}

func (m Model) handle_tasks_list_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Up):
		if m.tasks_cursor > 0 {
			m.tasks_cursor--
		}
		return m, nil

	case key.Matches(msg, Keys.Down):
		if m.tasks_cursor < len(m.tasks_list)-1 {
			m.tasks_cursor++
		}
		return m, nil

	case key.Matches(msg, Keys.Enter):
		if m.tasks_cursor >= 0 && m.tasks_cursor < len(m.tasks_list) {
			id := m.tasks_list[m.tasks_cursor].ID
			return m, cmd_fetch_task_detail(id)
		}
		return m, nil
	}

	task := m.selected_task()
	if task == nil {
		return m, nil
	}

	switch msg.String() {
	case "c":
		id := task.ID
		title := task.Title
		m.confirm_open = true
		m.confirm_prompt = fmt.Sprintf("Close task %s?\n%s", id, title)
		m.confirm_action = func(mdl *Model) (Model, tea.Cmd) {
			return *mdl, func() tea.Msg {
				err := beads.CloseTask(id)
				return MsgTaskActionDone{Err: err}
			}
		}
		return m, nil
	case "d":
		id := task.ID
		title := task.Title
		m.confirm_open = true
		m.confirm_prompt = fmt.Sprintf("Delete task %s?\n%s", id, title)
		m.confirm_action = func(mdl *Model) (Model, tea.Cmd) {
			return *mdl, func() tea.Msg {
				err := beads.DeleteTask(id)
				return MsgTaskActionDone{Err: err}
			}
		}
		return m, nil
	}

	return m, nil
}

func (m Model) selected_task() *beads.Task {
	if m.tasks_cursor >= 0 && m.tasks_cursor < len(m.tasks_list) {
		t := m.tasks_list[m.tasks_cursor]
		return &t
	}
	return nil
}

func (m Model) handle_tasks_detail_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	max_scroll := 0
	if m.tasks_detail != nil {
		inner_h := m.layout.TasksHeight - 2
		total := ui.TasksContentHeight(m.tasks_list, m.tasks_detail)
		max_scroll = total - inner_h
		if max_scroll < 0 {
			max_scroll = 0
		}
	}

	switch {
	case key.Matches(msg, Keys.Up):
		if m.tasks_detail_scroll > 0 {
			m.tasks_detail_scroll--
		}
		return m, nil

	case key.Matches(msg, Keys.Down):
		if m.tasks_detail_scroll < max_scroll {
			m.tasks_detail_scroll++
		}
		return m, nil
	}

	return m, nil
}
