#[cfg(test)]
#[path = "key_test.rs"]
mod key_tests;

use alacritty_terminal::grid::Dimensions as _;
use crossterm::event::{KeyCode, KeyEvent, KeyModifiers, MouseEvent, MouseEventKind};
use ratatui::prelude::*;

use crate::beads;
use crate::claude;
use crate::config;
use crate::docker;
use crate::pm2;
use crate::pty::PtyManager;
use crate::settings::Settings;
use crate::ui::{self, Layout, NotifyState, PickerAction, ResizeOpts};
use crate::ui::overlay;
use crate::worktree::{self, Service, Worktree, WorktreeType};

#[derive(Debug, PartialEq, Eq, Clone, Copy)]
pub enum Panel {
    Worktrees,
    Services,
    Details,
    Terminal,
    Tasks,
}

/// Init wizard state — multi-step config generator.
#[derive(Debug, Clone)]
pub struct InitWizard {
    pub step: usize,          // 0=name, 1=stack, 2=worktrees_dir, 3=preview
    pub name: String,
    pub stack_cursor: usize,  // index into STACKS
    pub worktrees_dir: String,
    pub detected_stack: Option<String>,
    pub project_root: String,
}

pub const STACKS: &[(&str, &str)] = &[
    ("node",            "Node.js"),
    ("nextjs",          "Next.js"),
    ("nuxt",            "Nuxt"),
    ("python",          "Python"),
    ("flask",           "Flask"),
    ("django",          "Django"),
    ("rails",           "Rails"),
    ("ruby",            "Ruby"),
    ("rust",            "Rust"),
    ("go",              "Go"),
    ("docker-compose",  "Docker Compose"),
];

impl InitWizard {
    pub fn stack_id(&self) -> &str {
        STACKS.get(self.stack_cursor).map(|(id, _)| *id).unwrap_or("node")
    }
    pub fn stack_label(&self) -> &str {
        STACKS.get(self.stack_cursor).map(|(_, label)| *label).unwrap_or("Node.js")
    }
}

/// Tab entry — holds a single session or a split group.
#[derive(Debug, Clone)]
pub struct Tab {
    pub session_id: usize,       // primary session ID
    pub label: String,
    pub alive: bool,
    pub split: Option<crate::pty::split::SplitNode>, // None = single session, Some = split layout
}

pub struct App {
    pub focus: Panel,
    pub terminal_focused: bool,
    pub settings: Settings,
    pub layout: Layout,
    pub worktrees: Vec<Worktree>,
    pub cursor: usize,

    // Services for selected worktree
    pub services: Vec<Service>,
    pub service_cursor: usize,

    // Terminal tabs and PTY sessions
    pub pty_mgr: PtyManager,
    pub tabs: Vec<Tab>,
    pub active_tab: usize,
    pub tab_cursor: usize,
    /// The session ID that receives keyboard input in terminal mode.
    /// In a split group, this is the pane the user selected with Enter.
    pub focused_session_id: Option<usize>,

    // HeiHei easter egg
    pub heihei_active: bool,

    // Notification auto-dismiss timer (tick count when notification should close)

    // Spinner frame counter (incremented every tick)
    pub spin_frame: usize,

    // Pending operations: sentinel paths — poll for completion
    pending_remove: Option<(String, String)>,
    pending_create_tab: bool,
    pending_build_tab: bool,
    pending_start_tab: bool,

    // Activity bar — shows spinner while long operations run
    pub activity: Option<String>,

    // Rename input state
    rename_session_id: Option<usize>,

    // Toast notifications (floating top-right)
    pub toasts: Vec<ui::overlay::Toast>,

    // Notification / overlay state
    pub notify_state: NotifyState,
    pending_split_dir: Option<crate::pty::split::SplitDir>,
    split_target_session_id: Option<usize>,
    split_target_alias: String,
    split_target_dir: String,
    pub help_open: bool,
    pub discovered: bool,
    pub settings_state: Option<ui::settings_tui::SettingsState>,

    // Service preview: session displayed in terminal area without a tab
    pub preview_session: Option<usize>,
    pub preview_svc_name: String,

    // Claude usage data
    pub usage_data: Option<claude::Usage>,
    pub usage_err: Option<String>,

    // Beads tasks data
    pub tasks_list: Vec<beads::Task>,
    pub tasks_err: Option<String>,
    pub tasks_cursor: usize,
    pub tasks_detail: Option<beads::Task>,
    pub tasks_detail_scroll: usize,
    pub tasks_detail_max_scroll: std::cell::Cell<usize>,
    pub tasks_search: Option<String>,
    pub tasks_search_active: bool,
    pub task_editor: Option<beads::TaskEditor>,

    // Fullscreen mode — hides left column, shows only focused session
    pub fullscreen: bool,
    // Sidebar hidden — Ctrl+B toggles left column visibility
    pub sidebar_hidden: bool,
    pub fullscreen_session_id: Option<usize>,

    // Panel visibility
    pub details_visible: bool,
    pub usage_visible: bool,
    pub tasks_visible: bool,
    pub services_visible: bool,

    // Debug mode
    pub debug: bool,
    pub should_quit: bool,

    // Init wizard (shown when no wt.config.js found)
    pub init_wizard: Option<InitWizard>,

    // Resolved paths and config
    flow_scripts_dir: String,
    repo_root: String,
    pub cfg: Option<config::Config>,
    pub palette: ui::style::Palette,
    pub agent_states: std::collections::HashMap<usize, crate::detect::AgentState>,

    // Terminal dimensions
    width: u16,
    height: u16,
    pub last_frame_width: u16,
    pub last_frame_height: u16,
}

impl App {
    pub fn new() -> Self {
        let settings = Settings::load();
        let details_visible = settings.default_panels.details;
        let usage_visible = settings.default_panels.usage;
        let tasks_visible = settings.default_panels.tasks;
        let services_visible = settings.default_panels.services;

        let cwd = std::env::current_dir().unwrap_or_default();
        let cfg = config::load(&cwd).ok();
        let repo_root = cfg.as_ref()
            .map(|c| c.repo_root.to_string_lossy().to_string())
            .unwrap_or_else(|| cwd.to_string_lossy().to_string());
        let flow_scripts_dir = resolve_flow_scripts_dir(&repo_root, cfg.as_ref());

        Self {
            focus: Panel::Worktrees,
            terminal_focused: false,
            settings,
            layout: Layout::default(),
            worktrees: Vec::new(),
            cursor: 0,
            services: Vec::new(),
            service_cursor: 0,
            pty_mgr: PtyManager::new(),
            tabs: Vec::new(),
            active_tab: 0,
            tab_cursor: 0,
            focused_session_id: None,
            heihei_active: false,
            spin_frame: 0,
            pending_remove: None,
            pending_create_tab: false,
            pending_build_tab: false,
            pending_start_tab: false,
            activity: None,
            rename_session_id: None,
            usage_data: None,
            usage_err: None,
            tasks_list: Vec::new(),
            tasks_err: None,
            tasks_cursor: 0,
            tasks_detail: None,
            tasks_detail_scroll: 0,
            tasks_detail_max_scroll: std::cell::Cell::new(0),
            tasks_search: None,
            tasks_search_active: false,
            task_editor: None,
            toasts: Vec::new(),
            notify_state: NotifyState::Idle,
            pending_split_dir: None,
            split_target_session_id: None,
            split_target_alias: String::new(),
            split_target_dir: String::new(),
            help_open: false,
            discovered: false,
            settings_state: None,
            preview_session: None,
            preview_svc_name: String::new(),
            debug: false,
            should_quit: false,
            init_wizard: None,
            flow_scripts_dir,
            repo_root,
            cfg,
            palette: ui::style::Palette::gruvbox(),
            agent_states: std::collections::HashMap::new(),
            fullscreen: false,
            fullscreen_session_id: None,
            sidebar_hidden: false,
            details_visible,
            usage_visible,
            tasks_visible,
            services_visible,
            width: 0,
            height: 0,
            last_frame_width: 0,
            last_frame_height: 0,
        }
    }

    pub fn run_discovery(&mut self) {
        let cwd = std::env::current_dir().unwrap_or_default();
        let worktrees_dir = worktree::resolve_worktrees_dir(&cwd, self.cfg.as_ref());
        let prev_cursor = if self.cursor < self.worktrees.len() {
            Some(self.worktrees[self.cursor].alias.clone())
        } else {
            None
        };
        let mut worktrees = worktree::discover(&worktrees_dir, &self.worktrees, self.cfg.as_ref());
        worktree::sort_worktrees(&mut worktrees);

        // Add project root as first entry
        let root_entry = worktree::Worktree {
            path: std::path::PathBuf::from(&self.repo_root),
            name: "root".to_string(),
            wt_type: worktree::WorktreeType::Local,
            alias: "Root".to_string(),
            container: String::new(),
            branch: String::new(),
            domain: String::new(),
            lan_domain: String::new(),
            db_name: String::new(),
            offset: 0,
            ports: std::collections::HashMap::new(),
            isolated_pm2: false,
            running: false,
            container_exists: false,
            health: String::new(),
            started: String::new(),
            uptime: String::new(),
            cpu: String::new(),
            mem: String::new(),
            mem_pct: String::new(),
        };
        worktrees.insert(0, root_entry);
        self.worktrees = worktrees;
        self.discovered = true;

        // Restore cursor position by alias
        if let Some(alias) = prev_cursor {
            if let Some(pos) = self.worktrees.iter().position(|w| w.alias == alias) {
                self.cursor = pos;
            }
        }
    }

    /// Start the init wizard (no wt.config.js found).
    pub fn start_init_wizard(&mut self) {
        let cwd = std::env::current_dir().unwrap_or_default();
        let dir_name = cwd.file_name()
            .map(|n| n.to_string_lossy().to_string())
            .unwrap_or_else(|| "myapp".into());

        let detected = crate::init::detect_stack(&cwd);
        let stack_cursor = detected.as_deref()
            .and_then(|s| STACKS.iter().position(|(id, _)| *id == s))
            .unwrap_or(0);
        let worktrees_dir = format!("../{}-worktrees", dir_name);

        self.init_wizard = Some(InitWizard {
            step: 0,
            name: dir_name,
            stack_cursor,
            worktrees_dir,
            detected_stack: detected,
            project_root: cwd.to_string_lossy().to_string(),
        });
    }

    /// Handle a keypress in the init wizard. Returns true if consumed.
    pub fn handle_wizard_key(&mut self, key: KeyEvent) -> bool {
        let wizard = match self.init_wizard.as_mut() {
            Some(w) => w,
            None => return false,
        };

        match key.code {
            KeyCode::Esc => {
                self.init_wizard = None;
                self.should_quit = true;
                return true;
            }
            KeyCode::Tab => {
                if wizard.step < 3 {
                    wizard.step += 1;
                }
                return true;
            }
            KeyCode::BackTab => {
                if wizard.step > 0 {
                    wizard.step -= 1;
                }
                return true;
            }
            KeyCode::Up => {
                if wizard.step == 1 {
                    // Stack picker: move up
                    if wizard.stack_cursor > 0 {
                        wizard.stack_cursor -= 1;
                    }
                } else if wizard.step > 0 {
                    wizard.step -= 1;
                }
                return true;
            }
            KeyCode::Down => {
                if wizard.step == 1 {
                    // Stack picker: move down
                    if wizard.stack_cursor + 1 < STACKS.len() {
                        wizard.stack_cursor += 1;
                    }
                } else if wizard.step < 3 {
                    wizard.step += 1;
                }
                return true;
            }
            KeyCode::Enter => {
                if wizard.step < 3 {
                    wizard.step += 1;
                } else {
                    self.finish_init_wizard();
                }
                return true;
            }
            KeyCode::Backspace => {
                match wizard.step {
                    0 => { wizard.name.pop(); }
                    2 => { wizard.worktrees_dir.pop(); }
                    _ => {}
                }
                return true;
            }
            KeyCode::Char(c) => {
                match wizard.step {
                    0 => wizard.name.push(c),
                    2 => wizard.worktrees_dir.push(c),
                    _ => {} // stack uses picker, preview has no input
                }
                return true;
            }
            _ => return true,
        }
    }

    fn finish_init_wizard(&mut self) {
        let wizard = match self.init_wizard.take() {
            Some(w) => w,
            None => return,
        };

        let content = crate::init::generate_config(&wizard.name, wizard.stack_id(), &wizard.worktrees_dir);
        let config_path = std::path::Path::new(&wizard.project_root).join("wt.config.js");

        if let Err(e) = std::fs::write(&config_path, &content) {
            self.push_toast("Error", &format!("Failed to write wt.config.js: {}", e), ui::overlay::ToastKind::Error);
            return;
        }

        // Reload config
        let cwd = std::env::current_dir().unwrap_or_default();
        self.cfg = config::load(&cwd).ok();
        self.repo_root = self.cfg.as_ref()
            .map(|c| c.repo_root.to_string_lossy().to_string())
            .unwrap_or_else(|| cwd.to_string_lossy().to_string());
        self.flow_scripts_dir = resolve_flow_scripts_dir(&self.repo_root, self.cfg.as_ref());

        // Run discovery with new config
        self.run_discovery();
        self.refresh_status();

        self.push_toast("Success", "Created wt.config.js", ui::overlay::ToastKind::Success);
    }

    /// Refresh running status for all worktrees.
    pub fn refresh_status(&mut self) {
        // Docker containers
        docker::fetch_container_status(&mut self.worktrees, self.cfg.as_ref());

        // Local worktrees: check daemon PID first, then PM2 fallback
        for wt in &mut self.worktrees {
            if wt.wt_type == WorktreeType::Local {
                wt.running = crate::daemon::is_running(&wt.path);
            }
        }

        // PM2 fallback for worktrees without daemon PID
        let mut pm2_paths: std::collections::HashMap<String, String> = std::collections::HashMap::new();
        for wt in &self.worktrees {
            if wt.wt_type == WorktreeType::Local && !wt.running {
                pm2_paths.insert(wt.path.to_string_lossy().to_string(), wt.alias.clone());
            }
        }
        if !pm2_paths.is_empty() {
            let running = pm2::fetch_running_worktrees(&pm2_paths);
            for wt in &mut self.worktrees {
                if wt.wt_type == WorktreeType::Local && !wt.running {
                    wt.running = running.get(&wt.alias).copied().unwrap_or(false);
                }
            }
        }

        // Refresh services for selected worktree
        self.refresh_services();
    }

    /// Refresh CPU/memory stats via docker stats.
    pub fn refresh_stats(&mut self) {
        docker::fetch_container_stats(&mut self.worktrees, self.cfg.as_ref());

        // Local worktree stats via PM2 or devTab
        let running_check = self.cfg.as_ref()
            .map(|c| c.dash.services.running_check.as_str())
            .unwrap_or("pm2");

        for wt in &mut self.worktrees {
            if wt.wt_type == WorktreeType::Local && wt.isolated_pm2 && running_check == "pm2" {
                let pm2_home = wt.pm2_home().to_string_lossy().to_string();
                let (running, cpu, mem) = pm2::status_with_home(&pm2_home);
                wt.running = running;
                wt.cpu = cpu;
                wt.mem = mem;
            }
        }
    }

    pub fn fetch_usage(&mut self) {
        match claude::fetch_usage() {
            Ok(data) => {
                self.usage_data = Some(data);
                self.usage_err = None;
            }
            Err(e) => {
                self.usage_err = Some(e);
            }
        }
    }

    pub fn fetch_tasks(&mut self) {
        match beads::fetch_tasks() {
            Ok(tasks) => {
                self.tasks_list = tasks;
                self.tasks_err = None;
            }
            Err(e) => {
                self.tasks_err = Some(e);
            }
        }
    }

    /// Return indices into `tasks_list` that match the current search filter.
    pub fn filtered_task_indices(&self) -> Vec<usize> {
        match &self.tasks_search {
            Some(q) if !q.is_empty() => {
                let lower = q.to_lowercase();
                self.tasks_list
                    .iter()
                    .enumerate()
                    .filter(|(_, t)| {
                        t.title.to_lowercase().contains(&lower)
                            || t.id.to_lowercase().contains(&lower)
                            || t.labels.iter().any(|l| l.to_lowercase().contains(&lower))
                    })
                    .map(|(i, _)| i)
                    .collect()
            }
            _ => (0..self.tasks_list.len()).collect(),
        }
    }

    /// Refresh services for the currently selected worktree.
    fn refresh_services(&mut self) {
        if self.cursor >= self.worktrees.len() {
            self.services.clear();
            return;
        }

        let wt = &self.worktrees[self.cursor];
        let manager = self.cfg.as_ref()
            .map(|c| c.service_manager())
            .unwrap_or("pm2");

        self.services = if wt.alias == "Root" {
            Vec::new()
        } else if manager == "static" {
            // Static services from config — check actual port listening
            self.cfg.as_ref()
                .map(|c| {
                    c.dash.services.list.iter().map(|entry| {
                        let port = entry.port + wt.offset;
                        let status = if !wt.running {
                            "stopped".to_string()
                        } else if port > 0 && crate::daemon::is_port_listening(port) {
                            "online".to_string()
                        } else {
                            "starting".to_string()
                        };
                        Service {
                            name: entry.name.clone(),
                            display_name: if port > 0 {
                                format!("{} :{}", entry.name, port)
                            } else {
                                entry.name.clone()
                            },
                            status,
                            memory: 0,
                            cpu: 0.0,
                            restart_count: 0,
                        }
                    }).collect()
                })
                .unwrap_or_default()
        } else if wt.wt_type == WorktreeType::Docker && !wt.container.is_empty() {
            docker::fetch_services(&wt.container, &wt.name)
        } else if wt.wt_type == WorktreeType::Local && wt.isolated_pm2 {
            let pm2_home = wt.pm2_home().to_string_lossy().to_string();
            pm2::fetch_services_with_home(&pm2_home)
        } else if wt.wt_type == WorktreeType::Local {
            let path = wt.path.to_string_lossy().to_string();
            pm2::fetch_services(&path)
        } else {
            Vec::new()
        };
        self.service_cursor = 0;
    }

    /// Resize all PTY sessions in split groups to match their actual area.
    /// Uses the same layout calculation as ratatui's render to get exact dimensions.
    pub fn resize_split_ptys(&mut self) {
        if self.tabs.is_empty() || self.active_tab >= self.tabs.len() {
            return;
        }
        let tab = &self.tabs[self.active_tab];
        if let Some(ref split) = tab.split {
            // Compute right panel area exactly like ratatui Layout does
            use ratatui::prelude::*;
            let frame_area = ratatui::layout::Rect::new(0, 0, self.last_frame_width, self.last_frame_height);
            let main_layout = ratatui::layout::Layout::default()
                .direction(Direction::Horizontal)
                .constraints([
                    Constraint::Percentage(self.settings.left_pane_pct),
                    Constraint::Percentage(100 - self.settings.left_pane_pct),
                ])
                .split(frame_area);
            let right_area = main_layout[1];
            resize_node_ptys(split, right_area.width, right_area.height, &mut self.pty_mgr);
        }
    }

    pub fn render(&self, frame: &mut Frame) {
        if !self.discovered {
            ui::splash::render_splash(frame, frame.area(), "Loading worktrees...");
            return;
        }
        ui::render(frame, self);
    }

    pub fn handle_mouse(&mut self, mouse: MouseEvent) {
        match mouse.kind {
            MouseEventKind::ScrollUp => {
                if self.terminal_focused {
                    if let Some(sid) = self.focused_session_id {
                        if let Some(session) = self.pty_mgr.get(sid) {
                            let mut term = session.term().lock().expect("term lock");
                            let offset = term.grid().display_offset();
                            let total = term.grid().total_lines().saturating_sub(term.grid().screen_lines());
                            if offset < total {
                                term.scroll_display(alacritty_terminal::grid::Scroll::Delta(3));
                            }
                        }
                    }
                }
            }
            MouseEventKind::ScrollDown => {
                if self.terminal_focused {
                    if let Some(sid) = self.focused_session_id {
                        if let Some(session) = self.pty_mgr.get(sid) {
                            let mut term = session.term().lock().expect("term lock");
                            if term.grid().display_offset() > 0 {
                                term.scroll_display(alacritty_terminal::grid::Scroll::Delta(-3));
                            }
                        }
                    }
                }
            }
            MouseEventKind::Down(crossterm::event::MouseButton::Left) => {
                if self.terminal_focused {
                    if let Some(sid) = self.focused_session_id {
                        if let Some(session) = self.pty_mgr.get(sid) {
                            let mut term = session.term().lock().expect("term lock");
                            let point = mouse_to_point(mouse.column, mouse.row, self);
                            let sel = alacritty_terminal::selection::Selection::new(
                                alacritty_terminal::selection::SelectionType::Simple,
                                point,
                                alacritty_terminal::index::Side::Left,
                            );
                            term.selection = Some(sel);
                        }
                    }
                }
            }
            MouseEventKind::Drag(crossterm::event::MouseButton::Left) => {
                if self.terminal_focused {
                    if let Some(sid) = self.focused_session_id {
                        if let Some(session) = self.pty_mgr.get(sid) {
                            let mut term = session.term().lock().expect("term lock");
                            let point = mouse_to_point(mouse.column, mouse.row, self);
                            if let Some(ref mut sel) = term.selection {
                                sel.update(point, alacritty_terminal::index::Side::Right);
                            }
                        }
                    }
                }
            }
            MouseEventKind::Up(crossterm::event::MouseButton::Left) => {
                if self.terminal_focused {
                    if let Some(sid) = self.focused_session_id {
                        if let Some(session) = self.pty_mgr.get(sid) {
                            let mut term = session.term().lock().expect("term lock");
                            if let Some(ref sel) = term.selection {
                                if let Some(range) = sel.to_range(&*term) {
                                    // Extract selected text
                                    let mut text = String::new();
                                    let grid = term.grid();
                                    for line in (range.start.line.0)..=(range.end.line.0) {
                                        let start_col = if line == range.start.line.0 { range.start.column.0 } else { 0 };
                                        let end_col = if line == range.end.line.0 { range.end.column.0 } else { grid.columns().saturating_sub(1) };
                                        for col in start_col..=end_col {
                                            let cell = &grid[alacritty_terminal::index::Line(line)][alacritty_terminal::index::Column(col)];
                                            if cell.c != '\0' {
                                                text.push(cell.c);
                                            }
                                        }
                                        if line != range.end.line.0 {
                                            text.push('\n');
                                        }
                                    }
                                    let text = text.trim_end().to_string();
                                    if !text.is_empty() {
                                        // Copy to clipboard via pbcopy (macOS)
                                        let _ = std::process::Command::new("pbcopy")
                                            .stdin(std::process::Stdio::piped())
                                            .spawn()
                                            .and_then(|mut child| {
                                                use std::io::Write;
                                                if let Some(ref mut stdin) = child.stdin {
                                                    let _ = stdin.write_all(text.as_bytes());
                                                }
                                                child.wait()
                                            });
                                    }
                                }
                            }
                            // Clear selection after copy
                            term.selection = None;
                        }
                    }
                }
            }
            _ => {}
        }
    }

    pub fn handle_resize(&mut self, width: u16, height: u16) {
        self.width = width;
        self.height = height;
        self.recalc_layout();

        // Resize all PTY sessions — split-aware
        self.resize_split_ptys();

        // Also resize standalone (non-split) tab PTYs
        let (cols, rows) = self.terminal_area_size();
        for tab in &self.tabs {
            if tab.split.is_none() {
                if let Some(session) = self.pty_mgr.get_mut(tab.session_id) {
                    let _ = session.resize(cols.saturating_sub(2), rows.saturating_sub(2));
                }
            }
        }
    }

    pub fn handle_key(&mut self, key: KeyEvent) {
        // ── Init wizard: intercept all input ──────────────────────────
        if self.init_wizard.is_some() {
            self.handle_wizard_key(key);
            return;
        }

        // ── Toasts: Ctrl+A confirms, Esc dismisses ──
        if !self.toasts.is_empty() {
            if key.code == KeyCode::Char('a') && key.modifiers.contains(KeyModifiers::CONTROL) {
                if let Some(i) = self.find_priority_toast(true) {
                    let toast = self.toasts.remove(i);
                    self.handle_toast_action(&toast);
                    return;
                }
            }
            if key.code == KeyCode::Esc {
                let i = self.find_priority_toast(false).unwrap_or(0);
                self.toasts.remove(i);
                return;
            }
        }

        // ── Global: Ctrl+Q to quit (shows confirm toast) ──────
        if key.code == KeyCode::Char('q') && key.modifiers.contains(KeyModifiers::CONTROL) {
            // Don't push duplicate confirm toasts
            let already_confirming = self.toasts.iter().any(|t| {
                matches!(&t.action, Some(ui::overlay::ToastAction::Confirm))
            });
            if !already_confirming {
                self.push_confirm_toast("Exit", "Quit wt dashboard?");
                self.terminal_focused = false;
            }
            return;
        }

        // ── Terminal focused: route input to active PTY ──────────────
        if self.terminal_focused {
            // Ctrl+] to return to dashboard
            // crossterm may report this as Char(']') with CONTROL, or as Char('\x1d')
            if self.debug {
                tracing::debug!("terminal key: code={:?} modifiers={:?}", key.code, key.modifiers);
            }

            // Ctrl+] — crossterm reports this as Char('5') with CONTROL on some terminals
            let is_detach = matches!(
                (key.code, key.modifiers),
                (KeyCode::Char(']') | KeyCode::Char('5'), m) if m.contains(KeyModifiers::CONTROL)
            ) || key.code == KeyCode::Char('\x1d');

            if is_detach {
                self.terminal_focused = false;
                if self.fullscreen {
                    self.fullscreen = false;
                    self.fullscreen_session_id = None;
                }
                return;
            }

            // Ctrl+B to toggle sidebar
            if key.code == KeyCode::Char('b') && key.modifiers.contains(KeyModifiers::CONTROL) {
                self.sidebar_hidden = !self.sidebar_hidden;
                // Showing sidebar
                if !self.sidebar_hidden {
                    self.terminal_focused = false;
                    if self.tabs.is_empty() {
                        self.focus = Panel::Worktrees;
                        self.cursor = 0;
                    } else {
                        self.focus = Panel::Terminal;
                    }
                }
                return;
            }

            // Ctrl+F to toggle fullscreen for the focused session
            if key.code == KeyCode::Char('f') && key.modifiers.contains(KeyModifiers::CONTROL) {
                self.fullscreen = !self.fullscreen;
                if self.fullscreen {
                    self.fullscreen_session_id = self.focused_session_id;
                } else {
                    self.fullscreen_session_id = None;
                }
                return;
            }

            // Ctrl+k to switch to next session
            if key.code == KeyCode::Char('k') && key.modifiers.contains(KeyModifiers::CONTROL) {
                self.cycle_focused_session(1);
                return;
            }
            // Ctrl+j to switch to previous session
            if key.code == KeyCode::Char('j') && key.modifiers.contains(KeyModifiers::CONTROL) {
                self.cycle_focused_session(-1);
                return;
            }

            // Shift+Up/Down or PgUp/PgDn to scroll terminal history
            // Shift+Up/PgUp = scroll up, Shift+Down/PgDn = scroll down
            let is_scroll_up = (key.code == KeyCode::Up && key.modifiers.contains(KeyModifiers::SHIFT))
                || key.code == KeyCode::PageUp;
            let is_scroll_down = (key.code == KeyCode::Down && key.modifiers.contains(KeyModifiers::SHIFT))
                || key.code == KeyCode::PageDown;

            if is_scroll_up || is_scroll_down {
                if let Some(sid) = self.focused_session_id {
                    if let Some(session) = self.pty_mgr.get(sid) {
                        let mut term = session.term().lock().expect("term lock");
                        let delta = if is_scroll_up { 3 } else { -3 };
                        term.scroll_display(alacritty_terminal::grid::Scroll::Delta(delta));
                    }
                }
                return;
            }

            // Ctrl+x to close the focused session from terminal mode
            if key.code == KeyCode::Char('x') && key.modifiers.contains(KeyModifiers::CONTROL) {
                self.close_focused_session();
                return;
            }

            // Forward key to the focused session (specific pane in split)
            let target_sid = self.focused_session_id
                .or_else(|| self.tabs.get(self.active_tab).map(|t| t.session_id));
            if let Some(sid) = target_sid {
                if let Some(session) = self.pty_mgr.get_mut(sid) {
                    if let Some(bytes) = key_to_bytes(&key) {
                        let _ = session.write_input(&bytes);
                    }
                }
            }
            return;
        }

        // (notification Esc handled above, before terminal focused)


        // ── Help page (shown in right panel) ─────────────────────
        if self.help_open {
            match key.code {
                KeyCode::Esc | KeyCode::Char('q') | KeyCode::Char('?') => {
                    self.help_open = false;
                }
                _ => {}
            }
            return;
        }

        // ── Settings TUI ──────────────────────────────────────────
        if let Some(ref mut state) = self.settings_state {
            use ui::settings_tui::SettingsField;
            match key.code {
                KeyCode::Up | KeyCode::Char('k') => state.navigate(-1),
                KeyCode::Down | KeyCode::Char('j') => state.navigate(1),
                KeyCode::Char(' ') | KeyCode::Enter => {
                    match state.current_field() {
                        SettingsField::Save => {
                            let new_settings = state.settings.clone();
                            let _ = new_settings.save();
                            self.details_visible = new_settings.default_panels.details;
                            self.usage_visible = new_settings.default_panels.usage;
                            self.tasks_visible = new_settings.default_panels.tasks;
                            self.services_visible = new_settings.default_panels.services;
                            self.settings = new_settings;
                            self.settings_state = None;
                            self.recalc_layout();
                        }
                        SettingsField::Cancel => {
                            self.settings_state = None;
                        }
                        _ => state.toggle(),
                    }
                }
                KeyCode::Left => state.adjust(-1),
                KeyCode::Right => state.adjust(1),
                KeyCode::Esc | KeyCode::Char('q') => {
                    self.settings_state = None;
                }
                _ => {}
            }
            return;
        }

        // ── Task editor (shown in right panel) ───────────────────
        if let Some(ref mut editor) = self.task_editor {
            match key.code {
                // Save
                KeyCode::Char('s') if key.modifiers.contains(KeyModifiers::CONTROL) => {
                    match editor.save() {
                        Ok(()) => {
                            self.task_editor = None;
                            self.fetch_tasks();
                            // Refresh detail if open
                            if let Some(ref detail) = self.tasks_detail {
                                let id = detail.id.clone();
                                if let Ok(updated) = beads::fetch_detail(&id) {
                                    self.tasks_detail = Some(updated);
                                }
                            }
                        }
                        Err(e) => {
                            self.tasks_err = Some(e);
                            self.task_editor = None;
                        }
                    }
                }
                // Cancel
                KeyCode::Char('x') if key.modifiers.contains(KeyModifiers::CONTROL) => {
                    self.task_editor = None;
                }
                KeyCode::Esc => {
                    self.task_editor = None;
                }
                // Navigate fields / lines
                KeyCode::Up if !key.modifiers.contains(KeyModifiers::CONTROL) => {
                    if !editor.move_cursor_line_up() {
                        editor.navigate(-1);
                    }
                }
                KeyCode::Down if !key.modifiers.contains(KeyModifiers::CONTROL) => {
                    if !editor.move_cursor_line_down() {
                        editor.navigate(1);
                    }
                }
                KeyCode::Tab => editor.navigate(1),
                KeyCode::BackTab => editor.navigate(-1),

                // Emacs cursor movement
                KeyCode::Char('a') if key.modifiers.contains(KeyModifiers::CONTROL) => editor.move_cursor_home(),
                KeyCode::Char('e') if key.modifiers.contains(KeyModifiers::CONTROL) => editor.move_cursor_end(),
                KeyCode::Char('f') if key.modifiers.contains(KeyModifiers::CONTROL) => editor.move_cursor_right(),
                KeyCode::Char('b') if key.modifiers.contains(KeyModifiers::CONTROL) => editor.move_cursor_left(),
                KeyCode::Left if key.modifiers.contains(KeyModifiers::CONTROL) => editor.move_cursor_word_left(),
                KeyCode::Right if key.modifiers.contains(KeyModifiers::CONTROL) => editor.move_cursor_word_right(),
                KeyCode::Left if key.modifiers.contains(KeyModifiers::ALT) => editor.move_cursor_word_left(),
                KeyCode::Right if key.modifiers.contains(KeyModifiers::ALT) => editor.move_cursor_word_right(),
                KeyCode::Left => editor.move_cursor_left(),
                KeyCode::Right => editor.move_cursor_right(),
                KeyCode::Home => editor.move_cursor_home(),
                KeyCode::End => editor.move_cursor_end(),

                // Emacs editing
                KeyCode::Char('k') if key.modifiers.contains(KeyModifiers::CONTROL) => editor.kill_to_end(),
                KeyCode::Char('u') if key.modifiers.contains(KeyModifiers::CONTROL) => editor.kill_to_start(),
                KeyCode::Char('w') if key.modifiers.contains(KeyModifiers::CONTROL) => editor.delete_word_backward(),
                KeyCode::Char('d') if key.modifiers.contains(KeyModifiers::CONTROL) => editor.delete_char_forward(),
                KeyCode::Delete => editor.delete_char_forward(),
                KeyCode::Backspace => editor.delete_char(),

                // Enter: newline in description, next field otherwise
                KeyCode::Enter => {
                    if editor.current_field() == beads::EditorField::Description {
                        editor.insert_char('\n');
                    } else {
                        editor.navigate(1);
                    }
                }

                // Regular typing
                KeyCode::Char(c) => editor.insert_char(c),
                _ => {}
            }
            return;
        }

        // ── Input mode (rename) ──────────────────────────────────
        if let NotifyState::Input { ref mut value, .. } = self.notify_state {
            match key.code {
                KeyCode::Enter => {
                    let new_name = value.trim().to_string();
                    if !new_name.is_empty() {
                        if let Some(sid) = self.rename_session_id.take() {
                            if let Some(session) = self.pty_mgr.get_mut(sid) {
                                session.label = new_name;
                            }
                            // Update tab label too
                            for tab in &mut self.tabs {
                                if tab.session_id == sid {
                                    tab.label = self.pty_mgr.get(sid).map(|s| s.label.clone()).unwrap_or_default();
                                }
                            }
                        }
                    }
                    self.notify_state = NotifyState::Idle;
                    self.rename_session_id = None;
                    self.recalc_layout();
                }
                KeyCode::Esc => {
                    self.notify_state = NotifyState::Idle;
                    self.rename_session_id = None;
                    self.recalc_layout();
                }
                KeyCode::Backspace => { value.pop(); }
                KeyCode::Char(c) => { value.push(c); }
                _ => {}
            }
            return;
        }

        // ── Picker open: handle picker navigation ─────────────────
        if let NotifyState::Picker { ref actions, ref mut cursor, .. } = self.notify_state {
            match key.code {
                KeyCode::Up | KeyCode::Char('k') => {
                    if *cursor > 0 {
                        *cursor -= 1;
                    }
                }
                KeyCode::Down | KeyCode::Char('j') => {
                    if *cursor + 1 < actions.len() {
                        *cursor += 1;
                    }
                }
                KeyCode::Enter => {
                    if let Some(action) = actions.get(*cursor).cloned() {
                        self.notify_state = NotifyState::Idle;
                        self.execute_picker_action(&action);
                        self.recalc_layout();
                    }
                }
                KeyCode::Esc => {
                    self.notify_state = NotifyState::Idle;
                    self.recalc_layout();
                }
                // Direct key selection
                KeyCode::Char(c) => {
                    let c_str = c.to_string();
                    if let Some(action) = actions.iter().find(|a| a.key == c_str).cloned() {
                        self.notify_state = NotifyState::Idle;
                        self.execute_picker_action(&action);
                        self.recalc_layout();
                    }
                }
                _ => {}
            }
            return;
        }

        // ── Tasks search mode (capturing keystrokes) ────────────────
        if self.tasks_search_active {
            let query = self.tasks_search.get_or_insert_with(String::new);
            match key.code {
                KeyCode::Esc => {
                    self.tasks_search = None;
                    self.tasks_search_active = false;
                    self.tasks_cursor = 0;
                }
                KeyCode::Enter => {
                    // Confirm: keep filter, stop capturing
                    self.tasks_search_active = false;
                    if self.tasks_search.as_ref().map_or(true, |q| q.is_empty()) {
                        self.tasks_search = None;
                    }
                }
                KeyCode::Backspace => {
                    query.pop();
                    self.tasks_cursor = 0;
                }
                KeyCode::Char(c) => {
                    query.push(c);
                    self.tasks_cursor = 0;
                }
                _ => {}
            }
            return;
        }

        // ── Dashboard focused: handle navigation ────────────────────
        match key.code {
            KeyCode::Tab | KeyCode::Right => self.cycle_panel(1),
            KeyCode::BackTab | KeyCode::Left => self.cycle_panel(-1),
            KeyCode::Char('w') => self.focus = Panel::Worktrees,
            KeyCode::Char('s') if self.services_visible => self.focus = Panel::Services,
            KeyCode::Char('a') => self.focus = Panel::Terminal,

            // Panel toggles (Shift+key)
            KeyCode::Char('D') => {
                self.details_visible = !self.details_visible;
                self.recalc_layout();
            }
            KeyCode::Char('U') => {
                self.usage_visible = !self.usage_visible;
                if self.usage_visible && self.usage_data.is_none() {
                    self.fetch_usage();
                }
                self.recalc_layout();
            }
            KeyCode::Char('T') => {
                self.tasks_visible = !self.tasks_visible;
                if self.tasks_visible {
                    self.tasks_cursor = 0;
                    self.tasks_detail = None;
                    self.tasks_detail_scroll = 0;
                    self.tasks_err = None;
                    self.tasks_search = None;
                    self.tasks_search_active = false;
                    self.focus = Panel::Tasks;
                    self.fetch_tasks();
                } else if self.focus == Panel::Tasks {
                    self.focus = Panel::Worktrees;
                }
                self.recalc_layout();
            }

            // Tasks panel — Ctrl+S to search
            KeyCode::Char('s') if self.focus == Panel::Tasks && key.modifiers.contains(KeyModifiers::CONTROL) => {
                self.tasks_search_active = true;
                self.tasks_search = Some(String::new());
                self.tasks_cursor = 0;
                self.tasks_detail = None;
            }
            // Tasks panel — Ctrl+E to edit task
            KeyCode::Char('e') if self.focus == Panel::Tasks && key.modifiers.contains(KeyModifiers::CONTROL) => {
                // Edit from detail view
                if let Some(ref task) = self.tasks_detail {
                    self.task_editor = Some(beads::TaskEditor::from_task(task));
                } else {
                    // Edit from list view — use selected task
                    let indices = self.filtered_task_indices();
                    if let Some(&real_idx) = indices.get(self.tasks_cursor) {
                        let id = self.tasks_list[real_idx].id.clone();
                        if let Ok(task) = beads::fetch_detail(&id) {
                            self.task_editor = Some(beads::TaskEditor::from_task(&task));
                        }
                    }
                }
            }
            // Tasks panel — Ctrl+N to create new task
            KeyCode::Char('n') if self.focus == Panel::Tasks && key.modifiers.contains(KeyModifiers::CONTROL) && self.tasks_detail.is_none() => {
                self.task_editor = Some(beads::TaskEditor::new_task());
            }
            // Tasks panel — Esc clears search filter (when not in detail view)
            KeyCode::Esc if self.focus == Panel::Tasks && self.tasks_search.is_some() && self.tasks_detail.is_none() => {
                self.tasks_search = None;
                self.tasks_cursor = 0;
            }

            // Tasks panel — Enter/Esc/c/d
            KeyCode::Enter if self.focus == Panel::Tasks => {
                if self.tasks_detail.is_some() {
                    // Already in detail — do nothing
                } else {
                    let indices = self.filtered_task_indices();
                    if let Some(&real_idx) = indices.get(self.tasks_cursor) {
                        let id = self.tasks_list[real_idx].id.clone();
                        match beads::fetch_detail(&id) {
                            Ok(detail) => {
                                self.tasks_detail = Some(detail);
                                self.tasks_detail_scroll = 0;
                                self.recalc_layout();
                            }
                            Err(e) => { self.tasks_err = Some(e); }
                        }
                    }
                }
            }
            KeyCode::Esc if self.focus == Panel::Tasks && self.tasks_detail.is_some() => {
                self.tasks_detail = None;
                self.tasks_detail_scroll = 0;
                self.recalc_layout();
            }
            KeyCode::Char('c') if self.focus == Panel::Tasks && key.modifiers.contains(KeyModifiers::CONTROL) => {
                let indices = self.filtered_task_indices();
                if let Some(&real_idx) = indices.get(self.tasks_cursor) {
                    let id = self.tasks_list[real_idx].id.clone();
                    if beads::close_task(&id).is_ok() {
                        self.fetch_tasks();
                        // Clamp cursor after list change
                        let new_len = self.filtered_task_indices().len();
                        if self.tasks_cursor >= new_len && new_len > 0 {
                            self.tasks_cursor = new_len - 1;
                        }
                    }
                }
            }
            KeyCode::Char('d') if self.focus == Panel::Tasks && key.modifiers.contains(KeyModifiers::CONTROL) => {
                let indices = self.filtered_task_indices();
                if let Some(&real_idx) = indices.get(self.tasks_cursor) {
                    let id = self.tasks_list[real_idx].id.clone();
                    let _ = beads::delete_task(&id);
                    self.fetch_tasks();
                    let new_len = self.filtered_task_indices().len();
                    if self.tasks_cursor >= new_len && new_len > 0 {
                        self.tasks_cursor = new_len - 1;
                    }
                }
            }

            // Navigation within panels
            KeyCode::Up | KeyCode::Char('k') => self.navigate(-1),
            KeyCode::Down | KeyCode::Char('j') => self.navigate(1),

            // Enter terminal focus from Terminal panel
            KeyCode::Enter if self.focus == Panel::Terminal => {
                if !self.tabs.is_empty() {
                    // Focus the session under the cursor
                    self.focused_session_id = self.session_id_at_cursor();
                    self.terminal_focused = true;
                }
            }

            // Service panel: Enter = preview, l = logs tab, r = restart
            KeyCode::Enter if self.focus == Panel::Services => {
                self.service_open_preview();
            }
            KeyCode::Char('l') if self.focus == Panel::Services => {
                self.open_service_logs_tab();
            }
            KeyCode::Char('r') if self.focus == Panel::Services => {
                self.restart_service();
            }

            // Open action picker on worktree
            KeyCode::Enter if self.focus == Panel::Worktrees => {
                self.open_worktree_picker();
            }

            // Direct actions from Worktrees panel
            KeyCode::Char('b') if self.focus == Panel::Worktrees && !key.modifiers.contains(KeyModifiers::CONTROL) => {
                self.open_shell_for_selected();
            }
            KeyCode::Char('c') if self.focus == Panel::Worktrees && !key.modifiers.contains(KeyModifiers::CONTROL) => {
                self.open_claude_for_selected();
            }
            KeyCode::Char('l') if self.focus == Panel::Worktrees => {
                self.open_logs_for_selected();
            }
            KeyCode::Char('n') if self.focus == Panel::Worktrees => {
                self.open_create_wizard();
            }
            KeyCode::Char('i') if self.focus == Panel::Worktrees => {
                self.open_worktree_info();
            }

            // Maintenance picker
            KeyCode::Char('M') => {
                self.open_picker("Maintenance", overlay::maintenance_actions());
            }

            // Skip-worktree toggle
            KeyCode::Char('K') => {
                if self.cursor < self.worktrees.len() {
                    let alias = self.worktrees[self.cursor].alias.clone();
                    let label = format!("Skip-WT ({})", &alias);
                    let arg = alias.clone();
                    let script = self.flow_script("wt-skip.js");
                    let root = self.repo_root.clone();
                    self.open_tab(label, "node", &[&script, &arg], &root, alias, String::new());
                }
            }

            // Settings
            KeyCode::Char('S') => {
                self.settings_state = Some(ui::settings_tui::SettingsState::new(
                    self.settings.clone(),
                ));
            }

            // Open help overlay (full keybindings)
            KeyCode::Char('?') => {
                self.help_open = !self.help_open;
            }

            // Ctrl+B toggle sidebar
            KeyCode::Char('b') if key.modifiers.contains(KeyModifiers::CONTROL) => {
                self.sidebar_hidden = !self.sidebar_hidden;
                if self.sidebar_hidden && !self.tabs.is_empty() {
                    // Hiding sidebar — enter terminal mode on last active session
                    self.focus = Panel::Terminal;
                    self.terminal_focused = true;
                    if self.focused_session_id.is_none() {
                        self.focused_session_id = Some(self.tabs[self.active_tab].session_id);
                    }
                } else if !self.sidebar_hidden {
                    // Showing sidebar
                    self.terminal_focused = false;
                    if self.tabs.is_empty() {
                        self.focus = Panel::Worktrees;
                        self.cursor = 0; // Root
                    } else {
                        self.focus = Panel::Terminal;
                    }
                }
            }

            // LAN mode toggle
            KeyCode::Char('L') => {
                if self.cursor < self.worktrees.len() {
                    let alias = self.worktrees[self.cursor].alias.clone();
                    let label = format!("LAN ({})", &alias);
                    let script = self.flow_script("wt-lan.js");
                    let root = self.repo_root.clone();
                    self.open_tab(label, "node", &[&script, &alias], &root, &alias, "");
                }
            }

            // HeiHei easter egg — show art + play audio
            KeyCode::Char('H') => {
                self.heihei_active = !self.heihei_active;
                if self.heihei_active {
                    crate::heihei::play();
                }
            }

            // Tab switching with number keys
            KeyCode::Char(c @ '1'..='9') if !self.tabs.is_empty() => {
                let target = (c as usize) - ('1' as usize);
                self.jump_to_flat_index(target);
                // Enter terminal mode directly
                if self.focused_session_id.is_some() {
                    self.focus = Panel::Terminal;
                    self.terminal_focused = true;
                }
            }

            // Tab navigation in Terminal panel
            KeyCode::Char('h') if self.focus == Panel::Terminal => {
                if self.active_tab > 0 {
                    self.active_tab -= 1;
                    // Sync cursor to first entry of new active group
                    self.tab_cursor = self.flat_index_for_tab(self.active_tab);
                }
            }
            KeyCode::Char('l') if self.focus == Panel::Terminal => {
                if self.active_tab + 1 < self.tabs.len() {
                    self.active_tab += 1;
                    self.tab_cursor = self.flat_index_for_tab(self.active_tab);
                }
            }

            // Close tab
            // Fullscreen toggle — fullscreen the session under cursor
            KeyCode::Char('f') if self.focus == Panel::Terminal && !self.tabs.is_empty() => {
                self.fullscreen = !self.fullscreen;
                if self.fullscreen {
                    self.focused_session_id = self.session_id_at_cursor();
                    self.fullscreen_session_id = self.focused_session_id;
                    self.terminal_focused = true;
                } else {
                    self.fullscreen_session_id = None;
                }
            }

            // Rename session
            KeyCode::Char('r') if self.focus == Panel::Terminal => {
                if let Some(sid) = self.session_id_at_cursor() {
                    self.rename_session_id = Some(sid);
                    self.notify_state = NotifyState::Input {
                        prompt: "Rename:".to_string(),
                        value: String::new(),
                    };
                    self.recalc_layout();
                }
            }

            // Close session
            KeyCode::Char('x') if self.focus == Panel::Terminal => {
                self.close_session_under_cursor();
            }

            // Split panes (Shift+\ for H-split, Shift+- for V-split)
            KeyCode::Char('|') if self.focus == Panel::Terminal && !self.tabs.is_empty() => {
                self.open_split_picker(crate::pty::split::SplitDir::Horizontal);
            }
            KeyCode::Char('_') if self.focus == Panel::Terminal && !self.tabs.is_empty() => {
                self.open_split_picker(crate::pty::split::SplitDir::Vertical);
            }

            _ => {}
        }
    }

    // ── Tab management ───────────────────────────────────────────────

    fn open_shell_for_selected(&mut self) {
        if self.cursor >= self.worktrees.len() {
            return;
        }
        let alias = self.worktrees[self.cursor].alias.clone();
        let path = self.worktrees[self.cursor].path.to_string_lossy().to_string();
        let is_docker = self.worktrees[self.cursor].wt_type == crate::worktree::WorktreeType::Docker;
        let container = self.worktrees[self.cursor].container.clone();

        if is_docker && !container.is_empty() {
            self.open_tab(
                format!("Shell ({})", alias),
                "docker",
                &["exec", "-it", &container, "bash"],
                "",
                alias,
                path,
            );
        } else {
            let shell = default_shell();
            self.open_tab(
                format!("Shell ({})", alias),
                &shell,
                &[],
                &path,
                alias,
                path.clone(),
            );
        }
    }

    fn open_claude_for_selected(&mut self) {
        if self.cursor >= self.worktrees.len() {
            return;
        }
        let alias = self.worktrees[self.cursor].alias.clone();
        let path = self.worktrees[self.cursor].path.to_string_lossy().to_string();
        let auto_mode = self.settings.claude_auto_mode;

        // Resolve claude binary: config → local install → PATH
        let claude_bin = {
            let from_config = self.cfg.as_ref()
                .and_then(|c| c.dash.commands.get("claude"))
                .map(|cmd| cmd.cmd.clone())
                .filter(|s| !s.is_empty());

            let home = std::env::var("HOME").unwrap_or_default();
            let local_path = format!("{}/.claude/local/claude", home);

            match from_config {
                Some(cmd) if std::path::Path::new(&cmd).exists() => cmd,
                Some(cmd) if cmd == "claude" && std::path::Path::new(&local_path).exists() => local_path,
                Some(cmd) => cmd,
                None if std::path::Path::new(&local_path).exists() => local_path,
                None => "claude".to_string(),
            }
        };

        let mut args: Vec<String> = Vec::new();
        if auto_mode {
            args.push("--enable-auto-mode".to_string());
        }
        let args_ref: Vec<&str> = args.iter().map(|s| s.as_ref()).collect();
        self.open_tab(
            format!("Claude ({})", alias),
            &claude_bin,
            &args_ref,
            &path,
            alias,
            path.clone(),
        );
    }

    fn open_logs_for_selected(&mut self) {
        if self.cursor >= self.worktrees.len() {
            return;
        }
        let wt = &self.worktrees[self.cursor];
        let alias = wt.alias.clone();
        let wt_path = wt.path.to_string_lossy().to_string();

        let is_docker = wt.wt_type == WorktreeType::Docker;
        let container = wt.container.clone();
        let wt_path_buf = wt.path.clone();

        // Resolve the log file path
        let log_file = if is_docker && !container.is_empty() {
            let label = format!("Logs ({})", alias);
            if let Some(tab_idx) = self.tabs.iter().position(|t| t.label == label) {
                self.active_tab = tab_idx;
                self.tab_cursor = self.flat_index_for_tab(self.active_tab);
                self.focus = Panel::Terminal;
                return;
            }
            self.open_tab(label, "docker",
                &["logs", "-f", "--tail", "100", &container],
                "", alias, wt_path);
            return;
        } else {
            // Local: check config logFile (with variable expansion), then daemon log
            self.cfg.as_ref()
                .filter(|c| !c.dash.log_file.is_empty())
                .map(|c| c.expand_cmd(&c.dash.log_file, wt))
                .or_else(|| {
                    let dl = crate::daemon::log_path(&wt_path_buf);
                    if dl.exists() { Some(dl.to_string_lossy().to_string()) } else { None }
                })
        };

        let log_file = match log_file {
            Some(f) => f,
            None => return, // no log source
        };

        let label = format!("Logs ({})", alias);

        // Check if already open — focus it
        if let Some(tab_idx) = self.tabs.iter().position(|t| t.label == label) {
            self.active_tab = tab_idx;
            self.tab_cursor = self.flat_index_for_tab(self.active_tab);
            self.focus = Panel::Terminal;
            return;
        }

        let tail_cmd = format!("touch '{}' && tail -f '{}'", log_file, log_file);
        let shell = default_shell();
        self.open_tab(label, &shell, &["-c", &tail_cmd], &wt_path, alias, wt_path.clone());
    }

    fn open_tab(
        &mut self,
        label: String,
        cmd: &str,
        args: &[&str],
        cwd: &str,
        worktree_alias: impl Into<String>,
        worktree_dir: impl Into<String>,
    ) {
        let (cols, rows) = self.terminal_area_size();
        match self.pty_mgr.spawn(
            label.clone(),
            cmd,
            args,
            cwd,
            worktree_alias.into(),
            worktree_dir.into(),
            cols,
            rows,
        ) {
            Ok(id) => {
                self.tabs.push(Tab {
                    session_id: id,
                    label,
                    alive: true,
                    split: None,
                });
                self.active_tab = self.tabs.len() - 1;
                self.tab_cursor = self.flat_index_for_tab(self.active_tab);
                self.focus = Panel::Terminal;
                self.terminal_focused = true;
                self.focused_session_id = Some(id);
                self.help_open = false;
            }
            Err(e) => {
                tracing::error!("Failed to spawn PTY: {}", e);
            }
        }
    }

    fn close_focused_session(&mut self) {
        let sid = match self.focused_session_id {
            Some(id) => id,
            None => {
                self.terminal_focused = false;
                return;
            }
        };
        self.close_session_by_id(sid);
        self.terminal_focused = false;
        self.focused_session_id = None;
    }

    fn close_session_under_cursor(&mut self) {
        let sid = match self.session_id_at_cursor() {
            Some(id) => id,
            None => return,
        };
        self.close_session_by_id(sid);
    }

    fn close_session_by_id(&mut self, sid: usize) {
        if self.tabs.is_empty() || self.active_tab >= self.tabs.len() {
            return;
        }

        let tab = &mut self.tabs[self.active_tab];

        if let Some(ref mut split) = tab.split {
            self.pty_mgr.remove(sid);
            let empty = split.remove_session(sid);

            if empty {
                self.tabs.remove(self.active_tab);
                if self.active_tab >= self.tabs.len() && !self.tabs.is_empty() {
                    self.active_tab = self.tabs.len() - 1;
                }
                self.tab_cursor = self.flat_index_for_tab(self.active_tab);
                self.focused_session_id = None;
            } else if split.leaf_count() == 1 {
                let remaining_id = split.first_leaf();
                let tab = &mut self.tabs[self.active_tab];
                tab.session_id = remaining_id;
                tab.split = None;
                if let Some(s) = self.pty_mgr.get(remaining_id) {
                    tab.label = s.label.clone();
                }
                let (cols, rows) = self.terminal_area_size();
                if let Some(session) = self.pty_mgr.get_mut(remaining_id) {
                    let _ = session.resize(cols.saturating_sub(2), rows.saturating_sub(2));
                }
                self.focused_session_id = Some(remaining_id);
            } else {
                // Set focus to next available session
                self.focused_session_id = Some(split.first_leaf());
                self.resize_split_ptys();
            }
        } else {
            self.pty_mgr.remove(sid);
            self.tabs.remove(self.active_tab);
            if self.active_tab >= self.tabs.len() && !self.tabs.is_empty() {
                self.active_tab = self.tabs.len() - 1;
            }
            self.tab_cursor = self.flat_index_for_tab(self.active_tab);
            self.focused_session_id = None;
        }
    }

    // ── Picker integration ─────────────────────────────────────────

    fn open_worktree_picker(&mut self) {
        if self.cursor >= self.worktrees.len() {
            return;
        }
        let wt = &self.worktrees[self.cursor];
        let title = format!("Actions: {}", wt.alias);

        // Root entry — shell, claude, and create
        if wt.alias == "Root" {
            let actions = vec![
                PickerAction::new("n", "Create", "Create a new worktree"),
                PickerAction::new("b", "Shell", "Open shell at project root"),
                PickerAction::new("c", "Claude", "Claude Code at project root"),
            ];
            self.open_picker(&title, actions);
            return;
        }

        let has_build = self.cfg.as_ref().map_or(false, |c| c.dash.build.is_some());
        let has_docker = wt.wt_type == WorktreeType::Docker;

        let mut actions = Vec::new();

        // Lifecycle actions — matches Go logic exactly
        if has_build {
            actions.push(PickerAction::new("u", "Build", "Compile and install binary"));
        } else if has_docker && wt.running {
            actions.push(PickerAction::new("r", "Restart", "Restart container"));
            actions.push(PickerAction::new("t", "Stop", "Stop container"));
        } else if has_docker && !wt.running && wt.container_exists {
            actions.push(PickerAction::new("u", "Start", "Start container"));
        } else if wt.running {
            // Local running
            actions.push(PickerAction::new("r", "Restart", "Restart services"));
            actions.push(PickerAction::new("t", "Stop", "Stop services"));
        } else {
            // Local stopped or Docker without container — always show Start
            actions.push(PickerAction::new("u", "Start", "Start worktree"));
        }

        // Shell
        if has_docker && !wt.container.is_empty() && wt.running {
            actions.push(PickerAction::new("b", "Bash", "Container bash"));
            actions.push(PickerAction::new("z", "Shell", "Host shell"));
        } else {
            actions.push(PickerAction::new("b", "Shell", "Open shell"));
        }
        actions.push(PickerAction::new("c", "Claude", "Claude Code"));
        actions.push(PickerAction::new("g", "Pull", "Pull latest changes"));

        // Logs — show when: running + has logs, OR logFile configured, OR daemon log exists
        {
            let has_log_file = self.cfg.as_ref()
                .map(|c| !c.dash.log_file.is_empty())
                .unwrap_or(false);
            let has_daemon_log = crate::daemon::log_path(&wt.path).exists();
            let has_running_logs = wt.running && (has_docker
                || self.cfg.as_ref().and_then(|c| c.lifecycle_cmd(&wt.wt_type, "logs")).is_some());
            if has_log_file || has_daemon_log || has_running_logs {
                actions.push(PickerAction::new("l", "Logs", "View logs"));
            }
        }

        // Individual service start/stop — when services exist
        if !self.services.is_empty() && wt.running {
            actions.push(PickerAction::new("o", "Start service", "Start a stopped service"));
            actions.push(PickerAction::new("p", "Stop service", "Stop a running service"));
        }

        // Info and remove — always
        actions.push(PickerAction::new("i", "Info", "Worktree info"));
        actions.push(PickerAction::new("x", "Remove", "Remove worktree"));

        self.open_picker(&title, actions);
    }

    fn open_picker(&mut self, title: &str, actions: Vec<PickerAction>) {
        self.notify_state = NotifyState::Picker {
            title: title.to_string(),
            actions,
            cursor: 0,
        };
        self.recalc_layout();
    }

    fn execute_picker_action(&mut self, action: &PickerAction) {
        // If a split direction is pending, create a split instead of a new tab
        if let Some(dir) = self.pending_split_dir.take() {
            self.execute_split_action(action, dir);
            return;
        }

        match action.key.as_str() {
            "b" => self.open_shell_for_selected(),
            "c" => self.open_claude_for_selected(),
            "l" => self.open_logs_for_selected(),
            "z" => self.open_zsh_for_selected(),
            "n" => {
                // "n" from remove picker = normal remove; from worktree picker = create
                // Disambiguate: if remove_actions was the source, remove normally
                if self.cursor < self.worktrees.len() {
                    // Check if this came from a remove context by checking action label
                    if action.label == "Normal" {
                        self.remove_worktree(false);
                    } else {
                        self.open_create_wizard();
                    }
                }
            }
            "i" => self.open_worktree_info(),
            "u" => self.start_selected_worktree(),
            "t" => self.stop_selected_worktree(),
            "r" => self.restart_selected_worktree(),
            "g" => self.pull_selected_worktree(),
            "x" => self.open_remove_picker(),
            // Remove sub-picker: n=normal, f=force
            "f" => self.remove_worktree(true),
            "o" => self.open_service_start_picker(),
            "p" => {
                // "p" from maintenance picker = Prune, from worktree picker = Stop service
                if action.label == "Prune" {
                    self.run_maintenance_script("wt-prune.js", "Prune");
                } else {
                    self.open_service_stop_picker();
                }
            }
            "s" => self.run_maintenance_script("wt-autostop.js", "Autostop"),
            _ => {
                tracing::debug!("Unhandled picker action: {}", action.key);
            }
        }
    }

    fn execute_split_action(&mut self, action: &PickerAction, dir: crate::pty::split::SplitDir) {
        use crate::pty::split::SplitNode;

        if self.tabs.is_empty() || self.active_tab >= self.tabs.len() {
            return;
        }

        // "l" (Logs) with multiple services → show service sub-picker
        if action.key == "l" && !self.services.is_empty() {
            let alias = self.split_target_alias.clone();
            // Re-set split direction for the sub-picker
            self.pending_split_dir = Some(dir);
            let mut actions = Vec::new();
            for (i, svc) in self.services.iter().enumerate() {
                if i >= 9 { break; }
                let key = format!("{}", i + 1);
                actions.push(PickerAction::new(
                    &key,
                    &svc.display_name,
                    "Service logs",
                ));
            }
            self.open_picker(&format!("Logs — {}", alias), actions);
            return;
        }

        let target_sid = self.split_target_session_id.take().unwrap_or(
            self.tabs[self.active_tab].session_id
        );
        let alias = std::mem::take(&mut self.split_target_alias);
        let cwd = std::mem::take(&mut self.split_target_dir);

        // Resolve command based on action
        let (cmd, args, label_prefix) = self.resolve_split_command(action, &alias, &cwd);
        let cmd = match cmd {
            Some(c) => c,
            None => return,
        };

        // For log actions: check if a tab already exists, focus it instead
        if label_prefix == "Logs" {
            let log_label = format!("Logs ({})", alias);
            if let Some(tab_idx) = self.tabs.iter().position(|t| t.label.starts_with("Logs:") && t.label.contains(&alias)) {
                self.active_tab = tab_idx;
                self.tab_cursor = self.flat_index_for_tab(self.active_tab);
                self.focus = Panel::Terminal;
                return;
            }
            // Also check for generic log tab
            if let Some(tab_idx) = self.tabs.iter().position(|t| t.label == log_label) {
                self.active_tab = tab_idx;
                self.tab_cursor = self.flat_index_for_tab(self.active_tab);
                self.focus = Panel::Terminal;
                return;
            }
        }

        let (cols, rows) = self.terminal_area_size();
        let (split_cols, split_rows) = match dir {
            crate::pty::split::SplitDir::Horizontal => ((cols / 2).max(5), rows),
            crate::pty::split::SplitDir::Vertical => (cols, (rows / 2).max(3)),
        };

        let args_refs: Vec<&str> = args.iter().map(|s| s.as_str()).collect();
        let label = format!("{} ({})", label_prefix, &alias);

        match self.pty_mgr.spawn(
            label, &cmd, &args_refs, &cwd,
            alias, cwd.clone(),
            split_cols, split_rows,
        ) {
            Ok(new_id) => {
                let tab = &mut self.tabs[self.active_tab];
                if let Some(ref mut split) = tab.split {
                    // Find target session and split it: target left/top, new right/bottom
                    if !split.find_and_split(target_sid, new_id, dir) {
                        // Fallback: add to root
                        split.add_split(new_id, dir);
                    }
                } else {
                    // Convert single session to split
                    tab.split = Some(SplitNode::split(
                        dir,
                        SplitNode::leaf(tab.session_id),
                        SplitNode::leaf(new_id),
                    ));
                }
                // Focus the new session, enter terminal mode, and resize
                self.focused_session_id = Some(new_id);
                self.focus = Panel::Terminal;
                self.terminal_focused = true;
                self.resize_split_ptys();
            }
            Err(e) => {
                tracing::error!("Failed to spawn split PTY: {}", e);
            }
        }
    }

    fn resolve_split_command(&self, action: &PickerAction, alias: &str, _cwd: &str) -> (Option<String>, Vec<String>, &'static str) {
        let wt = self.worktrees.iter().find(|w| w.alias == alias);

        match action.key.as_str() {
            "b" => {
                if let Some(wt) = wt {
                    if wt.wt_type == WorktreeType::Docker && !wt.container.is_empty() {
                        return (Some("docker".into()), vec!["exec".into(), "-it".into(), wt.container.clone(), "bash".into()], "Shell");
                    }
                }
                let shell = default_shell();
                (Some(shell), vec![], "Shell")
            }
            "c" => {
                let args = if self.settings.claude_auto_mode {
                    vec!["--enable-auto-mode".into()]
                } else {
                    vec![]
                };
                (Some("claude".into()), args, "Claude")
            }
            "z" => {
                let shell = default_shell();
                (Some(shell), vec![], "Shell")
            }
            "l" => {
                // Single log file (no services) — from config logFile or daemon log
                let log_file = self.cfg.as_ref()
                    .filter(|c| !c.dash.log_file.is_empty())
                    .and_then(|c| wt.map(|w| c.expand_cmd(&c.dash.log_file, w)))
                    .or_else(|| {
                        wt.map(|w| crate::daemon::log_path(&w.path).to_string_lossy().to_string())
                            .filter(|p| std::path::Path::new(p).exists())
                    });
                if let Some(path) = log_file {
                    let cmd = format!("touch '{}' && tail -f '{}'", path, path);
                    (Some("sh".into()), vec!["-c".into(), cmd], "Logs")
                } else {
                    (None, vec![], "")
                }
            }
            key => {
                // Numbered keys = service logs (1-9)
                if let Ok(idx) = key.parse::<usize>() {
                    let svc_idx = idx - 1;
                    if svc_idx < self.services.len() {
                        if let Some(wt) = wt {
                            let display = &self.services[svc_idx].display_name;
                            let manager = self.cfg.as_ref()
                                .map(|c| c.service_manager().to_string())
                                .unwrap_or_else(|| "pm2".to_string());
                            let (bin, args, _) = self.build_log_command(wt, &manager, display);
                            return (Some(bin), args, "Logs");
                        }
                    }
                }
                (None, vec![], "")
            }
        }
    }

    fn open_zsh_for_selected(&mut self) {
        if self.cursor >= self.worktrees.len() {
            return;
        }
        let alias = self.worktrees[self.cursor].alias.clone();
        let path = self.worktrees[self.cursor].path.to_string_lossy().to_string();
        let shell = default_shell();
        self.open_tab(
            format!("Shell ({})", alias),
            &shell,
            &[],
            &path,
            alias,
            path.clone(),
        );
    }

    fn flow_script(&self, name: &str) -> String {
        format!("{}/{}", self.flow_scripts_dir, name)
    }

    fn open_create_wizard(&mut self) {
        let script = self.flow_script("wt-create.js");
        let root = self.repo_root.clone();
        // Run directly — no wrapping, no pipes. Interactive CLI needs real TTY.
        self.open_tab(
            "Create Worktree".to_string(),
            "node",
            &[&script],
            &root,
            "",
            "",
        );
        // Track the tab for auto-close on process exit
        self.pending_create_tab = true;
        self.activity = Some("Creating worktree...".into());
    }

    fn open_worktree_info(&mut self) {
        self.details_visible = !self.details_visible;
        self.recalc_layout();
    }

    fn start_selected_worktree(&mut self) {
        if self.cursor >= self.worktrees.len() {
            return;
        }
        let wt = &self.worktrees[self.cursor];
        let alias = wt.alias.clone();
        let path = wt.path.to_string_lossy().to_string();

        // 1. Build takes priority (compiled tools like cargo/go build)
        if let Some(ref cfg) = self.cfg {
            if let Some(ref build) = cfg.dash.build {
                if !build.cmd.is_empty() {
                    let build_cmd = cfg.expand_cmd(&build.cmd, wt);
                    let install_path = cfg.expand_cmd(&build.install, wt);

                    // Ensure .wt/ and install directories exist
                    let wt_dir = format!("{}/.wt", path);
                    let build_log = format!("{}/build.log", wt_dir);
                    let mut full_cmd = format!("mkdir -p {} && ", wt_dir);
                    if !install_path.is_empty() {
                        if let Some(parent) = std::path::Path::new(&install_path).parent() {
                            full_cmd.push_str(&format!("mkdir -p {} && ", parent.display()));
                        }
                    }
                    // Tee build output to .wt/build.log
                    full_cmd.push_str(&format!("({}", build_cmd));
                    if !install_path.is_empty() {
                        full_cmd.push_str(&format!(
                            " && echo '\\nInstalled: {}'",
                            install_path,
                        ));
                    }
                    full_cmd.push_str(&format!(") 2>&1 | tee {}", build_log));

                    let shell = default_shell();
                    self.activity = Some(format!("Building {}...", alias));
                    self.open_tab(
                        format!("Build ({})", alias),
                        &shell,
                        &["-c", &full_cmd],
                        &path,
                        alias,
                        path.clone(),
                    );
                    self.pending_build_tab = true;
                    return;
                }
            }
        }

        // 2. Lifecycle config, stack defaults, or localDevCommand — daemon process
        let dev_cmd = self.cfg.as_ref()
            .and_then(|c| c.lifecycle_cmd(&wt.wt_type, "start").map(|s| s.to_string()))
            .or_else(|| {
                self.cfg.as_ref()
                    .filter(|c| !c.dash.local_dev_command.is_empty())
                    .filter(|_| wt.wt_type == WorktreeType::Local)
                    .map(|c| c.dash.local_dev_command.clone())
            });

        if let Some(cmd) = dev_cmd {
            let expanded = self.cfg.as_ref()
                .map(|c| c.expand_cmd(&cmd, wt))
                .unwrap_or(cmd);
            let wt_path = wt.path.clone();

            match crate::daemon::start(&wt_path, &expanded) {
                Ok(pid) => {
                    for w in &mut self.worktrees {
                        if w.alias == alias {
                            w.running = true;
                        }
                    }
                    self.refresh_services();
                    self.push_toast("Start", &format!("Started {} (pid {})", alias, pid), ui::overlay::ToastKind::Success);
                }
                Err(e) => {
                    self.push_toast("Start", &format!("Failed to start {}: {}", alias, e), ui::overlay::ToastKind::Error);
                }
            }
            return;
        }

        // 3. Fallback: delegate to wt-up.js
        let is_local = wt.wt_type == WorktreeType::Local;
        let script = self.flow_script("wt-up.js");
        let root = self.repo_root.clone();
        let name = wt.name.clone();
        let mut args = vec![script.clone(), name];
        if is_local {
            args.push("--no-docker".to_string());
        }
        self.activity = Some(format!("Starting {}...", alias));
        let args_ref: Vec<&str> = args.iter().map(|s| s.as_str()).collect();
        self.open_tab(
            format!("Starting ({})", alias),
            "node",
            &args_ref,
            &root,
            &alias,
            "",
        );
        self.pending_start_tab = true;
    }

    fn stop_selected_worktree(&mut self) {
        if self.cursor >= self.worktrees.len() {
            return;
        }
        let wt = &self.worktrees[self.cursor];
        let alias = wt.alias.clone();
        let wt_path = wt.path.clone();

        // 1. Daemon process — check .wt/dev.pid first
        if crate::daemon::is_running(&wt_path) {
            crate::daemon::stop(&wt_path);
            for w in &mut self.worktrees {
                if w.alias == alias {
                    w.running = false;
                }
            }
            self.refresh_services();
            self.push_toast("Stop", &format!("Stopped {}", alias), ui::overlay::ToastKind::Success);
            return;
        }

        // 2. Lifecycle stop command
        if let Some(ref cfg) = self.cfg {
            if let Some(cmd_template) = cfg.lifecycle_cmd(&wt.wt_type, "stop") {
                let expanded = cfg.expand_cmd(cmd_template, wt);
                let shell = default_shell();
                let _ = crate::cmd::run_cmd(&shell, &["-c", &expanded]);
                self.refresh_status();
                return;
            }
        }

        // 3. Fallback: built-in defaults
        match wt.wt_type {
            WorktreeType::Docker => {
                let container = wt.container.clone();
                if !container.is_empty() {
                    let _ = crate::cmd::run_cmd("docker", &["stop", &container]);
                }
            }
            WorktreeType::Local => {
                if wt.isolated_pm2 {
                    let pm2_home = wt.pm2_home().to_string_lossy().to_string();
                    let _ = pm2::kill(&pm2_home);
                } else {
                    for svc in &self.services {
                        if svc.name != "__all" {
                            let _ = crate::cmd::run_cmd("pm2", &["delete", &svc.name]);
                        }
                    }
                }
                self.push_toast("Stop", &format!("Stopped {}", alias), ui::overlay::ToastKind::Success);
            }
        }
        self.refresh_status();
    }

    fn restart_selected_worktree(&mut self) {
        if self.cursor >= self.worktrees.len() {
            return;
        }

        // Build-oriented: rebuild = same as start
        if let Some(ref cfg) = self.cfg {
            if cfg.dash.build.is_some() {
                self.start_selected_worktree();
                return;
            }
        }

        let wt = &self.worktrees[self.cursor];

        // 1. Explicit restart command in lifecycle config
        if let Some(ref cfg) = self.cfg {
            if let Some(cmd_template) = cfg.lifecycle_cmd(&wt.wt_type, "restart") {
                let expanded = cfg.expand_cmd(cmd_template, wt);
                let alias = wt.alias.clone();
                let path = wt.path.to_string_lossy().to_string();
                let shell = default_shell();
                self.open_tab(
                    format!("Restarting ({})", alias),
                    &shell,
                    &["-c", &expanded],
                    &path,
                    alias,
                    path.clone(),
                );
                return;
            }
        }

        // 2. If lifecycle/stack exists but no restart: stop then start
        if let Some(ref cfg) = self.cfg {
            if cfg.has_lifecycle() {
                self.stop_selected_worktree();
                self.start_selected_worktree();
                return;
            }
        }

        // 3. Fallback: built-in defaults
        match wt.wt_type {
            WorktreeType::Docker => {
                let container = wt.container.clone();
                if !container.is_empty() {
                    let _ = crate::cmd::run_cmd("docker", &["restart", &container]);
                }
            }
            WorktreeType::Local => {
                if wt.isolated_pm2 {
                    let pm2_home = wt.pm2_home().to_string_lossy().to_string();
                    let _ = pm2::kill(&pm2_home);
                }
                self.start_selected_worktree();
            }
        }
    }

    fn pull_selected_worktree(&mut self) {
        if self.cursor >= self.worktrees.len() {
            return;
        }
        let alias = self.worktrees[self.cursor].alias.clone();
        let path = self.worktrees[self.cursor].path.to_string_lossy().to_string();
        let script = self.flow_script("wt-pull.js");
        let root = self.repo_root.clone();
        self.open_tab(
            format!("Pull ({})", alias),
            "node",
            &[&script, &alias],
            &root,
            &alias,
            &path,
        );
    }

    fn remove_worktree(&mut self, force: bool) {
        if self.cursor >= self.worktrees.len() {
            return;
        }
        let alias = self.worktrees[self.cursor].alias.clone();
        let script = self.flow_script("wt-down.js");
        let root = self.repo_root.clone();

        // Run directly in a PTY tab
        let mut node_args: Vec<String> = vec![script, alias.clone(), "--remove".to_string()];
        if force {
            node_args.push("--force".to_string());
        }
        let args_ref: Vec<&str> = node_args.iter().map(|s| s.as_ref()).collect();
        self.open_tab(
            format!("Removing ({})", alias),
            "node",
            &args_ref,
            &root,
            &alias,
            "",
        );

        // Track for auto-close on process exit
        self.pending_remove = Some((alias.clone(), String::new()));
        self.activity = Some(format!("Removing {}...", alias));
    }

    fn open_service_start_picker(&mut self) {
        let actions: Vec<PickerAction> = self.services.iter()
            .filter(|s| s.name != "__all" && s.status != "online")
            .enumerate()
            .filter_map(|(i, svc)| {
                if i >= 26 { return None; }
                let key = String::from((b'a' + i as u8) as char);
                Some(PickerAction::new(&key, &svc.display_name, &svc.status))
            })
            .collect();

        if actions.is_empty() {
            self.push_toast("Start Service", "All services are already running", ui::overlay::ToastKind::Info);
            return;
        }
        self.open_picker("Start Service", actions);
    }

    fn open_service_stop_picker(&mut self) {
        let actions: Vec<PickerAction> = self.services.iter()
            .filter(|s| s.name != "__all" && s.status == "online")
            .enumerate()
            .filter_map(|(i, svc)| {
                if i >= 26 { return None; }
                let key = String::from((b'a' + i as u8) as char);
                Some(PickerAction::new(&key, &svc.display_name, &svc.status))
            })
            .collect();

        if actions.is_empty() {
            self.push_toast("Stop Service", "No services are running", ui::overlay::ToastKind::Info);
            return;
        }
        self.open_picker("Stop Service", actions);
    }

    fn run_maintenance_script(&mut self, script_name: &str, label: &str) {
        let script = self.flow_script(script_name);
        let root = self.repo_root.clone();
        self.open_tab(
            label.to_string(),
            "node",
            &[&script],
            &root,
            "",
            "",
        );
    }

    /// Enter on Services: open or switch log preview (never promotes to tab).
    fn service_open_preview(&mut self) {
        if self.service_cursor >= self.services.len() || self.cursor >= self.worktrees.len() {
            return;
        }

        let wt = &self.worktrees[self.cursor];
        if !wt.running {
            return;
        }

        let manager = self.cfg.as_ref()
            .map(|c| c.service_manager().to_string())
            .unwrap_or_else(|| "pm2".to_string());
        let svc_name = self.services[self.service_cursor].display_name.clone();

        // Already previewing this service — nothing to do
        if self.preview_session.is_some() && self.preview_svc_name == svc_name {
            return;
        }

        // Close existing preview
        if let Some(sid) = self.preview_session.take() {
            self.pty_mgr.remove(sid);
            self.preview_svc_name.clear();
        }

        // Build the log command
        let (bin, args, cwd) = self.build_log_command(wt, &manager, &svc_name);

        let (cols, rows) = self.terminal_area_size();
        let args_ref: Vec<&str> = args.iter().map(|s| s.as_str()).collect();
        if let Ok(id) = self.pty_mgr.spawn(
            format!("Preview: {}", svc_name),
            &bin,
            &args_ref,
            &cwd,
            String::new(),
            String::new(),
            cols,
            rows,
        ) {
            self.preview_session = Some(id);
            self.preview_svc_name = svc_name;
        }
    }

    /// Build the command to tail logs for a service.
    fn build_log_command(&self, wt: &Worktree, manager: &str, _svc_name: &str) -> (String, Vec<String>, String) {
        // Static + local: use config logFile (with expansion) or .wt/dev.log
        if manager == "static" && wt.wt_type == WorktreeType::Local {
            let log_str = self.cfg.as_ref()
                .filter(|c| !c.dash.log_file.is_empty())
                .map(|c| c.expand_cmd(&c.dash.log_file, wt))
                .unwrap_or_else(|| crate::daemon::log_path(&wt.path).to_string_lossy().to_string());
            let cmd = format!("touch '{}' && tail -f '{}'", log_str, log_str);
            return ("sh".into(), vec!["-c".into(), cmd], wt.path.to_string_lossy().to_string());
        }

        let container = wt.container.clone();
        let wt_path = wt.path.to_string_lossy().to_string();
        let svc = &self.services[self.service_cursor];
        let cmd_name = svc.name.clone();

        if wt.wt_type == WorktreeType::Docker && !container.is_empty() {
            if manager == "static" {
                ("docker".into(), vec!["logs".into(), "-f".into(), "--tail".into(), "80".into(), container], String::new())
            } else {
                ("docker".into(), vec!["exec".into(), "-it".into(), container, "pm2".into(), "logs".into(), cmd_name, "--lines".into(), "50".into()], String::new())
            }
        } else if wt.isolated_pm2 {
            let pm2_home = wt.pm2_home().to_string_lossy().to_string();
            ("bash".into(), vec!["-c".into(), format!("PM2_HOME={} exec pm2 logs '{}' --lines 80", pm2_home, cmd_name)], wt_path)
        } else {
            ("pm2".into(), vec!["logs".into(), cmd_name, "--lines".into(), "50".into()], wt_path)
        }
    }

    fn open_service_logs_tab(&mut self) {
        if self.service_cursor >= self.services.len() || self.cursor >= self.worktrees.len() {
            return;
        }
        let wt = &self.worktrees[self.cursor];
        if !wt.running {
            return;
        }
        let display = self.services[self.service_cursor].display_name.clone();
        let alias = wt.alias.clone();
        let label = format!("Logs: {} ({})", display, alias);

        // Check if a tab with this label already exists — focus it
        if let Some(tab_idx) = self.tabs.iter().position(|t| t.label == label) {
            self.active_tab = tab_idx;
            self.tab_cursor = self.flat_index_for_tab(self.active_tab);
            self.focus = Panel::Terminal;
            return;
        }

        // Close preview if open (promoting it conceptually)
        if let Some(sid) = self.preview_session.take() {
            self.pty_mgr.remove(sid);
            self.preview_svc_name.clear();
        }

        let manager = self.cfg.as_ref()
            .map(|c| c.service_manager().to_string())
            .unwrap_or_else(|| "pm2".to_string());
        let (bin, args, cwd) = self.build_log_command(wt, &manager, &display);
        let wt_path = wt.path.to_string_lossy().to_string();

        let args_ref: Vec<&str> = args.iter().map(|s| s.as_str()).collect();
        self.open_tab(
            label,
            &bin,
            &args_ref,
            &cwd,
            alias,
            wt_path,
        );
    }

    fn restart_service(&mut self) {
        if self.service_cursor >= self.services.len() || self.cursor >= self.worktrees.len() {
            return;
        }
        let container = self.worktrees[self.cursor].container.clone();
        let svc_name = self.services[self.service_cursor].name.clone();
        if !container.is_empty() {
            let _ = crate::cmd::run_cmd("docker", &["exec", &container, "pm2", "restart", &svc_name]);
        }
    }

    fn open_split_picker(&mut self, direction: crate::pty::split::SplitDir) {
        if self.tabs.is_empty() || self.active_tab >= self.tabs.len() {
            return;
        }

        let tab = &self.tabs[self.active_tab];

        // Check max panes
        let current_size = tab.split.as_ref().map(|s| s.leaf_count()).unwrap_or(1);
        let max_panes = self.settings.max_panes_per_group as usize;
        if current_size >= max_panes {
            self.push_toast("Info", &format!("Max {} sessions per group reached", max_panes), ui::overlay::ToastKind::Info);
            return;
        }

        // Find session under cursor in flat list
        let target_sid = self.session_id_at_cursor().unwrap_or(tab.session_id);

        // Get worktree info for the target session
        let (alias, dir) = self.pty_mgr.get(target_sid)
            .map(|s| (s.worktree_alias.clone(), s.worktree_dir.clone()))
            .unwrap_or_default();

        self.pending_split_dir = Some(direction);
        self.split_target_session_id = Some(target_sid);
        self.split_target_alias = alias.clone();
        self.split_target_dir = dir;

        let title = match direction {
            crate::pty::split::SplitDir::Horizontal => format!("Split Right — {}", alias),
            crate::pty::split::SplitDir::Vertical => format!("Split Below — {}", alias),
        };
        let actions = self.build_split_actions(&alias);
        self.open_picker(&title, actions);
    }

    fn build_split_actions(&self, alias: &str) -> Vec<PickerAction> {
        let mut actions = vec![
            PickerAction::new("b", "Shell", "Container shell"),
            PickerAction::new("c", "Claude", "Claude Code"),
            PickerAction::new("z", "Zsh", "Host shell"),
        ];

        // Show single "Logs" entry if any log source exists
        let wt = self.worktrees.iter().find(|w| w.alias == alias);
        let has_log = self.cfg.as_ref()
            .map(|c| !c.dash.log_file.is_empty())
            .unwrap_or(false)
            || wt.map_or(false, |w| crate::daemon::log_path(&w.path).exists())
            || (!self.services.is_empty() && wt.map_or(false, |w| w.running));

        if has_log {
            actions.push(PickerAction::new("l", "Logs", "View logs"));
        }

        actions
    }

    /// Find the session ID at the current tab_cursor position in the flat entry list.
    fn session_id_at_cursor(&self) -> Option<usize> {
        let mut pos = 0usize;
        for tab in &self.tabs {
            if let Some(ref split) = tab.split {
                // Header
                if self.tab_cursor == pos {
                    return Some(split.first_leaf());
                }
                pos += 1;
                // Children
                for sid in split.session_ids() {
                    if self.tab_cursor == pos {
                        return Some(sid);
                    }
                    pos += 1;
                }
            } else {
                if self.tab_cursor == pos {
                    return Some(tab.session_id);
                }
                pos += 1;
            }
        }
        None
    }

    fn open_remove_picker(&mut self) {
        if self.cursor >= self.worktrees.len() {
            return;
        }
        let alias = self.worktrees[self.cursor].alias.clone();
        self.open_picker(&format!("Remove: {}", alias), overlay::remove_actions());
    }

    /// Compute the terminal area size in cols/rows based on current layout.
    fn terminal_area_size(&self) -> (u16, u16) {
        let left_pct = self.settings.left_pane_pct;
        let cols = self.width.saturating_sub(self.width * left_pct / 100).saturating_sub(2); // 1 padding each side
        let rows = self.height.saturating_sub(3); // status bar + title row + bottom padding
        (cols.max(10), rows.max(5))
    }

    /// Cycle focus between sessions in the active tab's split group (or between tabs if no split).
    fn cycle_focused_session(&mut self, direction: i32) {
        if self.tabs.is_empty() || self.active_tab >= self.tabs.len() {
            return;
        }

        let tab = &self.tabs[self.active_tab];
        if let Some(ref split) = tab.split {
            // Split group: cycle through leaf sessions
            let ids = split.session_ids();
            if ids.len() <= 1 {
                return;
            }
            let current = self.focused_session_id.unwrap_or(0);
            let pos = ids.iter().position(|&id| id == current).unwrap_or(0);
            let next = ((pos as i32 + direction).rem_euclid(ids.len() as i32)) as usize;
            self.focused_session_id = Some(ids[next]);
        } else {
            // No split: cycle between tabs
            let n = self.tabs.len();
            if n <= 1 {
                return;
            }
            let next = ((self.active_tab as i32 + direction).rem_euclid(n as i32)) as usize;
            self.active_tab = next;
            self.tab_cursor = self.flat_index_for_tab(self.active_tab);
            self.focused_session_id = Some(self.tabs[next].session_id);
        }
    }

    fn cycle_panel(&mut self, direction: i8) {
        let mut panels = vec![Panel::Terminal, Panel::Worktrees];
        if self.services_visible {
            panels.push(Panel::Services);
        }
        if self.details_visible {
            panels.push(Panel::Details);
        }
        if self.tasks_visible {
            panels.push(Panel::Tasks);
        }

        let current = panels.iter().position(|p| *p == self.focus).unwrap_or(0);
        let next = (current as i8 + direction).rem_euclid(panels.len() as i8) as usize;
        self.focus = panels[next];
    }

    fn navigate(&mut self, delta: i8) {
        match self.focus {
            Panel::Worktrees => {
                if self.worktrees.is_empty() {
                    return;
                }
                let prev = self.cursor;
                let new = self.cursor as i32 + delta as i32;
                self.cursor = new.clamp(0, self.worktrees.len() as i32 - 1) as usize;
                if self.cursor != prev {
                    self.refresh_services();
                }
            }
            Panel::Services => {
                if self.services.is_empty() {
                    return;
                }
                let new = self.service_cursor as i32 + delta as i32;
                self.service_cursor = new.clamp(0, self.services.len() as i32 - 1) as usize;
            }
            Panel::Terminal => {
                if self.tabs.is_empty() {
                    return;
                }
                // Compute flat entry count (headers + children for split groups)
                let flat_count: usize = self.tabs.iter().map(|t| {
                    if let Some(ref split) = t.split {
                        1 + split.session_ids().len() // header + children
                    } else {
                        1 // standalone tab
                    }
                }).sum();

                let new = self.tab_cursor as i32 + delta as i32;
                self.tab_cursor = new.clamp(0, flat_count as i32 - 1) as usize;

                // Sync active_tab: find which tab group the cursor is in
                let mut pos = 0usize;
                for (tab_idx, tab) in self.tabs.iter().enumerate() {
                    let entry_count = if let Some(ref split) = tab.split {
                        1 + split.session_ids().len()
                    } else {
                        1
                    };
                    if self.tab_cursor >= pos && self.tab_cursor < pos + entry_count {
                        self.active_tab = tab_idx;
                        break;
                    }
                    pos += entry_count;
                }
            }
            Panel::Tasks => {
                if self.tasks_detail.is_some() {
                    let new = self.tasks_detail_scroll as i32 + delta as i32;
                    self.tasks_detail_scroll = (new.max(0) as usize).min(self.tasks_detail_max_scroll.get());
                } else {
                    let filtered_len = self.filtered_task_indices().len();
                    if filtered_len > 0 {
                        let new = self.tasks_cursor as i32 + delta as i32;
                        self.tasks_cursor = new.clamp(0, filtered_len as i32 - 1) as usize;
                    }
                }
            }
            _ => {}
        }
    }

    /// Jump to the N-th numbered session (1-indexed, used by number keys).
    /// Group headers are not numbered — only sessions (standalone + children).
    fn jump_to_flat_index(&mut self, target_num: usize) {
        let target = target_num + 1; // target_num is 0-based, display nums are 1-based
        let mut seq = 0usize;
        let mut flat_pos = 0usize;
        for (tab_idx, tab) in self.tabs.iter().enumerate() {
            if let Some(ref split) = tab.split {
                let session_ids = split.session_ids();
                // Group header — no number, skip
                flat_pos += 1;
                // Children — each has a number
                for &sid in &session_ids {
                    seq += 1;
                    if seq == target {
                        self.tab_cursor = flat_pos;
                        self.active_tab = tab_idx;
                        self.focused_session_id = Some(sid);
                        return;
                    }
                    flat_pos += 1;
                }
            } else {
                seq += 1;
                if seq == target {
                    self.tab_cursor = flat_pos;
                    self.active_tab = tab_idx;
                    self.focused_session_id = Some(tab.session_id);
                    return;
                }
                flat_pos += 1;
            }
        }
    }

    /// Compute the flat entry index for the start of a given tab group.
    fn flat_index_for_tab(&self, tab_idx: usize) -> usize {
        let mut pos = 0;
        for (i, tab) in self.tabs.iter().enumerate() {
            if i == tab_idx {
                return pos;
            }
            pos += if let Some(ref split) = tab.split {
                1 + split.session_ids().len()
            } else {
                1
            };
        }
        pos
    }

    /// Push a floating toast notification (top-right corner).
    /// All notifications auto-dismiss after 5s and can be dismissed with Esc.
    pub fn push_toast(&mut self, title: &str, message: &str, kind: ui::overlay::ToastKind) {
        use ui::overlay::Toast;
        // Limit to 3 toasts — dismiss oldest non-confirm toast when full
        while self.toasts.iter().filter(|t| !matches!(&t.action, Some(ui::overlay::ToastAction::Confirm))).count() >= 3 {
            if let Some(idx) = self.toasts.iter().position(|t| !matches!(&t.action, Some(ui::overlay::ToastAction::Confirm))) {
                self.toasts.remove(idx);
            } else {
                break;
            }
        }
        let duration = Some(std::time::Duration::from_secs(5));
        self.toasts.push(Toast {
            title: title.to_string(),
            message: message.to_string(),
            kind,
            created_at: std::time::Instant::now(),
            duration,
            action: None,
        });
    }

    /// Push a confirmation toast that requires Ctrl+A to confirm or Esc to cancel.
    pub fn push_confirm_toast(&mut self, title: &str, message: &str) {
        use ui::overlay::{Toast, ToastAction, ToastKind};
        self.toasts.push(Toast {
            title: title.to_string(),
            message: message.to_string(),
            kind: ToastKind::Info,
            created_at: std::time::Instant::now(),
            duration: None,
            action: Some(ToastAction::Confirm),
        });
    }


    /// Check if a pending remove operation completed.
    pub fn check_pending_remove(&mut self) {
        let (alias, _) = match &self.pending_remove {
            Some(p) => (p.0.clone(), p.1.clone()),
            None => return,
        };

        // Find the remove tab and check if process exited
        let tab_idx = match self.tabs.iter().position(|t| t.label.contains("Removing")) {
            Some(i) => i,
            None => {
                self.pending_remove = None;
                return;
            }
        };

        let session_id = self.tabs[tab_idx].session_id;
        let alive = self.pty_mgr.get_mut(session_id)
            .map(|s| s.check_alive())
            .unwrap_or(false);

        if alive {
            return;
        }

        // Process exited
        self.pending_remove = None;
        self.activity = None;

        // Get exit code and output before closing
        let exit_code = self.pty_mgr.get(session_id)
            .and_then(|s| s.exit_code);
        let output_lines = self.pty_mgr.get(session_id)
            .map(|s| s.last_lines(10))
            .unwrap_or_default();
        let output_text = output_lines.iter()
            .filter(|l| !l.is_empty())
            .cloned()
            .collect::<Vec<_>>()
            .join("\n");

        // Close tab
        let tab = self.tabs.remove(tab_idx);
        self.pty_mgr.remove(tab.session_id);
        if self.active_tab >= self.tabs.len() && !self.tabs.is_empty() {
            self.active_tab = self.tabs.len() - 1;
        }
        self.tab_cursor = self.flat_index_for_tab(self.active_tab);
        self.terminal_focused = false;
        self.focus = Panel::Worktrees;

        self.run_discovery();

        let success = exit_code == Some(0);

        if success {
            self.push_toast("Success", &format!("Removed worktree: {}", alias), ui::overlay::ToastKind::Success);
        } else {
            let err_msg = if output_text.is_empty() {
                format!("Failed to remove: {}", alias)
            } else {
                output_text
            };
            self.push_toast("Error", &err_msg, ui::overlay::ToastKind::Error);
        }
        // Clamp cursor to valid range
        if self.cursor >= self.worktrees.len() && !self.worktrees.is_empty() {
            self.cursor = self.worktrees.len() - 1;
        }
    }

    /// Check if a pending create operation completed.
    pub fn check_pending_create(&mut self) {
        if !self.pending_create_tab {
            return;
        }

        // Find the create tab
        let tab_idx = match self.tabs.iter().position(|t| t.label.contains("Create")) {
            Some(i) => i,
            None => {
                self.pending_create_tab = false;
                self.activity = None;
                return;
            }
        };

        let session_id = self.tabs[tab_idx].session_id;
        let alive = self.pty_mgr.get_mut(session_id)
            .map(|s| s.check_alive())
            .unwrap_or(false);

        // Check if new worktrees appeared (success even if process still runs)
        let old_count = self.worktrees.len();
        self.run_discovery();
        let new_count = self.worktrees.len();
        let new_worktree_found = new_count > old_count;

        if new_worktree_found {
            // Worktree created — dev server may still be running in the tab (that's fine)
            self.pending_create_tab = false;
            self.activity = None;
            self.refresh_status();
            self.push_toast("Success", "Worktree created successfully", ui::overlay::ToastKind::Success);
            return;
        }

        if alive {
            return; // Still running, no new worktree yet
        }

        // Process exited without creating a worktree — error
        self.pending_create_tab = false;
        self.activity = None;

        let exit_code = self.pty_mgr.get(session_id)
            .and_then(|s| s.exit_code);
        let output_lines = self.pty_mgr.get(session_id)
            .map(|s| s.last_lines(10))
            .unwrap_or_default();
        let output_text = output_lines.iter()
            .filter(|l| !l.is_empty())
            .cloned()
            .collect::<Vec<_>>()
            .join("\n");

        // Close failed tab
        let tab = self.tabs.remove(tab_idx);
        self.pty_mgr.remove(tab.session_id);
        if self.active_tab >= self.tabs.len() && !self.tabs.is_empty() {
            self.active_tab = self.tabs.len() - 1;
        }
        self.tab_cursor = self.flat_index_for_tab(self.active_tab);
        self.terminal_focused = false;
        self.focus = Panel::Worktrees;

        // Detect cancellation: exit 0 but no new worktree = user cancelled
        let cancelled = exit_code == Some(0) && !new_worktree_found;
        let failed = exit_code.map_or(true, |c| c != 0);

        if cancelled {
            self.push_toast("Info", "Worktree creation cancelled", ui::overlay::ToastKind::Info);
        } else if failed {
            let err_msg = if output_text.is_empty() {
                "Worktree creation failed".to_string()
            } else {
                output_text
            };
            self.push_toast("Error", &err_msg, ui::overlay::ToastKind::Error);
        }
    }

    pub fn check_pending_build(&mut self) {
        if !self.pending_build_tab {
            return;
        }

        let tab_idx = match self.tabs.iter().position(|t| t.label.starts_with("Build")) {
            Some(i) => i,
            None => {
                self.pending_build_tab = false;
                return;
            }
        };

        let session_id = self.tabs[tab_idx].session_id;
        let alive = self.pty_mgr.get_mut(session_id)
            .map(|s| s.check_alive())
            .unwrap_or(false);

        if alive {
            return;
        }

        self.pending_build_tab = false;
        self.activity = None;

        let exit_code = self.pty_mgr.get(session_id)
            .and_then(|s| s.exit_code);
        let output_lines = self.pty_mgr.get(session_id)
            .map(|s| s.last_lines(5))
            .unwrap_or_default();
        let output_text = output_lines.iter()
            .filter(|l| !l.is_empty())
            .cloned()
            .collect::<Vec<_>>()
            .join("\n");

        // Close the build tab
        let tab = self.tabs.remove(tab_idx);
        self.pty_mgr.remove(tab.session_id);
        if self.active_tab >= self.tabs.len() && !self.tabs.is_empty() {
            self.active_tab = self.tabs.len() - 1;
        }
        self.tab_cursor = self.flat_index_for_tab(self.active_tab);
        self.terminal_focused = false;
        self.focus = Panel::Worktrees;

        if exit_code == Some(0) {
            let msg = if output_text.is_empty() { "Build succeeded".into() } else { output_text };
            self.push_toast("Build", &msg, ui::overlay::ToastKind::Success);
        } else {
            let msg = if output_text.is_empty() { "Build failed".into() } else { output_text };
            self.push_toast("Build", &msg, ui::overlay::ToastKind::Error);
        }
    }

    pub fn check_pending_start(&mut self) {
        if !self.pending_start_tab {
            return;
        }

        let tab_idx = match self.tabs.iter().position(|t| t.label.starts_with("Starting")) {
            Some(i) => i,
            None => {
                self.pending_start_tab = false;
                self.activity = None;
                return;
            }
        };

        let session_id = self.tabs[tab_idx].session_id;
        let alive = self.pty_mgr.get_mut(session_id)
            .map(|s| s.check_alive())
            .unwrap_or(false);

        if alive {
            return;
        }

        self.pending_start_tab = false;
        self.activity = None;

        let exit_code = self.pty_mgr.get(session_id)
            .and_then(|s| s.exit_code);
        let started_alias = self.pty_mgr.get(session_id)
            .map(|s| s.worktree_alias.clone())
            .unwrap_or_default();
        let output_lines = self.pty_mgr.get(session_id)
            .map(|s| s.last_lines(5))
            .unwrap_or_default();
        let output_text = output_lines.iter()
            .filter(|l| !l.is_empty())
            .cloned()
            .collect::<Vec<_>>()
            .join("\n");

        // Close the start tab
        let tab = self.tabs.remove(tab_idx);
        self.pty_mgr.remove(tab.session_id);
        if self.active_tab >= self.tabs.len() && !self.tabs.is_empty() {
            self.active_tab = self.tabs.len() - 1;
        }
        self.tab_cursor = self.flat_index_for_tab(self.active_tab);
        self.terminal_focused = false;
        self.focus = Panel::Worktrees;

        self.refresh_status();

        // If start succeeded, mark the worktree as running
        if exit_code == Some(0) && !started_alias.is_empty() {
            for wt in &mut self.worktrees {
                if wt.alias == started_alias {
                    wt.running = true;
                }
            }
        }

        if exit_code == Some(0) {
            let msg = if output_text.is_empty() { "Started successfully".into() } else { output_text };
            self.push_toast("Start", &msg, ui::overlay::ToastKind::Success);
        } else {
            let msg = if output_text.is_empty() { "Start failed".into() } else { output_text };
            self.push_toast("Start", &msg, ui::overlay::ToastKind::Error);
        }
    }

    /// Check if notification should auto-dismiss.
    /// Update agent states by reading terminal screen content.
    /// Called periodically (every ~30 ticks / ~1 second).
    pub fn update_agent_states(&mut self) {
        for tab in &self.tabs {
            if let Some(ref split) = tab.split {
                for sid in split.session_ids() {
                    if let Some(session) = self.pty_mgr.get(sid) {
                        if session.alive {
                            let content = crate::detect::read_screen(&session.term());
                            let state = crate::detect::detect_state(&content, &session.label);
                            self.agent_states.insert(sid, state);
                        }
                    }
                }
            } else if let Some(session) = self.pty_mgr.get(tab.session_id) {
                if session.alive {
                    let content = crate::detect::read_screen(&session.term());
                    let state = crate::detect::detect_state(&content, &session.label);
                    self.agent_states.insert(tab.session_id, state);
                }
            }
        }
    }


    /// Find the highest-priority toast to act on.
    /// Priority: quit confirm > other Confirm > Acknowledge > oldest.
    /// `confirm_only`: if true, only return Confirm toasts (for Ctrl+A).
    fn find_priority_toast(&self, confirm_only: bool) -> Option<usize> {
        use ui::overlay::ToastAction;
        // 1. Quit confirm
        let idx = self.toasts.iter().position(|t| {
            matches!(&t.action, Some(ToastAction::Confirm))
                && (t.title.contains("Quit") || t.title.contains("Exit"))
        });
        if idx.is_some() { return idx; }
        // 2. Other Confirm toasts
        let idx = self.toasts.iter().position(|t| {
            matches!(&t.action, Some(ToastAction::Confirm))
        });
        if idx.is_some() { return idx; }
        if confirm_only { return None; }
        // 3. Acknowledge toasts
        let idx = self.toasts.iter().position(|t| {
            matches!(&t.action, Some(ToastAction::Acknowledge))
        });
        if idx.is_some() { return idx; }
        // 4. Any toast (oldest)
        Some(0)
    }

    /// Handle a confirmed toast action (Ctrl+A was pressed).
    fn handle_toast_action(&mut self, toast: &ui::overlay::Toast) {
        use ui::overlay::ToastAction;
        match &toast.action {
            Some(ToastAction::Confirm) => {
                // Currently only quit uses Confirm
                if toast.title.contains("Quit") || toast.title.contains("Exit") {
                    self.should_quit = true;
                }
            }
            _ => {} // Acknowledge just dismisses (already removed)
        }
    }

    /// Remove toasts whose duration has elapsed.
    pub fn dismiss_expired_toasts(&mut self, _tick: u64) {
        self.toasts.retain(|t| !t.is_expired());
    }

    fn recalc_layout(&mut self) {
        // Only picker/input overlays need space in the left column.
        // Messages and quit confirm render as a top bar now.
        let notify_height = match &self.notify_state {
            NotifyState::Picker { .. } | NotifyState::Confirm { .. } | NotifyState::Input { .. } => {
                self.notify_state.height()
            }
            _ => 0,
        };
        self.layout = self.layout.resize(
            self.width,
            self.height,
            &ResizeOpts {
                notify_height,
                details_visible: self.details_visible,
                usage_visible: self.usage_visible,
                tasks_visible: self.tasks_visible,
                tasks_content: if self.tasks_detail.is_some() { 20 } else { self.tasks_list.len().max(3) as u16 },
                services_visible: self.services_visible,
            },
        );
    }
}

/// Convert mouse screen coordinates to terminal grid point.
fn mouse_to_point(col: u16, row: u16, app: &App) -> alacritty_terminal::index::Point {
    // Subtract the left panel width and terminal border
    let left_w = if app.fullscreen { 0 } else {
        (app.last_frame_width * app.settings.left_pane_pct / 100) + 1 // +1 for border
    };
    let term_col = col.saturating_sub(left_w);
    let term_row = row.saturating_sub(1); // -1 for border/title
    alacritty_terminal::index::Point {
        line: alacritty_terminal::index::Line(term_row as i32),
        column: alacritty_terminal::index::Column(term_col as usize),
    }
}

fn default_shell() -> String {
    std::env::var("SHELL").unwrap_or_else(|_| "bash".to_string())
}

// ── Key to byte conversion ───────────────────────────────────────────

/// Convert a crossterm KeyEvent to the bytes that should be sent to a PTY.
pub(crate) fn key_to_bytes(key: &KeyEvent) -> Option<Vec<u8>> {
    let ctrl = key.modifiers.contains(KeyModifiers::CONTROL);
    let alt = key.modifiers.contains(KeyModifiers::ALT);

    match key.code {
        KeyCode::Char(c) if ctrl => {
            // Ctrl+A=1, Ctrl+B=2, ..., Ctrl+Z=26
            let byte = (c.to_ascii_lowercase() as u8).wrapping_sub(b'a').wrapping_add(1);
            if byte <= 26 {
                Some(vec![byte])
            } else {
                None
            }
        }
        KeyCode::Char(c) if alt => {
            let mut bytes = vec![0x1b]; // ESC
            let mut buf = [0u8; 4];
            bytes.extend_from_slice(c.encode_utf8(&mut buf).as_bytes());
            Some(bytes)
        }
        KeyCode::Char(c) => {
            let mut buf = [0u8; 4];
            Some(c.encode_utf8(&mut buf).as_bytes().to_vec())
        }
        KeyCode::Enter => Some(vec![b'\r']),
        KeyCode::Backspace => Some(vec![0x7f]),
        KeyCode::Delete => Some(b"\x1b[3~".to_vec()),
        KeyCode::Esc => Some(vec![0x1b]),
        KeyCode::Tab => Some(vec![b'\t']),
        KeyCode::BackTab => Some(b"\x1b[Z".to_vec()),
        KeyCode::Up => Some(b"\x1b[A".to_vec()),
        KeyCode::Down => Some(b"\x1b[B".to_vec()),
        KeyCode::Right => Some(b"\x1b[C".to_vec()),
        KeyCode::Left => Some(b"\x1b[D".to_vec()),
        KeyCode::Home => Some(b"\x1b[H".to_vec()),
        KeyCode::End => Some(b"\x1b[F".to_vec()),
        KeyCode::PageUp => Some(b"\x1b[5~".to_vec()),
        KeyCode::PageDown => Some(b"\x1b[6~".to_vec()),
        KeyCode::Insert => Some(b"\x1b[2~".to_vec()),
        KeyCode::F(n) => {
            let seq = match n {
                1 => "\x1bOP",
                2 => "\x1bOQ",
                3 => "\x1bOR",
                4 => "\x1bOS",
                5 => "\x1b[15~",
                6 => "\x1b[17~",
                7 => "\x1b[18~",
                8 => "\x1b[19~",
                9 => "\x1b[20~",
                10 => "\x1b[21~",
                11 => "\x1b[23~",
                12 => "\x1b[24~",
                _ => return None,
            };
            Some(seq.as_bytes().to_vec())
        }
        _ => None,
    }
}

// ── Flow scripts resolution ──────────────────────────────────────────

fn resolve_flow_scripts_dir(repo_root: &str, cfg: Option<&config::Config>) -> String {
    // 1. Config paths.flowScripts
    if let Some(c) = cfg {
        if !c.paths.flow_scripts.is_empty() {
            let p = &c.paths.flow_scripts;
            if std::path::Path::new(p).is_absolute() {
                return p.clone();
            }
            return format!("{}/{}", repo_root, p);
        }
    }

    // 2. WT_SCRIPTS_DIR env var
    if let Ok(dir) = std::env::var("WT_SCRIPTS_DIR") {
        if std::path::Path::new(&dir).is_dir() {
            return dir;
        }
    }

    // 3. Relative to binary (Homebrew or dev layout)
    if let Ok(exe) = std::env::current_exe() {
        if let Ok(exe) = exe.canonicalize() {
            if let Some(bin_dir) = exe.parent() {
                // Homebrew: <prefix>/share/wt/worktree-flow/
                let brew_path = bin_dir.join("../share/wt/worktree-flow");
                if brew_path.is_dir() {
                    return brew_path.to_string_lossy().to_string();
                }
                // Dev: <repo>/worktree-dash-rs/../worktree-flow/
                let dev_path = bin_dir.join("../worktree-flow");
                if dev_path.is_dir() {
                    return dev_path.to_string_lossy().to_string();
                }
            }
        }
    }

    // 4. Fallback: relative to repo root
    format!("{}/worktree-flow", repo_root)
}

/// Recursively resize PTY sessions in a split tree to match their actual layout area.
/// Uses the same Constraint::Ratio logic as the renderer for exact size matching.
fn resize_node_ptys(
    node: &crate::pty::split::SplitNode,
    cols: u16,
    rows: u16,
    pty_mgr: &mut crate::pty::PtyManager,
) {
    use crate::pty::split::{SplitDir, SplitNode};
    use ratatui::prelude::*;

    match node {
        SplitNode::Leaf(session_id) => {
            // Subtract 2 for borders
            let inner_cols = cols.saturating_sub(2).max(5);
            let inner_rows = rows.saturating_sub(2).max(3);
            if let Some(session) = pty_mgr.get_mut(*session_id) {
                let _ = session.resize(inner_cols, inner_rows);
            }
        }
        SplitNode::Split { direction, children } => {
            let n = children.len() as u32;
            if n == 0 {
                return;
            }

            // Use ratatui Layout with Ratio constraints — same as render
            let dir = match direction {
                SplitDir::Horizontal => Direction::Horizontal,
                SplitDir::Vertical => Direction::Vertical,
            };
            let constraints: Vec<Constraint> = (0..n)
                .map(|_| Constraint::Ratio(1, n))
                .collect();
            let area = ratatui::layout::Rect::new(0, 0, cols, rows);
            let chunks = ratatui::layout::Layout::default()
                .direction(dir)
                .constraints(constraints)
                .split(area);

            for (i, child) in children.iter().enumerate() {
                resize_node_ptys(child, chunks[i].width, chunks[i].height, pty_mgr);
            }
        }
    }
}
