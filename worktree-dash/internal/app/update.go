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
	"github.com/elvisnm/wt/internal/labels"
	"github.com/elvisnm/wt/internal/pm2"
	"github.com/elvisnm/wt/internal/settings"
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

		// Clear stale agent sentinel files from previous session
		if first_load {
			stale, _ := filepath.Glob(sentinel.Path(sentinel.AgentNotify + "-*"))
			for _, f := range stale {
				os.Remove(f)
			}
		}

		// Signal the outer process that we're ready (unblocks tmux attach).
		if first_load && m.pane_layout != nil {
			m.pane_layout.Server().Run("wait-for", "-S", "wt-ready")
		}
		cmds := []tea.Cmd{
			tick_after(5*time.Second, "status"),
			tick_after(3*time.Second, "stats"),
			tick_after(100*time.Millisecond, "render"),
			tick_after(1*time.Second, "agent-poll"),
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
		case "notify":
			if !m.notify_open {
				return m, nil
			}
			m.notify_open = false
			m.notify_title = ""
			m.notify_message = ""
			m.recalc_layout()
			return m, nil
		case "render":
			// Sentinel-driven post-action handlers
			if sr := sentinel.Read(sentinel.Create); sr != nil {
				return m.handle_create_sentinel(sr)
			} else if m.term_mgr.HasLabel(labels.Create) || m.has_create_alias_tab() {
				if m.term_mgr.CloseDeadByPrefixIfClean(labels.Create) {
					m.focus_worktrees_if_empty()
				}
			}
			if m.skip_worktree_running {
				if sr := sentinel.Read(sentinel.SkipWorktree); sr != nil {
					return m.handle_skip_worktree_sentinel(sr)
				}
			}
			if m.heihei_playing {
				if sentinel.Read(sentinel.HeiHei) != nil {
					m, _ = m.handle_heihei_sentinel()
				}
			}
			// Auto-close dead Logs tabs
			if m.term_mgr != nil && m.term_mgr.CloseDeadLogs() {
				m.focus_worktrees_if_empty()
			}
			// Auto-close dead Settings tab
			if m.term_mgr != nil && m.term_mgr.CloseDeadByLabel(labels.Settings) {
				reload_cmd := m.reload_settings()
				m.focus_worktrees_if_empty()

				// Check if user exited with unsaved changes (TUI writes draft to temp file)
				draft_path := settings.DraftPath()
				if data, err := os.ReadFile(draft_path); err == nil {
					os.Remove(draft_path)
					draft_data := data // capture for closure
					m2, confirm_cmd := m.open_panel_confirm("Settings", "Save unsaved changes?",
						func(mdl *Model) (Model, tea.Cmd) {
							settings.SaveRaw(draft_data)
							cmd := mdl.reload_settings()
							mdl.notify_open = true
							mdl.notify_title = "Notifications"
							mdl.notify_message = "Settings saved"
							mdl.recalc_layout()
							return *mdl, tea.Batch(cmd, tick_after(notifyDefaultDuration, "notify"))
						})
					return m2, tea.Batch(reload_cmd, confirm_cmd)
				}

				// No draft = saved via Save & Close — show success notification
				m2, notify_cmd := m.show_notification("Notifications", "Settings saved")
				m = m2
				if reload_cmd != nil {
					return m, tea.Batch(reload_cmd, notify_cmd)
				}
				return m, notify_cmd
			}
			// Keep tab_cursor in sync with the active group.
			// Open/FocusByLabel/etc. change active_tab but don't update tab_cursor.
			m.sync_tab_cursor_if_stale()

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

	case msgPanelInputResult:
		if msg.callback != nil && msg.value != "" {
			m, cmd := msg.callback(&m, msg.value)
			m.recalc_layout()
			return m, cmd
		}
		m.recalc_layout()
		return m, nil

	case tea.MouseMsg:
		return m.handle_mouse(msg)

	case tea.KeyMsg:
		// Shift+S opens settings from anywhere (even over overlays)
		if msg.String() == "S" {
			return m.open_settings()
		}
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
		return m.open_panel_confirm("Quit", "Quit worktree?", quit_action)

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
	case "D":
		return m.toggle_details()
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
			actions := m.actions_for_worktree(*wt)
			return m.open_panel_picker("Choose an option - "+wt.Alias, actions, pickerWorktree)
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
	case "d":
		return m.toggle_details()
	case "l":
		return m.open_logs(*wt)
	case "i":
		return m.open_worktree_info()
	case "g":
		return m.open_pull(*wt)
	case "r":
		if wt.Running {
			if wt.Type == worktree.TypeLocal {
				return m.restart_local_services(*wt)
			}
			return m, cmd_docker_action("restart", *wt, m.repo_root, m.cfg)
		}
	case "t":
		if wt.Running {
			if wt.Type == worktree.TypeLocal {
				return m.stop_dev_server(*wt)
			}
			return m, cmd_docker_action("stop", *wt, m.repo_root, m.cfg)
		}
	case "u":
		if !wt.Running {
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
	tab_labels := m.term_mgr.TabLabels()

	switch {
	case key.Matches(msg, Keys.Up):
		if m.tab_cursor > 0 {
			m.tab_cursor--
			m.sync_tab_cursor_to_group(tab_labels)
		}
		return m, nil

	case key.Matches(msg, Keys.Down):
		if m.tab_cursor < len(tab_labels)-1 {
			m.tab_cursor++
			m.sync_tab_cursor_to_group(tab_labels)
		}
		return m, nil

	case key.Matches(msg, Keys.Enter):
		// Focus the specific pane the cursor is on
		if m.pane_layout != nil {
			tab_labels = m.term_mgr.TabLabels()
			if m.tab_cursor >= 0 && m.tab_cursor < len(tab_labels) {
				tl := tab_labels[m.tab_cursor]
				if tl.IsGroupHead {
					// Focus the first pane in the group
					m.pane_layout.FocusRight()
				} else if tl.SessionID > 0 {
					// Focus the specific session's pane
					g := m.term_mgr.ActiveGroup()
					if g != nil {
						if sess := g.SessionByID(tl.SessionID); sess != nil && sess.PaneID() != "" {
							m.pane_layout.Server().Run("select-pane", "-t", sess.PaneID())
						} else {
							m.pane_layout.FocusRight()
						}
					}
				} else {
					m.pane_layout.FocusRight()
				}
			} else {
				m.pane_layout.FocusRight()
			}
		}
		return m, nil

	case msg.String() == "h":
		m.term_mgr.PrevTab()
		m.sync_tab_cursor_from_active()
		return m, nil

	case msg.String() == "l":
		m.term_mgr.NextTab()
		m.sync_tab_cursor_from_active()
		return m, nil

	case msg.String() == "f":
		// Fullscreen — zoom the right pane and focus it
		if s != nil && s.IsAlive() && m.pane_layout != nil {
			m.pane_layout.ZoomRight()
			m.pane_layout.FocusRight()
		}
		return m, nil

	case msg.String() == "x":
		tab_labels = m.term_mgr.TabLabels()
		if m.tab_cursor >= 0 && m.tab_cursor < len(tab_labels) {
			tl := tab_labels[m.tab_cursor]
			if tl.IsGroupHead {
				// Close entire group
				m.term_mgr.CloseActive()
			} else if tl.IsGroupChild {
				// Close just this one pane
				m.term_mgr.CloseBySessionID(tl.SessionID)
			} else {
				// Standalone tab
				m.term_mgr.CloseActive()
			}
		} else {
			m.term_mgr.CloseActive()
		}
		// Clamp tab_cursor after close
		new_labels := m.term_mgr.TabLabels()
		if m.tab_cursor >= len(new_labels) {
			m.tab_cursor = len(new_labels) - 1
		}
		if m.tab_cursor < 0 {
			m.tab_cursor = 0
		}
		if m.term_mgr.Count() == 0 {
			m.focus = PanelWorktrees
		}
		return m, nil

	case msg.String() == "|":
		return m.open_split_picker(SplitH)

	case msg.String() == "_":
		return m.open_split_picker(SplitV)

	case msg.String() == "m":
		return m.open_merge_picker()

	case msg.String() == "r":
		// Rename the session under the cursor
		tab_labels = m.term_mgr.TabLabels()
		if m.tab_cursor >= 0 && m.tab_cursor < len(tab_labels) {
			tl := tab_labels[m.tab_cursor]
			if tl.SessionID > 0 {
				session_id := tl.SessionID
				return m.open_panel_input("Rename", "New name:", func(mdl *Model, val string) (Model, tea.Cmd) {
					name := strings.TrimSpace(val)
					if name == "" {
						return *mdl, nil
					}
					for _, s := range mdl.term_mgr.Sessions() {
						if s.ID == session_id {
							s.Label = name
							break
						}
					}
					return *mdl, nil
				})
			}
		}
		return m, nil
	}

	return m, nil
}

// sync_tab_cursor_from_active sets tab_cursor to the first entry of the active group.
// Called after opening/closing tabs to keep the cursor in sync.
func (m *Model) sync_tab_cursor_from_active() {
	active_idx := m.term_mgr.ActiveIndex()
	tab_labels := m.term_mgr.TabLabels()
	groups := m.term_mgr.Groups()
	if active_idx < 0 || active_idx >= len(groups) || len(tab_labels) == 0 {
		m.tab_cursor = 0
		return
	}
	target_gid := groups[active_idx].ID
	for i, tl := range tab_labels {
		if tl.GroupID == target_gid {
			m.tab_cursor = i
			return
		}
	}
	m.tab_cursor = 0
}

// sync_tab_cursor_if_stale checks if the tab_cursor points to a different group
// than the active one, and if so, syncs it. This catches cases where Open/Focus
// changed the active group without updating the cursor (many call sites).
func (m *Model) sync_tab_cursor_if_stale() {
	tab_labels := m.term_mgr.TabLabels()
	if len(tab_labels) == 0 {
		return
	}
	if m.tab_cursor < 0 || m.tab_cursor >= len(tab_labels) {
		m.sync_tab_cursor_from_active()
		return
	}
	// Check if cursor's group matches the active group
	active_idx := m.term_mgr.ActiveIndex()
	groups := m.term_mgr.Groups()
	if active_idx < 0 || active_idx >= len(groups) {
		return
	}
	cursor_gid := tab_labels[m.tab_cursor].GroupID
	active_gid := groups[active_idx].ID
	if cursor_gid != active_gid {
		m.sync_tab_cursor_from_active()
	}
}

// sync_tab_cursor_to_group switches the active group if the tab_cursor moved to a different group.
func (m *Model) sync_tab_cursor_to_group(tab_labels []terminal.TabLabel) {
	if m.tab_cursor < 0 || m.tab_cursor >= len(tab_labels) {
		return
	}
	tl := tab_labels[m.tab_cursor]
	// Find the group index for this tab label
	groups := m.term_mgr.Groups()
	for i, g := range groups {
		if g.ID == tl.GroupID {
			if i != m.term_mgr.ActiveIndex() {
				m.term_mgr.FocusByIndex(i)
			}
			return
		}
	}
}

// open_split_picker opens the session type picker for creating a split.
// The new session will split from the cursor-selected session in the given direction.
func (m Model) open_split_picker(dir SplitDir) (tea.Model, tea.Cmd) {
	g := m.term_mgr.ActiveGroup()
	if g == nil {
		return m, nil
	}
	max_panes := m.term_mgr.MaxPanes()
	if g.Count() >= max_panes {
		m2, cmd := m.show_notification("Split", fmt.Sprintf("Max %d sessions per group reached", max_panes))
		return m2, cmd
	}

	// Find the session under the tab cursor
	tab_labels := m.term_mgr.TabLabels()
	var target *terminal.Session
	if m.tab_cursor >= 0 && m.tab_cursor < len(tab_labels) {
		target = g.SessionByID(tab_labels[m.tab_cursor].SessionID)
	}
	if target == nil {
		target = g.Primary()
	}
	if target == nil {
		return m, nil
	}

	m.split_target_session_id = target.ID
	m.split_target_alias = target.WorktreeAlias
	m.split_target_dir = target.WorktreeDir

	context := pickerSplitH
	title := "Split Right"
	if dir == SplitV {
		context = pickerSplitV
		title = "Split Below"
	}
	if m.split_target_alias != "" {
		title += " — " + m.split_target_alias
	}

	actions := ui.SplitSessionActions
	if !m.claude_auto_mode {
		actions = insert_claude_auto(actions)
	}
	return m.open_panel_picker(title, actions, context)
}

// open_merge_picker starts the merge flow: first pick a target tab to merge into.
func (m Model) open_merge_picker() (tea.Model, tea.Cmd) {
	active := m.term_mgr.Active()
	if active == nil {
		return m, nil
	}
	active_group := m.term_mgr.ActiveGroup()
	if active_group == nil || active_group.Count() != 1 {
		m.activity = "Can only move standalone tabs"
		return m, nil
	}
	if m.term_mgr.Count() < 2 {
		m.activity = "No other tabs to merge into"
		return m, nil
	}

	m.merge_source_session_id = active.ID

	// Build target list: all other groups
	var actions []ui.PickerAction
	for i, g := range m.term_mgr.Groups() {
		if g.Contains(active.ID) {
			continue // skip self
		}
		if g.Count() >= m.term_mgr.MaxPanes() {
			continue // skip full groups
		}
		key := fmt.Sprintf("%d", i+1)
		actions = append(actions, ui.PickerAction{
			Key:   key,
			Label: g.Label(),
			Desc:  fmt.Sprintf("%d pane(s)", g.Count()),
		})
	}

	if len(actions) == 0 {
		m.activity = "No available tabs to merge into"
		return m, nil
	}

	title := fmt.Sprintf("Move %q into", active.Label)
	return m.open_panel_picker(title, actions, pickerMergeTarget)
}

// execute_merge_target handles the target tab selection, then opens direction picker.
func (m Model) execute_merge_target(action ui.PickerAction) (Model, tea.Cmd) {
	// The action.Key is the 1-based tab index string
	var idx int
	fmt.Sscanf(action.Key, "%d", &idx)
	idx-- // 0-based

	groups := m.term_mgr.Groups()
	if idx < 0 || idx >= len(groups) {
		return m, nil
	}

	target := groups[idx]
	m.split_target_session_id = target.Primary().ID

	return m.open_panel_picker("Split Direction", ui.MergeDirectionActions, pickerMergeDir)
}

// execute_merge_direction completes the merge with the chosen direction.
func (m Model) execute_merge_direction(action ui.PickerAction) (Model, tea.Cmd) {
	dir := SplitH
	if action.Key == "_" {
		dir = SplitV
	}

	err := m.term_mgr.MoveInto(m.merge_source_session_id, m.split_target_session_id, dir)
	if err != nil {
		m.activity = fmt.Sprintf("Merge failed: %v", err)
		return m, nil
	}

	m.focus = PanelTerminal
	return m, tick_after(100*time.Millisecond, "render")
}

// SplitDir re-exports terminal.SplitDir for use in app package.
type SplitDir = terminal.SplitDir

const (
	SplitH = terminal.SplitH
	SplitV = terminal.SplitV
)

func (m Model) handle_picker_key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Quit), key.Matches(msg, Keys.CtrlC):
		m.picker_open = false
		m.recalc_layout()
		return m.open_panel_confirm("Quit", "Quit worktree?", quit_action)

	case key.Matches(msg, Keys.Escape):
		m.picker_open = false
		m.recalc_layout()
		return m, nil

	case key.Matches(msg, Keys.Tab):
		m.picker_open = false
		m.recalc_layout()
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
			m.recalc_layout()
			return m.dispatch_picker(action)
		}
		return m, nil
	}

	// Handle direct key presses in picker
	for _, a := range m.picker_actions {
		if msg.String() == a.Key {
			m.picker_open = false
			m.recalc_layout()
			return m.dispatch_picker(a)
		}
	}

	return m, nil
}

func (m Model) dispatch_picker(action ui.PickerAction) (Model, tea.Cmd) {
	debug_log("[picker] dispatch: key=%q label=%q context=%s", action.Key, action.Label, m.picker_context)
	switch m.picker_context {
	case pickerMaintenance:
		return m.execute_maintenance_action(action)
	case pickerRemove:
		return m.execute_remove_action(action)
	case pickerStartService:
		return m.execute_start_service_action(action)
	case pickerStopService:
		return m.execute_stop_service_action(action)
	case pickerSplitH:
		return m.execute_split_action(action, SplitH)
	case pickerSplitV:
		return m.execute_split_action(action, SplitV)
	case pickerMergeTarget:
		return m.execute_merge_target(action)
	case pickerMergeDir:
		return m.execute_merge_direction(action)
	default:
		return m.execute_picker_action(action)
	}
}

// execute_split_action creates a new split session based on the selected session type.
func (m Model) execute_split_action(action ui.PickerAction, dir SplitDir) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()
	alias := m.split_target_alias
	wt_dir := m.split_target_dir
	target_id := m.split_target_session_id

	var cmd_name string
	var args []string
	var session_dir string
	var label_prefix string

	switch action.Key {
	case "b":
		// Shell — docker exec if running container, otherwise host shell
		wt := m.find_worktree_by_alias(alias)
		if wt != nil && wt.Type == worktree.TypeDocker && wt.Running {
			cmd_name = "docker"
			args = []string{"exec", "-it", wt.Container, "bash"}
		} else {
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "bash"
			}
			cmd_name = shell
			session_dir = wt_dir
		}
		label_prefix = labels.Shell
	case "c", "C":
		claude_cmd := "claude"
		if m.cfg != nil {
			if c, ok := m.cfg.Dash.Commands["claude"]; ok && c.Cmd != "" {
				claude_cmd = c.Cmd
			}
		}
		if action.Key == "C" || m.claude_auto_mode {
			// Auto-mode: open a shell, then send-keys "claude --enable-auto-mode".
			// Passing --enable-auto-mode via exec doesn't activate auto mode.
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "zsh"
			}
			cmd_name = shell
			session_dir = wt_dir
			label_prefix = labels.Claude
			label := labels.Tab(label_prefix, alias)
			s, err := m.term_mgr.SplitInto(target_id, label, cmd_name, args, w, h, session_dir, dir)
			if err != nil {
				m.activity = fmt.Sprintf("Split failed: %v", err)
				return m, nil
			}
			s.SetWorktree(alias, wt_dir)
			if s.PaneID() != "" {
				m.term_mgr.Server().Run("send-keys", "-t", s.PaneID(), "claude --enable-auto-mode", "Enter")
			}
			m.prev_focus = m.focus
			m.focus = PanelTerminal
			return m, tick_after(100*time.Millisecond, "render")
		}
		cmd_name = claude_cmd
		session_dir = wt_dir
		label_prefix = labels.Claude
	case "z":
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "zsh"
		}
		cmd_name = shell
		session_dir = wt_dir
		label_prefix = labels.Zsh
	case "l":
		wt := m.find_worktree_by_alias(alias)
		if wt != nil && wt.Type == worktree.TypeDocker && wt.Running {
			cmd_name = "docker"
			args = []string{"exec", "-it", wt.Container, "pm2", "logs", "--lines", "100"}
		} else {
			cmd_name = "pm2"
			args = []string{"logs", "--lines", "100"}
			session_dir = wt_dir
		}
		label_prefix = labels.Logs
	default:
		return m, nil
	}

	label := labels.Tab(label_prefix, alias)

	s, err := m.term_mgr.SplitInto(target_id, label, cmd_name, args, w, h, session_dir, dir)
	if err != nil {
		m.activity = fmt.Sprintf("Split failed: %v", err)
		return m, nil
	}
	s.SetWorktree(alias, wt_dir)

	m.prev_focus = m.focus
	m.focus = PanelTerminal
	// Sync tab_cursor to point at the new session in the flat list
	tab_labels := m.term_mgr.TabLabels()
	for i, tl := range tab_labels {
		if tl.SessionID == s.ID {
			m.tab_cursor = i
			break
		}
	}
	return m, tick_after(100*time.Millisecond, "render")
}

// find_worktree_by_alias finds a worktree by its alias.
func (m Model) find_worktree_by_alias(alias string) *worktree.Worktree {
	for i := range m.worktrees {
		if m.worktrees[i].Alias == alias {
			return &m.worktrees[i]
		}
	}
	return nil
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
	case "C":
		return m.open_claude_auto(*wt)
	case "z":
		return m.open_local_shell(*wt)
	case "l":
		return m.open_logs(*wt)
	case "n":
		return m.open_create(wt)
	case "g":
		return m.open_pull(*wt)
	case "r":
		if wt.Type == worktree.TypeLocal {
			return m.restart_local_services(*wt)
		}
		return m, cmd_docker_action("restart", *wt, m.repo_root, m.cfg)
	case "t":
		if wt.Type == worktree.TypeLocal {
			return m.stop_dev_server(*wt)
		}
		return m, cmd_docker_action("stop", *wt, m.repo_root, m.cfg)
	case "u":
		return m.start_worktree(*wt)
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
	s, err := m.term_mgr.OpenNew(label, cmd_name, args, w, h, dir)
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to open bash: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}
	s.SetWorktree(wt.Alias, wt.Path)

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}
	return m, tick_after(100*time.Millisecond, "render")
}

// open_pull asks for confirmation then runs dc-pull.js to safely pull latest changes.
func (m Model) open_pull(wt worktree.Worktree) (Model, tea.Cmd) {
	return m.open_panel_confirm("Pull", fmt.Sprintf("Pull latest changes on %s?", wt.Alias),
		func(mdl *Model) (Model, tea.Cmd) { return mdl.run_pull(wt) })
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

// open_claude opens Claude Code in the worktree.
// When claude_auto_mode is enabled in settings, passes --enable-auto-mode.
func (m Model) open_claude(wt worktree.Worktree) (Model, tea.Cmd) {
	return m.open_claude_with_flags(wt, m.claude_auto_mode)
}

// open_claude_auto opens Claude Code with --enable-auto-mode regardless of settings.
func (m Model) open_claude_auto(wt worktree.Worktree) (Model, tea.Cmd) {
	return m.open_claude_with_flags(wt, true)
}

func (m Model) open_claude_with_flags(wt worktree.Worktree, auto_mode bool) (Model, tea.Cmd) {
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
	var s *terminal.Session
	var err error
	if auto_mode {
		// Use send-keys so claude receives the flag in an interactive shell context.
		// "exec claude --enable-auto-mode" via tmux new-window doesn't activate auto mode.
		// Use just "claude" (not full path) since the interactive shell has PATH.
		debug_log("[claude] open: send-keys 'claude --enable-auto-mode' dir=%s", dir)
		s, err = m.term_mgr.OpenNewSendKeys(label, "claude", []string{"--enable-auto-mode"}, w, h, dir)
	} else {
		debug_log("[claude] open: exec cmd=%s dir=%s", cmd_name, dir)
		s, err = m.term_mgr.OpenNew(label, cmd_name, args, w, h, dir)
	}
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to open Claude: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}
	s.SetWorktree(wt.Alias, wt.Path)

	m.terminal_output = ""
	m.prev_focus = m.focus
	m.focus = PanelTerminal
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
	s, err := m.term_mgr.OpenNew(label, shell, nil, w, h, wt.Path)
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to open shell: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}
	s.SetWorktree(wt.Alias, wt.Path)

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal
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

	s, err := m.term_mgr.Open(label, cmd_name, args, w, h, dir)
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to open logs: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}
	s.SetWorktree(wt.Alias, wt.Path)

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal

	return m, tick_after(100*time.Millisecond, "render")
}

// open_create runs the interactive dc-create.js script to create a new container
func (m Model) open_create(wt *worktree.Worktree) (Model, tea.Cmd) {
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
				args = []string{"-c", fmt.Sprintf("PM2_HOME=%s exec pm2 logs '%s' --lines 80", pm2_home, target)}
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
		if wt.IsolatedPM2 {
			pm2_home := wt.PM2Home()
			cmd_name = "bash"
			if svc.Name == "__all" {
				args = []string{"-c", fmt.Sprintf("PM2_HOME=%s exec pm2 logs --lines 80", pm2_home)}
			} else {
				target := m.pm2_log_target(svc, wt)
				args = []string{"-c", fmt.Sprintf("PM2_HOME=%s exec pm2 logs '%s' --lines 80", pm2_home, target)}
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

func (m Model) open_settings() (Model, tea.Cmd) {
	// Toggle — if already open, close and reload settings
	if m.term_mgr.HasLabel(labels.Settings) {
		m.term_mgr.CloseByLabel(labels.Settings)
		cmd := m.reload_settings()
		return m, cmd
	}

	// Return the current split group first so settings gets the full right viewport.
	// Without this, break-pane for extras may fail if their background windows
	// were destroyed, leaving split panes in the viewport.
	if m.pane_layout != nil {
		m.pane_layout.ReturnSession()
	}

	// Measure after returning — now we get the full right pane dimensions
	w, h := m.right_pane_dimensions()

	exe, err := os.Executable()
	if err != nil {
		return m, nil
	}
	exe, _ = filepath.EvalSymlinks(exe)

	_, err = m.term_mgr.Open(labels.Settings, exe, []string{"_settings"}, w, h, "")
	if err != nil {
		return m, nil
	}

	m.prev_focus = m.focus
	m.focus = PanelTerminal
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}
	return m, tick_after(100*time.Millisecond, "render")
}

// reload_settings re-reads ~/.wt/settings.json and applies panel visibility.
// Called after the settings tab closes. Returns commands to fetch data for
// newly enabled panels.
func (m *Model) reload_settings() tea.Cmd {
	s := settings.Load()
	m.details_visible = s.DefaultPanels.Details
	m.term_mgr.SetSplitLimits(s.MaxPanesPerGroup)
	m.claude_auto_mode = s.ClaudeAutoMode

	var cmds []tea.Cmd

	// Usage: trigger fetch if newly visible and no data loaded
	if s.DefaultPanels.Usage && !m.usage_visible {
		cmds = append(cmds, cmd_fetch_usage(m.usage_token), tick_after(80*time.Millisecond, "spin"))
	}
	m.usage_visible = s.DefaultPanels.Usage

	// Tasks: trigger fetch if newly visible and no data loaded
	if s.DefaultPanels.Tasks && !m.tasks_visible {
		m.tasks_cursor = 0
		m.tasks_detail = nil
		cmds = append(cmds, cmd_fetch_tasks())
	}
	m.tasks_visible = s.DefaultPanels.Tasks

	m.recalc_layout()

	if len(cmds) > 0 {
		return tea.Batch(cmds...)
	}
	return nil
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
		m.recalc_layout()
		return m, nil

	case key.Matches(msg, Keys.Enter):
		if m.confirm_action != nil {
			cb := m.confirm_action
			m.confirm_open = false
			m.confirm_prompt = ""
			m.confirm_action = nil
			m.recalc_layout()
			return cb(&m)
		}
		m.confirm_open = false
		m.recalc_layout()
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
		m.recalc_layout()
		return m, nil

	case key.Matches(msg, Keys.Enter):
		if m.input_callback != nil {
			cb := m.input_callback
			val := m.input_value
			m.input_active = false
			m.input_prompt = ""
			m.input_value = ""
			m.input_callback = nil
			m.recalc_layout()
			return m, cb(val)
		}
		m.input_active = false
		m.recalc_layout()
		return m, nil

	case msg.Type == tea.KeyBackspace:
		if len(m.input_value) > 0 {
			m.input_value = m.input_value[:len(m.input_value)-1]
		}
		return m, nil

	case msg.Type == tea.KeySpace:
		m.input_value += " "
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

const notifyDefaultDuration = 5 * time.Second

// show_notification displays a timed message in the notification area.
// Auto-clears after 5s. Any keypress dismisses it immediately.
func (m Model) show_notification(title, message string) (Model, tea.Cmd) {
	m.notify_open = true
	m.notify_title = title
	m.notify_message = message
	m.recalc_layout()
	return m, tick_after(notifyDefaultDuration, "notify")
}

// open_panel_picker opens the inline picker in the notification area.
// Keyboard input is handled by handle_picker_key.
func (m Model) open_panel_picker(title string, actions []ui.PickerAction, context string) (Model, tea.Cmd) {
	if len(actions) == 0 {
		return m, nil
	}
	m.picker_open = true
	m.picker_cursor = 0
	m.picker_actions = actions
	m.picker_context = context
	m.recalc_layout()
	return m, nil
}

// open_panel_confirm opens the inline confirm dialog in the notification area.
// Keyboard input is handled by handle_confirm_key.
func (m Model) open_panel_confirm(title, prompt string, action func(*Model) (Model, tea.Cmd)) (Model, tea.Cmd) {
	m.confirm_open = true
	m.confirm_prompt = prompt
	m.confirm_action = action
	m.recalc_layout()
	return m, nil
}

// open_panel_input opens the inline text input in the notification area.
// Keyboard input is handled by handle_input_key.
func (m Model) open_panel_input(title, prompt string, callback func(*Model, string) (Model, tea.Cmd)) (Model, tea.Cmd) {
	m.input_active = true
	m.input_prompt = prompt
	m.input_value = ""
	m.input_callback = func(val string) tea.Cmd {
		return func() tea.Msg {
			return msgPanelInputResult{value: val, callback: callback}
		}
	}
	m.recalc_layout()
	return m, nil
}

// send_macos_notification sends a native macOS notification via osascript.
func send_macos_notification(title, message string) {
	t := strings.ReplaceAll(title, `"`, `\"`)
	m := strings.ReplaceAll(message, `"`, `\"`)
	exec.Command("osascript", "-e",
		fmt.Sprintf(`display notification "%s" with title "%s"`, m, t),
	).Run()
}

// open_worktree_info ensures the Details panel is visible and focuses it.
func (m Model) open_worktree_info() (Model, tea.Cmd) {
	if !m.details_visible {
		m.details_visible = true
		m.recalc_layout()
	}
	m.prev_focus = m.focus
	m.focus = PanelDetails
	m.details_scroll = 0
	return m, nil
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

	return m.open_panel_confirm("LAN", fmt.Sprintf("LAN %s on %s?", action, wt.Alias),
		func(mdl *Model) (Model, tea.Cmd) { return mdl.run_lan_toggle(*wt, action) })
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

	verb := "Apply"
	if action == "remove" {
		verb = "Remove"
	}
	return m.open_panel_confirm("Skip-worktree", fmt.Sprintf("%s skip-worktree on %s?", verb, wt.Alias),
		func(mdl *Model) (Model, tea.Cmd) { return mdl.run_skip_worktree_toggle(*wt, action) })
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
	return m.open_panel_picker("Maintenance", ui.FilterMaintenanceActions(m.cfg), pickerMaintenance)
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
		return m.open_panel_confirm("Close Task", fmt.Sprintf("Close task %s?", id),
			func(mdl *Model) (Model, tea.Cmd) {
				return *mdl, func() tea.Msg {
					err := beads.CloseTask(id)
					return MsgTaskActionDone{Err: err}
				}
			})
	case "d":
		id := task.ID
		return m.open_panel_confirm("Delete Task", fmt.Sprintf("Delete task %s?", id),
			func(mdl *Model) (Model, tea.Cmd) {
				return *mdl, func() tea.Msg {
					err := beads.DeleteTask(id)
					return MsgTaskActionDone{Err: err}
				}
			})
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
