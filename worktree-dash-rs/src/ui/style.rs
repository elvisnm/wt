use ratatui::prelude::Color;

// Lazygit-inspired palette (matching Go dashboard)
pub const BORDER_COLOR: Color = Color::Indexed(240);
pub const FOCUS_BORDER_COLOR: Color = Color::Indexed(34);
pub const DIM_TEXT_COLOR: Color = Color::Indexed(250);
pub const HIGHLIGHT_COLOR: Color = Color::Indexed(34);
pub const SELECTED_BG_COLOR: Color = Color::Indexed(25);
pub const RUNNING_COLOR: Color = Color::Indexed(34);
pub const STOPPED_COLOR: Color = Color::Indexed(160);
pub const STARTING_COLOR: Color = Color::Indexed(214);
pub const HEADER_COLOR: Color = Color::Indexed(240);
pub const HINT_COLOR: Color = Color::Indexed(214);

pub const SPIN_FRAMES: &[&str] = &["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
