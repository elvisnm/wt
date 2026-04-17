//! DAG tab renderer — bordered status-colored cards with single-elbow edges.
//!
//! Consumes the pre-computed `GraphLayout` from `layout.rs` plus viewport
//! pan offset from `DagGraphState` and paints directly into a Ratatui buffer.

use super::layout::{Card, CardStatus, GraphLayout};
use super::DagGraphState;
use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Style};
use ratatui::widgets::{Block, BorderType, Borders, Widget};

const CARD_W: i32 = 22;
const CARD_H: i32 = 5;
const RANK_GAP: i32 = 4;
const ROW_GAP: i32 = 1;

const SPINNER: &[&str] = &["◐", "◓", "◑", "◒"];

/// Public entry. Called once per frame when the active tab is a DAG widget.
/// Card rects for mouse hit-testing are a future feature; this render pass
/// does not populate `DagGraphState::card_rects`.
pub fn render(area: Rect, buf: &mut Buffer, state: &DagGraphState, spin_frame: usize) {
    if let Some(err) = &state.load_error {
        render_error(area, buf, err);
        return;
    }

    if state.loading && state.layout.is_none() {
        render_loading(area, buf, state.load_progress, spin_frame);
        return;
    }

    let layout = match &state.layout {
        Some(l) if !l.cards.is_empty() => l,
        Some(_) => {
            render_empty(area, buf);
            return;
        }
        None => {
            render_loading(area, buf, state.load_progress, spin_frame);
            return;
        }
    };

    render_graph(area, buf, layout, state, spin_frame);
}

fn render_graph(
    area: Rect,
    buf: &mut Buffer,
    layout: &GraphLayout,
    state: &DagGraphState,
    spin_frame: usize,
) {
    let (vx, vy) = state.viewport;

    // Paint edges first so cards cover any stray edge characters.
    for edge in &layout.edges {
        let from = &layout.cards[edge.from];
        let to = &layout.cards[edge.to];
        draw_edge(area, buf, from, to, vx, vy);
    }

    for card in &layout.cards {
        let gx = card.rank as i32 * (CARD_W + RANK_GAP);
        let gy = card.row as i32 * (CARD_H + ROW_GAP);
        if let Some(rect) = to_screen_rect(gx, gy, CARD_W, CARD_H, vx, vy, area) {
            let selected = state.selected_id.as_deref() == Some(card.id.as_str());
            draw_card(buf, rect, card, selected);
        }
    }

    // "Refreshing…" indicator in the corner when a background refresh is
    // running on top of an already-rendered graph.
    if state.loading {
        let text = format!(" {} refreshing ", SPINNER[spin_frame % SPINNER.len()]);
        let x = area.x + area.width.saturating_sub(text.chars().count() as u16 + 1);
        let y = area.y;
        if y < area.y + area.height {
            buf.set_string(x, y, text, Style::default().fg(Color::Yellow));
        }
    }
}

fn draw_card(buf: &mut Buffer, rect: Rect, card: &Card, selected: bool) {
    let color = status_color(card.status);
    let border_type = if selected {
        BorderType::Double
    } else {
        BorderType::Rounded
    };
    let block = Block::default()
        .borders(Borders::ALL)
        .border_type(border_type)
        .border_style(Style::default().fg(color));
    block.render(rect, buf);

    if rect.width < 4 || rect.height < 3 {
        return;
    }
    let inner_x = rect.x + 1;
    let inner_y = rect.y + 1;
    let inner_w = rect.width.saturating_sub(2) as usize;

    // Line 1: id (left) + status glyph (right)
    let glyph = status_glyph(card.status);
    let id_max = inner_w.saturating_sub(2); // leave room for " X"
    let id = truncate(&card.id, id_max);
    buf.set_string(inner_x, inner_y, &id, Style::default().fg(color).add_modifier(Modifier::BOLD));
    if inner_w >= 1 {
        let gx = inner_x + inner_w.saturating_sub(1) as u16;
        buf.set_string(gx, inner_y, glyph, Style::default().fg(color));
    }

    // Line 2: title
    if rect.height >= 4 {
        let title = truncate(&card.title, inner_w);
        buf.set_string(inner_x, inner_y + 1, &title, Style::default().fg(Color::Gray));
    }

    // Line 3: P{n} · {status label}
    if rect.height >= 5 {
        let label = format!("P{} · {}", card.priority, status_label(card.status));
        let label = truncate(&label, inner_w);
        buf.set_string(inner_x, inner_y + 2, &label, Style::default().fg(Color::DarkGray));
    }
}

fn draw_edge(area: Rect, buf: &mut Buffer, from: &Card, to: &Card, vx: i32, vy: i32) {
    // Card graph-space bounds
    let from_gx = from.rank as i32 * (CARD_W + RANK_GAP);
    let from_gy = from.row as i32 * (CARD_H + ROW_GAP);
    let to_gx = to.rank as i32 * (CARD_W + RANK_GAP);
    let to_gy = to.row as i32 * (CARD_H + ROW_GAP);

    let from_right = from_gx + CARD_W; // exclusive
    let from_mid_y = from_gy + CARD_H / 2;
    let to_left = to_gx; // exclusive end going left
    let to_mid_y = to_gy + CARD_H / 2;

    let mid_x = (from_right + to_left) / 2;

    let color = status_color(from.status);
    let style = Style::default().fg(color);

    // Right leg: from (from_right, from_mid_y) → (mid_x, from_mid_y)
    for x in from_right..mid_x {
        paint(area, buf, x, from_mid_y, vx, vy, "─", style);
    }

    if from_mid_y == to_mid_y {
        // Straight shot — no elbow.
        for x in mid_x..(to_left.saturating_sub(1)) {
            paint(area, buf, x, from_mid_y, vx, vy, "─", style);
        }
    } else if from_mid_y < to_mid_y {
        // Elbow down: ─┐ on from_mid_y at mid_x, │ down, └─ to target.
        paint(area, buf, mid_x, from_mid_y, vx, vy, "┐", style);
        for y in (from_mid_y + 1)..to_mid_y {
            paint(area, buf, mid_x, y, vx, vy, "│", style);
        }
        paint(area, buf, mid_x, to_mid_y, vx, vy, "└", style);
        for x in (mid_x + 1)..(to_left.saturating_sub(1)) {
            paint(area, buf, x, to_mid_y, vx, vy, "─", style);
        }
    } else {
        // Elbow up
        paint(area, buf, mid_x, from_mid_y, vx, vy, "┘", style);
        for y in (to_mid_y + 1)..from_mid_y {
            paint(area, buf, mid_x, y, vx, vy, "│", style);
        }
        paint(area, buf, mid_x, to_mid_y, vx, vy, "┌", style);
        for x in (mid_x + 1)..(to_left.saturating_sub(1)) {
            paint(area, buf, x, to_mid_y, vx, vy, "─", style);
        }
    }

    // Arrow tip at target left edge
    paint(area, buf, to_left - 1, to_mid_y, vx, vy, "▶", style);
}

fn paint(area: Rect, buf: &mut Buffer, gx: i32, gy: i32, vx: i32, vy: i32, ch: &str, style: Style) {
    let sx = gx - vx + area.x as i32;
    let sy = gy - vy + area.y as i32;
    if sx < area.x as i32
        || sy < area.y as i32
        || sx >= (area.x + area.width) as i32
        || sy >= (area.y + area.height) as i32
    {
        return;
    }
    buf[(sx as u16, sy as u16)].set_symbol(ch).set_style(style);
}

fn to_screen_rect(
    gx: i32,
    gy: i32,
    w: i32,
    h: i32,
    vx: i32,
    vy: i32,
    area: Rect,
) -> Option<Rect> {
    let sx = gx - vx + area.x as i32;
    let sy = gy - vy + area.y as i32;
    // Fully clip out if the card doesn't overlap the area at all.
    if sx + w <= area.x as i32
        || sy + h <= area.y as i32
        || sx >= (area.x + area.width) as i32
        || sy >= (area.y + area.height) as i32
    {
        return None;
    }
    // Clamp to area. Partial off-screen cards render the visible portion.
    let left = sx.max(area.x as i32);
    let top = sy.max(area.y as i32);
    let right = (sx + w).min((area.x + area.width) as i32);
    let bottom = (sy + h).min((area.y + area.height) as i32);
    Some(Rect::new(
        left as u16,
        top as u16,
        (right - left) as u16,
        (bottom - top) as u16,
    ))
}

fn render_loading(area: Rect, buf: &mut Buffer, progress: Option<(usize, usize)>, spin_frame: usize) {
    let glyph = SPINNER[spin_frame % SPINNER.len()];
    let line1 = format!("{}  Loading task dependencies…", glyph);
    let line2 = match progress {
        Some((done, total)) if total > 0 => format!("fetched {} / {}", done, total),
        _ => "preparing…".to_string(),
    };
    render_centered_panel(area, buf, &[&line1, &line2], Color::Yellow);
}

fn render_empty(area: Rect, buf: &mut Buffer) {
    render_centered_panel(area, buf, &["No tasks to display"], Color::DarkGray);
}

fn render_error(area: Rect, buf: &mut Buffer, message: &str) {
    render_centered_panel(area, buf, &["Failed to load tasks", message], Color::Red);
}

fn render_centered_panel(area: Rect, buf: &mut Buffer, lines: &[&str], color: Color) {
    let width = lines
        .iter()
        .map(|l| l.chars().count())
        .max()
        .unwrap_or(0) as u16
        + 6;
    let height = lines.len() as u16 + 4;
    if area.width < width || area.height < height {
        return;
    }
    let x = area.x + (area.width.saturating_sub(width)) / 2;
    let y = area.y + (area.height.saturating_sub(height)) / 2;
    let rect = Rect::new(x, y, width, height);

    Block::default()
        .borders(Borders::ALL)
        .border_type(BorderType::Rounded)
        .border_style(Style::default().fg(color))
        .render(rect, buf);

    for (i, line) in lines.iter().enumerate() {
        let line_x = x + (width.saturating_sub(line.chars().count() as u16)) / 2;
        let line_y = y + 2 + i as u16;
        buf.set_string(line_x, line_y, *line, Style::default().fg(color));
    }
}

fn truncate(s: &str, max: usize) -> String {
    if max == 0 {
        return String::new();
    }
    let count = s.chars().count();
    if count <= max {
        s.to_string()
    } else if max <= 1 {
        "…".to_string()
    } else {
        let mut out: String = s.chars().take(max - 1).collect();
        out.push('…');
        out
    }
}

fn status_color(status: CardStatus) -> Color {
    match status {
        CardStatus::Done => Color::Green,
        CardStatus::Active => Color::Yellow,
        CardStatus::Ready => Color::Cyan,
        CardStatus::Blocked => Color::Red,
        CardStatus::Open => Color::Gray,
    }
}

fn status_glyph(status: CardStatus) -> &'static str {
    match status {
        CardStatus::Done => "●",
        CardStatus::Active => "◐",
        CardStatus::Ready => "○",
        CardStatus::Blocked => "⊘",
        CardStatus::Open => "◌",
    }
}

fn status_label(status: CardStatus) -> &'static str {
    match status {
        CardStatus::Done => "done",
        CardStatus::Active => "active",
        CardStatus::Ready => "ready",
        CardStatus::Blocked => "blocked",
        CardStatus::Open => "open",
    }
}
