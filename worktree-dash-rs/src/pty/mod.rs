pub mod split;

use std::collections::HashMap;
use std::io::Read;
use std::sync::{Arc, Mutex};
use std::thread;

use alacritty_terminal::event::{Event as TermEvent, EventListener};
use alacritty_terminal::grid::Dimensions;
use alacritty_terminal::term::Config as TermConfig;

struct TermSize {
    cols: usize,
    lines: usize,
}

impl TermSize {
    fn new(cols: u16, rows: u16) -> Self {
        Self { cols: cols as usize, lines: rows as usize }
    }
}

impl Dimensions for TermSize {
    fn total_lines(&self) -> usize { self.lines }
    fn screen_lines(&self) -> usize { self.lines }
    fn columns(&self) -> usize { self.cols }
}
use alacritty_terminal::vte::ansi::Processor;
use alacritty_terminal::Term;
use portable_pty::{CommandBuilder, PtySize, native_pty_system};

/// A single PTY session — a running process with a virtual terminal grid.
pub struct PtySession {
    pub id: usize,
    pub label: String,
    pub worktree_alias: String,
    pub worktree_dir: String,
    pub alive: bool,
    /// Exit code of the process (set when check_alive detects exit).
    pub exit_code: Option<u32>,

    /// PTY master (kept for resize — sends SIGWINCH to child)
    master: Box<dyn portable_pty::MasterPty + Send>,
    /// PTY master writer (for sending input)
    writer: Box<dyn std::io::Write + Send>,
    /// Handle to the child process
    child: Box<dyn portable_pty::Child + Send>,
    /// Terminal grid + VTE processor — shared with the reader thread
    term: Arc<Mutex<Term<PtyEventListener>>>,
}

#[derive(Clone)]
pub(crate) struct PtyEventListener;

impl EventListener for PtyEventListener {
    fn send_event(&self, _event: TermEvent) {}
}

impl PtySession {
    pub fn spawn(
        id: usize,
        label: String,
        cmd: &str,
        args: &[&str],
        cwd: &str,
        worktree_alias: String,
        worktree_dir: String,
        cols: u16,
        rows: u16,
    ) -> anyhow::Result<Self> {
        let pty_system = native_pty_system();

        let pty_pair = pty_system.openpty(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        })?;

        let mut command = CommandBuilder::new(cmd);
        command.args(args);
        if !cwd.is_empty() {
            command.cwd(cwd);
        }

        let child = pty_pair.slave.spawn_command(command)?;

        // Create alacritty_terminal for ANSI parsing
        // (columns, screen_lines) implements Dimensions
        let dimensions = TermSize::new(cols, rows);
        let mut term_config = TermConfig::default();
        term_config.scrolling_history = 500;
        let term = Term::new(term_config, &dimensions, PtyEventListener);
        let term = Arc::new(Mutex::new(term));

        // Reader thread: PTY output → VTE processor → alacritty_terminal grid
        let mut reader = pty_pair.master.try_clone_reader()?;
        let term_clone = Arc::clone(&term);
        thread::spawn(move || {
            let mut processor = Processor::<alacritty_terminal::vte::ansi::StdSyncHandler>::new();
            let mut buf = [0u8; 4096];
            loop {
                match reader.read(&mut buf) {
                    Ok(0) => break,
                    Ok(n) => {
                        let mut term = term_clone.lock().expect("terminal grid lock poisoned");
                        processor.advance(&mut *term, &buf[..n]);
                    }
                    Err(_) => break,
                }
            }
        });

        let writer = pty_pair.master.take_writer()?;

        Ok(Self {
            id,
            label,
            worktree_alias,
            worktree_dir,
            alive: true,
            exit_code: None,
            master: pty_pair.master,
            writer,
            child,
            term,
        })
    }

    /// Write input bytes to the PTY (keyboard input from user).
    pub fn write_input(&mut self, data: &[u8]) -> anyhow::Result<()> {
        use std::io::Write;
        self.writer.write_all(data)?;
        Ok(())
    }

    /// Resize the PTY (sends SIGWINCH to child process) and the terminal grid.
    pub fn resize(&mut self, cols: u16, rows: u16) -> anyhow::Result<()> {
        // Resize the actual PTY — this sends SIGWINCH to the child process
        self.master.resize(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        })?;

        // Resize the alacritty_terminal grid to match
        let mut term = self.term.lock().expect("terminal grid lock poisoned");
        let dimensions = TermSize::new(cols, rows);
        term.resize(dimensions);
        Ok(())
    }

    /// Check if the child process has exited. Captures exit code.
    pub fn check_alive(&mut self) -> bool {
        match self.child.try_wait() {
            Ok(Some(status)) => {
                self.alive = false;
                self.exit_code = Some(status.exit_code());
                false
            }
            Ok(None) => true,
            Err(_) => {
                self.alive = false;
                self.exit_code = Some(1); // assume failure on error
                false
            }
        }
    }

    /// Get a reference to the terminal grid (for rendering).
    pub(crate) fn term(&self) -> &Arc<Mutex<Term<PtyEventListener>>> {
        &self.term
    }

    /// Read the last N non-empty lines from the terminal grid.
    pub fn last_lines(&self, count: usize) -> Vec<String> {
        let term = self.term.lock().expect("terminal grid lock poisoned");
        let grid = term.grid();
        let cols = grid.columns();
        let lines = grid.screen_lines();

        let mut result: Vec<String> = Vec::new();
        for row in (0..lines).rev() {
            let mut line = String::new();
            for col in 0..cols {
                let cell = &grid[alacritty_terminal::index::Line(row as i32)][alacritty_terminal::index::Column(col)];
                if cell.c != '\0' {
                    line.push(cell.c);
                }
            }
            let trimmed = line.trim().to_string();
            if !trimmed.is_empty() {
                result.push(trimmed);
            }
            if result.len() >= count {
                break;
            }
        }
        result.reverse();
        result
    }
}

/// Manages all PTY sessions.
pub struct PtyManager {
    sessions: HashMap<usize, PtySession>,
    next_id: usize,
}

impl PtyManager {
    pub fn new() -> Self {
        Self {
            sessions: HashMap::new(),
            next_id: 1,
        }
    }

    pub fn spawn(
        &mut self,
        label: String,
        cmd: &str,
        args: &[&str],
        cwd: &str,
        worktree_alias: String,
        worktree_dir: String,
        cols: u16,
        rows: u16,
    ) -> anyhow::Result<usize> {
        let id = self.next_id;
        self.next_id += 1;

        let session = PtySession::spawn(
            id, label, cmd, args, cwd,
            worktree_alias, worktree_dir,
            cols, rows,
        )?;
        self.sessions.insert(id, session);
        Ok(id)
    }

    pub fn get(&self, id: usize) -> Option<&PtySession> {
        self.sessions.get(&id)
    }

    pub fn get_mut(&mut self, id: usize) -> Option<&mut PtySession> {
        self.sessions.get_mut(&id)
    }

    pub fn remove(&mut self, id: usize) -> Option<PtySession> {
        self.sessions.remove(&id)
    }

    pub fn session_ids(&self) -> Vec<usize> {
        let mut ids: Vec<_> = self.sessions.keys().copied().collect();
        ids.sort();
        ids
    }

    pub fn check_alive(&mut self) {
        for session in self.sessions.values_mut() {
            session.check_alive();
        }
    }
}
