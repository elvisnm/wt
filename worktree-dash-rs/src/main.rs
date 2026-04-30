#![allow(dead_code)]

mod app;
mod beads;
mod claude;
pub mod cmd;
mod heihei;
mod config;
mod daemon;
mod detect;
mod docker;
mod init;
mod perf;
mod pm2;
mod pty;
mod settings;
mod ui;
mod worktree;

use anyhow::Result;
use app::App;
use crossterm::{
    event::{self, Event},
    terminal::{disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen},
    ExecutableCommand,
};
use ratatui::prelude::*;
use std::io::stdout;

const VERSION: &str = env!("CARGO_PKG_VERSION");

pub fn is_dev_build() -> bool {
    std::env::args()
        .next()
        .and_then(|a| {
            std::path::Path::new(&a)
                .file_name()
                .map(|n| n.to_string_lossy().into_owned())
        })
        .map(|n| n.contains("wt-dev"))
        .unwrap_or(false)
}

pub fn version_string() -> String {
    if is_dev_build() {
        format!("{}+dev", VERSION)
    } else {
        VERSION.to_string()
    }
}

pub fn version_label() -> String {
    format!("Version {} - by @elvisnm", version_string())
}

fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().collect();
    let debug = args.iter().any(|a| a == "--debug");

    // Handle --version
    if args.iter().any(|a| a == "--version" || a == "version") {
        println!("wt {}", version_string());
        return Ok(());
    }

    let bin_name = args.first()
        .and_then(|a| std::path::Path::new(a).file_name())
        .map(|n| n.to_string_lossy().to_string())
        .unwrap_or_else(|| "wt".into());

    // Log path: /tmp/{bin_name}.log (per-binary, so each worktree build gets its own)
    let log_path = format!("/tmp/{}.log", bin_name);

    if debug {
        let _ = std::fs::remove_file(&log_path);

        let log_file = std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(&log_path)
            .expect("failed to open log file");

        tracing_subscriber::fmt()
            .with_writer(std::sync::Mutex::new(log_file))
            .with_env_filter("debug,alacritty_terminal=warn")
            .init();

        tracing::info!("{} starting (debug mode)", bin_name);
        eprintln!("Debug log: {}", log_path);
    }

    // Setup terminal
    enable_raw_mode()?;
    stdout().execute(EnterAlternateScreen)?;
    let backend = CrosstermBackend::new(stdout());
    let mut terminal = Terminal::new(backend)?;

    // Run app
    let result = run(&mut terminal, debug);

    // Restore terminal
    disable_raw_mode()?;
    let _ = stdout().execute(crossterm::event::DisableMouseCapture);
    stdout().execute(LeaveAlternateScreen)?;

    result
}

fn run(terminal: &mut Terminal<CrosstermBackend<std::io::Stdout>>, debug: bool) -> Result<()> {
    let mut app = App::new();
    app.debug = debug;

    // Initial size
    let size = terminal.size()?;
    app.handle_resize(size.width, size.height);

    // Show splash screen, then discover worktrees (or init wizard)
    terminal.draw(|frame| app.render(frame))?;
    std::thread::sleep(std::time::Duration::from_millis(800));

    if let Some(err) = app.cfg_load_error.clone() {
        // Config file exists but failed to load. Don't fire the wizard
        // (it would silently overwrite the user's broken file). Surface
        // the error so they can see why discovery is empty.
        app.push_toast("Config load failed", &err, ui::overlay::ToastKind::Error);
    } else if app.cfg.is_none() {
        app.start_init_wizard();
    } else {
        app.run_discovery();
        app.refresh_status();
    }
    if app.usage_visible {
        app.fetch_usage();
    }
    if app.tasks_visible {
        app.fetch_tasks();
    }

    let mut tick: u64 = 0;
    let mut mouse_enabled = false;
    let mut last_draw = std::time::Instant::now();

    loop {
        // Only redraw at ~30fps max, or immediately after events
        let now = std::time::Instant::now();
        if now.duration_since(last_draw) >= std::time::Duration::from_millis(33) {
            let _frame = perf::timed("frame.total", 25_000);
            {
                let _t = perf::timed("frame.draw", 15_000);
                terminal.draw(|frame| {
                    let size = frame.area();
                    app.last_frame_width = size.width;
                    app.last_frame_height = size.height;
                    app.render(frame);
                })?;
            }
            last_draw = now;

            // Sync PTY sizes
            {
                let _t = perf::timed("tick.resize_split_ptys", 2_000);
                app.resize_split_ptys();
            }

            // Periodic checks (every draw = ~30fps)
            tick += 1;
            app.spin_frame = (tick / 5) as usize;
            app.dismiss_expired_toasts(tick);
            {
                let _t = perf::timed("tick.check_pending", 2_000);
                app.check_pending_remove();
                app.check_pending_create();
                app.check_pending_build();
                app.check_pending_start();
            }
            {
                let _t = perf::timed("tick.poll_dag_fetch", 5_000);
                app.poll_dag_fetch();
            }
            {
                let _t = perf::timed("tick.poll_mutations", 5_000);
                app.poll_mutations();
            }

            // Update agent states every ~1 second (30 ticks at 30fps)
            if tick % 30 == 0 {
                let _t = perf::timed("tick.update_agent_states", 20_000);
                app.update_agent_states();
            }

            if app.heihei_active && crate::heihei::should_close() {
                app.heihei_active = false;
            }

            // Background refresh. Stats and status only hit Docker when the
            // project has Docker worktrees (gated inside the refresh fns),
            // so the interval mainly matters for Docker projects. 15s for
            // stats keeps `docker stats --no-stream`'s built-in 1s CPU
            // sample from dominating the main thread.
            if tick % 450 == 0 {
                let _t = perf::timed("tick.refresh_stats", 10_000);
                app.refresh_stats();
            }
            if tick % 300 == 0 {
                let _t = perf::timed("tick.refresh_status", 10_000);
                app.refresh_status();
            }
            if tick % 150 == 0 {
                let _t = perf::timed("tick.run_discovery", 10_000);
                app.run_discovery();
            }
            // Periodic refresh removed: the sync bd subprocess was blocking the
            // UI every 5s and causing perceptible lag during list navigation.
            // Task mutations (create / edit / close / delete) trigger explicit
            // refreshes via refresh_tasks_and_graph() instead.
            if tick % 300 == 0 && app.usage_visible {
                let _t = perf::timed("tick.fetch_usage", 10_000);
                app.fetch_usage();
            }
        }

        // Toggle mouse capture. Captures for focused PTYs (selection /
        // scroll) and for Widget tabs like the DAG graph (click to select
        // cards, click-drag to pan). Without this, Widget tabs would
        // never see Drag / Up events once the cursor leaves a PTY.
        let want_capture = app.terminal_focused || app.active_tab_is_widget();
        if want_capture && !mouse_enabled {
            let _ = stdout().execute(crossterm::event::EnableMouseCapture);
            mouse_enabled = true;
        } else if !want_capture && mouse_enabled {
            let _ = stdout().execute(crossterm::event::DisableMouseCapture);
            mouse_enabled = false;
        }

        // Drain ALL pending events before next draw (batching)
        let mut had_event = false;
        while event::poll(std::time::Duration::from_millis(if had_event { 0 } else { 10 }))? {
            had_event = true;
            match event::read()? {
                Event::Key(key) => {
                    app.handle_key(key);
                    if app.should_quit {
                        break;
                    }
                }
                Event::Resize(w, h) => {
                    app.handle_resize(w, h);
                }
                Event::Mouse(mouse) => {
                    app.handle_mouse(mouse);
                }
                _ => {}
            }
        }
        if app.should_quit {
            break;
        }
        if had_event {
            last_draw = std::time::Instant::now() - std::time::Duration::from_millis(50); // force redraw
        }
    }

    Ok(())
}
