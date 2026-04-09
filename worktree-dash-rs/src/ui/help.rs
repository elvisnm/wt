use ratatui::{
    prelude::*,
    widgets::Paragraph,
};

use super::style::*;
use super::guide::{box_top, box_bottom, box_line, s};

/// Render the help keybindings page in the right panel, two-column layout.
pub fn render_help(frame: &mut Frame, area: Rect) {
    let inner = Rect::new(
        area.x + 1, area.y + 1,
        area.width.saturating_sub(2), area.height.saturating_sub(2),
    );

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

    // Left column: Navigation, Active Tabs, Worktrees, Services
    let left: Vec<Vec<Span>> = [
        vec![box_top("Navigation", col_w, b, t)],
        vec![vec![]],
        vec![box_line(col_w, b, vec![s("  ", d), s("j", k), s(" / ", d), s("k", k), s("       navigate list", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Tab", k), s("         next panel", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Shift+Tab", k), s("   prev panel", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("a/w/s", k), s("       jump to panel", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("1-9", k), s("         jump to tab N", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Esc", k), s("         back / dismiss", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Ctrl+Q", k), s("      quit dashboard", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Ctrl+B", k), s("      toggle sidebar", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("?", k), s("           keybindings help", d)])],
        vec![box_bottom(col_w, b)],
        vec![box_top("Active Tabs", col_w, b, t)],
        vec![vec![]],
        vec![box_line(col_w, b, vec![s("  ", d), s("Enter", k), s("       focus terminal", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("h", k), s(" / ", d), s("l", k), s("       prev / next tab", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("x", k), s("           close tab", d)])],
        vec![box_bottom(col_w, b)],
        vec![box_top("Worktrees", col_w, b, t)],
        vec![vec![]],
        vec![box_line(col_w, b, vec![s("  ", d), s("Enter", k), s("       action menu", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("b", k), s("           shell", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("c", k), s("           claude code", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("g", k), s("           pull latest", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("z", k), s("           local shell (zsh)", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("l", k), s("           logs", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("n", k), s("           create worktree", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("i", k), s("           details toggle", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("r", k), s("           restart", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("u", k), s("           start", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("t", k), s("           stop", d)])],
        vec![box_bottom(col_w, b)],
        vec![box_top("Services", col_w, b, t)],
        vec![vec![]],
        vec![box_line(col_w, b, vec![s("  ", d), s("Enter", k), s("       preview logs", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("l", k), s("           pin logs (tab)", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("r", k), s("           restart service", d)])],
        vec![box_bottom(col_w, b)],
    ].concat();

    // Right column: Tasks, Operations, Terminal, Split Panels
    let right: Vec<Vec<Span>> = [
        vec![box_top("Tasks (Shift+T)", col_w, b, t)],
        vec![vec![]],
        vec![box_line(col_w, b, vec![s("  ", d), s("j", k), s(" / ", d), s("k", k), s("       navigate tasks", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Enter", k), s("       task detail", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("c", k), s("           close task", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("d", k), s("           delete task", d)])],
        vec![box_bottom(col_w, b)],
        vec![box_top("Operations", col_w, b, t)],
        vec![vec![]],
        vec![box_line(col_w, b, vec![s("  ", d), s("Shift+D", k), s("     details toggle", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Shift+K", k), s("     skip-worktree", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Shift+L", k), s("     LAN toggle", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Shift+M", k), s("     maintenance", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Shift+T", k), s("     tasks", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Shift+S", k), s("     settings", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Shift+U", k), s("     Claude usage", d)])],
        vec![box_bottom(col_w, b)],
        vec![box_top("Terminal  (Ctrl+])", col_w, b, t)],
        vec![vec![]],
        vec![box_line(col_w, b, vec![s("  ", d), s("Ctrl+]", k), s("      return to dashboard", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Ctrl+j/k", k), s("    switch session", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Ctrl+x", k), s("      close tab & return", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Ctrl+F", k), s("      fullscreen toggle", d)])],
        vec![box_bottom(col_w, b)],
        vec![box_top("Split Panels", col_w, b, t)],
        vec![vec![]],
        vec![box_line(col_w, b, vec![s("  ", d), s("Shift+\\", k), s(" ", d), s("(|)", b), s("  split side by side", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("Shift+-", k), s(" ", d), s("(_)", b), s("  split below", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("m", k), s("           move tab into group", d)])],
        vec![box_line(col_w, b, vec![s("  ", d), s("x", k), s("           close pane / group", d)])],
        vec![box_bottom(col_w, b)],
    ].concat();

    let max_rows = left.len().max(right.len());

    let mut content_lines: Vec<Line> = Vec::new();

    // Title
    let title = "wt — Keybindings";
    let title_pad = w.saturating_sub(title.len()) / 2;
    content_lines.push(Line::from(vec![
        Span::raw(" ".repeat(title_pad)),
        Span::styled(title, Style::default().fg(FOCUS_BORDER_COLOR).bold()),
    ]));
    content_lines.push(Line::from(""));

    for i in 0..max_rows {
        let l = if i < left.len() { &left[i] } else { &vec![] };
        let r = if i < right.len() { &right[i] } else { &vec![] };

        let mut spans: Vec<Span> = vec![Span::raw(pad.clone())];
        if l.is_empty() {
            spans.push(Span::raw(" ".repeat(col_w)));
        } else {
            spans.extend(l.clone());
        }
        spans.push(Span::raw(gap_s.clone()));
        if r.is_empty() {
            spans.push(Span::raw(" ".repeat(col_w)));
        } else {
            spans.extend(r.clone());
        }
        content_lines.push(Line::from(spans));
    }

    // Footer
    content_lines.push(Line::from(""));
    let footer_pad = w.saturating_sub(20) / 2;
    content_lines.push(Line::from(vec![
        Span::raw(" ".repeat(footer_pad)),
        Span::styled("Press ", d),
        Span::styled("?", k),
        Span::styled(" or ", d),
        Span::styled("Esc", k),
        Span::styled(" to close", d),
    ]));

    // Vertical centering
    let content_h = content_lines.len();
    let v_pad = h.saturating_sub(content_h) / 2;
    let mut final_lines: Vec<Line> = vec![Line::from(""); v_pad];
    final_lines.extend(content_lines);

    let content = Paragraph::new(final_lines);
    frame.render_widget(content, inner);
}
