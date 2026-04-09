use ratatui::prelude::Color;

/// Semantic color palette for the dashboard UI.
/// All rendering code should reference palette fields, not raw colors.
#[derive(Debug, Clone)]
pub struct Palette {
    /// Primary accent (highlight, active borders, focused elements).
    pub accent: Color,
    /// Background for floating panels, overlays.
    pub panel_bg: Color,
    /// Subtle surface background for selected/focused items.
    pub surface0: Color,
    /// Slightly lighter surface for hover/active states.
    pub surface1: Color,
    /// Very dim surface for separators, inactive borders.
    pub surface_dim: Color,
    /// Muted text (secondary info, descriptions).
    pub overlay0: Color,
    /// Slightly brighter overlay text.
    pub overlay1: Color,
    /// Main text color.
    pub text: Color,
    /// Subdued text (labels, hints).
    pub subtext0: Color,
    /// Done / online / success states.
    pub green: Color,
    /// Working / starting / warning states.
    pub yellow: Color,
    /// Error / stopped / attention states.
    pub red: Color,
    /// Info / notification accent.
    pub blue: Color,
    /// Secondary accent (branch names, special labels).
    pub teal: Color,
    /// Tertiary accent (ports, paths).
    pub orange: Color,
}

impl Palette {
    /// Gruvbox Dark — default theme.
    pub fn gruvbox() -> Self {
        Self {
            accent: Color::Rgb(215, 153, 33),      // gruvbox yellow
            panel_bg: Color::Rgb(40, 40, 40),       // bg0
            surface0: Color::Rgb(60, 56, 54),       // bg1
            surface1: Color::Rgb(80, 73, 69),       // bg2
            surface_dim: Color::Rgb(50, 48, 47),    // bg0_s
            overlay0: Color::Rgb(146, 131, 116),    // fg4
            overlay1: Color::Rgb(168, 153, 132),    // fg3
            text: Color::Rgb(235, 219, 178),        // fg1
            subtext0: Color::Rgb(213, 196, 161),    // fg2
            green: Color::Rgb(184, 187, 38),        // bright green
            yellow: Color::Rgb(250, 189, 47),       // bright yellow
            red: Color::Rgb(251, 73, 52),           // bright red
            blue: Color::Rgb(131, 165, 152),        // blue-gray
            teal: Color::Rgb(142, 192, 124),        // bright green (muted)
            orange: Color::Rgb(254, 128, 25),       // bright orange
        }
    }
}

impl Default for Palette {
    fn default() -> Self {
        Self::gruvbox()
    }
}

// ── Convenience aliases for backward compatibility during migration ───
// These will be removed once all UI code uses the palette directly.

pub const SPIN_FRAMES: &[&str] = &["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

// Legacy constants — mapped to gruvbox equivalents for gradual migration
pub const BORDER_COLOR: Color = Color::Rgb(50, 48, 47);        // surface_dim
pub const FOCUS_BORDER_COLOR: Color = Color::Rgb(215, 153, 33); // accent
pub const DIM_TEXT_COLOR: Color = Color::Rgb(146, 131, 116);    // overlay0
pub const HIGHLIGHT_COLOR: Color = Color::Rgb(215, 153, 33);    // accent
pub const SELECTED_BG_COLOR: Color = Color::Rgb(60, 56, 54);   // surface0
pub const RUNNING_COLOR: Color = Color::Rgb(184, 187, 38);     // green
pub const STOPPED_COLOR: Color = Color::Rgb(251, 73, 52);      // red
pub const STARTING_COLOR: Color = Color::Rgb(250, 189, 47);    // yellow
pub const HEADER_COLOR: Color = Color::Rgb(146, 131, 116);     // overlay0
pub const HINT_COLOR: Color = Color::Rgb(215, 153, 33);        // accent
pub const HEADER_BG: Color = Color::Indexed(236);               // dark bg for headers/bars
