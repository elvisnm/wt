package app

import (
	"fmt"

	"github.com/elvisnm/wt/internal/ui"
	"github.com/elvisnm/wt/internal/worktree"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if !m.ready || !m.discovered {
		return m.loading_view()
	}

	var selected_wt *worktree.Worktree
	if m.cursor >= 0 && m.cursor < len(m.worktrees) {
		wt := m.worktrees[m.cursor]
		selected_wt = &wt
	}

	// In pane layout mode, this app runs in the left tmux pane.
	// Layout order: status bar, tabs, worktrees, services, details.
	// The right pane (terminal) is managed natively by tmux.

	// 1 - Active Tabs panel
	labels := m.term_mgr.TabLabels()
	tab_infos := make([]ui.TabInfo, len(labels))
	for i, l := range labels {
		tab_infos[i] = ui.TabInfo{
			Index:  l.Index,
			Label:  l.Label,
			Active: l.Active,
			Alive:  l.Alive,
		}
	}
	tabs_panel := ui.RenderTabsPanel(
		tab_infos, m.term_mgr.ActiveIndex(),
		m.width, m.layout.TabsHeight,
		m.focus == PanelTerminal,
	)

	// 2 - Worktrees panel
	worktree_panel := ui.RenderWorktreePanel(
		m.worktrees, m.cursor,
		m.width, m.layout.WorktreeHeight,
		m.focus == PanelWorktrees,
	)

	// 3 - Services panel
	services_panel := ui.RenderServicesPanel(
		m.services, m.service_cursor,
		m.width, m.layout.ServicesHeight,
		m.focus == PanelServices,
	)

	// Details panel (bottom)
	details_panel := ui.RenderDetailsPanel(
		selected_wt,
		m.width, m.layout.DetailsHeight,
		m.details_scroll,
		m.focus == PanelDetails,
		m.cfg,
	)

	left_col := lipgloss.JoinVertical(lipgloss.Left,
		tabs_panel, worktree_panel, services_panel, details_panel)

	// Picker overlay — rendered within the left pane
	if m.picker_open {
		var picker_title string
		switch m.picker_context {
		case "db":
			picker_title = "Database"
			if selected_wt != nil {
				picker_title = fmt.Sprintf("Database — %s", selected_wt.Alias)
			}
		case "maintenance":
			picker_title = "Maintenance"
		case "remove":
			picker_title = "Remove"
			if selected_wt != nil {
				picker_title = fmt.Sprintf("Remove — %s", selected_wt.Alias)
			}
		default:
			picker_title = "Actions"
			if selected_wt != nil {
				picker_title = fmt.Sprintf("Actions — %s", selected_wt.Alias)
			}
		}
		picker_h := len(m.picker_actions) + 2
		if picker_h > m.height/2 {
			picker_h = m.height / 2
		}
		picker := ui.RenderPicker(
			m.picker_actions, m.picker_cursor,
			m.width, picker_h,
			picker_title,
		)
		return ui.OverlayCentered(left_col, picker, m.width, m.height)
	}

	if m.confirm_open {
		modal := ui.RenderConfirmModal(m.confirm_prompt, m.width, m.height)
		return ui.OverlayCentered(left_col, modal, m.width, m.height)
	}

	// Status bar input modes
	if m.input_active {
		input_bar := ui.RenderInputBar(m.width, m.input_prompt, m.input_value)
		return lipgloss.JoinVertical(lipgloss.Left, left_col, input_bar)
	}

	if m.result_text != "" {
		result_bar := ui.RenderResultBar(m.width, m.result_text)
		return lipgloss.JoinVertical(lipgloss.Left, left_col, result_bar)
	}

	return left_col
}

func (m Model) status_panel_name() string {
	if m.picker_open {
		return "Picker"
	}
	return panel_display_name(m.focus)
}

func panel_display_name(p Panel) string {
	switch p {
	case PanelTerminal:
		return "Active Tabs"
	case PanelWorktrees:
		return "Worktrees"
	case PanelServices:
		return "Services"
	case PanelDetails:
		return "Details"
	default:
		return ""
	}
}

func (m Model) status_hints() []ui.HintPair {
	if m.picker_open {
		return []ui.HintPair{
			{Key: "j/k", Desc: "navigate"},
			{Key: "Enter", Desc: "select"},
			{Key: "Esc", Desc: "close"},
		}
	}

	common := []ui.HintPair{
		{Key: "</>", Desc: "panel"},
		{Key: "a/w/s", Desc: "jump"},
		{Key: "q", Desc: "quit"},
	}

	switch m.focus {
	case PanelWorktrees:
		wt := m.selected_worktree()
		hints := []ui.HintPair{
			{Key: "j/k", Desc: "navigate"},
			{Key: "Enter", Desc: "actions"},
			{Key: "b", Desc: "shell"},
			{Key: "z", Desc: "zsh"},
			{Key: "c", Desc: "claude"},
		}
		if wt != nil && wt.Running {
			hints = append(hints, ui.HintPair{Key: "l", Desc: "logs"})
		}
		if wt != nil && wt.HostBuild && wt.Running && (m.cfg == nil || m.cfg.FeatureEnabled("hostBuild")) {
			hints = append(hints, ui.HintPair{Key: "e", Desc: "build"})
		}
		if wt != nil && !wt.ContainerExists && wt.Type != worktree.TypeLocal {
			hints = append(hints, ui.HintPair{Key: "n", Desc: "create"})
		}
		return append(hints, common...)
	case PanelDetails:
		return append([]ui.HintPair{
			{Key: "j/k", Desc: "scroll"},
			{Key: "Esc", Desc: "back"},
		}, common...)
	case PanelServices:
		if m.preview_session != nil {
			return append([]ui.HintPair{
				{Key: "j/k", Desc: "navigate"},
				{Key: "l", Desc: "pin logs"},
				{Key: "Esc", Desc: "close preview"},
			}, common...)
		}
		return append([]ui.HintPair{
			{Key: "j/k", Desc: "navigate"},
			{Key: "Enter", Desc: "preview"},
			{Key: "l", Desc: "logs"},
			{Key: "r", Desc: "restart"},
			{Key: "Esc", Desc: "back"},
		}, common...)
	case PanelTerminal:
		hints := []ui.HintPair{
			{Key: "j/k", Desc: "navigate"},
			{Key: "Enter", Desc: "focus"},
			{Key: "f", Desc: "fullscreen"},
			{Key: "x", Desc: "close"},
			{Key: "Esc", Desc: "back"},
		}
		return append(hints, common...)
	}

	return common
}

func (m Model) loading_view() string {
	w := m.width
	h := m.height
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}

	label := "Loading worktrees..."
	box := lipgloss.NewStyle().
		Width(w).
		Height(h).
		Align(lipgloss.Center, lipgloss.Center).
		Foreground(ui.HintColor)

	return box.Render(label)
}
