#[cfg(test)]
mod tests {
    use crate::ui::layout::{Layout, ResizeOpts};
    use crate::ui::visible_window;

    fn default_opts() -> ResizeOpts {
        ResizeOpts {
            notify_height: 2,
            details_visible: false,
            usage_visible: false,
            tasks_visible: false,
            tasks_content: 0,
            services_visible: true,
        }
    }

    #[test]
    fn layout_total_equals_height() {
        let layout = Layout::default();
        for h in [24, 40, 60, 80] {
            let l = layout.resize(80, h, &default_opts());
            let total = l.notify_height + l.tabs_height + l.worktree_height
                + l.services_height + l.details_height + l.usage_height + l.tasks_height;
            assert_eq!(total, h, "total {} != height {} at h={}", total, h, h);
        }
    }

    #[test]
    fn layout_with_all_panels() {
        let layout = Layout::default();
        let opts = ResizeOpts {
            notify_height: 2,
            details_visible: true,
            usage_visible: true,
            tasks_visible: true,
            tasks_content: 5,
            services_visible: true,
        };
        let l = layout.resize(80, 60, &opts);
        let total = l.notify_height + l.tabs_height + l.worktree_height
            + l.services_height + l.details_height + l.usage_height + l.tasks_height;
        assert!(total <= 60, "total {} > height 60", total);
        assert!(l.usage_height > 0);
        assert!(l.tasks_height > 0);
        assert!(l.details_height > 0);
    }

    #[test]
    fn layout_small_terminal() {
        let layout = Layout::default();
        let l = layout.resize(40, 12, &default_opts());
        assert!(l.tabs_height >= 4);
        assert!(l.worktree_height >= 2);
        assert!(l.services_height >= 2);
    }

    #[test]
    fn visible_window_all_fit() {
        let (start, end) = visible_window(5, 2, 10);
        assert_eq!(start, 0);
        assert_eq!(end, 5);
    }

    #[test]
    fn visible_window_scroll_down() {
        let (start, end) = visible_window(20, 15, 10);
        assert!(start > 0);
        assert_eq!(end - start, 10);
        assert!(start <= 15);
        assert!(end > 15);
    }

    #[test]
    fn visible_window_at_start() {
        let (start, end) = visible_window(20, 0, 10);
        assert_eq!(start, 0);
        assert_eq!(end, 10);
    }

    #[test]
    fn visible_window_at_end() {
        let (start, end) = visible_window(20, 19, 10);
        assert_eq!(end, 20);
        assert_eq!(start, 10);
    }

    #[test]
    fn visible_window_empty() {
        let (start, end) = visible_window(0, 0, 10);
        assert_eq!(start, 0);
        assert_eq!(end, 0);
    }
}
