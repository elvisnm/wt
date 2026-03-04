package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/labels"
	"github.com/elvisnm/wt/internal/sentinel"
	"github.com/elvisnm/wt/internal/terminal"
	"github.com/elvisnm/wt/internal/ui"
	"github.com/elvisnm/wt/internal/worktree"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout = m.layout.Resize(msg.Width, msg.Height, m.usage_visible)
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
		return m, m.cmd_discover()

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
		case "spin":
			if m.activity != "" {
				m.spin_frame++
				return m, tick_after(80*time.Millisecond, "spin")
			}
			return m, nil
		case "render":
			// Check if dc-create finished (via sentinel file).
			// Always check the sentinel first — the pane may have already
			// been reaped as dead before the file becomes visible on disk.
			if sr := sentinel.Read(sentinel.Create); sr != nil {
				lines := strings.SplitN(sr.Raw, "\n", 2)
				exit_code := sr.ExitCode
				created_alias := ""
				if len(lines) > 1 {
					created_alias = strings.TrimSpace(lines[1])
				}
				debug_log("[create] sentinel found: exit_code=%d alias=%q", exit_code, created_alias)

				// Close all Create tabs
				m.term_mgr.CloseByLabel(labels.Create)
				for _, wt := range m.worktrees {
					m.term_mgr.CloseByLabel(labels.Tab(labels.Create, wt.Alias))
				}

				// Defer esbuild watch for host-build worktrees until after discovery
				// refreshes the worktree list (the new worktree may not be in
				// m.worktrees yet, or may not have HostBuild=true yet).
				if exit_code == 0 && created_alias != "" {
					m.pending_esbuild_alias = created_alias
				}

				m.focus_worktrees_if_empty()
				return m, tea.Batch(
					tick_after(100*time.Millisecond, "render"),
					m.cmd_discover(),
				)
			} else if m.term_mgr.HasLabel(labels.Create) || m.has_create_alias_tab() {
				if m.term_mgr.CloseDeadByPrefix(labels.Create) {
					// Create process died without writing sentinel (e.g. Ctrl+C)
					m.focus_worktrees_if_empty()
				}
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
					debug_log("[aws] SUCCESS: reloading credentials and restarting services")
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
					m.activity = "AWS keys updated — restarting services..."
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
		m.focus = (m.focus + 1) % PanelCount
		return m, nil

	case key.Matches(msg, Keys.ShiftTab):
		m.close_preview()
		m.focus = (m.focus - 1 + PanelCount) % PanelCount
		return m, nil

	case key.Matches(msg, Keys.Escape):
		if m.focus == PanelTerminal {
			m.focus = m.prev_focus
		} else if m.focus != PanelWorktrees {
			m.focus = PanelWorktrees
		}
		return m, nil

	case key.Matches(msg, Keys.TabPrev):
		m.close_preview()
		m.focus = (m.focus - 1 + PanelCount) % PanelCount
		return m, nil

	case key.Matches(msg, Keys.TabNext):
		m.close_preview()
		m.focus = (m.focus + 1) % PanelCount
		return m, nil

	case key.Matches(msg, Keys.PanelLeft):
		m.close_preview()
		m.focus = (m.focus - 1 + PanelCount) % PanelCount
		return m, nil

	case key.Matches(msg, Keys.PanelRight):
		m.close_preview()
		m.focus = (m.focus + 1) % PanelCount
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
	case "d":
		m.focus = PanelDetails
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
			debug_log("[aws] Shift+A pressed: opening aws-keys")
			return m.open_aws_keys()
		}
	case "D":
		return m.open_db_picker()
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
			m.picker_actions = actions_for_worktree(*wt)
			m.picker_cursor = 0
			m.picker_open = true
			m.picker_context = pickerWorktree
		}
		return m, nil
	}

	// "n" works even with an empty worktree list
	if msg.String() == "n" {
		debug_log("[create] 'n' pressed: opening create wizard")
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
	case "l":
		return m.open_logs(*wt)
	case "i":
		return m.open_worktree_info()
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
			if wt.Type == worktree.TypeLocal {
				return m.start_dev_server(*wt)
			}
			if wt.HostBuild {
				return m.start_host_build(*wt)
			}
			if wt.Type == worktree.TypeDocker {
				return m, cmd_docker_action("start", *wt, m.repo_root, m.cfg)
			}
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
		m.focus = (m.focus + 1) % PanelCount
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
		cmd_name = "pm2"
		dir = wt.Path
		if svc.Name == "__all" {
			args = []string{"logs", "--lines", "80"}
			label = labels.Tab(labels.Logs, wt.Alias)
		} else {
			args = []string{"logs", svc.Name, "--lines", "80"}
			label = labels.Tab(labels.Logs, svc_label)
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
		} else {
			out, err = run_host_cmd("pm2", action, pm2_target)
		}
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
		cmd_name = "pm2"
		dir = wt.Path
		if svc.Name == "__all" {
			args = []string{"logs", "--lines", "80"}
		} else {
			args = []string{"logs", svc.Name, "--lines", "80"}
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

// start_dev_server opens a terminal tab running pnpm dev for a local worktree
func (m Model) start_dev_server(wt worktree.Worktree) (Model, tea.Cmd) {
	debug_log("[services] start_dev_server: alias=%s path=%s", wt.Alias, wt.Path)
	// Reload AWS credentials so the spawned process inherits the latest keys
	reload_aws_credentials()

	// Propagate AWS env vars to the tmux server so new windows inherit them
	if server := m.term_mgr.Server(); server != nil {
		for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"} {
			if val := os.Getenv(key); val != "" {
				server.SetEnv(key, val)
			}
		}
		debug_log("[services] start_dev_server: propagated AWS keys to tmux server")
	}

	w, h := m.right_pane_dimensions()

	path_env := "PROJECT_PATH"
	if m.cfg != nil {
		if v := m.cfg.EnvVar("projectPath"); v != "" {
			path_env = v
		}
	}
	dev_cmd := "pnpm dev"
	if m.cfg != nil && m.cfg.Dash.LocalDevCommand != "" {
		dev_cmd = m.cfg.Dash.LocalDevCommand
	}
	shell_cmd := fmt.Sprintf("%s=%s %s", path_env, wt.Path, dev_cmd)
	label := labels.Tab(labels.Dev, wt.Alias)
	debug_log("[services] start_dev_server: cmd=%q label=%q", shell_cmd, label)

	_, err := m.term_mgr.Open(label, "bash", []string{"-c", shell_cmd}, w, h, wt.Path)
	if err != nil {
		debug_log("[services] start_dev_server: FAILED: %v", err)
		m.terminal_output = fmt.Sprintf("Failed to start dev server: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}
	debug_log("[services] start_dev_server: terminal opened")

	m.terminal_output = ""
	m.activity = fmt.Sprintf("starting... %s", wt.Alias)
	m.prev_focus = m.focus; m.focus = PanelTerminal
	// Focus the right pane for native terminal interaction
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}

	return m, tea.Batch(
		tick_after(100*time.Millisecond, "render"),
		tick_after(3*time.Second, "status"),
	)
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

// open_aws_keys runs the aws-keys.js script in a terminal session.
// The render tick detects when the session exits and triggers service restarts.
func (m Model) open_aws_keys() (tea.Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()
	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "aws-keys.js")

	// Remove stale sentinel before opening
	sentinel.Clear(sentinel.AWSKeys)
	debug_log("[aws] open_aws_keys: removed stale sentinel")
	debug_log("[aws] open_aws_keys: script=%s pane=%dx%d", script, w, h)

	label := labels.AWSKeys
	_, err := m.term_mgr.Open(label, "node", []string{script}, w, h, m.repo_root)
	if err != nil {
		debug_log("[aws] open_aws_keys: FAILED to open terminal: %v", err)
		m.activity = fmt.Sprintf("Failed to open AWS keys: %v", err)
		return m, nil
	}

	debug_log("[aws] open_aws_keys: terminal opened, setting aws_keys_running=true")
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

func (m Model) toggle_usage() (tea.Model, tea.Cmd) {
	m.usage_visible = !m.usage_visible
	m.layout = m.layout.Resize(m.width, m.height, m.usage_visible)

	if !m.usage_visible {
		return m, nil
	}

	// Fire async fetch — cmd_fetch_usage handles token acquisition if needed.
	// The MsgUsageUpdated handler schedules the next 60s tick, so no tick here
	// (avoids duplicate tick chains on rapid toggle).
	return m, cmd_fetch_usage(m.usage_token)
}

// open_db_picker shows the database operations picker
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


