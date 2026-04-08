use ratatui::{
    prelude::*,
    widgets::{Block, Borders, Paragraph},
};

use crate::settings::{
    Settings, MAX_LEFT_PANE_PCT, MAX_MAX_PANES_PER_GROUP,
    MIN_LEFT_PANE_PCT, MIN_MAX_PANES_PER_GROUP,
};

use super::guide::{box_top, box_bottom, box_empty, box_line, s};
use super::style::*;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SettingsField {
    DefaultDetails,
    DefaultUsage,
    DefaultTasks,
    DefaultServices,
    LeftPaneWidth,
    MaxPanesPerGroup,
    ClaudeAutoMode,
    Save,
    Cancel,
}

const FIELDS: &[SettingsField] = &[
    SettingsField::DefaultDetails,
    SettingsField::DefaultUsage,
    SettingsField::DefaultTasks,
    SettingsField::DefaultServices,
    SettingsField::LeftPaneWidth,
    SettingsField::MaxPanesPerGroup,
    SettingsField::ClaudeAutoMode,
    SettingsField::Save,
    SettingsField::Cancel,
];

pub struct SettingsState {
    pub settings: Settings,
    pub cursor: usize,
}

impl SettingsState {
    pub fn new(settings: Settings) -> Self {
        Self { settings, cursor: 0 }
    }

    pub fn current_field(&self) -> SettingsField {
        FIELDS[self.cursor]
    }

    pub fn navigate(&mut self, delta: i8) {
        let new = self.cursor as i32 + delta as i32;
        self.cursor = new.clamp(0, FIELDS.len() as i32 - 1) as usize;
    }

    pub fn toggle(&mut self) {
        match self.current_field() {
            SettingsField::DefaultDetails => {
                self.settings.default_panels.details = !self.settings.default_panels.details;
            }
            SettingsField::DefaultUsage => {
                self.settings.default_panels.usage = !self.settings.default_panels.usage;
            }
            SettingsField::DefaultTasks => {
                self.settings.default_panels.tasks = !self.settings.default_panels.tasks;
            }
            SettingsField::DefaultServices => {
                self.settings.default_panels.services = !self.settings.default_panels.services;
            }
            SettingsField::ClaudeAutoMode => {
                self.settings.claude_auto_mode = !self.settings.claude_auto_mode;
            }
            _ => {}
        }
    }

    pub fn adjust(&mut self, delta: i16) {
        match self.current_field() {
            SettingsField::LeftPaneWidth => {
                let new = self.settings.left_pane_pct as i16 + delta;
                self.settings.left_pane_pct =
                    new.clamp(MIN_LEFT_PANE_PCT as i16, MAX_LEFT_PANE_PCT as i16) as u16;
            }
            SettingsField::MaxPanesPerGroup => {
                let new = self.settings.max_panes_per_group as i16 + delta;
                self.settings.max_panes_per_group = new
                    .clamp(MIN_MAX_PANES_PER_GROUP as i16, MAX_MAX_PANES_PER_GROUP as i16)
                    as u16;
            }
            _ => {}
        }
    }
}

pub fn render_settings(frame: &mut Frame, area: Rect, state: &SettingsState) {
    let block = Block::default()
        .borders(Borders::ALL)
        .border_type(ratatui::widgets::BorderType::Rounded)
        .border_style(Style::default().fg(BORDER_COLOR));

    let inner = block.inner(area);
    frame.render_widget(block, area);

    let w = inner.width as usize;
    let h = inner.height as usize;
    if w < 30 || h < 10 {
        return;
    }

    let col_w = 46usize.min(w.saturating_sub(4));
    let h_pad = w.saturating_sub(col_w) / 2;
    let pad = " ".repeat(h_pad);

    let k = Style::default().fg(HINT_COLOR).bold();
    let d = Style::default().fg(DIM_TEXT_COLOR);
    let b = Style::default().fg(BORDER_COLOR);
    let t = Style::default().fg(FOCUS_BORDER_COLOR).bold();
    let st = &state.settings;
    let cursor = state.cursor;

    let mut content_lines: Vec<Line> = Vec::new();

    // Title
    let title = "wt — Settings";
    let title_pad = w.saturating_sub(title.len()) / 2;
    content_lines.push(Line::from(vec![
        Span::raw(" ".repeat(title_pad)),
        Span::styled(title, Style::default().fg(FOCUS_BORDER_COLOR).bold()),
    ]));
    content_lines.push(Line::from(""));

    // Default Panels box
    let panel_rows = vec![
        toggle_line(col_w, b, cursor == 0, "Details panel", "Shift+D", st.default_panels.details),
        toggle_line(col_w, b, cursor == 1, "Usage panel", "Shift+U", st.default_panels.usage),
        toggle_line(col_w, b, cursor == 2, "Tasks panel", "Shift+T", st.default_panels.tasks),
        toggle_line(col_w, b, cursor == 3, "Services panel", "", st.default_panels.services),
    ];
    add_box(&mut content_lines, "Default Panels", &panel_rows, col_w, &pad, b, t);
    content_lines.push(Line::from(""));

    // Left Pane Width box
    let range_row = range_line(col_w, b, cursor == 4, st.left_pane_pct as i32, MIN_LEFT_PANE_PCT as i32, MAX_LEFT_PANE_PCT as i32, "%");
    let hint_row = box_line(col_w, b, vec![
        s("  ", d),
        s("Use ", d),
        s("←", k),
        s(" / ", d),
        s("→", k),
        s(" to adjust", d),
    ]);
    add_box(&mut content_lines, "Left Pane Width", &[range_row, hint_row], col_w, &pad, b, t);
    content_lines.push(Line::from(""));

    // Split Panes box
    let split_row = range_line_labeled(col_w, b, cursor == 5, "Max", st.max_panes_per_group as i32, MIN_MAX_PANES_PER_GROUP as i32, MAX_MAX_PANES_PER_GROUP as i32);
    let split_hint = box_line(col_w, b, vec![s("  Sessions per group", d)]);
    add_box(&mut content_lines, "Split Panes", &[split_row, split_hint], col_w, &pad, b, t);
    content_lines.push(Line::from(""));

    // Claude Code box
    let claude_row = toggle_line(col_w, b, cursor == 6, "Auto mode", "--enable-auto-mode", st.claude_auto_mode);
    add_box(&mut content_lines, "Claude Code", &[claude_row], col_w, &pad, b, t);
    content_lines.push(Line::from(""));

    // Actions box
    let save_row = action_line(col_w, b, cursor == 7, "Save & Close");
    let exit_row = action_line(col_w, b, cursor == 8, "Exit");
    add_box(&mut content_lines, "Actions", &[save_row, exit_row], col_w, &pad, b, t);

    // Footer
    content_lines.push(Line::from(""));
    let footer = "↑↓ navigate  Space/Enter select  ←→ adjust  q exit";
    let footer_pad = w.saturating_sub(footer.len()) / 2;
    content_lines.push(Line::from(vec![
        Span::raw(" ".repeat(footer_pad)),
        Span::styled(footer, d),
    ]));

    // Vertical centering
    let content_h = content_lines.len();
    let v_pad = h.saturating_sub(content_h) / 2;
    let mut final_lines: Vec<Line> = vec![Line::from(""); v_pad];
    final_lines.extend(content_lines);

    let content = Paragraph::new(final_lines);
    frame.render_widget(content, inner);
}

fn add_box(lines: &mut Vec<Line<'static>>, title: &str, rows: &[Vec<Span<'static>>], col_w: usize, pad: &str, b: Style, t: Style) {
    lines.push(Line::from({ let mut v = vec![Span::raw(pad.to_string())]; v.extend(box_top(title, col_w, b, t)); v }));
    lines.push(Line::from({ let mut v = vec![Span::raw(pad.to_string())]; v.extend(box_empty(col_w, b)); v }));
    for row in rows {
        lines.push(Line::from({ let mut v = vec![Span::raw(pad.to_string())]; v.extend(row.clone()); v }));
    }
    lines.push(Line::from({ let mut v = vec![Span::raw(pad.to_string())]; v.extend(box_empty(col_w, b)); v }));
    lines.push(Line::from({ let mut v = vec![Span::raw(pad.to_string())]; v.extend(box_bottom(col_w, b)); v }));
}

fn toggle_line(width: usize, border: Style, focused: bool, label: &str, shortcut: &str, on: bool) -> Vec<Span<'static>> {
    let prefix = if focused {
        vec![Span::styled("▸ ", Style::default().fg(FOCUS_BORDER_COLOR))]
    } else {
        vec![Span::raw("  ")]
    };

    let checkbox = if on {
        vec![
            Span::styled("[", Style::default().fg(BORDER_COLOR)),
            Span::styled("✓", Style::default().fg(FOCUS_BORDER_COLOR)),
            Span::styled("]", Style::default().fg(BORDER_COLOR)),
        ]
    } else {
        vec![Span::styled("[ ]", Style::default().fg(BORDER_COLOR))]
    };

    let mut content = prefix;
    content.extend(checkbox);
    content.push(Span::raw(format!(" {}  ", label)));
    if !shortcut.is_empty() {
        content.push(Span::styled(format!("({})", shortcut), Style::default().fg(DIM_TEXT_COLOR)));
    }

    box_line(width, border, content)
}

fn range_line(width: usize, border: Style, focused: bool, value: i32, min: i32, max: i32, suffix: &str) -> Vec<Span<'static>> {
    let prefix = if focused {
        vec![Span::styled("▸ ", Style::default().fg(FOCUS_BORDER_COLOR))]
    } else {
        vec![Span::raw("  ")]
    };

    let track_w = 30usize.min((width as i32 - 18).max(10) as usize);
    let filled = if max > min { ((value - min) * track_w as i32 / (max - min)).clamp(0, track_w as i32) as usize } else { 0 };
    let empty = track_w - filled;

    let mut content = prefix;
    content.push(Span::styled("█".repeat(filled), Style::default().fg(FOCUS_BORDER_COLOR)));
    content.push(Span::styled("░".repeat(empty), Style::default().fg(BORDER_COLOR)));
    content.push(Span::styled(format!("  {}{}", value, suffix), Style::default().bold()));

    box_line(width, border, content)
}

fn range_line_labeled(width: usize, border: Style, focused: bool, label: &str, value: i32, min: i32, max: i32) -> Vec<Span<'static>> {
    let prefix = if focused {
        vec![Span::styled("▸ ", Style::default().fg(FOCUS_BORDER_COLOR))]
    } else {
        vec![Span::raw("  ")]
    };

    let track_w = 20usize.min((width as i32 - 24).max(6) as usize);
    let filled = if max > min { ((value - min) * track_w as i32 / (max - min)).clamp(0, track_w as i32) as usize } else { 0 };
    let empty = track_w - filled;

    let mut content = prefix;
    content.push(Span::styled(format!("{}: ", label), Style::default().fg(DIM_TEXT_COLOR)));
    content.push(Span::styled("█".repeat(filled), Style::default().fg(FOCUS_BORDER_COLOR)));
    content.push(Span::styled("░".repeat(empty), Style::default().fg(BORDER_COLOR)));
    content.push(Span::styled(format!("  {}", value), Style::default().bold()));

    box_line(width, border, content)
}

fn action_line(width: usize, border: Style, focused: bool, label: &str) -> Vec<Span<'static>> {
    if focused {
        box_line(width, border, vec![
            Span::styled("▸ ", Style::default().fg(FOCUS_BORDER_COLOR)),
            Span::styled(label.to_string(), Style::default().fg(FOCUS_BORDER_COLOR).bold()),
        ])
    } else {
        box_line(width, border, vec![
            Span::raw("  "),
            Span::styled(label.to_string(), Style::default().fg(DIM_TEXT_COLOR)),
        ])
    }
}
