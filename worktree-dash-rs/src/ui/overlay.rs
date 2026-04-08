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

// ── Notify state ─────────────────────────────────────────────────────

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum NotifyKind {
    Info,
    Success,
    Error,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum NotifyState {
    Idle,
    Message { title: String, message: String, kind: NotifyKind },
    Picker { title: String, actions: Vec<PickerAction>, cursor: usize },
    Confirm { prompt: String },
    Input { prompt: String, value: String },
}

impl NotifyState {
    pub fn height(&self) -> u16 {
        match self {
            NotifyState::Idle => 0,
            NotifyState::Message { message, kind, .. } => {
                // Count actual lines + estimate wrapping at ~36 chars per line
                let msg_lines: u16 = message.lines()
                    .map(|l| ((l.len() as f32 / 36.0).ceil() as u16).max(1))
                    .sum::<u16>()
                    .max(1);
                let hint_lines: u16 = if *kind == NotifyKind::Error { 2 } else { 0 };
                msg_lines + hint_lines + 2 // +2 for borders
            }
            NotifyState::Picker { actions, .. } => (actions.len() as u16 + 2).max(3),
            NotifyState::Confirm { .. } => 5,
            NotifyState::Input { .. } => 3,
        }
    }
}

// ── Rendering ────────────────────────────────────────────────────────

pub fn render_notify(frame: &mut Frame, area: Rect, state: &NotifyState) {
    match state {
        NotifyState::Idle => render_idle(frame, area),
        NotifyState::Message { title, message, kind } => render_message(frame, area, title, message, kind),
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

fn render_message(frame: &mut Frame, area: Rect, title: &str, message: &str, kind: &NotifyKind) {
    let border_color = match kind {
        NotifyKind::Success => RUNNING_COLOR,
        NotifyKind::Error => STOPPED_COLOR,
        NotifyKind::Info => HINT_COLOR,
    };
    let block = Block::default()
        .borders(Borders::ALL)
        .border_type(ratatui::widgets::BorderType::Rounded)
        .border_style(Style::default().fg(border_color))
        .title(Span::styled(
            format!(" {} ", title),
            Style::default().fg(border_color).bold(),
        ));
    let mut lines: Vec<Line> = message.lines()
        .map(|l| Line::from(Span::styled(l.to_string(), Style::default().fg(DIM_TEXT_COLOR))))
        .collect();

    // Error notifications: add "Enter to close" hint
    if *kind == NotifyKind::Error {
        lines.push(Line::from(""));
        lines.push(Line::from(vec![
            Span::styled("Enter", Style::default().fg(HINT_COLOR)),
            Span::styled(": close", Style::default().fg(DIM_TEXT_COLOR)),
        ]).alignment(ratatui::layout::Alignment::Center));
    }

    let content = Paragraph::new(lines)
        .wrap(ratatui::widgets::Wrap { trim: true })
        .block(block);
    frame.render_widget(content, area);
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
