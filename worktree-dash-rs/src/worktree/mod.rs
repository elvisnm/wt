use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};

use crate::config::Config;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum WorktreeType {
    Docker,
    Local,
}

#[derive(Debug, Clone)]
pub struct Worktree {
    pub path: PathBuf,
    pub name: String,
    pub wt_type: WorktreeType,
    pub alias: String,
    pub container: String,
    pub branch: String,

    // Docker-specific
    pub domain: String,
    pub lan_domain: String,
    pub db_name: String,
    pub offset: i32,
    pub ports: HashMap<String, i32>,

    // Local worktree isolation
    pub isolated_pm2: bool,

    // Runtime state
    pub running: bool,
    pub container_exists: bool,
    pub health: String,
    pub started: String,
    pub uptime: String,
    pub cpu: String,
    pub mem: String,
    pub mem_pct: String,
    pub build_path: String,
}

impl Worktree {
    pub fn pm2_home(&self) -> PathBuf {
        self.path.join(".pm2")
    }
}

#[derive(Debug, Clone)]
pub struct Service {
    pub name: String,
    pub display_name: String,
    pub status: String,
    pub memory: i64,
    pub cpu: f64,
    pub restart_count: i32,
}

/// Scan the worktrees directory and return all found worktrees.
pub fn discover(worktrees_dir: &Path, existing: &[Worktree], cfg: Option<&Config>) -> Vec<Worktree> {
    let entries = match fs::read_dir(worktrees_dir) {
        Ok(e) => e,
        Err(_) => return Vec::new(),
    };

    let existing_map: HashMap<&Path, &Worktree> = existing
        .iter()
        .map(|wt| (wt.path.as_path(), wt))
        .collect();

    let traefik_port_map = build_traefik_port_map(cfg);

    let mut results = Vec::new();

    for entry in entries.flatten() {
        let full_path = entry.path();
        if !full_path.is_dir() {
            continue;
        }

        let name = entry.file_name().to_string_lossy().to_string();
        let env_filename = cfg
            .and_then(|c| {
                if c.env.filename.is_empty() {
                    None
                } else {
                    Some(c.env.filename.as_str())
                }
            })
            .unwrap_or(".env.worktree");

        let has_compose = full_path.join("docker-compose.worktree.yml").exists();
        let has_env = full_path.join(env_filename).exists();
        let has_pm2_home = full_path.join(".pm2").exists();
        let has_git = full_path.join(".git").exists();

        // Determine worktree type
        let mut has_docker = has_compose;
        if has_env {
            if let Some(wt_type) = read_env_file(&full_path, env_filename, "WORKTREE_TYPE") {
                has_docker = wt_type == "docker";
            }
        }

        if !has_docker && !has_git && !has_env {
            continue;
        }

        let branch = read_branch(&full_path);

        if !has_docker {
            let mut wt = Worktree {
                path: full_path.clone(),
                name: name.clone(),
                wt_type: WorktreeType::Local,
                alias: name.clone(),
                branch,
                isolated_pm2: has_pm2_home,
                container: String::new(),
                domain: String::new(),
                lan_domain: String::new(),
                db_name: String::new(),
                offset: 0,
                ports: HashMap::new(),
                running: false,
                container_exists: false,
                health: String::new(),
                started: String::new(),
                uptime: String::new(),
                cpu: String::new(),
                mem: String::new(),
                mem_pct: String::new(),
                build_path: String::new(),
            };

            if has_env {
                let alias_var = cfg
                    .and_then(|c| c.worktree_var("alias"))
                    .unwrap_or("WORKTREE_ALIAS");
                if let Some(a) = read_env_file(&full_path, env_filename, alias_var) {
                    wt.alias = a;
                }
                wt.offset = read_offset(&full_path, cfg);
                if let Some(c) = cfg {
                    if let Some(primary_port) = c.primary_port() {
                        let app_port = primary_port + wt.offset;
                        if let Some(routes) = traefik_port_map.get(&app_port) {
                            wt.domain = resolve_traefik_domain(routes, &wt.alias);
                        }
                    }
                }
            }

            // Preserve runtime state from previous discovery
            if let Some(prev) = existing_map.get(full_path.as_path()) {
                wt.running = prev.running;
                wt.cpu = prev.cpu.clone();
                wt.mem = prev.mem.clone();
                wt.uptime = prev.uptime.clone();
                wt.build_path = prev.build_path.clone();
            }

            results.push(wt);
            continue;
        }

        // Docker worktree
        let alias_var = cfg
            .and_then(|c| c.worktree_var("alias"))
            .unwrap_or("WORKTREE_ALIAS");
        let lan_domain_var = cfg.and_then(|c| c.env_var("lanDomain")).unwrap_or("");
        let db_conn_var = cfg.and_then(|c| c.env_var("dbConnection")).unwrap_or("");

        let alias = read_env_file(&full_path, env_filename, alias_var)
            .unwrap_or_else(|| name.clone());

        let container = read_container_name(&full_path).unwrap_or_else(|| {
            let is_shared_compose = cfg
                .is_some_and(|c| c.docker.compose_strategy != "generate" && c.compose_file_abs.is_some());

            if is_shared_compose {
                let c = cfg.expect("cfg checked by is_some_and above");
                let slug = read_env_file(&full_path, env_filename, "BRANCH_SLUG")
                    .unwrap_or_else(|| alias.clone());
                let primary = if !c.services.primary.is_empty() {
                    c.services.primary.clone()
                } else {
                    c.services.ports.keys().next().cloned().unwrap_or_default()
                };
                format!("{}-{}-{}", c.name, slug, primary)
            } else if let Some(c) = cfg {
                c.container_name(&name)
            } else {
                name.clone()
            }
        });

        let offset = read_offset(&full_path, cfg);
        let lan_domain = if lan_domain_var.is_empty() {
            String::new()
        } else {
            read_env_file(&full_path, env_filename, lan_domain_var).unwrap_or_default()
        };

        let app_port = cfg.and_then(|c| c.primary_port()).map(|p| p + offset);

        let container_prefix = cfg.map(|c| c.container_prefix()).unwrap_or_default();
        let container_alias = container.strip_prefix(&container_prefix).unwrap_or(&container);

        let domain = if !lan_domain.is_empty() {
            lan_domain.clone()
        } else if let Some(port) = app_port {
            if let Some(routes) = traefik_port_map.get(&port) {
                let d = resolve_traefik_domain(routes, container_alias);
                if d.is_empty() {
                    cfg.and_then(|c| c.domain_for(&alias)).unwrap_or_default()
                } else {
                    d
                }
            } else {
                cfg.and_then(|c| c.domain_for(&alias)).unwrap_or_default()
            }
        } else {
            cfg.and_then(|c| c.domain_for(&alias)).unwrap_or_default()
        };

        let mongo_url = if db_conn_var.is_empty() {
            None
        } else {
            read_env_file(&full_path, env_filename, db_conn_var)
        };

        let db_name = if let Some(ref url) = mongo_url {
            let parts: Vec<&str> = url.split('/').collect();
            let last = parts.last().unwrap_or(&"");
            if last.is_empty() {
                cfg.and_then(|c| c.database.as_ref())
                    .map(|db| {
                        if db.default_db.is_empty() {
                            "db".to_string()
                        } else {
                            db.default_db.clone()
                        }
                    })
                    .unwrap_or_else(|| "db".to_string())
            } else {
                last.to_string()
            }
        } else {
            cfg.and_then(|c| c.db_name(&alias))
                .unwrap_or_else(|| {
                    let safe: String = alias
                        .chars()
                        .map(|c| if c.is_ascii_alphanumeric() || c == '_' { c } else { '_' })
                        .collect();
                    format!("db_{}", safe)
                })
        };

        let mut wt = Worktree {
            path: full_path.clone(),
            name,
            wt_type: WorktreeType::Docker,
            alias,
            container,
            branch,
            domain,
            lan_domain,
            db_name,
            offset,
            ports: HashMap::new(),
            isolated_pm2: false,
            running: false,
            container_exists: false,
            health: String::new(),
            started: String::new(),
            uptime: String::new(),
            cpu: String::new(),
            mem: String::new(),
            mem_pct: String::new(),
            build_path: String::new(),
        };

        // Preserve runtime state from previous discovery
        if let Some(prev) = existing_map.get(full_path.as_path()) {
            wt.running = prev.running;
            wt.container_exists = prev.container_exists;
            wt.health = prev.health.clone();
            wt.started = prev.started.clone();
            wt.uptime = prev.uptime.clone();
            wt.cpu = prev.cpu.clone();
            wt.mem = prev.mem.clone();
            wt.mem_pct = prev.mem_pct.clone();
            wt.build_path = prev.build_path.clone();
        }

        results.push(wt);
    }

    results
}

/// Compute the worktrees directory from a repo root.
pub fn resolve_worktrees_dir(repo_root: &Path, cfg: Option<&Config>) -> PathBuf {
    if let Some(c) = cfg {
        if c.worktrees_dir_abs != PathBuf::new() {
            return c.worktrees_dir_abs.clone();
        }
    }
    let project_name = repo_root.file_name().unwrap_or_default().to_string_lossy();
    repo_root
        .parent()
        .unwrap_or(repo_root)
        .join(format!("{}-worktrees", project_name))
}

/// Sort worktrees: running docker > running local > stopped docker > stopped local > no-container
pub fn sort_worktrees(worktrees: &mut Vec<Worktree>) {
    worktrees.sort_by(|a, b| {
        fn rank(wt: &Worktree) -> u8 {
            match (&wt.wt_type, wt.running, wt.container_exists) {
                (WorktreeType::Docker, true, _) => 0,
                (WorktreeType::Local, true, _) => 1,
                (WorktreeType::Docker, false, true) => 2,
                (WorktreeType::Local, false, _) => 3,
                _ => 4,
            }
        }
        rank(a).cmp(&rank(b))
    });
}

// ── Internal helpers ────────────────────────────────────────────────────

fn read_env_file(worktree_path: &Path, filename: &str, key: &str) -> Option<String> {
    if key.is_empty() {
        return None;
    }
    let env_path = worktree_path.join(filename);
    let content = fs::read_to_string(env_path).ok()?;
    let prefix = format!("{}=", key);
    for line in content.lines() {
        let trimmed = line.trim();
        if let Some(val) = trimmed.strip_prefix(&prefix) {
            let val = val.trim();
            if !val.is_empty() {
                return Some(val.to_string());
            }
        }
    }
    None
}

fn read_container_name(worktree_path: &Path) -> Option<String> {
    let compose_path = worktree_path.join("docker-compose.worktree.yml");
    let content = fs::read_to_string(compose_path).ok()?;
    let re = regex_lite::Regex::new(r"container_name:\s*(\S+)").ok()?;
    re.captures(&content).map(|c| c[1].to_string())
}

fn read_offset(worktree_path: &Path, cfg: Option<&Config>) -> i32 {
    let env_filename = cfg
        .and_then(|c| {
            if c.env.filename.is_empty() {
                None
            } else {
                Some(c.env.filename.as_str())
            }
        })
        .unwrap_or(".env.worktree");

    let offset_var = cfg
        .and_then(|c| c.worktree_var("portOffset"))
        .unwrap_or("WORKTREE_PORT_OFFSET");
    let base_var = cfg
        .and_then(|c| c.worktree_var("portBase"))
        .unwrap_or("WORKTREE_PORT_BASE");

    let port_base = cfg
        .map(|c| {
            c.services
                .ports
                .values()
                .copied()
                .min()
                .unwrap_or(3000)
        })
        .unwrap_or(3000);

    let primary_port = cfg.and_then(|c| c.primary_port()).unwrap_or(3001);

    // Try reading from env file
    let env_path = worktree_path.join(env_filename);
    if let Ok(content) = fs::read_to_string(&env_path) {
        // Try offset var
        if let Some(val) = parse_env_int(&content, offset_var) {
            return val;
        }
        // Try base var
        if let Some(val) = parse_env_int(&content, base_var) {
            return val - port_base;
        }
    }

    // Try reading from compose file
    let compose_path = worktree_path.join("docker-compose.worktree.yml");
    if let Ok(content) = fs::read_to_string(compose_path) {
        let pattern = format!(r#""(\d+):{}""#, primary_port);
        if let Ok(re) = regex_lite::Regex::new(&pattern) {
            if let Some(caps) = re.captures(&content) {
                if let Ok(v) = caps[1].parse::<i32>() {
                    if v != primary_port {
                        return v - primary_port;
                    }
                }
            }
        }
    }

    0
}

fn parse_env_int(content: &str, key: &str) -> Option<i32> {
    let prefix = format!("{}=", key);
    for line in content.lines() {
        let trimmed = line.trim();
        if let Some(val) = trimmed.strip_prefix(&prefix) {
            return val.trim().parse().ok();
        }
    }
    None
}

fn read_branch(worktree_path: &Path) -> String {
    let git_path = worktree_path.join(".git");
    let content = match fs::read_to_string(&git_path) {
        Ok(c) => c,
        Err(_) => return String::new(),
    };

    let gitdir = content
        .lines()
        .find_map(|line| line.strip_prefix("gitdir:").map(|s| s.trim().to_string()));

    let gitdir = match gitdir {
        Some(g) => {
            if Path::new(&g).is_absolute() {
                PathBuf::from(g)
            } else {
                worktree_path.join(g)
            }
        }
        None => return String::new(),
    };

    let head_path = gitdir.join("HEAD");
    let head_content = match fs::read_to_string(head_path) {
        Ok(c) => c.trim().to_string(),
        Err(_) => return String::new(),
    };

    if let Some(branch) = head_content.strip_prefix("ref: refs/heads/") {
        branch.to_string()
    } else if head_content.len() >= 8 {
        head_content[..8].to_string()
    } else {
        head_content
    }
}

struct TraefikRoute {
    domain: String,
    name: String,
}

fn build_traefik_port_map(cfg: Option<&Config>) -> HashMap<i32, Vec<TraefikRoute>> {
    let traefik_dir = match cfg {
        Some(c) => match &c.proxy_dynamic_dir {
            Some(d) => d.clone(),
            None => return HashMap::new(),
        },
        None => return HashMap::new(),
    };

    let entries = match fs::read_dir(&traefik_dir) {
        Ok(e) => e,
        Err(_) => return HashMap::new(),
    };

    let host_re = regex_lite::Regex::new(r"Host\(`([^`]+)`\)").expect("invalid host regex");
    let port_re = regex_lite::Regex::new(r"host\.docker\.internal:(\d+)").expect("invalid port regex");

    let mut result: HashMap<i32, Vec<TraefikRoute>> = HashMap::new();

    for entry in entries.flatten() {
        let path = entry.path();
        let file_name = entry.file_name().to_string_lossy().to_string();
        if !file_name.ends_with(".yml") || path.is_dir() {
            continue;
        }

        let content = match fs::read_to_string(&path) {
            Ok(c) => c,
            Err(_) => continue,
        };

        let host = match host_re.captures(&content) {
            Some(c) => c[1].to_string(),
            None => continue,
        };

        let port: i32 = match port_re.captures(&content) {
            Some(c) => match c[1].parse() {
                Ok(p) => p,
                Err(_) => continue,
            },
            None => continue,
        };

        let name = file_name.strip_suffix(".yml").unwrap_or(&file_name).to_string();
        result
            .entry(port)
            .or_default()
            .push(TraefikRoute { domain: host, name });
    }

    result
}

fn resolve_traefik_domain(routes: &[TraefikRoute], alias: &str) -> String {
    if routes.is_empty() {
        return String::new();
    }
    if routes.len() == 1 {
        return routes[0].domain.clone();
    }
    for r in routes {
        if r.name != alias {
            return r.domain.clone();
        }
    }
    routes[0].domain.clone()
}
