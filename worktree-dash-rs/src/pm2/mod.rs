use std::collections::HashMap;

use crate::cmd::{run_cmd, run_cmd_env};
use crate::worktree::Service;

/// Fetch PM2 services on the host, filtering by worktree path (pm_cwd).
pub fn fetch_services(wt_path: &str) -> Vec<Service> {
    if wt_path.is_empty() {
        return Vec::new();
    }
    let procs = match fetch_procs("") {
        Some(p) => p,
        None => return Vec::new(),
    };
    parse_services(&procs, wt_path)
}

/// Fetch PM2 services with an isolated PM2_HOME (no pm_cwd filtering needed).
pub fn fetch_services_with_home(pm2_home: &str) -> Vec<Service> {
    if pm2_home.is_empty() {
        return Vec::new();
    }
    let procs = match fetch_procs(pm2_home) {
        Some(p) => p,
        None => return Vec::new(),
    };
    parse_services(&procs, "")
}

/// Check which worktree paths have at least one "online" PM2 process.
pub fn fetch_running_worktrees(wt_paths: &HashMap<String, String>) -> HashMap<String, bool> {
    if wt_paths.is_empty() {
        return HashMap::new();
    }
    let procs = match fetch_procs("") {
        Some(p) => p,
        None => return HashMap::new(),
    };

    let mut result = HashMap::new();
    for proc in &procs {
        let Some(env) = proc.get("pm2_env") else {
            continue;
        };
        let status = env.get("status").and_then(|v| v.as_str()).unwrap_or("");
        if status != "online" {
            continue;
        }
        let pm_cwd = env.get("pm_cwd").and_then(|v| v.as_str()).unwrap_or("");
        if pm_cwd.is_empty() {
            continue;
        }
        for (path, name) in wt_paths {
            if pm_cwd.starts_with(path.as_str()) {
                result.insert(name.clone(), true);
                break;
            }
        }
    }
    result
}

/// Query an isolated PM2 daemon for aggregate status.
pub fn status_with_home(pm2_home: &str) -> (bool, String, String) {
    let procs = match fetch_procs(pm2_home) {
        Some(p) => p,
        None => return (false, String::new(), String::new()),
    };

    let mut running = false;
    let mut total_cpu: f64 = 0.0;
    let mut total_mem: i64 = 0;

    for proc in &procs {
        if let Some(env) = proc.get("pm2_env") {
            if env.get("status").and_then(|v| v.as_str()) == Some("online") {
                running = true;
            }
        }
        if let Some(monit) = proc.get("monit") {
            total_cpu += monit.get("cpu").and_then(|v| v.as_f64()).unwrap_or(0.0);
            total_mem += monit.get("memory").and_then(|v| v.as_f64()).unwrap_or(0.0) as i64;
        }
    }

    let (cpu, mem) = if total_cpu > 0.0 || total_mem > 0 {
        let mem_mb = total_mem / 1024 / 1024;
        let mem_str = if mem_mb >= 1024 {
            format!("{:.1}GB", mem_mb as f64 / 1024.0)
        } else {
            format!("{}MB", mem_mb)
        };
        (format!("{:.1}%", total_cpu), mem_str)
    } else {
        (String::new(), String::new())
    };

    (running, cpu, mem)
}

/// Start PM2 with an ecosystem config file.
pub fn start(
    pm2_home: &str,
    ecosystem_config: &str,
    _cwd: &str,
    extra_env: &[(&str, &str)],
) -> Result<String, std::io::Error> {
    let mut env: Vec<(&str, &str)> = extra_env.to_vec();
    if !pm2_home.is_empty() {
        env.push(("PM2_HOME", pm2_home));
    }
    run_cmd_env(&env, "pm2", &["start", ecosystem_config])
}

/// Kill the PM2 daemon for an isolated worktree.
pub fn kill(pm2_home: &str) -> Result<String, std::io::Error> {
    if pm2_home.is_empty() {
        return Err(std::io::Error::new(
            std::io::ErrorKind::InvalidInput,
            "pm2_home required for kill",
        ));
    }
    run_cmd_env(&[("PM2_HOME", pm2_home)], "pm2", &["kill"])
}

fn fetch_procs(pm2_home: &str) -> Option<Vec<serde_json::Value>> {
    let raw = if !pm2_home.is_empty() {
        run_cmd_env(&[("PM2_HOME", pm2_home)], "pm2", &["jlist"]).ok()?
    } else {
        run_cmd("pm2", &["jlist"]).ok()?
    };

    serde_json::from_str(&raw).ok()
}

fn parse_services(procs: &[serde_json::Value], wt_path: &str) -> Vec<Service> {
    let mut services = vec![Service {
        name: "__all".to_string(),
        display_name: "All services".to_string(),
        status: "online".to_string(),
        memory: 0,
        cpu: 0.0,
        restart_count: 0,
    }];

    for proc in procs {
        let name = proc.get("name").and_then(|v| v.as_str()).unwrap_or("");
        if name.is_empty() {
            continue;
        }

        let env = proc.get("pm2_env");

        // Filter by pm_cwd if wt_path is set
        if !wt_path.is_empty() {
            let pm_cwd = env
                .and_then(|e| e.get("pm_cwd"))
                .and_then(|v| v.as_str())
                .unwrap_or("");
            if !pm_cwd.starts_with(wt_path) {
                continue;
            }
        }

        let status = env
            .and_then(|e| e.get("status"))
            .and_then(|v| v.as_str())
            .unwrap_or("");

        // Hide stopped processes (errored remain visible)
        if status == "stopped" {
            continue;
        }

        let restart_count = env
            .and_then(|e| e.get("restart_time"))
            .and_then(|v| v.as_f64())
            .unwrap_or(0.0) as i32;

        let memory = proc
            .get("monit")
            .and_then(|m| m.get("memory"))
            .and_then(|v| v.as_f64())
            .unwrap_or(0.0) as i64;

        let cpu = proc
            .get("monit")
            .and_then(|m| m.get("cpu"))
            .and_then(|v| v.as_f64())
            .unwrap_or(0.0);

        services.push(Service {
            name: name.to_string(),
            display_name: name.to_string(),
            status: status.to_string(),
            memory,
            cpu,
            restart_count,
        });
    }

    if services.len() == 1 {
        return Vec::new();
    }

    services
}
