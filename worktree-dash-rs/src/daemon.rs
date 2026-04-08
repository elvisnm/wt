use std::fs;
use std::io;
use std::os::unix::process::CommandExt;
use std::path::{Path, PathBuf};
use std::process::Command;

const WT_DIR: &str = ".wt";
const PID_FILE: &str = "dev.pid";
const LOG_FILE: &str = "dev.log";

fn wt_dir(worktree_path: &Path) -> PathBuf {
    let dir = worktree_path.join(WT_DIR);
    let _ = fs::create_dir_all(&dir);
    dir
}

pub fn pid_path(worktree_path: &Path) -> PathBuf {
    wt_dir(worktree_path).join(PID_FILE)
}

pub fn log_path(worktree_path: &Path) -> PathBuf {
    wt_dir(worktree_path).join(LOG_FILE)
}

/// Start a command as a daemon process.
/// Creates a new process group (setsid), writes PID to .wt/dev.pid,
/// pipes stdout/stderr to .wt/dev.log.
pub fn start(worktree_path: &Path, cmd: &str) -> io::Result<u32> {
    let wt = wt_dir(worktree_path);
    let log = wt.join(LOG_FILE);
    let pid_file = wt.join(PID_FILE);

    let log_out = fs::File::create(&log)?;
    let log_err = log_out.try_clone()?;

    let mut command = Command::new("sh");
    command
        .args(["-c", cmd])
        .current_dir(worktree_path)
        .stdout(log_out)
        .stderr(log_err)
        .stdin(std::process::Stdio::null());

    // Create a new session (process group) so we can kill the whole tree
    unsafe {
        command.pre_exec(|| {
            libc::setsid();
            Ok(())
        });
    }

    let child = command.spawn()?;
    let pid = child.id();
    fs::write(&pid_file, pid.to_string())?;

    Ok(pid)
}

/// Read the PID from .wt/dev.pid.
pub fn read_pid(worktree_path: &Path) -> Option<u32> {
    let pid_file = worktree_path.join(WT_DIR).join(PID_FILE);
    fs::read_to_string(pid_file).ok()?.trim().parse().ok()
}

/// Check if a process is alive.
fn is_alive(pid: u32) -> bool {
    unsafe { libc::kill(pid as i32, 0) == 0 }
}

/// Check if a worktree's dev process is running.
pub fn is_running(worktree_path: &Path) -> bool {
    match read_pid(worktree_path) {
        Some(pid) => {
            if is_alive(pid) {
                true
            } else {
                // Stale PID file — clean up
                let _ = fs::remove_file(worktree_path.join(WT_DIR).join(PID_FILE));
                false
            }
        }
        None => false,
    }
}

/// Stop the daemon by killing its process group.
pub fn stop(worktree_path: &Path) -> bool {
    let pid = match read_pid(worktree_path) {
        Some(p) => p,
        None => return false,
    };

    // Kill the process group (negative PID)
    let killed = unsafe { libc::kill(-(pid as i32), libc::SIGTERM) == 0 };

    // Fallback: kill just the process
    if !killed {
        unsafe { libc::kill(pid as i32, libc::SIGTERM); }
    }

    // Clean up PID file
    let _ = fs::remove_file(worktree_path.join(WT_DIR).join(PID_FILE));
    true
}

/// Stop all running worktree daemons. Called on dashboard exit.
pub fn stop_all(worktree_paths: &[PathBuf]) {
    for path in worktree_paths {
        if is_running(path) {
            stop(path);
        }
    }
}

/// Check if a specific port is listening.
pub fn is_port_listening(port: i32) -> bool {
    if port <= 0 {
        return false;
    }
    Command::new("lsof")
        .args(["-i", &format!(":{}", port), "-sTCP:LISTEN", "-P", "-n"])
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}
