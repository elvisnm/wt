#[cfg(test)]
mod tests {
    use crate::settings::*;

    #[test]
    fn defaults() {
        let s = Settings::default();
        assert_eq!(s.left_pane_pct, DEFAULT_LEFT_PANE_PCT);
        assert_eq!(s.max_panes_per_group, DEFAULT_MAX_PANES_PER_GROUP);
        assert!(!s.claude_auto_mode);
        assert!(!s.default_panels.details);
        assert!(!s.default_panels.usage);
        assert!(!s.default_panels.tasks);
    }

    #[test]
    fn clamp_left_pane_too_low() {
        let mut s = Settings::default();
        s.left_pane_pct = 5;
        s.clamp();
        assert_eq!(s.left_pane_pct, DEFAULT_LEFT_PANE_PCT);
    }

    #[test]
    fn clamp_left_pane_too_high() {
        let mut s = Settings::default();
        s.left_pane_pct = 99;
        s.clamp();
        assert_eq!(s.left_pane_pct, DEFAULT_LEFT_PANE_PCT);
    }

    #[test]
    fn clamp_valid_values_unchanged() {
        let mut s = Settings::default();
        s.left_pane_pct = 25;
        s.max_panes_per_group = 3;
        s.clamp();
        assert_eq!(s.left_pane_pct, 25);
        assert_eq!(s.max_panes_per_group, 3);
    }

    #[test]
    fn clamp_max_panes_too_low() {
        let mut s = Settings::default();
        s.max_panes_per_group = 1;
        s.clamp();
        assert_eq!(s.max_panes_per_group, DEFAULT_MAX_PANES_PER_GROUP);
    }

    #[test]
    fn clamp_max_panes_too_high() {
        let mut s = Settings::default();
        s.max_panes_per_group = 100;
        s.clamp();
        assert_eq!(s.max_panes_per_group, DEFAULT_MAX_PANES_PER_GROUP);
    }
}
