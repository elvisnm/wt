//! DAG layout engine — pure, testable.
//!
//! Assigns each task a (rank, row) in the graph based on longest-path
//! topological depth + within-rank ordering by priority, status, and id.
//! Also computes each card's graph-space y position and height so card
//! contents (`{glyph} ({id}) [{STATUS}] title`) can wrap across as many
//! lines as the title needs without breaking edge routing.

use crate::beads::{short_id, Task};
use std::collections::HashMap;

/// Card width in cells (fixed). Content area = `CARD_W - 2`.
pub const CARD_W: i32 = 22;
/// Horizontal gap between ranks.
pub const RANK_GAP: i32 = 4;
/// Vertical gap between cards within a rank.
pub const ROW_GAP: i32 = 1;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CardStatus {
    Done,
    Active,
    Ready,
    Blocked,
    Open,
}

impl CardStatus {
    pub fn glyph(self) -> &'static str {
        match self {
            CardStatus::Done => "●",
            CardStatus::Active => "◐",
            CardStatus::Ready => "○",
            CardStatus::Blocked => "⊘",
            CardStatus::Open => "◌",
        }
    }

    pub fn label(self) -> &'static str {
        match self {
            CardStatus::Done => "done",
            CardStatus::Active => "active",
            CardStatus::Ready => "ready",
            CardStatus::Blocked => "blocked",
            CardStatus::Open => "open",
        }
    }

    pub fn color(self) -> ratatui::style::Color {
        use ratatui::style::Color;
        match self {
            CardStatus::Done => Color::Green,
            CardStatus::Active => Color::Yellow,
            CardStatus::Ready => Color::Cyan,
            CardStatus::Blocked => Color::Red,
            CardStatus::Open => Color::Gray,
        }
    }
}

/// Map a raw beads status string to the DAG `CardStatus` that would be used
/// if dependency info was available. Without blocker data, an `open` task
/// defaults to `Open` (not `Ready`) — callers that have layout info should
/// prefer looking up the real `Card::status` instead.
pub fn status_from_raw(raw: &str) -> CardStatus {
    match raw {
        "closed" | "done" => CardStatus::Done,
        "in_progress" => CardStatus::Active,
        "blocked" => CardStatus::Blocked,
        _ => CardStatus::Open,
    }
}

#[derive(Debug, Clone)]
pub struct Card {
    pub id: String,
    pub title: String,
    pub priority: u8,
    pub status: CardStatus,
    pub rank: usize,
    pub row: usize,
    /// Graph-space y coordinate (top edge).
    pub y: i32,
    /// Card height in cells, sized to fit the wrapped content.
    pub height: u16,
}

#[derive(Debug, Clone)]
pub struct Edge {
    pub from: usize, // card index of blocker (earlier rank)
    pub to: usize,   // card index of dependent (later rank)
}

#[derive(Debug, Clone, Default)]
pub struct GraphLayout {
    pub cards: Vec<Card>,
    pub edges: Vec<Edge>,
    pub rank_count: usize,
    pub rows_per_rank: Vec<usize>,
}

pub fn compute_layout(tasks: &[Task]) -> GraphLayout {
    if tasks.is_empty() {
        return GraphLayout::default();
    }

    let id_to_idx: HashMap<&str, usize> = tasks
        .iter()
        .enumerate()
        .map(|(i, t)| (t.id.as_str(), i))
        .collect();

    let mut ranks: Vec<Option<usize>> = vec![None; tasks.len()];
    let mut visiting: Vec<bool> = vec![false; tasks.len()];
    for i in 0..tasks.len() {
        compute_rank(i, tasks, &id_to_idx, &mut ranks, &mut visiting);
    }
    let ranks: Vec<usize> = ranks.into_iter().map(|r| r.unwrap_or(0)).collect();

    let is_ready: Vec<bool> = tasks
        .iter()
        .map(|t| {
            if t.status != "open" {
                return false;
            }
            t.dependencies.iter().all(|dep_id| {
                id_to_idx
                    .get(dep_id.as_str())
                    .map(|&idx| is_closed(&tasks[idx].status))
                    .unwrap_or(true)
            })
        })
        .collect();

    let rank_count = ranks.iter().max().copied().unwrap_or(0) + 1;
    let mut by_rank: Vec<Vec<usize>> = vec![Vec::new(); rank_count];
    for (i, &r) in ranks.iter().enumerate() {
        by_rank[r].push(i);
    }
    for rank_items in by_rank.iter_mut() {
        rank_items.sort_by(|&a, &b| {
            let ta = &tasks[a];
            let tb = &tasks[b];
            priority_of(ta)
                .cmp(&priority_of(tb))
                .then_with(|| status_sort_key(ta).cmp(&status_sort_key(tb)))
                .then_with(|| ta.id.cmp(&tb.id))
        });
    }

    let mut cards: Vec<Card> = Vec::with_capacity(tasks.len());
    let mut card_idx_for_task: Vec<usize> = vec![usize::MAX; tasks.len()];
    for (rank, rank_items) in by_rank.iter().enumerate() {
        let mut rank_y: i32 = 0;
        for (row, &task_idx) in rank_items.iter().enumerate() {
            card_idx_for_task[task_idx] = cards.len();
            let t = &tasks[task_idx];
            let status = card_status(t, is_ready[task_idx]);
            let pri = priority_of(t);
            let height = card_height(&t.id, &t.title, status);
            cards.push(Card {
                id: t.id.clone(),
                title: t.title.clone(),
                priority: pri,
                status,
                rank,
                row,
                y: rank_y,
                height,
            });
            rank_y += height as i32 + ROW_GAP;
        }
    }

    let mut edges: Vec<Edge> = Vec::new();
    for (i, task) in tasks.iter().enumerate() {
        let dependent_card = card_idx_for_task[i];
        for dep_id in &task.dependencies {
            if let Some(&blocker_task_idx) = id_to_idx.get(dep_id.as_str()) {
                let blocker_card = card_idx_for_task[blocker_task_idx];
                edges.push(Edge {
                    from: blocker_card,
                    to: dependent_card,
                });
            }
        }
    }

    let rows_per_rank: Vec<usize> = by_rank.iter().map(|v| v.len()).collect();
    GraphLayout {
        cards,
        edges,
        rank_count,
        rows_per_rank,
    }
}

fn compute_rank(
    i: usize,
    tasks: &[Task],
    id_to_idx: &HashMap<&str, usize>,
    ranks: &mut [Option<usize>],
    visiting: &mut [bool],
) -> usize {
    if let Some(r) = ranks[i] {
        return r;
    }
    if visiting[i] {
        return 0; // cycle break
    }
    visiting[i] = true;
    let rank = if tasks[i].dependencies.is_empty() {
        0
    } else {
        let mut max_dep_rank = 0;
        for dep_id in &tasks[i].dependencies {
            if let Some(&dep_i) = id_to_idx.get(dep_id.as_str()) {
                let dr = compute_rank(dep_i, tasks, id_to_idx, ranks, visiting);
                if dr >= max_dep_rank {
                    max_dep_rank = dr;
                }
            }
        }
        max_dep_rank + 1
    };
    visiting[i] = false;
    ranks[i] = Some(rank);
    rank
}

fn priority_of(task: &Task) -> u8 {
    match &task.priority {
        serde_json::Value::Number(n) => n.as_u64().unwrap_or(2) as u8,
        serde_json::Value::String(s) => s.trim_start_matches('P').parse().unwrap_or(2),
        _ => 2,
    }
}

fn is_closed(status: &str) -> bool {
    matches!(status, "closed" | "done")
}

fn status_sort_key(task: &Task) -> u8 {
    match task.status.as_str() {
        "in_progress" => 0,
        "open" => 1,
        "blocked" => 2,
        "deferred" => 3,
        _ => 4,
    }
}

/// Combined card content as it will be rendered: `{glyph} ({id}) [{STATUS}] {title}`.
/// Used for sizing and for the rendered text.
pub fn card_text(id: &str, title: &str, status: CardStatus) -> String {
    format!(
        "{} ({}) [{}] {}",
        status.glyph(),
        short_id(id),
        status.label().to_uppercase(),
        title,
    )
}

/// Content width inside a card after the internal left/right padding columns.
pub const CARD_CONTENT_W: i32 = CARD_W - 4;

/// Compute a card's height in cells so the wrapped content fits with a 1-line
/// top/bottom padding. Horizontal padding is 2 cols on each side.
pub fn card_height(id: &str, title: &str, status: CardStatus) -> u16 {
    let text = card_text(id, title, status);
    let content_w = CARD_CONTENT_W.max(1) as usize;
    let lines = wrap_count(&text, content_w).max(1);
    (lines + 2).max(3) as u16
}

/// Word-wrap a single-line text at given cell width. Returns each wrapped
/// line. The render pass uses the same function so rendered heights match
/// what `card_height` pre-computed.
pub fn wrap_text_cells(text: &str, width: usize) -> Vec<String> {
    if width == 0 {
        return vec![text.to_string()];
    }
    let mut out = Vec::new();
    let mut remaining = text;
    while !remaining.is_empty() {
        if remaining.chars().count() <= width {
            out.push(remaining.to_string());
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
        out.push(chunk.to_string());
        remaining = rest.trim_start();
    }
    if out.is_empty() {
        out.push(String::new());
    }
    out
}

fn wrap_count(text: &str, width: usize) -> usize {
    wrap_text_cells(text, width).len()
}

fn card_status(task: &Task, is_ready: bool) -> CardStatus {
    match task.status.as_str() {
        "closed" | "done" => CardStatus::Done,
        "in_progress" => CardStatus::Active,
        "blocked" => CardStatus::Blocked,
        "open" => {
            if is_ready {
                CardStatus::Ready
            } else {
                CardStatus::Open
            }
        }
        _ => CardStatus::Open,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_task(id: &str, status: &str, deps: &[&str]) -> Task {
        Task {
            id: id.to_string(),
            title: format!("task {}", id),
            status: status.to_string(),
            priority: serde_json::Value::Number(2u64.into()),
            task_type: "task".into(),
            description: String::new(),
            labels: Vec::new(),
            created_at: String::new(),
            updated_at: String::new(),
            dependency_count: deps.len() as u32,
            dependent_count: 0,
            dependencies: deps.iter().map(|s| s.to_string()).collect(),
        }
    }

    #[test]
    fn empty_input() {
        let layout = compute_layout(&[]);
        assert_eq!(layout.cards.len(), 0);
        assert_eq!(layout.rank_count, 0);
    }

    #[test]
    fn single_task_rank_zero() {
        let tasks = vec![make_task("a", "open", &[])];
        let layout = compute_layout(&tasks);
        assert_eq!(layout.cards.len(), 1);
        assert_eq!(layout.cards[0].rank, 0);
        assert_eq!(layout.rank_count, 1);
    }

    #[test]
    fn chain_assigns_monotonic_ranks() {
        // a → b → c
        let tasks = vec![
            make_task("a", "open", &[]),
            make_task("b", "open", &["a"]),
            make_task("c", "open", &["b"]),
        ];
        let layout = compute_layout(&tasks);
        let card = |id: &str| layout.cards.iter().find(|c| c.id == id).unwrap();
        assert_eq!(card("a").rank, 0);
        assert_eq!(card("b").rank, 1);
        assert_eq!(card("c").rank, 2);
    }

    #[test]
    fn longest_path_rank() {
        // d depends on a (rank 0) and c (rank 2 via a→b→c). d should be rank 3.
        let tasks = vec![
            make_task("a", "open", &[]),
            make_task("b", "open", &["a"]),
            make_task("c", "open", &["b"]),
            make_task("d", "open", &["a", "c"]),
        ];
        let layout = compute_layout(&tasks);
        let card = |id: &str| layout.cards.iter().find(|c| c.id == id).unwrap();
        assert_eq!(card("d").rank, 3);
    }

    #[test]
    fn cycle_does_not_panic() {
        // a ↔ b cycle (pathological but defensive)
        let tasks = vec![
            make_task("a", "open", &["b"]),
            make_task("b", "open", &["a"]),
        ];
        let layout = compute_layout(&tasks);
        assert_eq!(layout.cards.len(), 2);
    }

    #[test]
    fn ready_vs_open_based_on_blockers() {
        // blocker a closed → b is ready. blocker c open → d is open.
        let tasks = vec![
            make_task("a", "closed", &[]),
            make_task("b", "open", &["a"]),
            make_task("c", "open", &[]),
            make_task("d", "open", &["c"]),
        ];
        let layout = compute_layout(&tasks);
        let card = |id: &str| layout.cards.iter().find(|c| c.id == id).unwrap();
        assert_eq!(card("b").status, CardStatus::Ready);
        assert_eq!(card("d").status, CardStatus::Open);
        assert_eq!(card("a").status, CardStatus::Done);
    }

    #[test]
    fn edges_point_blocker_to_dependent() {
        let tasks = vec![
            make_task("a", "closed", &[]),
            make_task("b", "open", &["a"]),
        ];
        let layout = compute_layout(&tasks);
        assert_eq!(layout.edges.len(), 1);
        let a_idx = layout.cards.iter().position(|c| c.id == "a").unwrap();
        let b_idx = layout.cards.iter().position(|c| c.id == "b").unwrap();
        assert_eq!(layout.edges[0].from, a_idx);
        assert_eq!(layout.edges[0].to, b_idx);
    }
}
