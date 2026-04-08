#[cfg(test)]
#[path = "layout_test.rs"]
mod tests;

/// Computed dimensions for the left-column panels.
#[derive(Debug, Clone, Default)]
pub struct Layout {
    pub width: u16,
    pub height: u16,

    pub notify_height: u16,
    pub tabs_height: u16,
    pub worktree_height: u16,
    pub services_height: u16,
    pub details_height: u16,
    pub usage_height: u16,
    pub tasks_height: u16,
}

/// Panel visibility and content sizing hints.
pub struct ResizeOpts {
    pub notify_height: u16,
    pub details_visible: bool,
    pub usage_visible: bool,
    pub tasks_visible: bool,
    pub tasks_content: u16,
    pub services_visible: bool,
}

impl Layout {
    pub fn resize(&self, w: u16, h: u16, opts: &ResizeOpts) -> Layout {
        let mut l = Layout {
            width: w,
            height: h,
            notify_height: opts.notify_height,
            ..Layout::default()
        };

        let mut panels_h = h.saturating_sub(l.notify_height);
        if panels_h < 8 {
            panels_h = 8;
        }

        // Reserve space for fixed-height bottom panels first
        if opts.usage_visible {
            l.usage_height = 5.min(panels_h / 4);
            panels_h -= l.usage_height;
        }

        if opts.tasks_visible {
            let content = opts.tasks_content.max(1);
            let mut tasks_h = content + 2;
            let max_tasks = (panels_h * 40 / 100).max(4);
            tasks_h = tasks_h.clamp(4, max_tasks);
            l.tasks_height = tasks_h;
            panels_h -= l.tasks_height;
        }

        // Core panels: tabs + worktrees always visible, services + details optional
        // Distribute equally among visible core panels
        let mut core_count = 2u16; // tabs + worktrees always
        if opts.services_visible { core_count += 1; }
        if opts.details_visible { core_count += 1; }

        let per_panel = panels_h / core_count;

        l.tabs_height = per_panel.max(4);
        l.worktree_height = per_panel.max(4);

        if opts.services_visible {
            l.services_height = per_panel.max(4);
        } else {
            l.services_height = 0;
        }

        if opts.details_visible {
            // Details gets the remainder to avoid rounding gaps
            l.details_height = panels_h - l.tabs_height - l.worktree_height - l.services_height;
        } else {
            l.details_height = 0;
            // Redistribute remainder to worktrees
            l.worktree_height = panels_h - l.tabs_height - l.services_height;
        }

        // Safety clamp: guarantee total never exceeds terminal height
        let total = l.notify_height
            + l.tabs_height
            + l.worktree_height
            + l.services_height
            + l.details_height
            + l.usage_height
            + l.tasks_height;

        if total > h {
            let overflow = total - h;
            if l.details_height > overflow + 2 {
                l.details_height -= overflow;
            } else if l.services_height > overflow + 2 {
                l.services_height -= overflow;
            } else if l.tasks_height > overflow + 2 {
                l.tasks_height -= overflow;
            } else if l.worktree_height > overflow + 2 {
                l.worktree_height -= overflow;
            }
        }

        l
    }
}
