use ratatui::{
    prelude::*,
    widgets::{Block, Borders, Paragraph},
};

use super::style::*;

// ── Picker types ─────────────────────────────────────────────────────

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PickerAction {
    pub key: String,
    pub label: String,
    pub desc: String,
}

impl PickerAction {
    pub fn new(key: &str, label: &str, desc: &str) -> Self {
        Self {
            key: key.to_string(),
            label: label.to_string(),
            desc: desc.to_string(),
        }
    }
}

// ── Predefined action sets ───────────────────────────────────────────

pub fn maintenance_actions() -> Vec<PickerAction> {
    vec![
        PickerAction::new("p", "Prune", "Remove orphaned volumes"),
        PickerAction::new("s", "Autostop", "Stop idle containers"),
        PickerAction::new("r", "Rebuild", "Rebuild base image"),
    ]
}

pub fn split_session_actions() -> Vec<PickerAction> {
    vec![
        PickerAction::new("b", "Shell", "Container shell"),
        PickerAction::new("c", "Claude", "Claude Code"),
        PickerAction::new("z", "Zsh", "Host shell"),
        PickerAction::new("l", "Logs", "Container logs"),
    ]
}

pub fn merge_direction_actions() -> Vec<PickerAction> {
    vec![
        PickerAction::new("|", "Side by side", "Vertical divider"),
        PickerAction::new("_", "Below", "Horizontal divider"),
    ]
}

pub fn remove_actions() -> Vec<PickerAction> {
    vec![
        PickerAction::new("n", "Normal", "Fails if dirty"),
        PickerAction::new("f", "Force", "Even if dirty"),
    ]
}

// ── Toast notifications (floating top-right) ────────────────────────

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ToastKind {
    Info,
    Success,
    Error,
}

/// Action that a toast can require before dismissal.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ToastAction {
    /// Confirm/cancel prompt (e.g. quit). Ctrl+A confirms, Esc cancels.
    Confirm,
    /// Error that must be acknowledged. Esc dismisses.
    Acknowledge,
}

#[derive(Debug, Clone)]
pub struct Toast {
    pub title: String,
    pub message: String,
    pub kind: ToastKind,
    pub created_at: std::time::Instant,
    pub duration: Option<std::time::Duration>, // None = requires manual dismiss
    pub action: Option<ToastAction>,
}

impl Toast {
    pub fn height(&self, wrap_width: usize) -> u16 {
        let inner_w = wrap_width.saturating_sub(3); // 2 left pad + 1 right pad
        let msg_lines: u16 = self.message.lines()
            .map(|l| if l.is_empty() { 1 } else { ((l.len() as f32 / inner_w.max(1) as f32).ceil() as u16).max(1) })
            .sum::<u16>()
            .max(1);
        msg_lines + 1 + 1 + 2 // +1 title, +1 action line, +2 top/bottom padding
    }

    pub fn requires_action(&self) -> bool {
        self.action.is_some()
    }

    pub fn is_expired(&self) -> bool {
        match self.duration {
            Some(d) => self.created_at.elapsed() >= d,
            None => false,
        }
    }
}

// ── Overlay state (pickers, input — left sidebar) ───────────────────

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum NotifyState {
    Idle,
    Picker { title: String, actions: Vec<PickerAction>, cursor: usize },
    Confirm { prompt: String },
    Input { prompt: String, value: String },
}

impl NotifyState {
    pub fn height(&self) -> u16 {
        match self {
            NotifyState::Idle => 0,
            NotifyState::Picker { actions, .. } => (actions.len() as u16 + 2).max(3),
            NotifyState::Confirm { .. } => 5,
            NotifyState::Input { .. } => 3,
        }
    }
}

// ── Toast rendering (floating top-right) ────────────────────────────

/// Render floating toast notifications stacked in the top-right corner.
/// `area` is the full terminal area. Toasts use 30% width, 1 space padding from top/right.
/// Style matches the original notification bar: HEADER_BG background, no borders.
pub fn render_toasts(frame: &mut Frame, area: Rect, toasts: &[Toast]) {
    use ratatui::widgets::Clear;

    if toasts.is_empty() || area.width < 10 || area.height < 4 {
        return;
    }

    let toast_width = (area.width * 30 / 100).max(20);
    let x = area.x + area.width - toast_width; // flush right
    let mut y = area.y + 1; // 1 space top padding

    let bg = HEADER_BG;

    for toast in toasts {
        let h = toast.height(toast_width as usize);

        if y + h > area.y + area.height {
            break; // no more room
        }

        let toast_area = Rect::new(x, y, toast_width, h);

        let title_color = match toast.kind {
            ToastKind::Success => RUNNING_COLOR,
            ToastKind::Error => STOPPED_COLOR,
            ToastKind::Info => HINT_COLOR,
        };

        let bar_style = Style::default().fg(DIM_TEXT_COLOR).bg(bg);
        let bold = Style::default().fg(title_color).bg(bg).add_modifier(ratatui::style::Modifier::BOLD);
        let action_label = Style::default().fg(HEADER_COLOR).bg(bg);

        // Clear content underneath
        frame.render_widget(Clear, toast_area);

        let mut lines: Vec<Line> = Vec::new();
        let w = toast_width as usize;

        // Top padding
        lines.push(Line::from(Span::styled(" ".repeat(w), bar_style)));

        // Title row (with left padding)
        lines.push(Line::from(Span::styled(format!("  {} ", toast.title), bold)));

        // Message lines — wrap long lines to fit
        let inner_w = w.saturating_sub(3); // 2 left pad + 1 right pad
        for l in toast.message.lines() {
            if l.is_empty() {
                lines.push(Line::from(Span::styled(" ".repeat(w), bar_style)));
            } else {
                for chunk in super::wrap_text(l, inner_w) {
                    lines.push(Line::from(Span::styled(format!("  {}", chunk), bar_style)));
                }
            }
        }

        // Action line — right-aligned on its own line
        let action_spans: Vec<Span> = match &toast.action {
            Some(ToastAction::Confirm) => vec![
                Span::styled("Ctrl+a", bold),
                Span::styled(": confirm ", action_label),
                Span::styled("Esc", bold),
                Span::styled(": cancel ", action_label),
            ],
            _ => vec![
                Span::styled("Esc", bold),
                Span::styled(": dismiss ", action_label),
            ],
        };
        let action_w: usize = action_spans.iter().map(|s| s.width()).sum();
        let padding = w.saturating_sub(action_w + 1);
        let mut action_line = vec![Span::styled(" ".repeat(padding), bar_style)];
        action_line.extend(action_spans);
        action_line.push(Span::styled(" ", bar_style));
        lines.push(Line::from(action_line));

        // Bottom padding
        lines.push(Line::from(Span::styled(" ".repeat(w), bar_style)));

        let content = Paragraph::new(lines).style(Style::default().bg(bg));
        frame.render_widget(content, toast_area);

        y += h + 1; // stack next toast below + 1 line gap
    }
}

// ── Overlay rendering (pickers, input, confirm) ─────────────────────

pub fn render_notify(frame: &mut Frame, area: Rect, state: &NotifyState) {
    match state {
        NotifyState::Idle => render_idle(frame, area),
        NotifyState::Picker { title, actions, cursor } => render_picker(frame, area, actions, *cursor, title),
        NotifyState::Confirm { prompt } => render_confirm(frame, area, prompt),
        NotifyState::Input { prompt, value } => render_input(frame, area, prompt, value),
    }
}

fn render_idle(frame: &mut Frame, area: Rect) {
    if area.height < 2 {
        return;
    }
    let block = Block::default()
        .borders(Borders::ALL)
        .border_type(ratatui::widgets::BorderType::Rounded)
        .border_style(Style::default().fg(BORDER_COLOR))
        .title(Span::styled(" Notifications ", Style::default().fg(DIM_TEXT_COLOR)));
    frame.render_widget(block, area);
}

fn render_picker(frame: &mut Frame, area: Rect, actions: &[PickerAction], cursor: usize, title: &str) {
    let block = Block::default()
        .borders(Borders::ALL)
        .border_type(ratatui::widgets::BorderType::Rounded)
        .border_style(Style::default().fg(FOCUS_BORDER_COLOR))
        .title(Span::styled(
            format!(" {} ", title),
            Style::default().fg(FOCUS_BORDER_COLOR).bold(),
        ));

    // Find max label width for alignment
    let max_label_w = actions.iter().map(|a| a.label.len()).max().unwrap_or(10).max(10);

    let lines: Vec<Line> = actions
        .iter()
        .enumerate()
        .map(|(i, a)| {
            let formatted = format!(" {:<3}  {:<width$} {}", a.key, a.label, a.desc, width = max_label_w);
            if i == cursor {
                Line::from(Span::styled(
                    formatted,
                    Style::default().fg(Color::White).bg(SELECTED_BG_COLOR).bold(),
                ))
            } else {
                Line::from(vec![
                    Span::styled(format!(" {:<3}", a.key), Style::default().fg(FOCUS_BORDER_COLOR).bold()),
                    Span::styled(format!("  {:<width$}", a.label, width = max_label_w), Style::default()),
                    Span::styled(format!(" {}", a.desc), Style::default().fg(DIM_TEXT_COLOR)),
                ])
            }
        })
        .collect();

    let content = Paragraph::new(lines).block(block);
    frame.render_widget(content, area);
}

fn render_confirm(frame: &mut Frame, area: Rect, prompt: &str) {
    let block = Block::default()
        .borders(Borders::ALL)
        .border_type(ratatui::widgets::BorderType::Rounded)
        .border_style(Style::default().fg(FOCUS_BORDER_COLOR))
        .title(Span::styled(
            " Confirm ",
            Style::default().fg(FOCUS_BORDER_COLOR).bold(),
        ));

    let inner_w = area.width.saturating_sub(4) as usize;

    // Line 1: message centered
    let msg_pad = inner_w.saturating_sub(prompt.len()) / 2;

    // Line 2: options centered
    let opts = "Enter: confirm  Esc: cancel";
    let opts_pad = inner_w.saturating_sub(opts.len()) / 2;

    let lines = vec![
        Line::from(vec![
            Span::raw(" ".repeat(msg_pad)),
            Span::styled(prompt.to_string(), Style::default().fg(Color::White).bold()),
        ]),
        Line::from(""),
        Line::from(vec![
            Span::raw(" ".repeat(opts_pad)),
            Span::styled("Enter", Style::default().fg(HINT_COLOR)),
            Span::styled(": confirm  ", Style::default().fg(DIM_TEXT_COLOR)),
            Span::styled("Esc", Style::default().fg(HINT_COLOR)),
            Span::styled(": cancel", Style::default().fg(DIM_TEXT_COLOR)),
        ]),
    ];

    let content = Paragraph::new(lines).block(block);
    frame.render_widget(content, area);
}

fn render_input(frame: &mut Frame, area: Rect, prompt: &str, value: &str) {
    let block = Block::default()
        .borders(Borders::ALL)
        .border_type(ratatui::widgets::BorderType::Rounded)
        .border_style(Style::default().fg(FOCUS_BORDER_COLOR))
        .title(Span::styled(
            " Input ",
            Style::default().fg(FOCUS_BORDER_COLOR).bold(),
        ));

    let line = Line::from(vec![
        Span::styled(format!("{} ", prompt), Style::default().fg(DIM_TEXT_COLOR)),
        Span::styled(value.to_string(), Style::default()),
        Span::styled("█", Style::default().fg(HEADER_COLOR)),
    ]);

    let content = Paragraph::new(line).block(block);
    frame.render_widget(content, area);
}
