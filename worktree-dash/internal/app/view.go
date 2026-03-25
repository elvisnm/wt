package app

import (
	"strings"

	"github.com/elvisnm/wt/internal/labels"
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

	// 0 - Notification area (top of left column)
	notify_panel := m.render_notify_panel(selected_wt)

	// 1 - Active Tabs panel
	// Clamp tab_cursor to valid range
	cursor := m.tab_cursor
	tab_labels := m.term_mgr.TabLabelsWithCursor(cursor)
	if cursor >= len(tab_labels) {
		cursor = len(tab_labels) - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	tab_infos := make([]ui.TabInfo, len(tab_labels))
	for i, l := range tab_labels {
		tab_infos[i] = ui.TabInfo{
			Index:        l.Index,
			Label:        l.Label,
			Active:       l.Active,
			Alive:        l.Alive,
			IsGroupHead:  l.IsGroupHead,
			IsGroupChild: l.IsGroupChild,
			GroupSize:    l.GroupSize,
			LayoutMap:    l.LayoutMap,
		}
	}
	tabs_panel := ui.RenderTabsPanel(
		tab_infos, cursor,
		m.width, m.layout.TabsHeight,
		m.focus == PanelTerminal,
	)

	// 2 - Worktrees panel
	worktree_panel := ui.RenderWorktreePanel(
		m.worktrees, m.cursor,
		m.width, m.layout.WorktreeHeight,
		m.focus == PanelWorktrees,
		m.cfg,
	)

	// 3 - Services panel
	services_panel := ui.RenderServicesPanel(
		m.services, m.service_cursor,
		m.width, m.layout.ServicesHeight,
		m.focus == PanelServices,
	)

	// Build left column panels
	panels := []string{notify_panel, tabs_panel, worktree_panel, services_panel}

	if m.details_visible {
		details_panel := ui.RenderDetailsPanel(
			selected_wt,
			m.width, m.layout.DetailsHeight,
			m.details_scroll, m.spin_frame,
			m.focus == PanelDetails,
			m.cfg,
		)
		panels = append(panels, details_panel)
	}

	if m.usage_visible {
		usage_panel := ui.RenderUsagePanel(m.usage_data, m.usage_err, m.width, m.layout.UsageHeight, m.spin_frame)
		panels = append(panels, usage_panel)
	}

	if m.tasks_visible {
		tasks_panel := ui.RenderTasksPanel(
			m.tasks_list, m.tasks_cursor,
			m.tasks_detail, m.tasks_detail_scroll,
			m.width, m.layout.TasksHeight,
			m.focus == PanelTasks,
			m.tasks_err,
		)
		panels = append(panels, tasks_panel)
	}

	left_col := lipgloss.JoinVertical(lipgloss.Left, panels...)

	// Debug: detect height overflow
	rendered_lines := strings.Count(left_col, "\n") + 1
	if rendered_lines != m.height {
		for i, p := range panels {
			pl := strings.Count(p, "\n") + 1
			debug_log("[view] panel[%d]: rendered=%d", i, pl)
		}
		debug_log("[view] HEIGHT MISMATCH: rendered=%d allocated=%d (notify=%d tabs=%d wt=%d svc=%d det=%d usage=%d tasks=%d)",
			rendered_lines, m.height,
			m.layout.NotifyHeight, m.layout.TabsHeight, m.layout.WorktreeHeight,
			m.layout.ServicesHeight, m.layout.DetailsHeight, m.layout.UsageHeight, m.layout.TasksHeight)
	}

	if m.result_text != "" {
		result_bar := ui.RenderResultBar(m.width, m.result_text)
		return lipgloss.JoinVertical(lipgloss.Left, left_col, result_bar)
	}

	return left_col
}

// render_notify_panel renders the notification area at the top of the left column.
func (m Model) render_notify_panel(selected_wt *worktree.Worktree) string {
	h := m.layout.NotifyHeight
	switch {
	case m.picker_open:
		picker_title := m.picker_title(selected_wt)
		return ui.RenderNotifyPicker(m.picker_actions, m.picker_cursor, m.width, picker_title)

	case m.confirm_open:
		return ui.RenderNotifyConfirm(m.confirm_prompt, m.width, h)

	case m.input_active:
		return ui.RenderNotifyInput(m.input_prompt, m.input_value, m.width, h)

	case m.notify_open:
		return ui.RenderNotifyMessage(m.notify_title, m.notify_message, m.width, h)

	default:
		return ui.RenderNotifyIdle(m.width)
	}
}

// picker_title returns the title string for the current picker context.
func (m Model) picker_title(selected_wt *worktree.Worktree) string {
	switch m.picker_context {
	case pickerDB:
		if selected_wt != nil {
			return labels.Tab(labels.Database, selected_wt.Alias)
		}
		return labels.Database
	case pickerMaintenance:
		return labels.Maintenance
	case pickerStartService:
		if selected_wt != nil {
			return labels.Tab("Start Service", selected_wt.Alias)
		}
		return "Start Service"
	case pickerStopService:
		if selected_wt != nil {
			return labels.Tab("Stop Service", selected_wt.Alias)
		}
		return "Stop Service"
	case pickerRemove:
		if selected_wt != nil {
			return labels.Tab(labels.Remove, selected_wt.Alias)
		}
		return labels.Remove
	case pickerSplitH:
		title := "Split Right"
		if m.split_target_alias != "" {
			title += " — " + m.split_target_alias
		}
		return title
	case pickerSplitV:
		title := "Split Below"
		if m.split_target_alias != "" {
			title += " — " + m.split_target_alias
		}
		return title
	case pickerMergeTarget:
		return "Move into"
	case pickerMergeDir:
		return "Split Direction"
	default:
		if selected_wt != nil {
			return labels.Tab(labels.Actions, selected_wt.Alias)
		}
		return labels.Actions
	}
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
