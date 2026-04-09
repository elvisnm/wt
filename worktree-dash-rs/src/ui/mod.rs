pub mod guide;
pub mod help;
mod layout;
pub mod overlay;
pub mod settings_tui;
pub mod splash;
pub mod style;
pub mod term_view;

pub use layout::{Layout, ResizeOpts};
pub use overlay::{NotifyKind, NotifyState, PickerAction};
pub use style::*;

use ratatui::{
    prelude::*,
    widgets::{Block, Borders, Paragraph},
};

use crate::app::{App, Panel};

pub fn render(frame: &mut Frame, app: &App) {
    // When a blocking overlay is active (confirm, error, picker),
    // no panel should show green — only the overlay gets focus color
    let overlay_active = app.confirm_quit
        || matches!(&app.notify_state, overlay::NotifyState::Picker { .. })
        || matches!(&app.notify_state, overlay::NotifyState::Confirm { .. })
        || matches!(&app.notify_state, overlay::NotifyState::Input { .. })
        || matches!(&app.notify_state, overlay::NotifyState::Message { kind: overlay::NotifyKind::Error, .. });

    let area = frame.area();

    // Init wizard: render full-screen overlay
    if let Some(ref wiz) = app.init_wizard {
        render_init_wizard(frame, area, wiz);
        return;
    }

    // Compute top bar height: activity spinner OR notification OR quit confirm
    let notify_bar_height = if app.confirm_quit {
        2 // title + question
    } else if let overlay::NotifyState::Message { message, .. } = &app.notify_state {
        let lines = message.lines().count().max(1) as u16;
        lines + 1 // +1 for the title/hint row
    } else if app.activity.is_some() {
        1 // single-line activity spinner
    } else {
        0
    };

    // Split: [optional notify bar] + main content + status bar
    let (main_area, status_area, notify_area) = if notify_bar_height > 0 {
        let outer = ratatui::layout::Layout::default()
            .direction(Direction::Vertical)
            .constraints([
                Constraint::Length(notify_bar_height),
                Constraint::Min(1),
                Constraint::Length(1),
            ])
            .split(area);
        (outer[1], outer[2], Some(outer[0]))
    } else {
        let outer = ratatui::layout::Layout::default()
            .direction(Direction::Vertical)
            .constraints([Constraint::Min(1), Constraint::Length(1)])
            .split(area);
        (outer[0], outer[1], None)
    };

    // Render notification/confirm bar
    if let Some(bar_area) = notify_area {
        if app.confirm_quit {
            let bg = Color::Yellow;
            let fg = Color::Black;
            let bold = Style::default().fg(fg).bg(bg).add_modifier(ratatui::style::Modifier::BOLD);
            let text = Style::default().fg(fg).bg(bg);
            let action_label = Style::default().fg(Color::Rgb(100, 80, 0)).bg(bg);

            let msg = " Quit wt dashboard?";
            let actions = "Enter: confirm  Esc: cancel ";
            // Split actions into styled spans
            let action_spans = vec![
                Span::styled("Enter", bold),
                Span::styled(": confirm  ", action_label),
                Span::styled("Esc", bold),
                Span::styled(": cancel ", action_label),
            ];
            let actions_len: usize = actions.len();
            let padding = (bar_area.width as usize).saturating_sub(msg.len() + actions_len);

            let bar = Paragraph::new(vec![
                Line::from(Span::styled(" Exit ", bold)),
                Line::from({
                    let mut spans = vec![
                        Span::styled(msg, text),
                        Span::styled(" ".repeat(padding), text),
                    ];
                    spans.extend(action_spans);
                    spans
                }),
            ]).style(Style::default().bg(bg));
            frame.render_widget(bar, bar_area);
        } else if let Some(ref activity) = app.activity {
            // Activity spinner bar
            let bg = HINT_COLOR; // cyan/info color
            let fg = Color::Black;
            let style = Style::default().fg(fg).bg(bg);
            let spinner = SPIN_FRAMES[app.spin_frame % SPIN_FRAMES.len()];
            let bar = Paragraph::new(Line::from(vec![
                Span::styled(format!(" {} {} ", spinner, activity), style),
            ])).style(style);
            frame.render_widget(bar, bar_area);
        } else {
            render_notify_bar(frame, bar_area, &app.notify_state);
        }
    }

    if app.fullscreen || app.sidebar_hidden {
        if app.fullscreen {
            if let Some(sid) = app.fullscreen_session_id {
                if let Some(session) = app.pty_mgr.get(sid) {
                    let term_area = pad_rect(Rect::new(main_area.x, main_area.y, main_area.width, main_area.height));
                    let term_widget = term_view::TermView::new(session.term());
                    frame.render_widget(term_widget, term_area);
                }
            } else {
                render_terminal_area(frame, main_area, app, overlay_active);
            }
        } else {
            // Sidebar hidden — just show terminal area full width
            render_terminal_area(frame, main_area, app, overlay_active);
        }
        render_status_bar(frame, status_area, app);
        return;
    }

    // Main split: left panel | right terminal area
    let main_layout = ratatui::layout::Layout::default()
        .direction(Direction::Horizontal)
        .constraints([
            Constraint::Percentage(app.settings.left_pane_pct),
            Constraint::Percentage(100 - app.settings.left_pane_pct),
        ])
        .split(main_area);

    render_left_column(frame, main_layout[0], app, overlay_active);
    render_terminal_area(frame, main_layout[1], app, overlay_active);
    render_status_bar(frame, status_area, app);
}

fn render_left_column(frame: &mut Frame, area: Rect, app: &App, overlay_active: bool) {
    let layout = &app.layout;

    // Draw vertical separator on the right edge
    let has_focus = !overlay_active && matches!(app.focus, Panel::Worktrees | Panel::Services | Panel::Details | Panel::Terminal);
    render_sidebar_separator(frame, area, has_focus && !app.terminal_focused);

    // Content area: full width minus 1 for separator
    let content_area = Rect::new(area.x, area.y, area.width.saturating_sub(1), area.height);

    // Build constraints dynamically based on visible panels
    let mut constraints: Vec<Constraint> = Vec::new();
    let mut panel_ids: Vec<&str> = Vec::new();

    // Overlay area (picker, input, confirm — only when active)
    let needs_overlay = matches!(
        &app.notify_state,
        overlay::NotifyState::Picker { .. }
        | overlay::NotifyState::Confirm { .. }
        | overlay::NotifyState::Input { .. }
    );
    if needs_overlay {
        constraints.push(Constraint::Length(layout.notify_height));
        panel_ids.push("notify");
    }

    // Tabs panel
    constraints.push(Constraint::Length(layout.tabs_height));
    panel_ids.push("tabs");

    // Worktrees panel
    constraints.push(Constraint::Length(layout.worktree_height));
    panel_ids.push("worktrees");

    // Services panel (optional)
    if app.services_visible {
        constraints.push(Constraint::Length(layout.services_height));
        panel_ids.push("services");
    }

    // Optional panels
    if app.details_visible {
        constraints.push(Constraint::Length(layout.details_height));
        panel_ids.push("details");
    }
    if app.usage_visible {
        constraints.push(Constraint::Length(layout.usage_height));
        panel_ids.push("usage");
    }
    if app.tasks_visible {
        constraints.push(Constraint::Length(layout.tasks_height));
        panel_ids.push("tasks");
    }

    let chunks = ratatui::layout::Layout::default()
        .direction(Direction::Vertical)
        .constraints(constraints)
        .split(content_area);

    for (i, &id) in panel_ids.iter().enumerate() {
        let chunk = chunks[i];
        match id {
            "notify" => render_notify_panel(frame, chunk, app),
            "tabs" => render_tabs_panel(frame, chunk, app, overlay_active),
            "worktrees" => render_worktrees_panel(frame, chunk, app, overlay_active),
            "services" => render_services_panel(frame, chunk, app, overlay_active),
            "details" => render_details_panel(frame, chunk, app, overlay_active),
            "usage" => render_usage_panel(frame, chunk, app),
            "tasks" => render_tasks_panel(frame, chunk, app, overlay_active),
            _ => {}
        }
    }
}

fn render_notify_panel(frame: &mut Frame, area: Rect, app: &App) {
    if area.height == 0 {
        return;
    }
    // Messages and confirm_quit render as top bar — show idle state here
    if app.confirm_quit || matches!(&app.notify_state, overlay::NotifyState::Message { .. }) {
        return;
    }
    // Picker, Input, Confirm overlays still render in this panel
    overlay::render_notify(frame, area, &app.notify_state);
}

fn render_tabs_panel(frame: &mut Frame, area: Rect, app: &App, overlay_active: bool) {
    let focused = app.focus == Panel::Terminal && !app.terminal_focused && !overlay_active;
    render_section_header(frame, area, "tabs", focused);

    if app.tabs.is_empty() {
        if area.height > 1 {
            let content_area = Rect::new(area.x, area.y + 1, area.width, area.height.saturating_sub(1));
            frame.render_widget(
                Paragraph::new(Line::from(Span::styled(
                    " No sessions open",
                    Style::default().fg(DIM_TEXT_COLOR),
                ))),
                content_area,
            );
        }
        return;
    }

    let inner_w = area.width.saturating_sub(1) as usize;
    let inner_h = area.height.saturating_sub(1) as usize;

    // Build flat list of tab entries with sequential numbering (1-9)
    // Group headers have no number — only sessions get numbers
    let mut entries: Vec<TabEntry> = Vec::new();
    let mut seq = 1usize;
    let mut group_num = 0usize;
    for (tab_idx, tab) in app.tabs.iter().enumerate() {
        let is_active = tab_idx == app.active_tab;

        if let Some(ref split) = tab.split {
            let session_ids = split.session_ids();
            group_num += 1;

            entries.push(TabEntry {
                label: format!("Group {}", group_num),
                session_id: tab.session_id,
                num: 0,
                is_active,
                is_focused: false,
                alive: tab.alive,
                exit_code: None,
                agent_state: crate::detect::AgentState::Unknown,
                is_group_head: true,
                is_group_child: false,
            });

            // Each child gets a number
            for sid in &session_ids {
                let child_label = app.pty_mgr.get(*sid)
                    .map(|s| s.label.clone())
                    .unwrap_or_else(|| format!("session-{}", sid));
                let child_exit = app.pty_mgr.get(*sid).and_then(|s| s.exit_code);
                let child_focused = app.focused_session_id == Some(*sid) && app.terminal_focused;
                entries.push(TabEntry {
                    label: child_label,
                    session_id: *sid,
                    num: seq,
                    is_active,
                    is_focused: child_focused,
                    alive: app.pty_mgr.get(*sid).map(|s| s.alive).unwrap_or(false),
                    exit_code: child_exit,
                    agent_state: app.agent_states.get(sid).copied().unwrap_or(crate::detect::AgentState::Unknown),
                    is_group_head: false,
                    is_group_child: true,
                });
                seq += 1;
            }
        } else {
            let tab_exit = app.pty_mgr.get(tab.session_id).and_then(|s| s.exit_code);
            let tab_focused = app.focused_session_id == Some(tab.session_id) && app.terminal_focused && is_active;
            entries.push(TabEntry {
                label: tab.label.clone(),
                session_id: tab.session_id,
                num: seq,
                is_active,
                is_focused: tab_focused,
                alive: tab.alive,
                exit_code: tab_exit,
                agent_state: app.agent_states.get(&tab.session_id).copied().unwrap_or(crate::detect::AgentState::Unknown),
                is_group_head: false,
                is_group_child: false,
            });
            seq += 1;
        }
    }

    let total = entries.len();
    let cursor = app.tab_cursor.min(total.saturating_sub(1));
    let (start, end) = visible_window(total, cursor, inner_h);

    // Find the session ID under the cursor (for dot map highlighting)
    let cursor_session_id = entries.get(cursor).map(|e| e.session_id);

    // Find active group's dot map
    let active_tab = app.tabs.get(app.active_tab);
    let dot_map_lines = active_tab
        .and_then(|t| t.split.as_ref())
        .map(|split| split.dot_map(cursor_session_id))
        .unwrap_or_default();

    let mut lines: Vec<Line> = entries[start..end]
        .iter()
        .enumerate()
        .map(|(i, entry)| {
            let idx = start + i;
            let is_cursor = idx == cursor;
            format_tab_entry(entry, inner_w, is_cursor, focused, app.spin_frame)
        })
        .collect();

    // Pad to fill inner_h, then overlay dot map at bottom-right
    while lines.len() < inner_h {
        lines.push(Line::from(""));
    }

    if !dot_map_lines.is_empty() {
        use crate::pty::split::DotMapSpan;
        let map_h = dot_map_lines.len();
        for (j, map_line) in dot_map_lines.iter().enumerate() {
            let line_idx = inner_h.saturating_sub(map_h) + j;
            if line_idx < lines.len() {
                let map_spans: Vec<Span> = map_line.spans.iter().map(|s| match s {
                    DotMapSpan::Dot { highlighted: true } => {
                        Span::styled("●", Style::default().fg(HINT_COLOR))
                    }
                    DotMapSpan::Dot { highlighted: false } => {
                        Span::styled("●", Style::default().fg(BORDER_COLOR))
                    }
                    DotMapSpan::Space => Span::raw(" "),
                }).collect();
                let map_w: usize = map_line.spans.len(); // each span is 1 char wide
                let pad = inner_w.saturating_sub(map_w + 1);
                let mut spans = vec![Span::raw(" ".repeat(pad))];
                spans.extend(map_spans);
                lines[line_idx] = Line::from(spans);
            }
        }
    }

    let content_area = Rect::new(area.x, area.y + 1, area.width, area.height.saturating_sub(1));
    let content = Paragraph::new(lines);
    frame.render_widget(content, content_area);
}

struct TabEntry {
    label: String,
    session_id: usize,
    num: usize,
    is_active: bool,
    is_focused: bool,
    alive: bool,
    exit_code: Option<u32>,
    agent_state: crate::detect::AgentState,
    is_group_head: bool,
    is_group_child: bool,
}

fn format_tab_entry(entry: &TabEntry, width: usize, is_cursor: bool, panel_focused: bool, spin_frame: usize) -> Line<'static> {
    use crate::detect::AgentState;

    // Icon based on state
    let (icon, icon_color) = if entry.is_group_head {
        ("☰", HEADER_COLOR) // sandwich for group
    } else if !entry.alive {
        if entry.exit_code == Some(0) {
            ("●", RUNNING_COLOR)
        } else if entry.exit_code.is_some() {
            ("●", STOPPED_COLOR)
        } else {
            ("○", DIM_TEXT_COLOR)
        }
    } else {
        match entry.agent_state {
            AgentState::Working => {
                let frame = SPIN_FRAMES[spin_frame % SPIN_FRAMES.len()];
                (frame, HINT_COLOR)
            }
            AgentState::Blocked => ("!", STOPPED_COLOR),
            AgentState::Idle => ("●", RUNNING_COLOR),
            AgentState::Unknown => ("■", if entry.is_active { HIGHLIGHT_COLOR } else { DIM_TEXT_COLOR }),
        }
    };

    // Number suffix: thin gray (N)
    let num_suffix = if entry.num > 0 && entry.num <= 9 {
        format!(" ({})", entry.num)
    } else {
        String::new()
    };

    let num_style = Style::default().fg(HEADER_COLOR);
    let label_style = if entry.is_group_head {
        Style::default().fg(DIM_TEXT_COLOR)
    } else if entry.is_group_child {
        Style::default().fg(DIM_TEXT_COLOR)
    } else if entry.is_active {
        Style::default().fg(HIGHLIGHT_COLOR)
    } else {
        Style::default()
    };

    // Build the line
    if entry.is_group_head {
        // ☰ Group N
        let line_text = format!(" {} {}", icon, entry.label);
        if is_cursor && panel_focused {
            return Line::from(Span::styled(
                truncate_pad(&line_text, width),
                Style::default().fg(Color::White).bg(SELECTED_BG_COLOR).bold(),
            ));
        }
        return Line::from(vec![
            Span::raw(" "),
            Span::styled(icon.to_string(), Style::default().fg(icon_color)),
            Span::raw(" "),
            Span::styled(entry.label.clone(), label_style),
        ]);
    }

    // indent icon label (N)
    let indent = if entry.is_group_child { "   " } else { " " };

    if is_cursor && panel_focused {
        let line = format!("{}{} {}{}", indent, icon, entry.label, num_suffix);
        return Line::from(Span::styled(
            truncate_pad(&line, width),
            Style::default().fg(Color::White).bg(SELECTED_BG_COLOR).bold(),
        ));
    }
    if is_cursor {
        let line = format!("{}{} {}{}", indent, icon, entry.label, num_suffix);
        return Line::from(Span::styled(
            truncate_pad(&line, width),
            Style::default().bold(),
        ));
    }

    Line::from(vec![
        Span::raw(indent.to_string()),
        Span::styled(icon.to_string(), Style::default().fg(icon_color)),
        Span::raw(" "),
        Span::styled(entry.label.clone(), label_style),
        Span::styled(num_suffix, num_style),
    ])
}

fn truncate_pad(s: &str, width: usize) -> String {
    if s.len() > width {
        format!("{}~", &s[..width.saturating_sub(1)])
    } else {
        format!("{}{}", s, " ".repeat(width.saturating_sub(s.len())))
    }
}

fn render_worktrees_panel(frame: &mut Frame, area: Rect, app: &App, overlay_active: bool) {
    let focused = app.focus == Panel::Worktrees && !overlay_active;
    render_section_header(frame, area, "worktrees", focused);

    let content_area = Rect::new(area.x, area.y + 1, area.width, area.height.saturating_sub(1));
    if app.worktrees.is_empty() {
        frame.render_widget(
            Paragraph::new(Line::from(Span::styled(" No worktrees discovered", Style::default().fg(DIM_TEXT_COLOR)))),
            content_area,
        );
        return;
    }

    let inner_w = content_area.width.saturating_sub(1) as usize;
    let inner_h = content_area.height as usize;
    let total = app.worktrees.len();
    let visible_items = inner_h / 2; // 2 lines per worktree
    let (start, end) = visible_window(total, app.cursor, visible_items);

    let lines: Vec<Line> = app.worktrees[start..end]
        .iter()
        .enumerate()
        .flat_map(|(i, wt)| {
            let idx = start + i;
            let selected = idx == app.cursor;
            let is_build_project = app.cfg.as_ref().map_or(false, |c| c.dash.build.is_some());
            format_worktree_lines(wt, inner_w, selected, focused, is_build_project)
        })
        .collect();

    frame.render_widget(Paragraph::new(lines), content_area);
}

fn format_worktree_lines(wt: &crate::worktree::Worktree, _width: usize, selected: bool, panel_focused: bool, is_build_project: bool) -> Vec<Line<'static>> {
    use crate::worktree::WorktreeType;

    let name = if wt.alias.is_empty() { &wt.name } else { &wt.alias };
    let is_root = wt.alias == "Root";

    // Second line: type/status
    let sub = if is_root {
        "project".to_string()
    } else if wt.health.ends_with("...") && !wt.running {
        wt.health.trim_end_matches("...").to_string()
    } else if is_build_project {
        "build".to_string()
    } else if wt.running && wt.wt_type == WorktreeType::Local {
        "running".to_string()
    } else if wt.running {
        "running".to_string()
    } else if wt.container_exists {
        "docker".to_string()
    } else if wt.wt_type == WorktreeType::Local {
        "local".to_string()
    } else {
        "local".to_string()
    };

    // Status indicator
    let (icon, icon_color) = if is_root {
        ("◆", FOCUS_BORDER_COLOR)
    } else if wt.health.ends_with("...") {
        ("◐", STARTING_COLOR)
    } else if wt.running {
        ("●", RUNNING_COLOR)
    } else if wt.container_exists {
        ("○", STOPPED_COLOR)
    } else {
        ("◇", DIM_TEXT_COLOR)
    };

    let name_style = if selected && panel_focused {
        Style::default().fg(Color::White).bg(SELECTED_BG_COLOR).bold()
    } else if selected {
        Style::default().bold()
    } else {
        Style::default()
    };

    let icon_style = if selected && panel_focused {
        Style::default().fg(Color::White).bg(SELECTED_BG_COLOR)
    } else {
        Style::default().fg(icon_color)
    };

    let sub_style = if selected && panel_focused {
        Style::default().fg(Color::White).bg(SELECTED_BG_COLOR)
    } else {
        Style::default().fg(HEADER_COLOR)
    };

    let bg_style = if selected && panel_focused {
        Style::default().bg(SELECTED_BG_COLOR)
    } else {
        Style::default()
    };

    vec![
        Line::from(vec![
            Span::styled(" ", bg_style),
            Span::styled(icon.to_string(), icon_style),
            Span::styled(" ", bg_style),
            Span::styled(name.to_string(), name_style),
        ]),
        Line::from(vec![
            Span::styled("   ", bg_style),
            Span::styled(sub, sub_style),
        ]),
    ]
}

fn render_services_panel(frame: &mut Frame, area: Rect, app: &App, overlay_active: bool) {
    let focused = app.focus == Panel::Services && !overlay_active;
    render_section_header(frame, area, "services", focused);

    let content_area = Rect::new(area.x, area.y + 2, area.width, area.height.saturating_sub(2));
    if app.services.is_empty() {
        frame.render_widget(
            Paragraph::new(Line::from(Span::styled(" No services", Style::default().fg(DIM_TEXT_COLOR)))),
            content_area,
        );
        return;
    }

    let inner_w = content_area.width.saturating_sub(1) as usize;
    let inner_h = content_area.height as usize;
    let total = app.services.len();
    let (start, end) = visible_window(total, app.service_cursor, inner_h);

    let lines: Vec<Line> = app.services[start..end]
        .iter()
        .enumerate()
        .map(|(i, svc)| {
            let idx = start + i;
            let selected = idx == app.service_cursor;
            format_service_line(svc, inner_w, selected, focused)
        })
        .collect();

    frame.render_widget(Paragraph::new(lines), content_area);
}

fn format_service_line(svc: &crate::worktree::Service, width: usize, selected: bool, panel_focused: bool) -> Line<'static> {
    let (icon, icon_color) = match svc.status.as_str() {
        "online" => ("●", RUNNING_COLOR),
        "stopped" => ("○", STOPPED_COLOR),
        _ => ("?", DIM_TEXT_COLOR),
    };

    let right = if svc.status == "online" && (svc.cpu > 0.0 || svc.memory > 0) {
        let mem_mb = svc.memory / (1024 * 1024);
        format!("{:.0}% {}MB", svc.cpu, mem_mb)
    } else {
        svc.status.clone()
    };

    let name = &svc.display_name;
    let label_w = 1 + 1 + 1 + name.len();
    let right_w = right.len();
    let pad = width.saturating_sub(label_w + right_w + 1).max(1);
    let padding = " ".repeat(pad);

    if selected && panel_focused {
        let line_str = format!(" {} {} {}{} ", icon, name, padding, right);
        Line::from(Span::styled(
            line_str,
            Style::default().fg(Color::White).bg(SELECTED_BG_COLOR).bold(),
        ))
    } else if selected {
        let line_str = format!(" {} {} {}{} ", icon, name, padding, right);
        Line::from(Span::styled(line_str, Style::default().bold()))
    } else {
        Line::from(vec![
            Span::raw(" "),
            Span::styled(icon.to_string(), Style::default().fg(icon_color)),
            Span::raw(" "),
            Span::styled(name.to_string(), Style::default().fg(DIM_TEXT_COLOR)),
            Span::styled(padding, Style::default()),
            Span::styled(right, Style::default().fg(HEADER_COLOR)),
            Span::raw(" "),
        ])
    }
}

fn render_details_panel(frame: &mut Frame, area: Rect, app: &App, overlay_active: bool) {
    let focused = app.focus == Panel::Details && !overlay_active;
    render_section_header(frame, area, "details", focused);

    let content_area = Rect::new(area.x, area.y + 2, area.width, area.height.saturating_sub(2));
    let text = if app.cursor < app.worktrees.len() {
        build_detail_lines(&app.worktrees[app.cursor], app.spin_frame, app.cfg.as_ref())
    } else {
        vec![Line::from(Span::styled(
            " No worktree selected",
            Style::default().fg(DIM_TEXT_COLOR),
        ))]
    };

    frame.render_widget(Paragraph::new(text), content_area);
}

fn build_detail_lines(wt: &crate::worktree::Worktree, spin_frame: usize, cfg: Option<&crate::config::Config>) -> Vec<Line<'static>> {
    use crate::worktree::WorktreeType;

    let label_style = Style::default().fg(Color::Indexed(248));

    let type_label = if cfg.map_or(false, |c| c.dash.build.is_some()) {
        "build"
    } else {
        match wt.wt_type { WorktreeType::Docker => "docker", WorktreeType::Local => "local" }
    };

    let mut lines = vec![
        detail_line("Branch", &wt.branch, label_style),
        detail_line("Alias", &wt.alias, label_style),
        detail_line("Type", type_label, label_style),
    ];

    // Status
    if wt.health.ends_with("...") {
        let action = wt.health.trim_end_matches("...");
        lines.push(Line::from(vec![
            Span::styled("Status:   ", label_style),
            Span::styled(format!("{} {}", SPIN_FRAMES[spin_frame % SPIN_FRAMES.len()], action), Style::default().fg(STARTING_COLOR)),
        ]));
    } else if wt.wt_type == WorktreeType::Docker {
        let (status_text, color) = if wt.running {
            let s = if wt.health.is_empty() { "running" } else { &wt.health };
            let c = if wt.health == "starting" { STARTING_COLOR } else { RUNNING_COLOR };
            (s.to_string(), c)
        } else {
            ("stopped".to_string(), STOPPED_COLOR)
        };
        lines.push(Line::from(vec![
            Span::styled("Status:   ", label_style),
            Span::styled(status_text, Style::default().fg(color)),
        ]));
    } else if wt.running {
        lines.push(Line::from(vec![
            Span::styled("Status:   ", label_style),
            Span::styled("running (dev)", Style::default().fg(RUNNING_COLOR)),
        ]));
    } else {
        lines.push(Line::from(vec![
            Span::styled("Status:   ", label_style),
            Span::styled("stopped", Style::default().fg(STOPPED_COLOR)),
        ]));
    }

    if !wt.domain.is_empty() {
        lines.push(detail_line("Domain", &wt.domain, label_style));
    }
    if !wt.db_name.is_empty() {
        lines.push(detail_line("Database", &wt.db_name, label_style));
    }

    if wt.running {
        if !wt.cpu.is_empty() {
            lines.push(detail_line("CPU", &wt.cpu, label_style));
        }
        if !wt.mem.is_empty() {
            lines.push(detail_line("Memory", &wt.mem, label_style));
        }
        if !wt.uptime.is_empty() {
            lines.push(detail_line("Uptime", &wt.uptime, label_style));
        }
    }

    if !wt.container.is_empty() {
        lines.push(detail_line("Container", &wt.container, label_style));
    }

    // Ports — from worktree (Docker) or config (local)
    let show_ports = if wt.offset != 0 {
        // Docker: use computed ports from worktree
        let mut ports: Vec<_> = wt.ports.iter().map(|(k, v)| (k.clone(), *v)).collect();
        ports.sort_by_key(|(_, v)| *v);
        Some(ports)
    } else if wt.running {
        // Local: read base ports from config
        cfg.and_then(|c| {
            let ports = &c.services.ports;
            if ports.is_empty() { return None; }
            let offset = wt.offset;
            let mut result: Vec<_> = ports.iter()
                .map(|(k, v)| (k.clone(), v + offset))
                .collect();
            result.sort_by_key(|(_, v)| *v);
            Some(result)
        })
    } else {
        None
    };

    if let Some(ports) = show_ports {
        if !ports.is_empty() {
            lines.push(Line::from(""));
            if wt.running {
                // Show as clickable URLs
                for (name, port) in &ports {
                    let url = format!("http://localhost:{}", port);
                    lines.push(Line::from(vec![
                        Span::styled(format!("  {:<12}", name), label_style),
                        Span::styled(url, Style::default().fg(FOCUS_BORDER_COLOR)),
                    ]));
                }
            } else {
                lines.push(Line::from(Span::styled("Ports", label_style)));
                for (name, port) in &ports {
                    lines.push(Line::from(vec![
                        Span::styled(format!("  {:<20}", name), Style::default().fg(Color::Indexed(248))),
                        Span::styled(format!("{}", port), Style::default().fg(Color::Indexed(252))),
                    ]));
                }
            }
        }
    }

    lines.push(Line::from(""));
    // Path — canonicalize and wrap across lines (~34 chars per line for the panel)
    let canon_path = wt.path.canonicalize()
        .unwrap_or_else(|_| wt.path.clone())
        .to_string_lossy()
        .to_string();
    lines.push(Line::from(Span::styled("Path:", label_style)));
    let wrap_width = 34;
    let mut remaining = canon_path.as_str();
    while !remaining.is_empty() {
        let (chunk, rest) = if remaining.len() > wrap_width {
            // Try to break at a /
            let break_at = remaining[..wrap_width]
                .rfind('/')
                .map(|i| i + 1)
                .unwrap_or(wrap_width);
            (&remaining[..break_at], &remaining[break_at..])
        } else {
            (remaining, "")
        };
        lines.push(Line::from(Span::styled(
            format!("  {}", chunk),
            Style::default().fg(Color::Indexed(252)),
        )));
        remaining = rest;
    }

    lines
}

fn detail_line(label: &str, value: &str, label_style: Style) -> Line<'static> {
    Line::from(vec![
        Span::styled(format!("{:<10}", format!("{}:", label)), label_style),
        Span::styled(value.to_string(), Style::default()),
    ])
}

fn render_usage_panel(frame: &mut Frame, area: Rect, app: &App) {
    render_section_header(frame, area, "usage", false);

    let content_area = Rect::new(area.x, area.y + 2, area.width, area.height.saturating_sub(2));
    let text = if let Some(ref err) = app.usage_err {
        vec![Line::from(Span::styled(err.clone(), Style::default().fg(STOPPED_COLOR)))]
    } else if let Some(ref data) = app.usage_data {
        let inner_w = content_area.width.saturating_sub(1) as usize;
        vec![
            render_usage_line("5h", data.five_hour.utilization, data.five_hour.resets_at.as_deref(), inner_w),
            render_usage_line("7d", data.seven_day.utilization, data.seven_day.resets_at.as_deref(), inner_w),
        ]
    } else {
        vec![Line::from(Span::styled(" Not loaded", Style::default().fg(DIM_TEXT_COLOR)))]
    };

    frame.render_widget(Paragraph::new(text), content_area);
}

fn render_tasks_panel(frame: &mut Frame, area: Rect, app: &App, overlay_active: bool) {
    let focused = app.focus == crate::app::Panel::Tasks && !overlay_active;
    let is_detail = app.tasks_detail.is_some();
    let header = if is_detail { "task detail" } else { "tasks" };
    render_section_header(frame, area, header, focused);

    let content_area = Rect::new(area.x, area.y + 2, area.width, area.height.saturating_sub(2));

    if let Some(ref err) = app.tasks_err {
        frame.render_widget(Paragraph::new(Line::from(Span::styled(
            err.clone(), Style::default().fg(STOPPED_COLOR),
        ))), content_area);
        return;
    }

    // Detail view
    if let Some(ref task) = app.tasks_detail {
        let inner_h = content_area.height as usize;

        let p = match &task.priority {
            serde_json::Value::Number(n) => n.as_u64().unwrap_or(9),
            _ => 9,
        };
        let priority_color = match p {
            0 => STOPPED_COLOR,
            1 => HINT_COLOR,
            2 => Color::Yellow,
            _ => DIM_TEXT_COLOR,
        };

        let mut lines: Vec<Line> = Vec::new();
        lines.push(Line::from(Span::styled(&task.title, Style::default().bold())));
        lines.push(Line::from(vec![
            Span::styled(format!("P{} ", p), Style::default().fg(priority_color)),
            Span::styled(format!("{} ", task.task_type), Style::default().fg(FOCUS_BORDER_COLOR)),
            Span::styled(&task.id, Style::default().fg(DIM_TEXT_COLOR)),
        ]));
        lines.push(Line::from(Span::styled(
            format!("Status: {}", task.status),
            Style::default().fg(if task.status == "open" { RUNNING_COLOR } else { DIM_TEXT_COLOR }),
        )));
        if !task.description.is_empty() {
            lines.push(Line::from(""));
            for desc_line in task.description.lines() {
                lines.push(Line::from(Span::styled(desc_line.to_string(), Style::default().fg(DIM_TEXT_COLOR))));
            }
        }
        if !task.labels.is_empty() {
            lines.push(Line::from(""));
            lines.push(Line::from(Span::styled(
                format!("Labels: {}", task.labels.join(", ")),
                Style::default().fg(DIM_TEXT_COLOR),
            )));
        }

        let total = lines.len();
        let scroll = app.tasks_detail_scroll.min(total.saturating_sub(inner_h));
        let end = (scroll + inner_h).min(total);
        let visible = lines[scroll..end].to_vec();

        frame.render_widget(Paragraph::new(visible), content_area);
        return;
    }

    // List view
    if app.tasks_list.is_empty() {
        frame.render_widget(Paragraph::new(Line::from(Span::styled(
            " No open tasks", Style::default().fg(DIM_TEXT_COLOR),
        ))), content_area);
        return;
    }

    let inner_h = content_area.height as usize;
    let inner_w = content_area.width.saturating_sub(1) as usize;
    let total = app.tasks_list.len();
    let (start, end) = visible_window(total, app.tasks_cursor, inner_h);

    let lines: Vec<Line> = app.tasks_list[start..end]
        .iter()
        .enumerate()
        .map(|(i, task)| {
            let idx = start + i;
            let selected = idx == app.tasks_cursor;

            let p = match &task.priority {
                serde_json::Value::Number(n) => n.as_u64().unwrap_or(9),
                serde_json::Value::String(s) => s.trim_start_matches('P').parse().unwrap_or(9),
                _ => 9,
            };
            let priority_color = match p {
                0 => STOPPED_COLOR,
                1 => HINT_COLOR,
                2 => Color::Yellow,
                _ => DIM_TEXT_COLOR,
            };
            let type_color = match task.task_type.as_str() {
                "bug" => STOPPED_COLOR,
                "feature" | "feat" => FOCUS_BORDER_COLOR,
                _ => DIM_TEXT_COLOR,
            };

            let max_title = inner_w.saturating_sub(task.id.len() + task.task_type.len() + 8);

            if selected && focused {
                Line::from(Span::styled(
                    format!(" P{} {} {} {}", p, task.task_type, task.id, truncate(&task.title, max_title)),
                    Style::default().fg(Color::White).bg(SELECTED_BG_COLOR).bold(),
                ))
            } else {
                Line::from(vec![
                    Span::styled(format!(" P{} ", p), Style::default().fg(priority_color)),
                    Span::styled(format!("{} ", task.task_type), Style::default().fg(type_color)),
                    Span::styled(format!("{} ", task.id), Style::default().fg(DIM_TEXT_COLOR)),
                    Span::styled(truncate(&task.title, max_title), Style::default().fg(if selected { Color::White } else { DIM_TEXT_COLOR })),
                ])
            }
        })
        .collect();

    frame.render_widget(Paragraph::new(lines), content_area);
}

fn render_usage_line<'a>(label: &str, pct: f64, resets_at: Option<&str>, width: usize) -> Line<'a> {
    let color = if pct >= 80.0 {
        Color::Indexed(196) // bright red
    } else if pct >= 50.0 {
        STARTING_COLOR
    } else {
        RUNNING_COLOR
    };

    // Format: "5h [████░░░░░░]  15% resets 4h32m"
    let countdown = resets_at
        .and_then(|s| chrono::DateTime::parse_from_rfc3339(s).ok())
        .map(|t| {
            let diff = t.signed_duration_since(chrono::Utc::now());
            let mins = diff.num_minutes();
            if mins <= 0 { "now".to_string() }
            else if mins < 60 { format!("{}m", mins) }
            else if mins < 1440 { format!("{}h {}m", mins / 60, mins % 60) }
            else { let hours = mins / 60; format!("{}d {}h {}m", hours / 24, hours % 24, mins % 60) }
        })
        .unwrap_or_default();

    let prefix = format!("{} ", label);
    let pct_str = format!(" {:3.0}%", pct);
    let reset_str = if countdown.is_empty() {
        String::new()
    } else {
        format!(" resets {}", countdown)
    };

    let bar_w = width.saturating_sub(prefix.len() + pct_str.len() + reset_str.len());
    let inner = bar_w.saturating_sub(2).max(1);
    let filled = ((pct / 100.0) * inner as f64).clamp(0.0, inner as f64) as usize;
    let empty = inner - filled;

    let mut spans = vec![
        Span::styled(prefix, Style::default().fg(HEADER_COLOR)),
        Span::styled("[", Style::default().fg(DIM_TEXT_COLOR)),
        Span::styled("█".repeat(filled), Style::default().fg(color)),
        Span::styled("░".repeat(empty), Style::default().fg(BORDER_COLOR)),
        Span::styled("]", Style::default().fg(DIM_TEXT_COLOR)),
        Span::styled(pct_str, Style::default().fg(color)),
    ];
    if !reset_str.is_empty() {
        spans.push(Span::styled(reset_str, Style::default().fg(DIM_TEXT_COLOR)));
    }
    Line::from(spans)
}

fn truncate(s: &str, max: usize) -> String {
    if s.len() > max {
        format!("{}~", &s[..max.saturating_sub(1)])
    } else {
        s.to_string()
    }
}

fn render_terminal_area(frame: &mut Frame, area: Rect, app: &App, overlay_active: bool) {
    // Help overlay in right panel
    if app.help_open {
        help::render_help(frame, area);
        return;
    }

    // HeiHei easter egg in right panel
    if app.heihei_active {
        splash::render_splash(frame, area, "");
        return;
    }

    // Settings in right panel
    if let Some(ref state) = app.settings_state {
        settings_tui::render_settings(frame, area, state);
        return;
    }

    let focused = app.terminal_focused && !overlay_active;
    let border_color = if focused {
        FOCUS_BORDER_COLOR
    } else {
        BORDER_COLOR
    };

    // Check for active tab or preview session with a PTY to render
    let active_session_id = if let Some(preview_id) = app.preview_session {
        Some(preview_id)
    } else if !app.tabs.is_empty() && app.active_tab < app.tabs.len() {
        Some(app.tabs[app.active_tab].session_id)
    } else {
        None
    };

    // Check if active tab has a split layout
    if !app.tabs.is_empty() && app.active_tab < app.tabs.len() {
        let tab = &app.tabs[app.active_tab];

        if let Some(ref split) = tab.split {
            render_split_node(frame, area, split, &app.pty_mgr, border_color, app.focused_session_id, focused);
            return;
        }
    }

    if let Some(session_id) = active_session_id {
        if let Some(session) = app.pty_mgr.get(session_id) {
            // Focus indicator: ◥ on top-right corner of title row
            if focused && area.width > 0 {
                let indicator_x = area.x + area.width - 1;
                let buf = frame.buffer_mut();
                if area.y < buf.area().height && indicator_x < buf.area().width {
                    buf[(indicator_x, area.y)].set_symbol("◥");
                    buf[(indicator_x, area.y)].set_style(Style::default().fg(FOCUS_BORDER_COLOR));
                }
            }

            // Terminal content below empty title row
            let term_area = pad_rect(Rect::new(area.x, area.y + 1, area.width, area.height.saturating_sub(1)));
            let term_widget = term_view::TermView::new(session.term());
            frame.render_widget(term_widget, term_area);
            return;
        }
    }

    // No active session — show guide as default content
    guide::render_guide(frame, area);
}

/// Recursively render a split node tree as nested Ratatui layouts.
fn render_split_node(
    frame: &mut Frame,
    area: Rect,
    node: &crate::pty::split::SplitNode,
    pty_mgr: &crate::pty::PtyManager,
    border_color: Color,
    focused_session_id: Option<usize>,
    terminal_focused: bool,
) {
    use crate::pty::split::{SplitDir, SplitNode};

    match node {
        SplitNode::Leaf(session_id) => {
            if let Some(session) = pty_mgr.get(*session_id) {
                let is_focused = focused_session_id == Some(*session_id);

                // Focus indicator: ◥ on top-right corner of title row
                if is_focused && terminal_focused && area.width > 0 {
                    let indicator_x = area.x + area.width - 1;
                    let buf = frame.buffer_mut();
                    if indicator_x < buf.area().width && area.y < buf.area().height {
                        buf[(indicator_x, area.y)].set_symbol("◥");
                        buf[(indicator_x, area.y)].set_style(Style::default().fg(FOCUS_BORDER_COLOR));
                    }
                }

                // Terminal content below empty title row
                let term_area = pad_rect(Rect::new(area.x, area.y + 1, area.width, area.height.saturating_sub(1)));
                let term_widget = term_view::TermView::new(session.term());
                frame.render_widget(term_widget, term_area);
            }
        }
        SplitNode::Split { direction, children } => {
            let dir = match direction {
                SplitDir::Horizontal => Direction::Horizontal,
                SplitDir::Vertical => Direction::Vertical,
            };
            let sep_style = Style::default().fg(BORDER_COLOR);
            let n = children.len();

            // Build constraints: child, sep, child, sep, ..., child
            let mut constraints: Vec<Constraint> = Vec::new();
            for i in 0..n {
                constraints.push(Constraint::Ratio(1, n as u32));
                if i + 1 < n {
                    constraints.push(Constraint::Length(1)); // separator
                }
            }

            let chunks = ratatui::layout::Layout::default()
                .direction(dir)
                .constraints(constraints)
                .split(area);

            for (i, child) in children.iter().enumerate() {
                let chunk_idx = i * 2;
                render_split_node(frame, chunks[chunk_idx], child, pty_mgr, border_color, focused_session_id, terminal_focused);

                // Draw separator between children
                if i + 1 < n {
                    let sep_rect = chunks[chunk_idx + 1];
                    let buf = frame.buffer_mut();
                    match direction {
                        SplitDir::Horizontal => {
                            // Vertical line │ (skip first and last row)
                            let y_start = sep_rect.y + 1;
                            let y_end = (sep_rect.y + sep_rect.height).saturating_sub(1);
                            for y in y_start..y_end {
                                if sep_rect.x < buf.area().width {
                                    buf[(sep_rect.x, y)].set_symbol("│");
                                    buf[(sep_rect.x, y)].set_style(sep_style);
                                }
                            }
                        }
                        SplitDir::Vertical => {
                            // Horizontal line ─ (skip first and last col)
                            let x_start = sep_rect.x + 1;
                            let x_end = (sep_rect.x + sep_rect.width).saturating_sub(1);
                            for x in x_start..x_end {
                                if sep_rect.y < buf.area().height {
                                    buf[(x, sep_rect.y)].set_symbol("─");
                                    buf[(x, sep_rect.y)].set_style(sep_style);
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

// ── Helpers ──────────────────────────────────────────────────────────────

fn render_status_bar(frame: &mut Frame, area: Rect, app: &App) {
    let k = Style::default().fg(HINT_COLOR);
    let d = Style::default().fg(DIM_TEXT_COLOR);
    let bg = Style::default().bg(Color::Indexed(236));

    let shortcuts: Vec<(&str, &str)> = if app.terminal_focused {
        vec![
            ("Ctrl+]", "Detach"),
            ("Ctrl+j/k", "Switch"),
            ("Ctrl+f", "Fullscreen"),
            ("Ctrl+x", "Close"),
            ("Shift+ ↑/↓", "Scroll"),
        ]
    } else if app.confirm_quit {
        vec![
            ("Enter", "Confirm"),
            ("Esc", "Cancel"),
        ]
    } else if matches!(&app.notify_state, overlay::NotifyState::Picker { .. }) {
        vec![
            ("↑/↓", "Navigate"),
            ("Enter", "Select"),
            ("Esc", "Cancel"),
        ]
    } else if matches!(&app.notify_state, overlay::NotifyState::Input { .. }) {
        vec![
            ("Enter", "Confirm"),
            ("Esc", "Cancel"),
        ]
    } else {
        match app.focus {
            Panel::Worktrees => vec![
                ("Enter", "Actions"),
                ("b", "Shell"),
                ("c", "Claude"),
                ("n", "Create"),
                ("j/k", "Navigate"),
                ("Tab", "Next"),
                ("Shift+ Tab", "Back"),
                ("?", "Help"),
                ("Ctrl+q", "Quit"),
            ],
            Panel::Terminal => vec![
                ("Enter", "Focus"),
                ("x", "Close"),
                ("f", "Fullscreen"),
                ("r", "Rename"),
                ("Shift+ \\", "Split H"),
                ("Shift+ -", "Split V"),
                ("Tab", "Next"),
                ("Shift+ Tab", "Back"),
            ],
            Panel::Services => vec![
                ("Enter", "Preview"),
                ("l", "Logs Tab"),
                ("r", "Restart"),
                ("j/k", "Navigate"),
                ("Tab", "Next"),
                ("Shift+ Tab", "Back"),
            ],
            Panel::Details => vec![
                ("j/k", "Scroll"),
                ("Tab", "Next"),
                ("Shift+ Tab", "Back"),
            ],
            Panel::Tasks => if app.tasks_detail.is_some() {
                vec![
                    ("↑/↓", "Scroll"),
                    ("Esc", "Back to list"),
                ]
            } else {
                vec![
                    ("↑/↓", "Navigate"),
                    ("Enter", "Detail"),
                    ("c", "Close"),
                    ("d", "Delete"),
                    ("Tab", "Next"),
                ("Shift+ Tab", "Back"),
                ]
            },
        }
    };

    let mut spans: Vec<Span> = Vec::new();
    spans.push(Span::styled(" ", d)); // left padding
    for (i, (key, desc)) in shortcuts.iter().enumerate() {
        if i > 0 {
            spans.push(Span::styled("   ", d));
        }
        spans.push(Span::styled("[", d));
        spans.push(Span::styled(key.to_string(), k));
        spans.push(Span::styled(" → ", d));
        spans.push(Span::styled(desc.to_string(), d));
        spans.push(Span::styled("]", d));
    }

    let line = Line::from(spans);
    let bar = Paragraph::new(line).style(bg);
    frame.render_widget(bar, area);
}

fn panel_block(_focused: bool) -> Block<'static> {
    Block::default()
}

fn panel_title(title: &str, focused: bool) -> Span<'static> {
    let style = if focused {
        Style::default().fg(FOCUS_BORDER_COLOR).bold()
    } else {
        Style::default().fg(DIM_TEXT_COLOR)
    };
    Span::styled(title.to_string(), style)
}

/// Render a section header with trailing line: ─ title ─────────
/// Takes 2 rows: header line + blank spacer.
fn render_section_header(frame: &mut Frame, area: Rect, title: &str, focused: bool) {
    if area.height == 0 || area.width == 0 {
        return;
    }
    let title_style = if focused {
        Style::default().fg(FOCUS_BORDER_COLOR).bold()
    } else {
        Style::default().fg(HEADER_COLOR)
    };
    let line_style = Style::default().fg(BORDER_COLOR);

    let label = format!(" {} ", title);
    let remaining = (area.width as usize).saturating_sub(label.len() + 1);
    let rule = "─".repeat(remaining);

    let header_area = Rect::new(area.x, area.y, area.width, 1);
    frame.render_widget(
        Paragraph::new(Line::from(vec![
            Span::styled(label, title_style),
            Span::styled(rule, line_style),
        ])),
        header_area,
    );
}

/// Add 1-char padding on all sides of a rect.
fn pad_rect(r: Rect) -> Rect {
    Rect::new(
        r.x + 1,
        r.y,
        r.width.saturating_sub(2),
        r.height.saturating_sub(1),
    )
}

/// Draw the vertical separator between sidebar and terminal area.
fn render_sidebar_separator(frame: &mut Frame, area: Rect, _focused: bool) {
    let sep_x = area.x + area.width.saturating_sub(1);
    let style = Style::default().fg(BORDER_COLOR);
    let buf = frame.buffer_mut();
    let y_start = area.y + 1;
    let y_end = (area.y + area.height).saturating_sub(1);
    for y in y_start..y_end {
        if sep_x < buf.area().width {
            buf[(sep_x, y)].set_symbol("│");
            buf[(sep_x, y)].set_style(style);
        }
    }
}

/// Returns (start, end) indices for a scrollable window.
pub fn visible_window(total: usize, cursor: usize, max_lines: usize) -> (usize, usize) {
    if total <= max_lines {
        return (0, total);
    }
    let mut start = 0;
    if cursor >= max_lines {
        start = cursor - max_lines + 1;
    }
    let mut end = start + max_lines;
    if end > total {
        end = total;
        start = end - max_lines;
    }
    (start, end)
}

// ── Notification Bar Renderer ─────────────────────────────────────────────

fn render_notify_bar(frame: &mut Frame, area: Rect, state: &overlay::NotifyState) {
    if let overlay::NotifyState::Message { title, message, kind } = state {
        let (bg, fg) = match kind {
            overlay::NotifyKind::Success => (Color::Green, Color::Black),
            overlay::NotifyKind::Error => (Color::Red, Color::White),
            overlay::NotifyKind::Info => (Color::Cyan, Color::Black),
        };
        let action_label_fg = match kind {
            overlay::NotifyKind::Success => Color::Rgb(40, 80, 40),
            overlay::NotifyKind::Error => Color::Rgb(180, 120, 120),
            overlay::NotifyKind::Info => Color::Rgb(40, 80, 80),
        };

        let bar_style = Style::default().fg(fg).bg(bg);
        let bold = bar_style.add_modifier(ratatui::style::Modifier::BOLD);
        let action_label = Style::default().fg(action_label_fg).bg(bg);

        let mut lines: Vec<Line> = Vec::new();

        // Title row — add outcome badge only when title doesn't already say it
        let title_is_outcome = matches!(title.as_str(), "Success" | "Error" | "Info");
        if title_is_outcome {
            lines.push(Line::from(Span::styled(format!(" {} ", title), bold)));
        } else {
            let badge = match kind {
                overlay::NotifyKind::Success => " SUCCESS ",
                overlay::NotifyKind::Error => " ERROR ",
                overlay::NotifyKind::Info => " INFO ",
            };
            lines.push(Line::from(vec![
                Span::styled(format!(" {} ", title), bold),
                Span::styled(badge, action_label),
            ]));
        }

        // Message lines
        let msg_lines: Vec<&str> = message.lines().collect();
        for (i, line) in msg_lines.iter().enumerate() {
            let is_last = i == msg_lines.len() - 1;
            if is_last {
                // Last message line: right-align actions
                let hint_key = "Esc";
                let hint_label = ": close ";
                let msg_text = format!(" {}", line);
                let padding = (area.width as usize)
                    .saturating_sub(msg_text.len() + hint_key.len() + hint_label.len());
                lines.push(Line::from(vec![
                    Span::styled(msg_text, bar_style),
                    Span::styled(" ".repeat(padding), bar_style),
                    Span::styled(hint_key, bold),
                    Span::styled(hint_label, action_label),
                ]));
            } else {
                lines.push(Line::from(Span::styled(format!(" {}", line), bar_style)));
            }
        }

        let bar = Paragraph::new(lines).style(bar_style);
        frame.render_widget(bar, area);
    }
}

// ── Init Wizard Renderer ─────────────────────────────────────────────────

fn render_init_wizard(frame: &mut Frame, area: Rect, wiz: &crate::app::InitWizard) {
    use crate::app::STACKS;
    use ratatui::widgets::Clear;

    // Center a box — taller to fit stack list
    let w = 54.min(area.width);
    let h = (8 + STACKS.len() as u16 + 6).min(area.height);
    let x = (area.width.saturating_sub(w)) / 2;
    let y = (area.height.saturating_sub(h)) / 2;
    let box_area = Rect::new(x, y, w, h);

    frame.render_widget(Clear, box_area);

    let block = Block::default()
        .title(Span::styled(
            " wt init ",
            Style::default().fg(FOCUS_BORDER_COLOR).bold(),
        ))
        .borders(Borders::ALL)
        .border_style(Style::default().fg(FOCUS_BORDER_COLOR));
    let inner = block.inner(box_area);
    frame.render_widget(block, box_area);

    let mut lines: Vec<Line> = Vec::new();

    // Header
    if let Some(ref detected) = wiz.detected_stack {
        lines.push(Line::from(vec![
            Span::styled("Detected: ", Style::default().fg(Color::DarkGray)),
            Span::styled(detected.as_str(), Style::default().fg(Color::Green).bold()),
        ]));
    } else {
        lines.push(Line::styled(
            "No stack detected",
            Style::default().fg(Color::Yellow),
        ));
    }
    lines.push(Line::from(""));

    // Step 0: Project name
    let name_active = wiz.step == 0;
    let ind = if name_active { "> " } else { "  " };
    let ls = if name_active { Style::default().fg(FOCUS_BORDER_COLOR).bold() } else { Style::default().fg(Color::DarkGray) };
    let vs = if name_active { Style::default().fg(Color::White) } else { Style::default().fg(Color::Gray) };
    let cur = if name_active { "_" } else { "" };
    lines.push(Line::from(vec![
        Span::raw(ind), Span::styled("Project name: ", ls),
        Span::styled(&wiz.name, vs), Span::styled(cur, Style::default().fg(FOCUS_BORDER_COLOR)),
    ]));

    // Step 1: Stack picker
    let stack_active = wiz.step == 1;
    let ls = if stack_active { Style::default().fg(FOCUS_BORDER_COLOR).bold() } else { Style::default().fg(Color::DarkGray) };
    lines.push(Line::from(vec![
        Span::raw(if stack_active { "> " } else { "  " }),
        Span::styled("Stack:", ls),
    ]));
    for (i, (_id, label)) in STACKS.iter().enumerate() {
        let selected = i == wiz.stack_cursor;
        let marker = if selected && stack_active { " > " } else if selected { " * " } else { "   " };
        let style = if selected && stack_active {
            Style::default().fg(Color::White).bold()
        } else if selected {
            Style::default().fg(Color::Green)
        } else if stack_active {
            Style::default().fg(Color::Gray)
        } else {
            Style::default().fg(Color::DarkGray)
        };
        lines.push(Line::from(vec![
            Span::raw("    "),
            Span::styled(marker, style),
            Span::styled(*label, style),
        ]));
    }

    // Step 2: Worktrees dir
    let dir_active = wiz.step == 2;
    let ls = if dir_active { Style::default().fg(FOCUS_BORDER_COLOR).bold() } else { Style::default().fg(Color::DarkGray) };
    let vs = if dir_active { Style::default().fg(Color::White) } else { Style::default().fg(Color::Gray) };
    let cur = if dir_active { "_" } else { "" };
    lines.push(Line::from(vec![
        Span::raw(if dir_active { "> " } else { "  " }), Span::styled("Worktrees dir: ", ls),
        Span::styled(&wiz.worktrees_dir, vs), Span::styled(cur, Style::default().fg(FOCUS_BORDER_COLOR)),
    ]));

    // Preview
    lines.push(Line::from(""));
    if wiz.step == 3 {
        lines.push(Line::styled(
            "> Preview wt.config.js:",
            Style::default().fg(FOCUS_BORDER_COLOR).bold(),
        ));
    } else {
        lines.push(Line::styled(
            "  Preview:",
            Style::default().fg(Color::DarkGray),
        ));
    }
    lines.push(Line::styled(
        format!("  name: '{}'", wiz.name),
        Style::default().fg(Color::Gray),
    ));
    lines.push(Line::styled(
        format!("  stack: '{}'", wiz.stack_id()),
        Style::default().fg(Color::Gray),
    ));
    lines.push(Line::styled(
        format!("  worktreesDir: '{}'", wiz.worktrees_dir),
        Style::default().fg(Color::Gray),
    ));

    // Footer
    lines.push(Line::from(""));
    let footer = if wiz.step == 3 {
        "Enter: Create  |  Esc: Cancel"
    } else {
        "Tab/Enter: Next  |  Shift+Tab: Back  |  Esc: Cancel"
    };
    lines.push(Line::styled(footer, Style::default().fg(Color::DarkGray)));

    let paragraph = Paragraph::new(lines);
    frame.render_widget(paragraph, inner);
}
