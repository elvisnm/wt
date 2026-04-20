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
/// Caches the panel's screen rect into `state.dag_area` every frame so
/// the mouse handler can check whether a click lands on the graph.
pub fn render(area: Rect, buf: &mut Buffer, state: &DagGraphState, spin_frame: usize) {
    state.dag_area.set(Some(area));
    // Wipe last frame's hit rects up-front; they'll be repopulated as
    // cards paint. For loading / error / empty states we leave the
    // vector empty so the mouse handler can't match stale cards.
    state.card_rects.borrow_mut().clear();

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

    {
        let mut rects = state.card_rects.borrow_mut();
        rects.reserve(layout.cards.len());
        for card in &layout.cards {
            if let Some(rect) = to_screen_rect(card.x, card.y, CARD_W, card.height as i32, vx, vy, area)
            {
                let selected = state.selected_id.as_deref() == Some(card.id.as_str());
                draw_card(area, buf, rect, card, selected, vx, vy);
                // Record the *visible* rect — we only want clicks on
                // actually-painted cells to hit, not on the virtual
                // area of a card that's scrolled offscreen.
                rects.push((card.id.clone(), rect));
            }
        }
    }

    if state.overlays_visible {
        // Fixed legend boxes in the bottom-right. Painted before the tooltip
        // so the tooltip always wins the z-order when it would overlap.
        draw_legends(area, buf);

        // Minimap lives in the bottom-left, mirroring the legends on the
        // right. Painted before the tooltip for the same z-order reason.
        draw_minimap(area, buf, layout, vx, vy);

        // Progress bar spans the bottom-middle gap between minimap and
        // legends. Same z-order as the other overlays — the tooltip
        // paints afterward so it still wins on top.
        draw_progress_bar(area, buf, layout);
    }

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

fn draw_card(area: Rect, buf: &mut Buffer, rect: Rect, card: &Card, selected: bool, vx: i32, vy: i32) {
    let status = status_color(card.status);
    let bg = if selected { SELECTED_BG } else { HEADER_BG };

    // Opaque card fill — brighter shade when selected so it lifts off the
    // graph without breaking arrow connections on the card's edge.
    fill_rect(buf, rect, bg);

    if rect.width < 4 || rect.height < 3 {
        return;
    }

    // Text is positioned in graph space and clipped per-cell via paint_text.
    // Using rect-relative coordinates (rect.x + 2) drifts the header toward
    // the screen edge when a card is clipped on the left, pushing the
    // id · STATUS text past the card's visible fill.
    let content_w = (CARD_W - 4) as usize;
    let inner_gx = card.x + 2;
    let inner_gy = card.y + 1;
    let inner_h = (card.height as i32 - 2).max(0) as usize;

    let glyph = card.status.glyph();
    let short = crate::beads::short_id(&card.id);
    let status_up = card.status.label().to_uppercase();
    let plain_style = Style::default().fg(Color::White).bg(bg);

    // Row 0 of content: header line `{glyph} {id}·{STATUS}` — always on its
    // own row. Title never shares this line even if it would fit.
    if inner_h >= 1 {
        let y = inner_gy;
        let mut x = inner_gx;

        let g = format!("{} ", glyph);
        paint_text(area, buf, x, y, &g, Style::default().fg(status).bg(bg), vx, vy);
        x += g.chars().count() as i32;

        paint_text(area, buf, x, y, short, Style::default().fg(Color::Gray).bg(bg), vx, vy);
        x += short.chars().count() as i32;

        paint_text(area, buf, x, y, " · ", Style::default().fg(Color::Gray).bg(bg), vx, vy);
        x += 3;

        paint_text(
            area,
            buf,
            x,
            y,
            &status_up,
            Style::default()
                .fg(status)
                .bg(bg)
                .add_modifier(Modifier::BOLD),
            vx,
            vy,
        );
    }

    // Row 1..: wrapped title.
    let title_lines = super::layout::wrap_text_cells(&card.title, content_w);
    for (i, line_text) in title_lines.iter().enumerate() {
        let row = 1 + i;
        if row >= inner_h {
            break;
        }
        let y = inner_gy + row as i32;
        paint_text(area, buf, inner_gx, y, line_text, plain_style, vx, vy);
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
    // title — prefixed with "P{n} · " so the priority reads at a glance
    // without having to cross-reference the card itself.
    let titled = format!("P{} · {}", selected.priority, selected.title);
    for chunk in wrap(&titled, content_w) {
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
/// Bottom-left minimap: a scaled-down snapshot of the full DAG with a
/// rectangle overlay showing where the viewport is.
///
/// Sizing rule: the panel's height scales with the number of cards so
/// small graphs get a compact minimap and large graphs get a roomier
/// one. Height lands in [10, 20] cells; width is 2× height so the panel
/// renders as a visual square on terminals (cells are ~2x tall as wide).
///
/// Each card draws as its DAG status glyph at a position scaled from
/// its graph-space coordinates, so the minimap reads like a miniature
/// of the main graph.
///
/// The viewport rectangle is sized from the ratio of visible screen
/// area to total graph extent. As the user pans the DAG, the rectangle
/// slides around the minimap to indicate which slice is on screen.
fn draw_minimap(area: Rect, buf: &mut Buffer, layout: &GraphLayout, vx: i32, vy: i32) {
    const EXT_PAD: u16 = 1;

    if layout.cards.is_empty() {
        return;
    }

    // Height grows with task count. The divisor is empirical: divides
    // by 3 so a 30-task graph lands around 10 (the minimum), and a
    // 60-task graph lands around 20 (the maximum).
    let card_count = layout.cards.len() as u16;
    let minimap_h = (card_count / 3).clamp(10, 20);
    let minimap_w = minimap_h * 2;

    if area.width < minimap_w + 2 * EXT_PAD || area.height < minimap_h + 2 * EXT_PAD {
        return;
    }

    // Bottom-left, mirroring the legend panels on the right.
    let rect = Rect::new(
        area.x + EXT_PAD,
        area.y + area.height - minimap_h - EXT_PAD,
        minimap_w,
        minimap_h,
    );
    fill_rect(buf, rect, HEADER_BG);

    // Graph bounds anchored at (0, 0) so the priority-column headers
    // above the first card are included — the viewport covers those too.
    let mut max_gx = 0_i32;
    let mut max_gy = 0_i32;
    for c in &layout.cards {
        let right = c.x + CARD_W;
        if right > max_gx { max_gx = right; }
        let bottom = c.y + c.height as i32;
        if bottom > max_gy { max_gy = bottom; }
    }
    let graph_w = max_gx.max(1);
    let graph_h = max_gy.max(1);

    // Two nested regions inside the panel:
    //   - vp_area : where the viewport rectangle is allowed to draw.
    //   - icon_area: where task glyphs may be placed. Always 1 cell
    //     inside vp_area so that at max zoom (viewport covers the
    //     whole graph) the rectangle's edges never cross an icon.
    //
    // Outer panel padding: 2 cells horizontally, 1 row vertically.
    // Border reserve inside that: 1 cell all around so a fully-sized
    // viewport rectangle has its own track.
    let pad_x: u16 = 2;
    let pad_y: u16 = 1;
    let border_reserve: u16 = 1;

    let vp_area_x = rect.x + pad_x;
    let vp_area_y = rect.y + pad_y;
    let vp_area_w = rect.width as i32 - 2 * pad_x as i32;
    let vp_area_h = rect.height as i32 - 2 * pad_y as i32;

    let inner_x = vp_area_x + border_reserve;
    let inner_y = vp_area_y + border_reserve;
    let inner_w = vp_area_w - 2 * border_reserve as i32;
    let inner_h = vp_area_h - 2 * border_reserve as i32;
    if inner_w <= 0 || inner_h <= 0 || vp_area_w <= 0 || vp_area_h <= 0 {
        return;
    }

    // Viewport rectangle: size derived from the visible-area to
    // total-graph ratio, position tracks the pan. Size stays fixed as
    // the user pans — only the position changes.
    //
    // Scaled against vp_area (the wider region), not inner. That way
    // the border at max size sits in its reserved track and never
    // overlaps the icons.
    //
    // Painted BEFORE the card glyphs so the border reads as transparent:
    // any task on the rectangle's edge keeps its glyph, so the user can
    // see through the border to the cards the rectangle encloses.
    let vp_w_cells =
        ((area.width as i32 * vp_area_w + graph_w - 1) / graph_w).clamp(1, vp_area_w);
    let vp_h_cells =
        ((area.height as i32 * vp_area_h + graph_h - 1) / graph_h).clamp(1, vp_area_h);

    let vp_x = {
        let raw = vx * vp_area_w / graph_w;
        raw.clamp(0, vp_area_w - vp_w_cells) as u16
    };
    let vp_y = {
        let raw = vy * vp_area_h / graph_h;
        raw.clamp(0, vp_area_h - vp_h_cells) as u16
    };
    let left = vp_area_x + vp_x;
    let top = vp_area_y + vp_y;
    let right = left + vp_w_cells as u16 - 1;
    let bottom = top + vp_h_cells as u16 - 1;

    // Muted gray matches the section header rules (the `── worktrees ──`
    // separator line), so the viewport indicator reads as structural
    // chrome rather than competing with the task glyphs.
    let border_style = Style::default()
        .fg(crate::ui::style::HEADER_COLOR)
        .bg(HEADER_BG);

    if left == right && top == bottom {
        // Viewport collapses to a single minimap cell — mark it.
        let cell = &mut buf[(left, top)];
        cell.set_symbol("□");
        cell.set_style(border_style);
    } else {
        // Edges.
        for x in left..=right {
            buf[(x, top)].set_symbol("─").set_style(border_style);
            buf[(x, bottom)].set_symbol("─").set_style(border_style);
        }
        for y in top..=bottom {
            buf[(left, y)].set_symbol("│").set_style(border_style);
            buf[(right, y)].set_symbol("│").set_style(border_style);
        }
        // Corners.
        buf[(left, top)].set_symbol("┌").set_style(border_style);
        buf[(right, top)].set_symbol("┐").set_style(border_style);
        buf[(left, bottom)].set_symbol("└").set_style(border_style);
        buf[(right, bottom)].set_symbol("┘").set_style(border_style);
    }

    // Collect the distinct card y-centers and assign each an ordinal
    // minimap row. Scaling from raw y in graph-space would leave blank
    // rows between icons whenever adjacent cards sit far apart in the
    // graph; an ordinal mapping packs every occupied row tight so the
    // vertical column reads as ●/●/● with no gaps.
    let mut y_centers: Vec<i32> = layout
        .cards
        .iter()
        .map(|c| c.y + c.height as i32 / 2)
        .collect();
    y_centers.sort_unstable();
    y_centers.dedup();
    let num_rows = y_centers.len() as i32;

    // Paint cards on top of the rectangle border.
    //
    // Horizontal placement snaps to EVEN cells so adjacent tasks land
    // with a 1-cell gap between them (icons in the same rank would
    // otherwise paste shoulder-to-shoulder).
    //
    // Vertical placement uses the card's y-rank so rows are dense.
    // When there are more distinct rows than the minimap can show,
    // rows compress proportionally.
    for c in &layout.cards {
        let gx = c.x + CARD_W / 2;
        let gy_center = c.y + c.height as i32 / 2;
        let half_w = (inner_w / 2).max(1);
        let mx = ((gx * half_w / graph_w) * 2).clamp(0, inner_w - 1) as u16;

        let ordinal = y_centers
            .binary_search(&gy_center)
            .unwrap_or(0) as i32;
        let my = if num_rows <= inner_h {
            ordinal.clamp(0, inner_h - 1) as u16
        } else {
            (ordinal * inner_h / num_rows).clamp(0, inner_h - 1) as u16
        };

        let cell = &mut buf[(inner_x + mx, inner_y + my)];
        cell.set_symbol(c.status.glyph());
        cell.set_style(Style::default().fg(status_color(c.status)).bg(HEADER_BG));
    }
}

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

/// Bottom-middle progress bar with a color-coded caption row beneath it.
///
/// Rows occupied (bottom-up):
///   `area.height - 1`  empty pad (matches minimap / legend pad row)
///   `area.height - 2`  caption: "done · ready/open · active · blocked"
///                      each label painted in its bucket color
///   `area.height - 3`  the bar itself, 50 cells wide, proportional
///                      segments with inline `NN%` labels where space
///                      allows
///
/// The caption may be wider than the bar depending on task counts (it
/// includes each bucket's count as `done (N)` etc.) and is centered on
/// the same axis, so it extends past each edge of the bar when needed.
/// Skipped entirely if the area is too small to fit both.
fn draw_progress_bar(area: Rect, buf: &mut Buffer, layout: &GraphLayout) {
    const BAR_W: u16 = 50;
    const EXT_PAD: u16 = 1;
    const REQUIRED_H: u16 = 3;

    if layout.cards.is_empty() {
        return;
    }

    let counts = bucket_counts(&layout.cards);
    let segs = allocate_segments(counts, BAR_W as usize);
    if segs.iter().all(|s| s.width == 0) {
        return;
    }

    // Captions carry the per-bucket count as "label (N)" so the user
    // can read distribution without reaching for `bd stats`.
    let captions: [(String, Color); 4] = [
        (format!("done ({})", counts[BUCKET_DONE]), status_color(CardStatus::Done)),
        (format!("ready/open ({})", counts[BUCKET_READY]), status_color(CardStatus::Ready)),
        (format!("active ({})", counts[BUCKET_ACTIVE]), status_color(CardStatus::Active)),
        (format!("blocked ({})", counts[BUCKET_BLOCKED]), status_color(CardStatus::Blocked)),
    ];
    const SEP: &str = " · ";
    let sep_w = SEP.chars().count();
    let caption_w: usize = captions.iter().map(|(l, _)| l.chars().count()).sum::<usize>()
        + sep_w * (captions.len() - 1);

    let needed_w = (BAR_W as usize).max(caption_w) as u16 + 2 * EXT_PAD;
    if area.width < needed_w || area.height < REQUIRED_H + EXT_PAD {
        return;
    }

    let bar_y = area.y + area.height - 3;
    let caption_y = area.y + area.height - 2;
    let bar_x = area.x + (area.width - BAR_W) / 2;
    let caption_x = area.x + (area.width - caption_w as u16) / 2;

    // --- Bar row ---
    let mut cursor = bar_x;
    for (i, seg) in segs.iter().enumerate() {
        if seg.width == 0 {
            continue;
        }
        let fg = captions[i].1;
        let fill_style = Style::default().fg(fg).bg(HEADER_BG);
        for dx in 0..seg.width {
            let cell = &mut buf[(cursor + dx as u16, bar_y)];
            cell.set_symbol("█");
            cell.set_style(fill_style);
        }

        let label = format!("{}%", seg.pct);
        let label_len = label.chars().count();
        // `>=` (not `>`) so a 2-cell segment showing "8%" still gets
        // its label. Tight fit with no side padding, but a visible
        // percentage beats a silent colored stub.
        if seg.width >= label_len {
            let offset = (seg.width - label_len) / 2;
            let label_x = cursor + offset as u16;
            // Contrasting text color: dark for light fills, light for
            // dark fills. Keeps the number legible across the palette.
            let label_fg = match fg {
                Color::Green | Color::Yellow | Color::Cyan | Color::LightGreen
                | Color::LightYellow | Color::LightCyan => Color::Black,
                _ => Color::White,
            };
            let label_style = Style::default()
                .fg(label_fg)
                .bg(fg)
                .add_modifier(Modifier::BOLD);
            buf.set_string(label_x, bar_y, &label, label_style);
        }

        cursor += seg.width as u16;
    }

    // --- Caption row ---
    let sep_style = Style::default().fg(Color::DarkGray).bg(HEADER_BG);
    let mut cx = caption_x;
    for (i, (text, color)) in captions.iter().enumerate() {
        if i > 0 {
            buf.set_string(cx, caption_y, SEP, sep_style);
            cx += sep_w as u16;
        }
        let style = Style::default().fg(*color).bg(HEADER_BG);
        buf.set_string(cx, caption_y, text.as_str(), style);
        cx += text.chars().count() as u16;
    }
}

// ─── Progress bar helpers ────────────────────────────────────────────────
//
// The progress bar folds the 5 CardStatus variants into 4 display buckets
// (DONE, READY/OPEN, ACTIVE, BLOCKED) and splits a fixed-width bar into
// proportional colored segments. Both the percentage and cell-width
// splits use largest-remainder rounding so the values sum exactly to
// their target (100% and bar_w respectively) without 99%/101% drift.

const BUCKET_DONE: usize = 0;
const BUCKET_READY: usize = 1;
const BUCKET_ACTIVE: usize = 2;
const BUCKET_BLOCKED: usize = 3;

/// One segment of the progress bar after proportional allocation.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct Segment {
    pct: u8,
    width: usize,
}

/// Fold the 5 `CardStatus` variants into 4 progress-bar buckets:
/// `[DONE, READY/OPEN, ACTIVE, BLOCKED]`. `Ready` and `Open` share a
/// bucket so the user sees a single "available work" slice rather than
/// two near-identical ones.
fn bucket_counts(cards: &[Card]) -> [usize; 4] {
    let mut counts = [0usize; 4];
    for c in cards {
        let idx = match c.status {
            CardStatus::Done => BUCKET_DONE,
            CardStatus::Ready | CardStatus::Open => BUCKET_READY,
            CardStatus::Active => BUCKET_ACTIVE,
            CardStatus::Blocked => BUCKET_BLOCKED,
        };
        counts[idx] += 1;
    }
    counts
}

/// Split a `bar_w`-cell bar into 4 segments proportional to `counts`.
///
/// Guarantees (when total > 0):
/// - `widths` sum exactly to `bar_w`
/// - `pct` values sum exactly to 100
/// - Zero-count buckets get `width = 0, pct = 0`
/// - A nonzero-count bucket that would round to 0 cells is forced to 1
///   cell (stolen from the widest donor with `width >= 2`). This keeps
///   a present-but-small bucket visible. If no donor qualifies (bar too
///   narrow for all nonzero buckets), the bucket stays at 0.
///
/// When `total == 0` or `bar_w == 0` all widths are 0; percentages are
/// all 0 only if `total == 0`.
fn allocate_segments(counts: [usize; 4], bar_w: usize) -> [Segment; 4] {
    let total: usize = counts.iter().sum();
    if total == 0 {
        return [Segment { pct: 0, width: 0 }; 4];
    }

    let pcts = largest_remainder(&counts, total, 100);
    let widths = if bar_w == 0 {
        [0usize; 4]
    } else {
        let raw = largest_remainder(&counts, total, bar_w);
        enforce_min_one(raw, counts)
    };

    let mut out = [Segment { pct: 0, width: 0 }; 4];
    for i in 0..4 {
        out[i] = Segment {
            pct: pcts[i] as u8,
            width: widths[i],
        };
    }
    out
}

/// Largest-remainder allocation with a count-equality guard.
///
/// Each bucket starts at `floor(count * target / total)`. Leftover units
/// go to the highest-remainder buckets, but with one extra rule: if a
/// tie group (all not-yet-done buckets sharing a count) can't all
/// receive a unit, the whole group is skipped so same-count buckets
/// never diverge in width. Zero-count buckets are never bumped. Ties
/// break toward the lower index so results stay deterministic.
fn largest_remainder(counts: &[usize; 4], total: usize, target: usize) -> [usize; 4] {
    let mut raw = [0usize; 4];
    let mut remainders = [0usize; 4];
    for i in 0..4 {
        raw[i] = counts[i] * target / total;
        remainders[i] = counts[i] * target - raw[i] * total;
    }

    let assigned: usize = raw.iter().sum();
    let mut leftover = target.saturating_sub(assigned);
    if leftover == 0 {
        return raw;
    }

    let mut order: [usize; 4] = [0, 1, 2, 3];
    order.sort_by(|&a, &b| remainders[b].cmp(&remainders[a]).then_with(|| a.cmp(&b)));

    let mut done = [false; 4];
    for &idx in &order {
        if done[idx] || leftover == 0 || counts[idx] == 0 {
            continue;
        }
        let mut group: [usize; 4] = [0; 4];
        let mut group_len = 0usize;
        for j in 0..4 {
            if !done[j] && counts[j] == counts[idx] {
                group[group_len] = j;
                group_len += 1;
            }
        }
        if group_len <= leftover {
            for &j in group.iter().take(group_len) {
                raw[j] += 1;
                done[j] = true;
            }
            leftover -= group_len;
        } else {
            for &j in group.iter().take(group_len) {
                done[j] = true;
            }
        }
    }

    // Unusual fallback: every remaining tie group was too large to
    // absorb its leftover, so units haven't found a home yet. Dump them
    // into nonzero-count buckets in index order so bar_w stays exact.
    if leftover > 0 {
        for (i, slot) in raw.iter_mut().enumerate() {
            if leftover == 0 {
                break;
            }
            if counts[i] == 0 {
                continue;
            }
            *slot += 1;
            leftover -= 1;
        }
    }

    raw
}

/// Force nonzero-count buckets to at least 1 cell by stealing from the
/// widest segment (must have `width >= 2` so it doesn't become invisible
/// itself). Runs until every nonzero-count bucket has width ≥ 1 or no
/// donor qualifies.
fn enforce_min_one(mut widths: [usize; 4], counts: [usize; 4]) -> [usize; 4] {
    loop {
        let Some(victim) = (0..4).find(|&i| counts[i] > 0 && widths[i] == 0) else {
            break;
        };
        let donor = (0..4)
            .filter(|&i| widths[i] >= 2)
            .max_by_key(|&i| widths[i]);
        let Some(d) = donor else {
            break;
        };
        widths[d] -= 1;
        widths[victim] += 1;
    }
    widths
}

#[cfg(test)]
mod tests {
    use super::*;

    fn card_with_status(id: &str, status: CardStatus) -> Card {
        Card {
            id: id.into(),
            title: String::new(),
            priority: 2,
            status,
            rank: 0,
            row: 0,
            x: 0,
            y: 0,
            height: 3,
            list_order: 0,
        }
    }

    #[test]
    fn bucket_counts_folds_ready_and_open_together() {
        let cards = vec![
            card_with_status("a", CardStatus::Done),
            card_with_status("b", CardStatus::Ready),
            card_with_status("c", CardStatus::Open),
            card_with_status("d", CardStatus::Active),
            card_with_status("e", CardStatus::Blocked),
            card_with_status("f", CardStatus::Blocked),
        ];
        let counts = bucket_counts(&cards);
        assert_eq!(counts[BUCKET_DONE], 1);
        assert_eq!(counts[BUCKET_READY], 2); // Ready + Open
        assert_eq!(counts[BUCKET_ACTIVE], 1);
        assert_eq!(counts[BUCKET_BLOCKED], 2);
    }

    #[test]
    fn bucket_counts_empty_input() {
        assert_eq!(bucket_counts(&[]), [0, 0, 0, 0]);
    }

    #[test]
    fn allocate_empty_returns_all_zero() {
        let segs = allocate_segments([0, 0, 0, 0], 30);
        for s in segs {
            assert_eq!(s.pct, 0);
            assert_eq!(s.width, 0);
        }
    }

    #[test]
    fn allocate_single_bucket_takes_full_bar() {
        let segs = allocate_segments([10, 0, 0, 0], 30);
        assert_eq!(segs[BUCKET_DONE], Segment { pct: 100, width: 30 });
        assert_eq!(segs[BUCKET_READY], Segment { pct: 0, width: 0 });
        assert_eq!(segs[BUCKET_ACTIVE], Segment { pct: 0, width: 0 });
        assert_eq!(segs[BUCKET_BLOCKED], Segment { pct: 0, width: 0 });
    }

    #[test]
    fn allocate_bar_width_zero() {
        let segs = allocate_segments([2, 1, 1, 0], 0);
        // Widths all 0, but pcts still sum to 100.
        assert!(segs.iter().all(|s| s.width == 0));
        let pct_sum: u32 = segs.iter().map(|s| s.pct as u32).sum();
        assert_eq!(pct_sum, 100);
    }

    #[test]
    fn allocate_widths_sum_to_bar_w() {
        // Fuzz-like coverage across many count configurations.
        let cases: &[[usize; 4]] = &[
            [1, 1, 1, 1],
            [1, 1, 1, 0],
            [3, 1, 1, 1],
            [1, 2, 3, 4],
            [10, 0, 0, 0],
            [5, 5, 0, 0],
            [7, 3, 2, 1],
            [100, 1, 1, 1],
            [1, 100, 1, 1],
            [13, 17, 23, 29],
        ];
        for bar_w in [10usize, 20, 30, 40, 50] {
            for counts in cases {
                let segs = allocate_segments(*counts, bar_w);
                let sum: usize = segs.iter().map(|s| s.width).sum();
                assert_eq!(sum, bar_w, "counts={:?} bar_w={}", counts, bar_w);
            }
        }
    }

    #[test]
    fn allocate_pcts_sum_to_100() {
        let cases: &[[usize; 4]] = &[
            [1, 1, 1, 1],
            [1, 1, 1, 0],
            [3, 1, 1, 1],
            [1, 2, 3, 4],
            [10, 0, 0, 0],
            [7, 3, 2, 1],
            [100, 1, 1, 1],
            [13, 17, 23, 29],
        ];
        for counts in cases {
            let segs = allocate_segments(*counts, 30);
            let sum: u32 = segs.iter().map(|s| s.pct as u32).sum();
            assert_eq!(sum, 100, "counts={:?}", counts);
        }
    }

    #[test]
    fn allocate_zero_buckets_stay_zero() {
        // Zero-count buckets must never get forced to 1 by min-one logic.
        let segs = allocate_segments([5, 0, 0, 0], 30);
        assert_eq!(segs[BUCKET_READY].width, 0);
        assert_eq!(segs[BUCKET_READY].pct, 0);
        assert_eq!(segs[BUCKET_ACTIVE].width, 0);
        assert_eq!(segs[BUCKET_BLOCKED].width, 0);
    }

    #[test]
    fn allocate_forces_min_one_for_small_nonzero_bucket() {
        // One huge bucket + three tiny ones in a narrow bar. Without the
        // min-one fix, the tiny buckets round to 0 cells and disappear.
        let segs = allocate_segments([100, 1, 1, 1], 10);
        assert!(segs[BUCKET_READY].width >= 1);
        assert!(segs[BUCKET_ACTIVE].width >= 1);
        assert!(segs[BUCKET_BLOCKED].width >= 1);
        // Widths still total the bar width.
        let sum: usize = segs.iter().map(|s| s.width).sum();
        assert_eq!(sum, 10);
    }

    #[test]
    fn allocate_min_one_skipped_when_no_donor_qualifies() {
        // bar_w=2, four nonzero buckets — impossible to give every bucket
        // 1 cell without making some bucket invisible. Must not loop
        // forever and must still sum to bar_w.
        let segs = allocate_segments([1, 1, 1, 1], 2);
        let sum: usize = segs.iter().map(|s| s.width).sum();
        assert_eq!(sum, 2);
        // Exactly two buckets get 1 cell, the other two stay at 0.
        let nonzero = segs.iter().filter(|s| s.width > 0).count();
        assert_eq!(nonzero, 2);
    }

    #[test]
    fn allocate_zero_count_never_gets_leftover_cell() {
        // Three buckets tied at count=1 with 1 leftover cell forces the
        // tie group to be skipped. The zero-count bucket must still not
        // absorb the leftover — it has no tasks to represent.
        let segs = allocate_segments([1, 1, 1, 0], 10);
        assert_eq!(segs[BUCKET_BLOCKED].width, 0);
        let sum: usize = segs.iter().map(|s| s.width).sum();
        assert_eq!(sum, 10);
    }

    #[test]
    fn allocate_same_count_buckets_get_same_width() {
        // Two buckets with identical counts must end up with identical
        // widths even when only one leftover cell would fit. Guards
        // against the "21% and 21% but different pixel widths" bug.
        let segs = allocate_segments([21, 63, 8, 8], 50);
        assert_eq!(segs[BUCKET_ACTIVE].width, segs[BUCKET_BLOCKED].width);
        assert_eq!(segs[BUCKET_ACTIVE].pct, segs[BUCKET_BLOCKED].pct);
        let sum: usize = segs.iter().map(|s| s.width).sum();
        assert_eq!(sum, 50);

        // Same property across a narrower bar where the tie group is
        // the bottleneck for leftover distribution.
        let segs = allocate_segments([21, 63, 8, 8], 30);
        assert_eq!(segs[BUCKET_ACTIVE].width, segs[BUCKET_BLOCKED].width);
    }

    #[test]
    fn allocate_even_split_deterministic() {
        // Identical counts → deterministic order. Leftover cells go to
        // lowest-index buckets first (stable tie-break).
        let segs = allocate_segments([1, 1, 1, 1], 30);
        // 30 / 4 = 7 each, 2 leftover → buckets 0,1 get 8; 2,3 get 7.
        assert_eq!(segs[0].width, 8);
        assert_eq!(segs[1].width, 8);
        assert_eq!(segs[2].width, 7);
        assert_eq!(segs[3].width, 7);
    }
}
