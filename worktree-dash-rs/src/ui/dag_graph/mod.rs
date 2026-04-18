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
///
/// `moved` flips to `true` on the first Drag event. It lets the Up
/// handler tell a "click and drag" apart from a "plain click without
/// movement": dragging keeps the tooltip open (the user is moving the
/// canvas, not dismissing the selection), while a still click deselects.
#[derive(Debug, Clone, Copy)]
pub struct PanDrag {
    pub start_col: u16,
    pub start_row: u16,
    pub vx0: i32,
    pub vy0: i32,
    pub moved: bool,
}

impl DagGraphState {
    pub fn new_loading() -> Self {
        Self {
            loading: true,
            ..Self::default()
        }
    }

    /// Clamp `viewport` so the user can't pan into empty space beyond
    /// a small halo around the graph. Horizontal halo: 16 cells each
    /// side; vertical halo: 4 rows each side. Past those, the graph
    /// would scroll off-screen and the user would be staring at a
    /// blank area — off-limits. Requires `layout` and `dag_area` to
    /// have been populated at least once.
    pub fn clamp_viewport(&mut self) {
        const PAD_X: i32 = 16;
        const PAD_Y: i32 = 4;

        let Some(layout) = &self.layout else { return; };
        if layout.cards.is_empty() { return; }
        let Some(area) = self.dag_area.get() else { return; };

        let mut max_gx = 0_i32;
        let mut max_gy = 0_i32;
        for c in &layout.cards {
            let right = c.x + layout::CARD_W;
            if right > max_gx { max_gx = right; }
            let bottom = c.y + c.height as i32;
            if bottom > max_gy { max_gy = bottom; }
        }

        let area_w = area.width as i32;
        let area_h = area.height as i32;
        let min_vx = -PAD_X;
        let max_vx = (max_gx - area_w + PAD_X).max(min_vx);
        let min_vy = -PAD_Y;
        let max_vy = (max_gy - area_h + PAD_Y).max(min_vy);

        self.viewport.0 = self.viewport.0.clamp(min_vx, max_vx);
        self.viewport.1 = self.viewport.1.clamp(min_vy, max_vy);
    }
}
