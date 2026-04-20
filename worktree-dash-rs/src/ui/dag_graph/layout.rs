//! DAG layout engine — pure, testable.
//!
//! Each priority level gets its own vertical column in the graph. Within a
//! column, connected components stack vertically and each component lays
//! out its cards by longest-path rank + within-rank order (priority, list
//! position, id). Cards store absolute graph-space `(x, y)` so the renderer
//! never has to re-derive a position from the rank index.

use crate::beads::Task;
use std::collections::{BTreeMap, HashMap};

/// Card width in cells (fixed). Content area = `CARD_W - 2`.
pub const CARD_W: i32 = 22;
/// Horizontal gap between ranks.
pub const RANK_GAP: i32 = 14;
/// Vertical gap between cards within a rank.
pub const ROW_GAP: i32 = 5;
/// Empty rows between two components stacked inside the same priority column.
pub const COMPONENT_GAP: i32 = 4;
/// Rows reserved above the first card of each priority column for the title:
///   row 0: empty (breathing room)
///   row 1: "Tasks group - Priority N ─────" header
///   row 2: empty (separates header from the first card)
pub const COMPONENT_TOP_PAD: i32 = 3;
/// Empty row under a component's last card before the next component starts.
pub const COMPONENT_BOTTOM_PAD: i32 = 1;
/// Empty cols between two adjacent priority columns.
pub const COLUMN_GAP: i32 = 6;

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
    /// Graph-space x coordinate (left edge), absolute across priority columns.
    pub x: i32,
    /// Graph-space y coordinate (top edge).
    pub y: i32,
    /// Card height in cells, sized to fit the wrapped content.
    pub height: u16,
    /// Original order in the beads list. Used as the secondary sort key
    /// within a rank so the user's list order controls vertical placement
    /// for same-priority tasks.
    pub list_order: usize,
}

#[derive(Debug, Clone)]
pub struct Edge {
    pub from: usize, // card index of blocker (earlier rank)
    pub to: usize,   // card index of dependent (later rank)
}

/// One vertical column in the graph, dedicated to a single priority level.
/// The header row (`Tasks group - Priority N ───`) is painted inside the
/// column's width starting at `x`.
#[derive(Debug, Clone)]
pub struct PriorityColumn {
    pub x: i32,
    pub y: i32,
    pub width: i32,
    pub priority: u8,
}

#[derive(Debug, Clone, Default)]
pub struct GraphLayout {
    pub cards: Vec<Card>,
    pub edges: Vec<Edge>,
    pub rank_count: usize,
    pub rows_per_rank: Vec<usize>,
    pub priority_columns: Vec<PriorityColumn>,
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

    // Three-way classification of a task's direct blockers. Drives the
    // DAG status so that a task waiting on normal open work reads
    // differently from one stuck behind a blocked dep. Applies to any bd
    // status, not just "open" — if a task is manually marked blocked but
    // its blockers have since been closed, we surface it as ready
    // because the dependency graph says it can proceed.
    let blocker_states: Vec<BlockerState> =
        tasks.iter().map(|t| compute_blocker_state(t, tasks, &id_to_idx)).collect();

    // Connected components via dependency graph (undirected).
    let components = compute_components(tasks, &id_to_idx);

    // Group components by the highest-priority (lowest number) task they
    // contain. Each group shares one vertical column in the graph.
    let mut by_priority: BTreeMap<u8, Vec<Vec<usize>>> = BTreeMap::new();
    for comp in components {
        let pri = comp
            .iter()
            .map(|&i| priority_of(&tasks[i]))
            .min()
            .unwrap_or(u8::MAX);
        by_priority.entry(pri).or_default().push(comp);
    }
    // Stable component order inside each priority column.
    for comps in by_priority.values_mut() {
        comps.sort_by_key(|comp| comp.iter().min().copied().unwrap_or(usize::MAX));
    }

    let mut cards: Vec<Card> = Vec::with_capacity(tasks.len());
    let mut card_idx_for_task: Vec<usize> = vec![usize::MAX; tasks.len()];
    let mut priority_columns: Vec<PriorityColumn> = Vec::new();
    let mut x_offset: i32 = 0;
    let mut max_rank_count: usize = 0;

    for (priority, comps) in by_priority.iter() {
        // Column width = widest component in this priority (in ranks).
        let max_ranks_in_col = comps
            .iter()
            .map(|comp| comp.iter().map(|&i| ranks[i]).max().unwrap_or(0) + 1)
            .max()
            .unwrap_or(1);
        let col_width = max_ranks_in_col as i32 * (CARD_W + RANK_GAP) - RANK_GAP;
        max_rank_count = max_rank_count.max(max_ranks_in_col);

        priority_columns.push(PriorityColumn {
            x: x_offset,
            y: 0,
            width: col_width,
            priority: *priority,
        });

        // Components in this column stack downwards starting just below the
        // column header. Each component uses rank 0 at `x_offset`.
        let mut y_cursor: i32 = COMPONENT_TOP_PAD;
        for comp in comps {
            let comp_rank_count = comp.iter().map(|&i| ranks[i]).max().unwrap_or(0) + 1;

            let mut by_rank: Vec<Vec<usize>> = vec![Vec::new(); comp_rank_count];
            for &i in comp {
                by_rank[ranks[i]].push(i);
            }
            for rank_items in by_rank.iter_mut() {
                rank_items.sort_by(|&a, &b| {
                    priority_of(&tasks[a])
                        .cmp(&priority_of(&tasks[b]))
                        .then_with(|| a.cmp(&b))
                        .then_with(|| tasks[a].id.cmp(&tasks[b].id))
                });
            }

            let mut rank_y: Vec<i32> = vec![y_cursor; comp_rank_count];
            let mut comp_bottom_y: i32 = y_cursor;

            for (rank, rank_items) in by_rank.iter().enumerate() {
                for (row, &task_idx) in rank_items.iter().enumerate() {
                    let t = &tasks[task_idx];
                    let status = card_status(t, blocker_states[task_idx]);
                    let pri = priority_of(t);
                    let height = card_height(&t.id, &t.title, status);

                    let card_x = x_offset + rank as i32 * (CARD_W + RANK_GAP);

                    card_idx_for_task[task_idx] = cards.len();
                    cards.push(Card {
                        id: t.id.clone(),
                        title: t.title.clone(),
                        priority: pri,
                        status,
                        rank,
                        row,
                        x: card_x,
                        y: rank_y[rank],
                        height,
                        list_order: task_idx,
                    });

                    comp_bottom_y = comp_bottom_y.max(rank_y[rank] + height as i32);
                    rank_y[rank] += height as i32 + ROW_GAP;
                }
            }

            // Next component in this column starts below the bottom pad +
            // inter-component gap.
            y_cursor = comp_bottom_y + COMPONENT_BOTTOM_PAD + COMPONENT_GAP;
        }

        x_offset += col_width + COLUMN_GAP;
    }

    let mut edges: Vec<Edge> = Vec::new();
    for (i, task) in tasks.iter().enumerate() {
        let dependent_card = card_idx_for_task[i];
        if dependent_card == usize::MAX {
            continue;
        }
        for dep_id in &task.dependencies {
            if let Some(&blocker_task_idx) = id_to_idx.get(dep_id.as_str()) {
                let blocker_card = card_idx_for_task[blocker_task_idx];
                if blocker_card != usize::MAX {
                    edges.push(Edge {
                        from: blocker_card,
                        to: dependent_card,
                    });
                }
            }
        }
    }

    GraphLayout {
        cards,
        edges,
        rank_count: max_rank_count,
        rows_per_rank: vec![0; max_rank_count],
        priority_columns,
    }
}

/// Union-find over the dependency graph (treated undirected). Two tasks
/// belong to the same component when there is a chain of dependencies
/// connecting them in either direction.
fn compute_components(tasks: &[Task], id_to_idx: &HashMap<&str, usize>) -> Vec<Vec<usize>> {
    let n = tasks.len();
    let mut parent: Vec<usize> = (0..n).collect();

    fn find(parent: &mut [usize], x: usize) -> usize {
        let mut root = x;
        while parent[root] != root {
            root = parent[root];
        }
        // Path compression.
        let mut cur = x;
        while parent[cur] != root {
            let next = parent[cur];
            parent[cur] = root;
            cur = next;
        }
        root
    }

    for (i, task) in tasks.iter().enumerate() {
        for dep_id in &task.dependencies {
            if let Some(&dep_idx) = id_to_idx.get(dep_id.as_str()) {
                let ri = find(&mut parent, i);
                let rd = find(&mut parent, dep_idx);
                if ri != rd {
                    parent[ri] = rd;
                }
            }
        }
    }

    let mut components: HashMap<usize, Vec<usize>> = HashMap::new();
    for i in 0..n {
        let root = find(&mut parent, i);
        components.entry(root).or_default().push(i);
    }
    components.into_values().collect()
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

/// Content width inside a card after the internal left/right padding columns.
pub const CARD_CONTENT_W: i32 = CARD_W - 4;

/// Compute a card's height in cells. Layout is:
///   row 0:   empty (top padding)
///   row 1:   header (`{glyph} {id}·{STATUS}`)
///   row 2..: wrapped title
///   last:    empty (bottom padding)
/// So height = 1 top pad + 1 header + N title lines + 1 bottom pad.
pub fn card_height(_id: &str, title: &str, _status: CardStatus) -> u16 {
    let content_w = CARD_CONTENT_W.max(1) as usize;
    let title_lines = wrap_count(title, content_w).max(1);
    (title_lines + 3) as u16
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

/// Three-way summary of a task's direct blockers. Distinguishes "waiting
/// on normal open work" (`SomeOpen`) from "stuck behind an explicitly
/// blocked dep" (`SomeBlocked`), so the DAG can surface the two cases in
/// different colors.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum BlockerState {
    AllClear,
    SomeOpen,
    SomeBlocked,
}

fn compute_blocker_state(
    task: &Task,
    tasks: &[Task],
    id_to_idx: &HashMap<&str, usize>,
) -> BlockerState {
    let mut any_blocked = false;
    let mut any_open = false;
    for dep_id in &task.dependencies {
        let Some(&idx) = id_to_idx.get(dep_id.as_str()) else {
            continue;
        };
        let status = tasks[idx].status.as_str();
        if is_closed(status) {
            continue;
        }
        if status == "blocked" {
            any_blocked = true;
        } else {
            any_open = true;
        }
    }
    if any_blocked {
        BlockerState::SomeBlocked
    } else if any_open {
        BlockerState::SomeOpen
    } else {
        BlockerState::AllClear
    }
}

fn card_status(task: &Task, blockers: BlockerState) -> CardStatus {
    match task.status.as_str() {
        "closed" | "done" => CardStatus::Done,
        "in_progress" => CardStatus::Active,
        _ => match blockers {
            BlockerState::AllClear => CardStatus::Ready,
            BlockerState::SomeOpen => CardStatus::Open,
            BlockerState::SomeBlocked => CardStatus::Blocked,
        },
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
    fn blocked_when_any_dep_is_blocked() {
        // e has two blockers: x (open) and y (blocked). The "blocked"
        // dep should dominate — e reads as Blocked, not Open.
        let tasks = vec![
            make_task("x", "open", &[]),
            make_task("y", "blocked", &[]),
            make_task("e", "open", &["x", "y"]),
        ];
        let layout = compute_layout(&tasks);
        let card = |id: &str| layout.cards.iter().find(|c| c.id == id).unwrap();
        assert_eq!(card("e").status, CardStatus::Blocked);
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
