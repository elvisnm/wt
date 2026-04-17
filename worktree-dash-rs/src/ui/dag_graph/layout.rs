//! DAG layout engine — pure, testable.
//!
//! Assigns each task a (rank, row) in the graph based on longest-path
//! topological depth + within-rank ordering by priority, status, and id.

use crate::beads::Task;
use std::collections::HashMap;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CardStatus {
    Done,
    Active,
    Ready,
    Blocked,
    Open,
}

#[derive(Debug, Clone)]
pub struct Card {
    pub id: String,
    pub title: String,
    pub priority: u8,
    pub status: CardStatus,
    pub rank: usize,
    pub row: usize,
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
        for (row, &task_idx) in rank_items.iter().enumerate() {
            card_idx_for_task[task_idx] = cards.len();
            let t = &tasks[task_idx];
            cards.push(Card {
                id: t.id.clone(),
                title: t.title.clone(),
                priority: priority_of(t),
                status: card_status(t, is_ready[task_idx]),
                rank,
                row,
            });
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
