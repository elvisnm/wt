//! DAG graph tab — dependency visualization for beads tasks.
//!
//! - `layout`  pure layout engine (ranks, ordering, edges)
//! - `render`  Ratatui renderer (bordered cards, elbowed arrows, loading/error states)
//!
//! The tab's per-instance state lives in `DagGraphState` and is owned by a
//! `TabKind::Widget::DagGraph` variant. The background fetch channel lives on
//! `App` (populated in a later commit) so the state stays `Clone + Debug`.

pub mod layout;
pub mod render;

pub use layout::GraphLayout;
pub use render::render;

use ratatui::layout::Rect;

/// Per-tab state for a DAG graph widget tab.
#[derive(Debug, Clone, Default)]
pub struct DagGraphState {
    /// Final laid-out graph. `None` until the first fetch completes.
    pub layout: Option<GraphLayout>,
    /// True while a fetch is in flight. Drives the loading spinner.
    pub loading: bool,
    /// (done, total) pairs for dep fetch progress.
    pub load_progress: Option<(usize, usize)>,
    /// Fatal error message from a failed fetch; clears on next successful load.
    pub load_error: Option<String>,
    /// Id of the task currently highlighted (double-border). Driven by the
    /// tasks list panel's cursor.
    pub selected_id: Option<String>,
    /// Pan offset into graph-space in cells (`(x, y)`). `(0, 0)` shows rank 0.
    pub viewport: (i32, i32),
    /// Rects populated each render so a future mouse handler can hit-test cards.
    pub card_rects: Vec<(String, Rect)>,
}

impl DagGraphState {
    pub fn new_loading() -> Self {
        Self {
            loading: true,
            ..Self::default()
        }
    }
}
