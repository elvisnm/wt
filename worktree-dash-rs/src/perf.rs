//! Lightweight RAII timer for perf instrumentation.
//!
//! A `Timed` guard records the elapsed time between construction and drop,
//! emitting a `tracing::debug!` event under the `perf` target only when the
//! scope exceeds its per-callsite threshold. The threshold keeps the log
//! from drowning in sub-millisecond entries while still surfacing the slow
//! frames or hotspots worth looking at.
//!
//! Logs only surface when the app was launched with `--debug` (that's what
//! installs a tracing subscriber). In non-debug runs the cost is a single
//! `Instant::now()` at construction plus the elapsed check at drop.
//!
//! Usage:
//!
//! ```ignore
//! let _t = perf::timed("render", 15_000); // log if > 15ms
//! app.render(frame);
//! ```
//!
//! Inspect with `grep perf /tmp/wt-dev.log`.

use std::time::Instant;

pub struct Timed {
    label: &'static str,
    start: Instant,
    threshold_us: u128,
}

pub fn timed(label: &'static str, threshold_us: u128) -> Timed {
    Timed {
        label,
        start: Instant::now(),
        threshold_us,
    }
}

impl Drop for Timed {
    fn drop(&mut self) {
        let elapsed_us = self.start.elapsed().as_micros();
        if elapsed_us >= self.threshold_us {
            tracing::debug!(target: "perf", "{} {}us", self.label, elapsed_us);
        }
    }
}
