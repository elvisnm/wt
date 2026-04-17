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
/// Selected card background — two shades lighter than HEADER_BG so the
/// selection reads clearly while still keeping text legible.
const SELECTED_BG: Color = Color::Indexed(238);

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

    // 1. Priority-column headers. Each column gets a centered section-style
    //    row above its first component: "──── Tasks group - Priority N ────".
    //    The line spans the full column width (aligned with where the first
    //    card starts and the last card ends), with dashes padding both sides
    //    of the centered label.
    for col in &layout.priority_columns {
        let title_y = col.y + 1; // middle of the 3-row top pad
        let label = "Tasks group - ";
        let pri_text = format!("Priority {}", col.priority);
        let text_len = label.chars().count() + pri_text.chars().count();
        // +2 cols: one space on each side of the centered text.
        let padded_text_len = text_len + 2;

        let rule_style = Style::default().fg(Color::Indexed(239));
        let label_style = Style::default().fg(Color::Gray);
        let pri_style = Style::default()
            .fg(priority_text_color(col.priority))
            .add_modifier(Modifier::BOLD);

        if (col.width as usize) <= padded_text_len {
            // Column too narrow for rules — just paint label + priority.
            let mut x = col.x;
            paint_text(area, buf, x, title_y, label, label_style, vx, vy);
            x += label.chars().count() as i32;
            paint_text(area, buf, x, title_y, &pri_text, pri_style, vx, vy);
            continue;
        }

        let rule_total = col.width as usize - padded_text_len;
        let left_rule = rule_total / 2;
        let right_rule = rule_total - left_rule;
        let left_dashes: String = "─".repeat(left_rule);
        let right_dashes: String = "─".repeat(right_rule);

        let mut x = col.x;
        paint_text(area, buf, x, title_y, &left_dashes, rule_style, vx, vy);
        x += left_rule as i32;
        paint_text(area, buf, x, title_y, " ", rule_style, vx, vy);
        x += 1;
        paint_text(area, buf, x, title_y, label, label_style, vx, vy);
        x += label.chars().count() as i32;
        paint_text(area, buf, x, title_y, &pri_text, pri_style, vx, vy);
        x += pri_text.chars().count() as i32;
        paint_text(area, buf, x, title_y, " ", rule_style, vx, vy);
        x += 1;
        paint_text(area, buf, x, title_y, &right_dashes, rule_style, vx, vy);
    }

    // 2. Paint trunks next so cards cover any stray trunk characters.
    //    Each source card with one or more outgoing edges into the same target
    //    rank gets ONE trunk that bundles those edges, instead of drawing N
    //    separate elbow paths.
    let trunks = compute_trunks(layout);
    for trunk in &trunks {
        draw_trunk(area, buf, layout, trunk, vx, vy);
    }

    for card in &layout.cards {
        if let Some(rect) = to_screen_rect(card.x, card.y, CARD_W, card.height as i32, vx, vy, area)
        {
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
            draw_tooltip(area, buf, selected, &layout.cards, &state.tasks, vx, vy);
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
    let plain_style = Style::default().fg(Color::White).bg(bg);

    // Row 0 of content: header line `{glyph} {id}·{STATUS}` — always on its
    // own row. Title never shares this line even if it would fit.
    if inner_h >= 1 {
        let y = inner_y;
        let mut x = inner_x;

        let g = format!("{} ", glyph);
        buf.set_string(x, y, &g, Style::default().fg(status).bg(bg));
        x += g.chars().count() as u16;

        buf.set_string(x, y, short, Style::default().fg(Color::Gray).bg(bg));
        x += short.chars().count() as u16;

        buf.set_string(x, y, " · ", Style::default().fg(Color::Gray).bg(bg));
        x += 3;

        buf.set_string(
            x,
            y,
            &status_up,
            Style::default()
                .fg(status)
                .bg(bg)
                .add_modifier(Modifier::BOLD),
        );
    }

    // Row 1..: wrapped title.
    let title_lines = super::layout::wrap_text_cells(&card.title, content_w);
    for (i, line_text) in title_lines.iter().enumerate() {
        let row = 1 + i;
        if row >= inner_h {
            break;
        }
        let y = inner_y + row as u16;
        buf.set_string(inner_x, y, line_text, plain_style);
    }
}

/// One bundled dependency flow from a source card into all its dependents in
/// a single target rank. The trunk column sits near the target rank's left
/// edge so the lines "touch" the target cards with minimal stubs.
struct Trunk {
    source: usize,
    target_rank: usize,
    targets: Vec<usize>,
    trunk_x: i32,
}

fn compute_trunks(layout: &GraphLayout) -> Vec<Trunk> {
    use std::collections::HashMap;
    let mut grouped: HashMap<(usize, usize), Vec<usize>> = HashMap::new();
    for edge in &layout.edges {
        let target_rank = layout.cards[edge.to].rank;
        grouped.entry((edge.from, target_rank)).or_default().push(edge.to);
    }

    // Stable lane assignment per target rank: trunks sit just left of the
    // target cards, stacked in 2-col lanes so no two trunks share a column.
    // Source order within the target rank decides the slot; a stable sort
    // keeps layout deterministic.
    let mut by_target_rank: HashMap<usize, Vec<(usize, Vec<usize>)>> = HashMap::new();
    for ((source, target_rank), targets) in grouped {
        by_target_rank
            .entry(target_rank)
            .or_default()
            .push((source, targets));
    }

    let mut trunks = Vec::new();
    let mut keys: Vec<usize> = by_target_rank.keys().copied().collect();
    keys.sort();
    for target_rank in keys {
        let mut group = by_target_rank.remove(&target_rank).unwrap();
        group.sort_by_key(|(src, _)| {
            (
                layout.cards[*src].rank,
                layout.cards[*src].row,
                layout.cards[*src].id.clone(),
            )
        });
        for (slot, (source, mut targets)) in group.into_iter().enumerate() {
            targets.sort_by_key(|&t| layout.cards[t].y);
            // All targets in this trunk share the same column x (same rank
            // within the same priority column), so the first target's x is
            // the trunk's anchor.
            let target_left = targets
                .first()
                .map(|&t| layout.cards[t].x)
                .unwrap_or(0);
            let trunk_x = target_left - 2 - slot as i32 * 2;
            trunks.push(Trunk {
                source,
                target_rank,
                targets,
                trunk_x,
            });
        }
    }
    trunks
}

fn draw_trunk(area: Rect, buf: &mut Buffer, layout: &GraphLayout, trunk: &Trunk, vx: i32, vy: i32) {
    let source = &layout.cards[trunk.source];
    let source_right = source.x + CARD_W;
    let source_mid_y = source.y + source.height as i32 / 2;
    let target_left = trunk
        .targets
        .first()
        .map(|&t| layout.cards[t].x)
        .unwrap_or(source.x + CARD_W + RANK_GAP);

    let color = if source.status == CardStatus::Done {
        Color::LightGreen
    } else {
        Color::LightRed
    };
    let style = Style::default().fg(color).add_modifier(Modifier::BOLD);

    let target_ys: Vec<i32> = trunk
        .targets
        .iter()
        .map(|&idx| layout.cards[idx].y + layout.cards[idx].height as i32 / 2)
        .collect();
    let mut span_ys = target_ys.clone();
    span_ys.push(source_mid_y);
    let y_min = *span_ys.iter().min().unwrap_or(&source_mid_y);
    let y_max = *span_ys.iter().max().unwrap_or(&source_mid_y);

    // 1. Horizontal lead from the source's right edge to the trunk column.
    //    The cell under the trunk itself gets the source-junction character
    //    painted on top in step 3, so stop one cell early here.
    for x in source_right..trunk.trunk_x {
        paint(area, buf, x, source_mid_y, vx, vy, "─", style);
    }

    // 2. Vertical trunk spanning the union of source y and all target y.
    for y in y_min..=y_max {
        paint(area, buf, trunk.trunk_x, y, vx, vy, "│", style);
    }

    // 3. Source junction (line enters the trunk from the left).
    let source_ch = if y_min == y_max {
        // Single-target trunk at same y as source — straight line passes through.
        "─"
    } else if source_mid_y == y_min {
        "┐"
    } else if source_mid_y == y_max {
        "┘"
    } else {
        "┤"
    };
    paint(area, buf, trunk.trunk_x, source_mid_y, vx, vy, source_ch, style);

    // 4. Target junctions + stubs + arrow tips.
    for (&target_idx, &target_mid_y) in trunk.targets.iter().zip(target_ys.iter()) {
        let target = &layout.cards[target_idx];
        let _ = target;
        // Junction char on the trunk at this target's y.
        let same_as_source = target_mid_y == source_mid_y;
        let target_ch = if same_as_source {
            // Source and target share a y — trunk is already the straight line.
            source_ch
        } else if target_mid_y == y_min {
            "┌"
        } else if target_mid_y == y_max {
            "└"
        } else {
            "├"
        };
        paint(area, buf, trunk.trunk_x, target_mid_y, vx, vy, target_ch, style);

        // Horizontal stub from trunk to just before the target's left edge,
        // then the arrow tip on target_left - 1.
        for x in (trunk.trunk_x + 1)..(target_left - 1) {
            paint(area, buf, x, target_mid_y, vx, vy, "─", style);
        }
        if target_left - 1 > trunk.trunk_x {
            paint(area, buf, target_left - 1, target_mid_y, vx, vy, "▶", style);
        }
    }
}

#[allow(dead_code)]
fn draw_edge(area: Rect, buf: &mut Buffer, from: &Card, to: &Card, vx: i32, vy: i32) {
    // Two-color rule: an edge is GREEN when its source is done (dependency
    // satisfied) and RED otherwise (dependency still pending).
    let _ = EDGE_COLOR;
    let color = if from.status == CardStatus::Done {
        Color::LightGreen
    } else {
        Color::LightRed
    };
    let style = Style::default().fg(color).add_modifier(Modifier::BOLD);

    let from_right = from.x + CARD_W;
    let from_mid_y = from.y + from.height as i32 / 2;
    let to_left = to.x;
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
/// - Width up to 45% of graph area (clamped [30, 60]), height up to 90%.
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
) {
    let card_gx = selected.x;
    let card_gy = selected.y;
    let card_h = selected.height as i32;
    if to_screen_rect(card_gx, card_gy, CARD_W, card_h, vx, vy, area).is_none() {
        return;
    }
    let Some(task) = tasks.iter().find(|t| t.id == selected.id) else {
        return;
    };

    // Single tooltip size — the earlier "collapsed" variant is gone, every
    // selection gets the full-detail tooltip.
    let (pct_w, min_w, max_w_cap, pct_h) = (45u32, 30u16, 60u16, 90u32);
    let max_w = ((area.width as u32) * pct_w / 100) as u16;
    let tooltip_w = max_w.clamp(min_w, max_w_cap).min(area.width.saturating_sub(2));
    if tooltip_w < 12 {
        return;
    }
    let content_w = tooltip_w.saturating_sub(4) as usize;

    let mut lines: Vec<(String, TipStyle)> = Vec::new();

    // Header line: "{id} · P{n} · {dag_status}" (+ expanded marker).
    // title
    for chunk in wrap(&selected.title, content_w) {
        lines.push((chunk, TipStyle::Title));
    }

    // description
    if !task.description.trim().is_empty() {
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

    // labels
    if !task.labels.is_empty() {
        lines.push((String::new(), TipStyle::Default));
        lines.push(("Labels:".to_string(), TipStyle::Header));
        for label in &task.labels {
            lines.push((format!("- {}", label), TipStyle::Default));
        }
    }

    // timestamps
    let created = date_only(&task.created_at);
    let updated = date_only(&task.updated_at);
    if !created.is_empty() || !updated.is_empty() {
        lines.push((String::new(), TipStyle::Default));
        lines.push(("Timestamps:".to_string(), TipStyle::Header));
    }
    if !created.is_empty() {
        lines.push((format!("- Created: {}", created), TipStyle::Default));
    }
    if !updated.is_empty() {
        lines.push((format!("- Updated: {}", updated), TipStyle::Default));
    }

    let max_h = ((area.height as u32) * pct_h / 100).max(8) as u16;
    let content_h = lines.len() as u16 + 2;
    let tooltip_h = content_h.min(max_h);
    // Tasks with no dependencies and a short title legitimately produce a
    // 3-row tooltip (1 pad + 1 title line + 1 pad). Only skip when even the
    // padding can't fit.
    if tooltip_h < 3 {
        return;
    }

    let visible_rows = tooltip_h.saturating_sub(2) as usize;
    if lines.len() > visible_rows {
        lines.truncate(visible_rows.saturating_sub(1));
        lines.push(("…".to_string(), TipStyle::Dim));
    }

    // Placement: always to the right of the card so the tooltip's position
    // is predictable (pan_to_selected keeps the card + tooltip in view).
    let screen_card_x = card_gx - vx + area.x as i32;
    let screen_card_y = card_gy - vy + area.y as i32;
    let card_right = screen_card_x + CARD_W;
    let card_mid_y = screen_card_y + card_h / 2;

    let tooltip_x = card_right + 1;

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
            // Tooltip content stays identical regardless of the selected
            // task's status — only the card boxes in the DAG carry color.
            // Title uses the same tone as the "Depends on" list so both read
            // as neutral body text with the same weight of gray.
            TipStyle::Header => Style::default().fg(Color::White).bg(TOOLTIP_BG).add_modifier(Modifier::BOLD),
            TipStyle::Title => Style::default().fg(Color::Gray).bg(TOOLTIP_BG).add_modifier(Modifier::BOLD),
            TipStyle::Dim => Style::default().fg(Color::DarkGray).bg(TOOLTIP_BG),
            TipStyle::Default => Style::default().fg(Color::Gray).bg(TOOLTIP_BG),
        };
        buf.set_string(ix, iy + i as u16, line, style);
    }

    // The tooltip no longer has a header row — title is the first line.

    // Speech-bubble tail: always on the left side of the tooltip pointing
    // at the card, since placement is always to the right now.
    let tooltip_top = tooltip_y;
    let tooltip_bottom_incl = tooltip_y + tooltip_h as i32 - 1;
    let tail_y = card_mid_y.clamp(tooltip_top, tooltip_bottom_incl);
    let tail_x = tooltip_x - 1;
    let tail_ch = "◀";
    let buf_left = area.x as i32;
    let buf_right = (area.x + area.width) as i32;
    let buf_top = area.y as i32;
    let buf_bottom = (area.y + area.height) as i32;
    if tail_x >= buf_left && tail_x < buf_right && tail_y >= buf_top && tail_y < buf_bottom {
        // The arrow glyph itself is rendered in the tooltip's background
        // color on the graph's default background — it reads as a silhouette
        // pointing at the card, visually continuing the tooltip without a
        // filled cell around it.
        let cell = &mut buf[(tail_x as u16, tail_y as u16)];
        cell.set_symbol(tail_ch);
        cell.set_fg(TOOLTIP_BG);
        cell.set_bg(Color::Reset);
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
        ("closed        ", CardStatus::Done),
        ("in progress   ", CardStatus::Active),
        ("blockers clear", CardStatus::Ready),
        ("blockers wait ", CardStatus::Blocked),
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

/// Priority accent colors — shared with the tasks list panel so priorities
/// read the same everywhere in the app.
fn priority_text_color(pri: u8) -> Color {
    use crate::ui::style::{DIM_TEXT_COLOR, HINT_COLOR, STOPPED_COLOR};
    match pri {
        0 => STOPPED_COLOR,
        1 => HINT_COLOR,
        2 => Color::Yellow,
        _ => DIM_TEXT_COLOR,
    }
}

/// Paint a string at a graph-space position, clipped to the visible area.
fn paint_text(
    area: Rect,
    buf: &mut Buffer,
    gx: i32,
    gy: i32,
    text: &str,
    style: Style,
    vx: i32,
    vy: i32,
) {
    let sy = gy - vy + area.y as i32;
    if sy < area.y as i32 || sy >= (area.y + area.height) as i32 {
        return;
    }
    let start_sx = gx - vx + area.x as i32;
    let buf_right = (area.x + area.width) as i32;
    for (i, ch) in text.chars().enumerate() {
        let sx = start_sx + i as i32;
        if sx < area.x as i32 || sx >= buf_right {
            continue;
        }
        let mut tmp = [0u8; 4];
        let s = ch.encode_utf8(&mut tmp);
        buf[(sx as u16, sy as u16)].set_symbol(s).set_style(style);
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
