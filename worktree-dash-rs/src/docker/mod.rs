use std::collections::HashMap;

use crate::cmd::{run_cmd, parse_json_lines};
use crate::config::Config;
use crate::worktree::{Service, Worktree, WorktreeType};

/// Update running state, health, and uptime for all docker worktrees.
pub fn fetch_container_status(worktrees: &mut [Worktree], cfg: Option<&Config>) {
    let mut args = vec!["ps", "-a", "--format", "json"];
    let filter;
    if let Some(c) = cfg {
        filter = c.container_filter();
        args = vec!["ps", "-a", "--filter", &filter, "--format", "json"];
    }

    let raw = match run_cmd("docker", &args) {
        Ok(r) => r,
        Err(_) => return,
    };

    let containers = parse_json_lines(&raw);

    let wd_re = regex_lite::Regex::new(
        r"com\.docker\.compose\.project\.working_dir=([^,]+)",
    )
    .expect("invalid workdir regex");

    let mut by_name: HashMap<&str, &serde_json::Value> = HashMap::new();
    let mut by_workdir: HashMap<String, &serde_json::Value> = HashMap::new();

    for c in &containers {
        let name = get_str(c, &["Names", "names"]);
        if !name.is_empty() {
            by_name.insert(name, c);
        }
        let labels = get_str(c, &["Labels", "labels"]);
        if let Some(caps) = wd_re.captures(labels) {
            by_workdir.insert(caps[1].to_string(), c);
        }
    }

    for wt in worktrees.iter_mut() {
        if wt.wt_type != WorktreeType::Docker {
            continue;
        }

        let path_str = wt.path.to_string_lossy().to_string();
        let mut matched: Option<&serde_json::Value> = None;

        // Match by workdir label
        if let Some(c) = by_workdir.get(&path_str) {
            matched = Some(c);
        }
        // Match by container name
        if matched.is_none() {
            if let Some(c) = by_name.get(wt.container.as_str()) {
                matched = Some(c);
            }
        }
        // Match by config-derived name
        if matched.is_none() {
            if let Some(c) = cfg {
                let derived = c.container_name(&wt.alias);
                if let Some(val) = by_name.get(derived.as_str()) {
                    matched = Some(val);
                }
                if matched.is_none() {
                    let derived2 = c.container_name(&wt.name);
                    if let Some(val) = by_name.get(derived2.as_str()) {
                        matched = Some(val);
                    }
                }
            }
        }
        // Shared compose prefix matching
        if matched.is_none() {
            if let Some(c) = cfg {
                if c.docker.compose_strategy != "generate" {
                    let slug_prefix = format!("{}-{}-", c.name, wt.alias);
                    for (name, val) in &by_name {
                        if name.starts_with(&slug_prefix) {
                            matched = Some(val);
                            break;
                        }
                    }
                }
            }
        }

        let Some(m) = matched else {
            wt.running = false;
            wt.container_exists = false;
            wt.health.clear();
            wt.started.clear();
            wt.uptime.clear();
            continue;
        };

        let matched_name = get_str(m, &["Names", "names"]);
        if !matched_name.is_empty() && matched_name != wt.container {
            wt.container = matched_name.to_string();
        }

        wt.container_exists = true;
        let state = get_str(m, &["State", "state"]).to_lowercase();
        wt.running = state == "running";

        let status = get_str(m, &["Status", "status"]);
        if status.contains("healthy") {
            wt.health = "healthy".to_string();
        } else if status.contains("starting") {
            wt.health = "starting".to_string();
        } else {
            wt.health.clear();
        }
    }

    // Inspect running containers for detailed health/uptime
    for wt in worktrees.iter_mut() {
        if wt.wt_type != WorktreeType::Docker || !wt.running {
            continue;
        }

        let Ok(inspect_raw) = run_cmd("docker", &["inspect", "--format", "json", &wt.container])
        else {
            continue;
        };

        let Ok(parsed) = serde_json::from_str::<Vec<serde_json::Value>>(&inspect_raw) else {
            continue;
        };
        let Some(info) = parsed.first() else {
            continue;
        };

        if let Some(state) = info.get("State") {
            if let Some(health) = state.get("Health") {
                if let Some(s) = health.get("Status").and_then(|v| v.as_str()) {
                    wt.health = s.to_string();
                }
            }
            if let Some(started) = state.get("StartedAt").and_then(|v| v.as_str()) {
                wt.started = started.to_string();
                wt.uptime = format_uptime(started);
            }
        }
    }
}

/// Update CPU and memory stats for all docker worktrees.
pub fn fetch_container_stats(worktrees: &mut [Worktree], cfg: Option<&Config>) {
    let Ok(raw) = run_cmd("docker", &["stats", "--no-stream", "--format", "json"]) else {
        return;
    };

    let prefix = cfg.map(|c| c.container_prefix()).unwrap_or_default();
    let stats = parse_json_lines(&raw);

    let mut stats_map: HashMap<String, &serde_json::Value> = HashMap::new();
    for s in &stats {
        let name = get_str(s, &["Name", "name"]);
        if !name.is_empty() && name.starts_with(&prefix) {
            stats_map.insert(name.to_string(), s);
        }
    }

    for wt in worktrees.iter_mut() {
        if let Some(s) = stats_map.get(&wt.container) {
            wt.cpu = get_str(s, &["CPUPerc"]).to_string();
            let mem_usage = get_str(s, &["MemUsage"]);
            if let Some(used) = mem_usage.split('/').next() {
                wt.mem = used.trim().to_string();
            }
            wt.mem_pct = get_str(s, &["MemPerc"]).to_string();
        } else if !wt.running {
            wt.cpu.clear();
            wt.mem.clear();
            wt.mem_pct.clear();
        }
    }
}

/// Fetch PM2 services from inside a docker container.
pub fn fetch_services(container: &str, wt_name: &str) -> Vec<Service> {
    if container.is_empty() {
        return Vec::new();
    }

    let Ok(raw) = run_cmd("docker", &["exec", container, "pm2", "jlist"]) else {
        return Vec::new();
    };

    let Ok(procs) = serde_json::from_str::<Vec<serde_json::Value>>(&raw) else {
        return Vec::new();
    };

    let suffix = if wt_name.is_empty() {
        String::new()
    } else {
        format!("-{}", wt_name)
    };

    let mut services = vec![Service {
        name: "__all".to_string(),
        display_name: "All services".to_string(),
        status: "online".to_string(),
        memory: 0,
        cpu: 0.0,
        restart_count: 0,
    }];

    for proc in &procs {
        let name = proc.get("name").and_then(|v| v.as_str()).unwrap_or("");
        if name.is_empty() {
            continue;
        }

        let display = if !suffix.is_empty() && name.ends_with(&suffix) {
            &name[..name.len() - suffix.len()]
        } else {
            name
        };

        let mut svc = Service {
            name: name.to_string(),
            display_name: display.to_string(),
            status: String::new(),
            memory: 0,
            cpu: 0.0,
            restart_count: 0,
        };

        if let Some(env) = proc.get("pm2_env") {
            svc.status = env
                .get("status")
                .and_then(|v| v.as_str())
                .unwrap_or("")
                .to_string();
            svc.restart_count = env
                .get("restart_time")
                .and_then(|v| v.as_f64())
                .unwrap_or(0.0) as i32;
        }

        if let Some(monit) = proc.get("monit") {
            svc.memory = monit.get("memory").and_then(|v| v.as_f64()).unwrap_or(0.0) as i64;
            svc.cpu = monit.get("cpu").and_then(|v| v.as_f64()).unwrap_or(0.0);
        }

        services.push(svc);
    }

    services
}

fn format_uptime(started_at: &str) -> String {
    use chrono::{DateTime, Utc};
    let Ok(t) = started_at.parse::<DateTime<Utc>>() else {
        return String::new();
    };
    let diff = Utc::now() - t;
    let mins = diff.num_minutes();
    if mins < 0 {
        return String::new();
    }
    if mins < 60 {
        return format!("{}m", mins);
    }
    let hours = mins / 60;
    if hours < 24 {
        return format!("{}h {}m", hours, mins % 60);
    }
    let days = hours / 24;
    format!("{}d {}h", days, hours % 24)
}

fn get_str<'a>(val: &'a serde_json::Value, keys: &[&str]) -> &'a str {
    for key in keys {
        if let Some(s) = val.get(key).and_then(|v| v.as_str()) {
            return s;
        }
    }
    ""
}
