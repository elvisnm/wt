//! Agent state detection via terminal screen content pattern matching.
//! Reads the visible text from a PTY session's alacritty_terminal grid
//! and matches against known agent output patterns.

/// The detected state of a terminal session.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AgentState {
    /// Prompt visible, nothing happening.
    Idle,
    /// Actively working/processing.
    Working,
    /// Needs human input (confirmation, selection).
    Blocked,
    /// Plain shell or unrecognized — no detection.
    Unknown,
}

/// Detect agent state from visible screen content.
/// Returns Unknown if no agent patterns are recognized.
pub fn detect_state(screen_content: &str) -> AgentState {
    let lower = screen_content.to_lowercase();

    // --- Claude Code detection ---
    if is_claude_session(&lower) {
        return detect_claude(screen_content, &lower);
    }

    AgentState::Unknown
}

/// Check if this looks like a Claude Code session.
fn is_claude_session(lower: &str) -> bool {
    lower.contains("claude")
        || lower.contains("esc to interrupt")
        || lower.contains("ctrl+c to interrupt")
        || lower.contains("⌕ search")
}

/// Claude Code state detection.
fn detect_claude(content: &str, lower: &str) -> AgentState {
    // Search prompt is idle
    if content.contains("⌕ Search") {
        return AgentState::Idle;
    }

    // --- Blocked: needs user input ---

    // Confirmation prompts
    if lower.contains("do you want")
        || lower.contains("would you like")
        || lower.contains("esc to cancel")
        || lower.contains("[y/n]")
    {
        return AgentState::Blocked;
    }

    // Selection prompt (numbered options)
    if has_selection_prompt(content) {
        return AgentState::Blocked;
    }

    // --- Working: agent is processing ---

    if lower.contains("esc to interrupt")
        || lower.contains("ctrl+c to interrupt")
    {
        return AgentState::Working;
    }

    // Spinner activity in output
    if has_spinner(content) {
        return AgentState::Working;
    }

    AgentState::Idle
}

/// Check for numbered selection prompts (❯ followed by options).
fn has_selection_prompt(content: &str) -> bool {
    let lines: Vec<&str> = content.lines().collect();
    for (i, line) in lines.iter().enumerate() {
        if line.contains('❯') && i + 1 < lines.len() {
            let next = lines[i + 1].trim();
            if next.starts_with('1') || next.starts_with('○') || next.starts_with('●') {
                return true;
            }
        }
    }
    false
}

/// Check for braille spinner characters in the last few lines.
fn has_spinner(content: &str) -> bool {
    let spinners = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];
    let last_lines: Vec<&str> = content.lines().rev().take(5).collect();
    for line in last_lines {
        for ch in line.chars() {
            if spinners.contains(&ch) {
                return true;
            }
        }
    }
    false
}

/// Extract visible text from an alacritty_terminal grid.
pub fn read_screen(
    term: &std::sync::Arc<std::sync::Mutex<alacritty_terminal::Term<crate::pty::PtyEventListener>>>,
) -> String {
    use alacritty_terminal::grid::Dimensions as _;

    let term = term.lock().expect("term lock");
    let grid = term.grid();
    let cols = grid.columns();
    let rows = grid.screen_lines();
    let mut lines = Vec::new();

    for row_idx in 0..rows {
        let row = &grid[alacritty_terminal::index::Line(row_idx as i32)];
        let mut line = String::with_capacity(cols);
        for col in 0..cols {
            let cell = &row[alacritty_terminal::index::Column(col)];
            line.push(cell.c);
        }
        lines.push(line.trim_end().to_string());
    }

    lines.join("\n")
}
