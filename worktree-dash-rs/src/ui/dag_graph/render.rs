//! DAG tab renderer — bordered status-colored cards with single-elbow edges.
//!
//! Consumes the pre-computed `GraphLayout` from `layout.rs` plus viewport
//! pan offset from `DagGraphState` and paints directly into a Ratatui buffer.

use super::layout::{Card, CardStatus, GraphLayout, CARD_W, RANK_GAP};
use super::DagGraphState;
use crate::ui::style::HEADER_BG;
use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Style};

/// Edge/divider color, matches section-header rules (`─ worktrees ───`).
const EDGE_COLOR: Color = Color::Indexed(239);
/// Tooltip background — one shade darker than HEADER_BG so it stands out
/// from the cards.
const TOOLTIP_BG: Color = Color::Indexed(234);
/// Selected card background — a few shades lighter than HEADER_BG so the
/// card visually "rises" without a border line that could clash with arrow
/// tips connecting to the card edge.
const SELECTED_BG: Color = Color::Indexed(240);

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
        if let Some(rect) = to_screen_rect(gx, card.y, CARD_W, card.height as i32, vx, vy, area) {
            let selected = state.selected_id.as_deref() == Some(card.id.as_str());
            draw_card(buf, rect, card, selected);
        }
    }

    // Fixed legend boxes in the bottom-right. Painted before the tooltip so
    // the tooltip always wins the z-order when it would overlap.
    draw_legends(area, buf);

    // Tooltip for the selected card, anchored next to it.
    if let Some(id) = &state.selected_id {
        if let Some(selected) = layout.cards.iter().find(|c| c.id == *id) {
            draw_tooltip(
                area,
                buf,
                selected,
                &layout.cards,
                &state.tasks,
                vx,
                vy,
                state.tooltip_expanded,
            );
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
    let status = status_color(card.status);
    let bg = if selected { SELECTED_BG } else { HEADER_BG };

    // Opaque card fill — brighter shade when selected so it lifts off the
    // graph without breaking arrow connections on the card's edge.
    fill_rect(buf, rect, bg);

    if rect.width < 4 || rect.height < 3 {
        return;
    }

    // 2-column left/right internal padding; 1-row top/bottom internal padding.
    let content_w = rect.width.saturating_sub(4) as usize;
    let inner_x = rect.x + 2;
    let inner_y = rect.y + 1;
    let inner_h = rect.height.saturating_sub(2) as usize;

    let glyph = card.status.glyph();
    let short = crate::beads::short_id(&card.id);
    let status_up = card.status.label().to_uppercase();
    let prefix = format!("{} ({}) [{}] ", glyph, short, status_up);
    let prefix_trimmed = prefix.trim_end();
    let prefix_len = prefix.chars().count();

    let full_text = format!("{}{}", prefix, card.title);
    let lines = super::layout::wrap_text_cells(&full_text, content_w);

    let plain_style = Style::default().fg(Color::White).bg(bg);

    for (i, line_text) in lines.iter().enumerate() {
        if i >= inner_h {
            break;
        }
        let y = inner_y + i as u16;

        let styled_prefix = i == 0 && line_text.starts_with(prefix_trimmed);
        if styled_prefix {
            let mut x = inner_x;
            let g = format!("{} ", glyph);
            buf.set_string(x, y, &g, Style::default().fg(status).bg(bg));
            x += g.chars().count() as u16;

            let id_part = format!("({}) ", short);
            buf.set_string(x, y, &id_part, Style::default().fg(Color::Gray).bg(bg));
            x += id_part.chars().count() as u16;

            let status_part = format!("[{}] ", status_up);
            buf.set_string(
                x,
                y,
                &status_part,
                Style::default()
                    .fg(status)
                    .bg(bg)
                    .add_modifier(Modifier::BOLD),
            );
            x += status_part.chars().count() as u16;

            let rest: String = line_text.chars().skip(prefix_len).collect();
            if !rest.is_empty() {
                buf.set_string(x, y, &rest, plain_style);
            }
        } else {
            buf.set_string(inner_x, y, line_text, plain_style);
        }
    }
}

fn draw_edge(area: Rect, buf: &mut Buffer, from: &Card, to: &Card, vx: i32, vy: i32) {
    let style = Style::default().fg(EDGE_COLOR);
    let from_gx = from.rank as i32 * (CARD_W + RANK_GAP);
    let to_gx = to.rank as i32 * (CARD_W + RANK_GAP);

    let from_right = from_gx + CARD_W;
    let from_mid_y = from.y + from.height as i32 / 2;
    let to_left = to_gx;
    let to_mid_y = to.y + to.height as i32 / 2;

    let mid_x = (from_right + to_left) / 2;

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

    // Match card backgrounds — no border, opaque HEADER_BG fill.
    fill_rect(buf, rect, HEADER_BG);

    for (i, line) in lines.iter().enumerate() {
        let line_x = x + (width.saturating_sub(line.chars().count() as u16)) / 2;
        let line_y = y + 2 + i as u16;
        buf.set_string(line_x, line_y, *line, Style::default().fg(color).bg(HEADER_BG));
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

#[derive(Debug, Clone, Copy)]
enum TipStyle {
    Default,
    Title,
    Header,
    Dim,
}

/// Draw a tooltip next to the selected card with task details.
///
/// Placement rules:
/// - Normal: width up to 30% of graph area (clamped [24, 40]), height up to 70%.
/// - Expanded: width up to 60% of graph area (clamped [40, 80]), height up to 90%.
/// - Positioned to the right of the card; flips left if it would overflow.
/// - Vertically centered on the card, clamped to the graph area.
/// - Skipped entirely if the selected card isn't visible in the viewport.
fn draw_tooltip(
    area: Rect,
    buf: &mut Buffer,
    selected: &Card,
    cards: &[Card],
    tasks: &[crate::beads::Task],
    vx: i32,
    vy: i32,
    expanded: bool,
) {
    let card_gx = selected.rank as i32 * (CARD_W + RANK_GAP);
    let card_gy = selected.y;
    let card_h = selected.height as i32;
    if to_screen_rect(card_gx, card_gy, CARD_W, card_h, vx, vy, area).is_none() {
        return;
    }
    let Some(task) = tasks.iter().find(|t| t.id == selected.id) else {
        return;
    };

    let (pct_w, min_w, max_w_cap, pct_h) = if expanded {
        (60u32, 40u16, 80u16, 90u32)
    } else {
        (30u32, 24u16, 40u16, 70u32)
    };
    let max_w = ((area.width as u32) * pct_w / 100) as u16;
    let tooltip_w = max_w.clamp(min_w, max_w_cap).min(area.width.saturating_sub(2));
    if tooltip_w < 12 {
        return;
    }
    let content_w = tooltip_w.saturating_sub(4) as usize;
    let color = status_color(selected.status);

    let mut lines: Vec<(String, TipStyle)> = Vec::new();

    // Header line: "{id} · P{n} · {dag_status}" (+ expanded marker).
    // Use the DAG-level label (done/active/ready/blocked/open) for consistency
    // with the card text; the legend in the bottom-right explains the
    // mapping from bd's raw statuses.
    let short = crate::beads::short_id(&selected.id);
    let header = format!(
        "{} · P{} · {}",
        short,
        selected.priority,
        selected.status.label()
    );
    let header = if expanded {
        format!("{}  ⤢ expanded", header)
    } else {
        header
    };
    lines.push((truncate(&header, content_w), TipStyle::Header));
    lines.push((String::new(), TipStyle::Default));

    // title
    for chunk in wrap(&selected.title, content_w) {
        lines.push((chunk, TipStyle::Title));
    }

    // description — expanded only
    if expanded && !task.description.trim().is_empty() {
        lines.push((String::new(), TipStyle::Default));
        lines.push(("Description".to_string(), TipStyle::Header));
        for chunk in wrap(task.description.trim(), content_w) {
            lines.push((chunk, TipStyle::Default));
        }
    }

    // dependencies with titles — always shown
    if !task.dependencies.is_empty() {
        lines.push((String::new(), TipStyle::Default));
        lines.push(("Depends on:".to_string(), TipStyle::Header));
        for dep_id in &task.dependencies {
            let dep_short = crate::beads::short_id(dep_id);
            let dep_title = cards
                .iter()
                .find(|c| c.id == *dep_id)
                .map(|c| c.title.clone())
                .unwrap_or_else(|| "?".to_string());
            let full = format!("- ({}) {}", dep_short, dep_title);
            for (i, chunk) in wrap(&full, content_w).into_iter().enumerate() {
                let rendered = if i == 0 { chunk } else { format!("  {}", chunk) };
                lines.push((rendered, TipStyle::Default));
            }
        }
    }

    // labels — expanded only
    if expanded && !task.labels.is_empty() {
        lines.push((String::new(), TipStyle::Default));
        let labels_str = format!("Labels: {}", task.labels.join(", "));
        for chunk in wrap(&labels_str, content_w) {
            lines.push((chunk, TipStyle::Dim));
        }
    }

    // Expanded-only extras
    if expanded {
        let created = date_only(&task.created_at);
        let updated = date_only(&task.updated_at);
        if !created.is_empty() || !updated.is_empty() {
            lines.push((String::new(), TipStyle::Default));
        }
        if !created.is_empty() {
            lines.push((format!("Created: {}", created), TipStyle::Dim));
        }
        if !updated.is_empty() {
            lines.push((format!("Updated: {}", updated), TipStyle::Dim));
        }
        lines.push((String::new(), TipStyle::Default));
        lines.push(("Press Esc to collapse".to_string(), TipStyle::Dim));
    }

    let max_h = ((area.height as u32) * pct_h / 100).max(8) as u16;
    let content_h = lines.len() as u16 + 2;
    let tooltip_h = content_h.min(max_h);
    if tooltip_h < 5 {
        return;
    }

    let visible_rows = tooltip_h.saturating_sub(2) as usize;
    if lines.len() > visible_rows {
        lines.truncate(visible_rows.saturating_sub(1));
        lines.push(("…".to_string(), TipStyle::Dim));
    }

    // Placement: prefer right of card, flip left on overflow.
    let screen_card_x = card_gx - vx + area.x as i32;
    let screen_card_y = card_gy - vy + area.y as i32;
    let card_right = screen_card_x + CARD_W;
    let card_left = screen_card_x;
    let card_mid_y = screen_card_y + card_h / 2;

    let fits_right = card_right + 1 + tooltip_w as i32 <= (area.x + area.width) as i32;
    let tooltip_x = if fits_right {
        card_right + 1
    } else {
        (card_left - 1 - tooltip_w as i32).max(area.x as i32)
    };

    let tooltip_y = (card_mid_y - tooltip_h as i32 / 2)
        .max(area.y as i32)
        .min((area.y + area.height - tooltip_h) as i32);

    let rect = Rect::new(tooltip_x as u16, tooltip_y as u16, tooltip_w, tooltip_h);

    // Opaque fill with a darker bg than cards so the tooltip is visually
    // distinct from the graph behind it.
    fill_rect(buf, rect, TOOLTIP_BG);

    let ix = rect.x + 2;
    let iy = rect.y + 1;
    for (i, (line, style_kind)) in lines.iter().enumerate() {
        if (i as u16) >= rect.height.saturating_sub(2) {
            break;
        }
        let style = match style_kind {
            TipStyle::Header => Style::default().fg(color).bg(TOOLTIP_BG).add_modifier(Modifier::BOLD),
            TipStyle::Title => Style::default().fg(Color::White).bg(TOOLTIP_BG).add_modifier(Modifier::BOLD),
            TipStyle::Dim => Style::default().fg(Color::DarkGray).bg(TOOLTIP_BG),
            TipStyle::Default => Style::default().fg(Color::Gray).bg(TOOLTIP_BG),
        };
        buf.set_string(ix, iy + i as u16, line, style);
    }
}

/// Extract the YYYY-MM-DD prefix from a beads ISO timestamp. Returns empty
/// if the input is empty or malformed.
fn date_only(ts: &str) -> String {
    ts.split('T').next().unwrap_or("").to_string()
}

/// Paint the two fixed reference panels in the bottom-right: the glyph
/// legend on the left, the bd-to-DAG status translation on the right. Same
/// HEADER_BG as cards. Skipped if the area is too small to fit them.
fn draw_legends(area: Rect, buf: &mut Buffer) {
    const ICONS_W: u16 = 16;
    const STATUS_W: u16 = 28;
    /// Box height = 1 top pad + 1 header + 5 entries + 1 bottom pad.
    const LEGEND_H: u16 = 8;
    const GAP: u16 = 1;
    /// External padding — matches the 1-line breathing room notifications use.
    const EXT_PAD: u16 = 1;

    let total_w = ICONS_W + GAP + STATUS_W + EXT_PAD;
    if area.width < total_w || area.height < LEGEND_H + EXT_PAD {
        return;
    }

    let y = area.y + area.height - LEGEND_H - EXT_PAD;
    let status_x = area.x + area.width - STATUS_W - EXT_PAD;
    let icons_x = status_x - GAP - ICONS_W;

    let icons_rect = Rect::new(icons_x, y, ICONS_W, LEGEND_H);
    fill_rect(buf, icons_rect, HEADER_BG);
    draw_icons_legend(buf, icons_rect);

    let status_rect = Rect::new(status_x, y, STATUS_W, LEGEND_H);
    fill_rect(buf, status_rect, HEADER_BG);
    draw_status_legend(buf, status_rect);
}

fn draw_icons_legend(buf: &mut Buffer, rect: Rect) {
    // 2-col left internal padding, 1-row top internal padding.
    let ix = rect.x + 2;
    let iy = rect.y + 1;
    buf.set_string(
        ix,
        iy,
        "Icons",
        Style::default().fg(Color::White).bg(HEADER_BG).add_modifier(Modifier::BOLD),
    );
    let entries = [
        CardStatus::Done,
        CardStatus::Active,
        CardStatus::Ready,
        CardStatus::Blocked,
        CardStatus::Open,
    ];
    for (i, s) in entries.iter().enumerate() {
        let row = iy + 1 + i as u16;
        // Bottom internal padding: stop before the last cell.
        if row + 1 >= rect.y + rect.height {
            break;
        }
        let color = status_color(*s);
        buf.set_string(
            ix,
            row,
            format!("{} ", s.glyph()),
            Style::default().fg(color).bg(HEADER_BG),
        );
        buf.set_string(
            ix + 2,
            row,
            s.label(),
            Style::default().fg(Color::Gray).bg(HEADER_BG),
        );
    }
}

fn draw_status_legend(buf: &mut Buffer, rect: Rect) {
    let ix = rect.x + 2;
    let iy = rect.y + 1;
    buf.set_string(
        ix,
        iy,
        "bd status → DAG",
        Style::default().fg(Color::White).bg(HEADER_BG).add_modifier(Modifier::BOLD),
    );
    let rows: &[(&str, CardStatus)] = &[
        ("closed       ", CardStatus::Done),
        ("in progress  ", CardStatus::Active),
        ("open (clear) ", CardStatus::Ready),
        ("open (waits) ", CardStatus::Open),
        ("blocked      ", CardStatus::Blocked),
    ];
    for (i, (raw, mapped)) in rows.iter().enumerate() {
        let row = iy + 1 + i as u16;
        if row + 1 >= rect.y + rect.height {
            break;
        }
        let mapped_color = status_color(*mapped);
        buf.set_string(
            ix,
            row,
            raw,
            Style::default().fg(Color::DarkGray).bg(HEADER_BG),
        );
        buf.set_string(
            ix + raw.chars().count() as u16,
            row,
            "→ ",
            Style::default().fg(Color::DarkGray).bg(HEADER_BG),
        );
        buf.set_string(
            ix + raw.chars().count() as u16 + 2,
            row,
            mapped.label(),
            Style::default().fg(mapped_color).bg(HEADER_BG),
        );
    }
}

fn fill_rect(buf: &mut Buffer, rect: Rect, bg: Color) {
    let buf_area = buf.area();
    let right = (rect.x + rect.width).min(buf_area.x + buf_area.width);
    let bottom = (rect.y + rect.height).min(buf_area.y + buf_area.height);
    let style = Style::default().bg(bg);
    for y in rect.y..bottom {
        for x in rect.x..right {
            let cell = &mut buf[(x, y)];
            cell.set_symbol(" ");
            cell.set_style(style);
        }
    }
}

fn wrap(text: &str, width: usize) -> Vec<String> {
    if width == 0 {
        return vec![];
    }
    let mut result = Vec::new();
    for line in text.lines() {
        if line.is_empty() {
            result.push(String::new());
            continue;
        }
        let mut remaining = line;
        while !remaining.is_empty() {
            if remaining.chars().count() <= width {
                result.push(remaining.to_string());
                break;
            }
            let break_at = remaining
                .char_indices()
                .take(width)
                .filter(|(_, c)| *c == ' ')
                .map(|(i, _)| i)
                .last()
                .unwrap_or_else(|| {
                    remaining
                        .char_indices()
                        .nth(width)
                        .map(|(i, _)| i)
                        .unwrap_or(remaining.len())
                });
            let (chunk, rest) = remaining.split_at(break_at);
            result.push(chunk.to_string());
            remaining = rest.trim_start();
        }
    }
    result
}
