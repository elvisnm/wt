use ratatui::{
    prelude::*,
    widgets::{Block, Borders, Paragraph},
};

use super::style::*;

/// Render the guide page in the terminal area, centered like the Go dashboard.
pub fn render_guide(frame: &mut Frame, area: Rect) {
    let block = Block::default()
        .borders(Borders::ALL)
        .border_type(ratatui::widgets::BorderType::Rounded)
        .border_style(Style::default().fg(BORDER_COLOR));

    let inner = block.inner(area);
    frame.render_widget(block, area);

    let w = inner.width as usize;
    let h = inner.height as usize;
    if w < 40 || h < 10 {
        return;
    }

    let col_w = 43usize;
    let gap = 2usize;
    let total_w = col_w * 2 + gap;
    let h_pad = w.saturating_sub(total_w) / 2;
    let pad = " ".repeat(h_pad);
    let gap_s = " ".repeat(gap);

    let k = Style::default().fg(HINT_COLOR).bold();
    let d = Style::default().fg(DIM_TEXT_COLOR);
    let b = Style::default().fg(BORDER_COLOR);
    let t = Style::default().fg(FOCUS_BORDER_COLOR).bold();

    // Pre-build all lines as (left_spans, right_spans)
    let rows: Vec<(Vec<Span>, Vec<Span>)> = vec![
        // Worktree box + Panels box
        (box_top("Worktree", col_w, b, t), box_top("Panels", col_w, b, t)),
        (box_empty(col_w, b), box_empty(col_w, b)),
        (
            box_line(col_w, b, vec![s("  Select a worktree, then press:", d)]),
            box_line(col_w, b, vec![s("  [ Worktrees | Services | Terminal ]", d)]),
        ),
        (box_empty(col_w, b), box_empty(col_w, b)),
        (
            box_line(col_w, b, vec![s("    ", d), s("b", k), s("  bash shell       ", d), s("c", k), s("  claude code", d)]),
            box_line(col_w, b, vec![s("    ", d), s("Tab", k), s(" / ", d), s("Shift+Tab", k), s("  cycle panels", d)]),
        ),
        (
            box_line(col_w, b, vec![s("    ", d), s("g", k), s("  pull latest      ", d), s("l", k), s("  logs", d)]),
            box_empty(col_w, b),
        ),
        (
            box_line(col_w, b, vec![s("    ", d), s("n", k), s("  create new", d)]),
            box_line(col_w, b, vec![s("    ", d), s("w", k), s("  worktrees", d)]),
        ),
        (
            box_empty(col_w, b),
            box_line(col_w, b, vec![s("    ", d), s("s", k), s("  services     jump directly", d)]),
        ),
        (
            box_bottom(col_w, b),
            box_line(col_w, b, vec![s("    ", d), s("a", k), s("  active tabs", d)]),
        ),
        // Terminal box + Panels bottom
        (
            box_top("Terminal", col_w, b, t),
            box_empty(col_w, b),
        ),
        (
            box_empty(col_w, b),
            box_bottom(col_w, b),
        ),
        // Terminal + More box
        (
            box_line(col_w, b, vec![s("  Sessions open in the right pane.", d)]),
            box_top("More", col_w, b, t),
        ),
        (
            box_line(col_w, b, vec![s("  To get back:", d)]),
            box_empty(col_w, b),
        ),
        (
            box_empty(col_w, b),
            box_line(col_w, b, vec![s("  ", d), s("Shift+D", k), s("  details    ", d), s("Shift+K", k), s("  skip-wt", d)]),
        ),
        (
            box_line(col_w, b, vec![s("    ", d), s("Ctrl+]", k), s("  return to dashboard", d)]),
            box_line(col_w, b, vec![s("  ", d), s("Shift+L", k), s("  LAN mode   ", d), s("Shift+U", k), s("  usage", d)]),
        ),
        (
            box_empty(col_w, b),
            box_line(col_w, b, vec![s("  ", d), s("Shift+T", k), s("  tasks      ", d), s("Shift+M", k), s("  maint.", d)]),
        ),
        (
            box_bottom(col_w, b),
            box_line(col_w, b, vec![s("  ", d), s("Shift+S", k), s("  settings   ", d), s("Ctrl+B", k), s("  sidebar", d)]),
        ),
        // Tabs box + More bottom
        (
            box_top("Tabs", col_w, b, t),
            box_line(col_w, b, vec![s("  ────────────────────────────────────", b)]),
        ),
        (
            box_empty(col_w, b),
            box_line(col_w, b, vec![s("  ", d), s("i", k), s("  details  ", d), s("r", k), s("  restart  ", d), s("u", k), s("  start", d)]),
        ),
        (
            box_line(col_w, b, vec![s("  Each session becomes a tab.", d)]),
            box_line(col_w, b, vec![s("  ", d), s("t", k), s("  stop     ", d), s("g", k), s("  pull     ", d), s("l", k), s("  logs", d)]),
        ),
        (
            box_empty(col_w, b),
            box_empty(col_w, b),
        ),
        (
            box_line(col_w, b, vec![s("    ", d), s("h", k), s(" / ", d), s("l", k), s("     prev / next tab", d)]),
            box_bottom(col_w, b),
        ),
        (
            box_line(col_w, b, vec![s("    ", d), s("1-9", k), s("       jump to tab N", d)]),
            vec![],
        ),
        (
            box_line(col_w, b, vec![s("    ", d), s("x", k), s("         close tab", d)]),
            box_top("Exit", col_w, b, t),
        ),
        (
            box_empty(col_w, b),
            box_line(col_w, b, vec![s("  ", d), s("Ctrl+Q", k), s("      quit dashboard", d)]),
        ),
        (
            box_bottom(col_w, b),
            box_line(col_w, b, vec![s("  ", d), s("?", k), s("           keybindings help", d)]),
        ),
        (vec![], box_bottom(col_w, b)),
    ];

    // Build final lines with centering
    let mut content_lines: Vec<Line> = Vec::new();

    // Title
    let title = "wt — Quick Start";
    let title_pad = w.saturating_sub(title.len()) / 2;
    content_lines.push(Line::from(vec![
        Span::raw(" ".repeat(title_pad)),
        Span::styled(title, Style::default().fg(FOCUS_BORDER_COLOR).bold()),
    ]));
    content_lines.push(Line::from(""));

    for (left, right) in &rows {
        let mut spans: Vec<Span> = vec![Span::raw(pad.clone())];
        spans.extend(left.clone());
        spans.push(Span::raw(gap_s.clone()));
        if right.is_empty() {
            spans.push(Span::raw(" ".repeat(col_w)));
        } else {
            spans.extend(right.clone());
        }
        content_lines.push(Line::from(spans));
    }

    // Footer
    content_lines.push(Line::from(""));
    let ver = crate::version_label();
    let ver_pad = w.saturating_sub(ver.len()) / 2;
    content_lines.push(Line::from(Span::styled(
        format!("{}{}", " ".repeat(ver_pad), ver),
        d,
    )));
    let footer = "Press ? for all keybindings";
    let footer_pad = w.saturating_sub(footer.len()) / 2;
    content_lines.push(Line::from(vec![
        Span::raw(" ".repeat(footer_pad)),
        Span::styled("Press ", d),
        Span::styled("?", k),
        Span::styled(" for all keybindings", d),
    ]));

    // Vertical centering
    let content_h = content_lines.len();
    let v_pad = h.saturating_sub(content_h) / 2;
    let mut final_lines: Vec<Line> = vec![Line::from(""); v_pad];
    final_lines.extend(content_lines);

    let content = Paragraph::new(final_lines);
    frame.render_widget(content, inner);
}

pub fn s(text: &str, style: Style) -> Span<'static> {
    Span::styled(text.to_string(), style)
}

pub fn box_top(title: &str, width: usize, border: Style, title_style: Style) -> Vec<Span<'static>> {
    let inner = width - 2;
    let t = format!(" {} ", title);
    let dashes = inner.saturating_sub(1 + t.len());
    vec![
        Span::styled("╭─".to_string(), border),
        Span::styled(t, title_style),
        Span::styled(format!("{}╮", "─".repeat(dashes)), border),
    ]
}

pub fn box_bottom(width: usize, border: Style) -> Vec<Span<'static>> {
    let inner = width - 2;
    vec![Span::styled(format!("╰{}╯", "─".repeat(inner)), border)]
}

pub fn box_empty(width: usize, border: Style) -> Vec<Span<'static>> {
    let inner = width - 2;
    vec![
        Span::styled("│".to_string(), border),
        Span::raw(" ".repeat(inner)),
        Span::styled("│".to_string(), border),
    ]
}

pub fn box_line(width: usize, border: Style, content: Vec<Span<'static>>) -> Vec<Span<'static>> {
    let inner = width - 2;
    // Calculate content visual width
    let content_w: usize = content.iter().map(|s| s.width()).sum();
    let pad = inner.saturating_sub(content_w);

    let mut spans = vec![Span::styled("│".to_string(), border)];
    spans.extend(content);
    spans.push(Span::raw(" ".repeat(pad)));
    spans.push(Span::styled("│".to_string(), border));
    spans
}
