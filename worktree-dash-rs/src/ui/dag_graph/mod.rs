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
use std::cell::{Cell, RefCell};
use std::collections::HashMap;

/// Per-tab state for a DAG graph widget tab.
#[derive(Debug, Clone, Default)]
pub struct DagGraphState {
    /// Final laid-out graph. `None` until the first fetch completes.
    pub layout: Option<GraphLayout>,
    /// Full task data cached alongside the layout; the tooltip looks up
    /// description, labels, and dependency titles from here.
    pub tasks: Vec<crate::beads::Task>,
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
    /// Screen rect the DAG tab last rendered into. Read by the mouse
    /// handler to decide whether a click lands on the graph at all.
    /// Written inside render(); uses `Cell` so the renderer can keep its
    /// `&DagGraphState` signature.
    pub dag_area: Cell<Option<Rect>>,
    /// Unclipped screen rect of each card from the last frame, paired
    /// with its task id. Used by the mouse handler to hit-test clicks.
    /// Stored unclipped so a card that's partially off-screen still
    /// picks up clicks in its visible portion.
    pub card_rects: RefCell<Vec<(String, Rect)>>,
    /// Click-and-drag pan state. `Some` only while the left button is
    /// held after pressing empty graph area. `(start_col, start_row,
    /// vx0, vy0)` captures the mouse anchor and the viewport at the
    /// moment the drag started, so each Drag event can recompute the
    /// viewport as an absolute offset from the anchor.
    pub pan_drag: Option<PanDrag>,
    /// Fast `id -> CardStatus` lookup so the task list panel can color each
    /// id without scanning the card list every frame. Rebuilt alongside
    /// `layout` when a fetch completes.
    pub status_by_id: HashMap<String, layout::CardStatus>,
}

/// Anchor for an in-flight click-drag pan.
#[derive(Debug, Clone, Copy)]
pub struct PanDrag {
    pub start_col: u16,
    pub start_row: u16,
    pub vx0: i32,
    pub vy0: i32,
}

impl DagGraphState {
    pub fn new_loading() -> Self {
        Self {
            loading: true,
            ..Self::default()
        }
    }
}
