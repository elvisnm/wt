#[cfg(test)]
#[path = "config_test.rs"]
mod tests;

use anyhow::{bail, Context, Result};
use serde::{Deserialize, Deserializer};
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::process::Command;

const CONFIG_FILENAME: &str = "wt.config.js";

/// Deserialize a String that may be null in JSON → empty String.
fn deserialize_null_string<'de, D: Deserializer<'de>>(d: D) -> std::result::Result<String, D::Error> {
    Option::<String>::deserialize(d).map(|o| o.unwrap_or_default())
}

/// Deserialize an i32 that may be null in JSON → 0.
fn deserialize_null_i32<'de, D: Deserializer<'de>>(d: D) -> std::result::Result<i32, D::Error> {
    Option::<i32>::deserialize(d).map(|o| o.unwrap_or_default())
}

/// Deserialize a bool that may be null in JSON → false.
fn deserialize_null_bool<'de, D: Deserializer<'de>>(d: D) -> std::result::Result<bool, D::Error> {
    Option::<bool>::deserialize(d).map(|o| o.unwrap_or_default())
}

/// Deserialize a HashMap<String, String> where values may be null → filters out nulls.
fn deserialize_nullable_string_map<'de, D: Deserializer<'de>>(d: D) -> std::result::Result<HashMap<String, String>, D::Error> {
    let raw: HashMap<String, Option<String>> = HashMap::deserialize(d)?;
    Ok(raw.into_iter().filter_map(|(k, v)| v.map(|val| (k, val))).collect())
}

/// Top-level wt configuration.
#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Config {
    pub name: String,
    #[serde(default)]
    pub stack: Option<String>,

    #[serde(default)]
    pub repo: RepoConfig,
    #[serde(default)]
    pub docker: DockerConfig,
    #[serde(default)]
    pub services: ServicesConfig,
    #[serde(default)]
    pub port_offset: PortOffsetConfig,
    pub database: Option<DatabaseConfig>,
    pub redis: Option<RedisConfig>,
    #[serde(default)]
    pub env: EnvConfig,
    #[serde(default)]
    pub features: FeaturesConfig,
    #[serde(default)]
    pub dash: DashConfig,
    #[serde(default)]
    pub git: GitConfig,
    #[serde(default)]
    pub paths: PathsConfig,

    // Resolved paths (not from JSON)
    #[serde(skip)]
    pub repo_root: PathBuf,
    #[serde(skip)]
    pub config_path: PathBuf,
    #[serde(skip)]
    pub worktrees_dir_abs: PathBuf,
    #[serde(skip)]
    pub compose_path_abs: Option<PathBuf>,
    #[serde(skip)]
    pub proxy_dynamic_dir: Option<PathBuf>,
    #[serde(skip)]
    pub compose_file_abs: Option<PathBuf>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RepoConfig {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub worktrees_dir: String,
    #[serde(default)]
    pub branch_prefixes: Vec<String>,
    #[serde(default)]
    pub base_refs: Vec<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DockerConfig {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub base_image: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub compose_strategy: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub compose_file: String,
    pub generate: Option<DockerGenConfig>,
    #[serde(default)]
    pub shared_infra: SharedInfraConfig,
    #[serde(default)]
    pub proxy: ProxyConfig,
    #[serde(default)]
    pub env_files: Vec<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DockerGenConfig {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub container_workdir: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub entrypoint: String,
    #[serde(default)]
    pub extra_mounts: Vec<String>,
    #[serde(default)]
    pub extra_env: HashMap<String, String>,
    #[serde(default)]
    pub override_files: Vec<OverrideFile>,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct OverrideFile {
    pub src: String,
    pub dst: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SharedInfraConfig {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub network: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub compose_path: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProxyConfig {
    #[serde(rename = "type", default, deserialize_with = "deserialize_null_string")]
    pub proxy_type: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub dynamic_dir: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub domain_template: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ServicesConfig {
    #[serde(default)]
    pub ports: HashMap<String, i32>,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub primary: String,
    #[serde(default)]
    pub quick_links: Vec<QuickLink>,
    #[serde(default)]
    pub pm2: PM2Config,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PM2Config {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub ecosystem_config: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct QuickLink {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub label: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub service: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub path_prefix: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PortOffsetConfig {
    #[serde(default = "default_algorithm")]
    pub algorithm: String,
    #[serde(default = "default_min")]
    pub min: i32,
    #[serde(default = "default_range")]
    pub range: i32,
    #[serde(default)]
    pub auto_resolve: bool,
}

fn default_algorithm() -> String { "sha256".to_string() }
fn default_min() -> i32 { 100 }
fn default_range() -> i32 { 2000 }

impl Default for PortOffsetConfig {
    fn default() -> Self {
        Self {
            algorithm: default_algorithm(),
            min: default_min(),
            range: default_range(),
            auto_resolve: false,
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DatabaseConfig {
    #[serde(rename = "type", default, deserialize_with = "deserialize_null_string")]
    pub db_type: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub host: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub container_host: String,
    #[serde(default, deserialize_with = "deserialize_null_i32")]
    pub port: i32,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub default_db: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub replica_set: String,
    #[serde(default = "default_db_name_prefix")]
    pub db_name_prefix: String,
}

fn default_db_name_prefix() -> String { "db_".to_string() }

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RedisConfig {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub container_host: String,
    #[serde(default, deserialize_with = "deserialize_null_i32")]
    pub port: i32,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct EnvConfig {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub prefix: String,
    #[serde(default = "default_env_filename")]
    pub filename: String,
    #[serde(default, deserialize_with = "deserialize_nullable_string_map")]
    pub vars: HashMap<String, String>,
    #[serde(default, deserialize_with = "deserialize_nullable_string_map")]
    pub worktree_vars: HashMap<String, String>,
}

fn default_env_filename() -> String { ".env.worktree".to_string() }

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct FeaturesConfig {
    #[serde(default)]
    pub lan: bool,
    #[serde(default)]
    pub autostop: bool,
    #[serde(default)]
    pub prune: bool,
    #[serde(default)]
    pub rebuild_base: bool,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DashConfig {
    #[serde(default)]
    pub commands: HashMap<String, DashCommand>,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub local_dev_command: String,
    #[serde(default)]
    pub build: Option<DashBuildConfig>,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub log_file: String,
    #[serde(default)]
    pub services: DashServicesConfig,
    #[serde(default)]
    pub lifecycle: Option<DashLifecycleConfig>,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct DashLifecycleConfig {
    #[serde(default)]
    pub docker: LifecycleCommands,
    #[serde(default)]
    pub local: LifecycleCommands,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct LifecycleCommands {
    #[serde(default)]
    pub start: Option<String>,
    #[serde(default)]
    pub stop: Option<String>,
    #[serde(default)]
    pub restart: Option<String>,
    #[serde(default)]
    pub logs: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct DashBuildConfig {
    /// Build command. {alias} is replaced with the worktree alias.
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub cmd: String,
    /// Install path for symlink. {alias} is replaced. e.g. /usr/local/bin/wt-{alias}
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub install: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct DashCommand {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub label: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub cmd: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DashServicesConfig {
    #[serde(default = "default_manager")]
    pub manager: String,
    #[serde(default)]
    pub list: Vec<DashServiceEntry>,
    #[serde(default = "default_running_check")]
    pub running_check: String,
    pub docker: Option<DashDockerSvc>,
}

fn default_manager() -> String { "pm2".to_string() }
fn default_running_check() -> String { "pm2".to_string() }

impl Default for DashServicesConfig {
    fn default() -> Self {
        Self {
            manager: default_manager(),
            list: Vec::new(),
            running_check: default_running_check(),
            docker: None,
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct DashServiceEntry {
    pub name: String,
    #[serde(default, deserialize_with = "deserialize_null_i32")]
    pub port: i32,
    #[serde(default)]
    pub processes: Vec<String>,
}

impl DashServiceEntry {
    pub fn base_processes(&self) -> Vec<String> {
        if !self.processes.is_empty() {
            self.processes.clone()
        } else {
            vec![self.name.clone()]
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct DashDockerSvc {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub manager: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PathsConfig {
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub flow_scripts: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub docker_overrides: String,
    #[serde(default, deserialize_with = "deserialize_null_string")]
    pub build_script: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct GitConfig {
    #[serde(default)]
    pub skip_worktree: Vec<String>,
}

// ── Finding the config ──────────────────────────────────────────────────

/// Walk upward from start_dir looking for wt.config.js.
/// Returns (config_path, repo_root) or None.
pub fn find_config(start_dir: &Path) -> Option<(PathBuf, PathBuf)> {
    let mut dir = start_dir.canonicalize().ok()?;

    loop {
        let candidate = dir.join(CONFIG_FILENAME);
        if candidate.exists() {
            return Some((candidate, dir));
        }

        if !dir.pop() {
            break;
        }
    }

    None
}

// ── Loading the config ──────────────────────────────────────────────────

/// Find and parse wt.config.js, resolve defaults and paths.
pub fn load(start_dir: &Path) -> Result<Config> {
    let (config_path, repo_root) = find_config(start_dir)
        .context(format!(
            "could not find {} in {} or any parent directory",
            CONFIG_FILENAME,
            start_dir.display()
        ))?;

    load_from_path(&config_path, &repo_root)
}

/// Load a specific config file with a known repo root.
pub fn load_from_path(config_path: &Path, repo_root: &Path) -> Result<Config> {
    let script = format!(
        r#"try {{ const c = require("{}"); console.log(JSON.stringify(c)); }} catch(e) {{ console.error(e.message); process.exit(1); }}"#,
        config_path.display()
    );

    let output = Command::new("node")
        .args(["-e", &script])
        .current_dir(repo_root)
        .output()
        .map_err(|e| anyhow::anyhow!("{}", crate::cmd::friendly_cmd_error("node", &e)))?;

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        bail!(
            "failed to evaluate {}: {}",
            config_path.display(),
            stderr.trim()
        );
    }

    let mut cfg: Config = serde_json::from_slice(&output.stdout)
        .context("failed to parse config JSON")?;

    if cfg.name.is_empty() {
        bail!("{}: \"name\" is required", CONFIG_FILENAME);
    }

    cfg.config_path = config_path.to_path_buf();
    cfg.repo_root = repo_root.to_path_buf();

    cfg.apply_defaults();
    cfg.resolve_env_templates();
    cfg.resolve_paths();

    Ok(cfg)
}

impl Config {
    fn apply_defaults(&mut self) {
        if self.repo.worktrees_dir.is_empty() {
            self.repo.worktrees_dir = format!("../{}-worktrees", self.name);
        }

        if self.env.prefix.is_empty() {
            self.env.prefix = self.name.replace(['-', '.'], "_").to_uppercase();
        }

        if self.env.filename.is_empty() {
            self.env.filename = ".env.worktree".to_string();
        }

        if self.port_offset.algorithm.is_empty() {
            self.port_offset.algorithm = "sha256".to_string();
        }
        if self.port_offset.range == 0 {
            self.port_offset.range = 2000;
        }
        if self.port_offset.min == 0 {
            self.port_offset.min = 100;
        }

        if let Some(ref mut db) = self.database {
            if db.db_name_prefix.is_empty() {
                db.db_name_prefix = "db_".to_string();
            }
        }

        if self.dash.services.manager.is_empty() {
            self.dash.services.manager = "pm2".to_string();
        }
        if self.dash.services.running_check.is_empty() {
            self.dash.services.running_check = "pm2".to_string();
        }

        if self.docker.proxy.dynamic_dir.is_empty() && self.docker.proxy.proxy_type == "traefik" {
            self.docker.proxy.dynamic_dir = "traefik/dynamic".to_string();
        }
        if self.docker.proxy.domain_template.is_empty() && !self.docker.proxy.proxy_type.is_empty()
        {
            self.docker.proxy.domain_template = "{alias}.localhost".to_string();
        }
    }

    fn resolve_env_templates(&mut self) {
        if self.env.prefix.is_empty() {
            return;
        }
        let prefix = self.env.prefix.clone();
        for val in self.env.vars.values_mut() {
            *val = val.replace("{PREFIX}", &prefix);
        }
    }

    fn resolve_paths(&mut self) {
        self.worktrees_dir_abs = resolve_path(&self.repo_root, &self.repo.worktrees_dir);

        if !self.docker.shared_infra.compose_path.is_empty() {
            let expanded = expand_tilde(&self.docker.shared_infra.compose_path);
            self.compose_path_abs = Some(resolve_path(&self.repo_root, &expanded));
        }

        if self.docker.proxy.proxy_type == "traefik" {
            if let Some(ref compose_path) = self.compose_path_abs {
                if !self.docker.proxy.dynamic_dir.is_empty() {
                    self.proxy_dynamic_dir =
                        Some(compose_path.join(&self.docker.proxy.dynamic_dir));
                }
            }
        }

        if !self.docker.compose_file.is_empty() {
            self.compose_file_abs =
                Some(resolve_path(&self.repo_root, &self.docker.compose_file));
        }
    }

    // ── Derived value helpers ───────────────────────────────────────────

    pub fn container_name(&self, alias: &str) -> String {
        format!("{}-{}", self.name, alias)
    }

    pub fn volume_prefix(&self, alias: &str) -> String {
        format!("{}_{}", self.name, alias)
    }

    pub fn container_filter(&self) -> String {
        format!("name={}-", self.name)
    }

    pub fn container_prefix(&self) -> String {
        format!("{}-", self.name)
    }

    pub fn domain_for(&self, alias: &str) -> Option<String> {
        if self.docker.proxy.domain_template.is_empty() {
            return None;
        }
        Some(self.docker.proxy.domain_template.replace("{alias}", alias))
    }

    pub fn db_name(&self, alias: &str) -> Option<String> {
        let db = self.database.as_ref()?;
        if db.db_type.is_empty() {
            return None;
        }
        let safe: String = alias
            .chars()
            .map(|c| {
                if c.is_ascii_alphanumeric() || c == '_' {
                    c
                } else {
                    '_'
                }
            })
            .collect();
        Some(format!("{}{}", db.db_name_prefix, safe))
    }

    pub fn primary_port(&self) -> Option<i32> {
        if self.services.primary.is_empty() {
            return None;
        }
        self.services.ports.get(&self.services.primary).copied()
    }

    pub fn compute_ports(&self, offset: i32) -> HashMap<String, i32> {
        self.services
            .ports
            .iter()
            .map(|(name, base)| (name.clone(), base + offset))
            .collect()
    }

    pub fn feature_enabled(&self, name: &str) -> bool {
        match name {
            "lan" => self.features.lan,
            "autostop" => self.features.autostop,
            "prune" => self.features.prune,
            "rebuildBase" => self.features.rebuild_base,
            _ => false,
        }
    }

    pub fn env_var(&self, key: &str) -> Option<&str> {
        self.env.vars.get(key).map(|s| s.as_str())
    }

    pub fn worktree_var(&self, key: &str) -> Option<&str> {
        self.env.worktree_vars.get(key).map(|s| s.as_str())
    }

    pub fn service_manager(&self) -> &str {
        &self.dash.services.manager
    }

    pub fn docker_service_manager(&self) -> &str {
        if let Some(ref docker) = self.dash.services.docker {
            if !docker.manager.is_empty() {
                return &docker.manager;
            }
        }
        &self.dash.services.manager
    }

    pub fn pm2_ecosystem_config(&self) -> &str {
        &self.services.pm2.ecosystem_config
    }

    /// Expand lifecycle/build command placeholders.
    pub fn expand_cmd(&self, template: &str, wt: &crate::worktree::Worktree) -> String {
        let wt_path = wt.path.canonicalize()
            .unwrap_or_else(|_| wt.path.clone());
        let root = self.repo_root.canonicalize()
            .unwrap_or_else(|_| self.repo_root.clone());
        template
            .replace("{alias}", &wt.alias)
            .replace("{name}", &self.name)
            .replace("{path}", &wt_path.to_string_lossy())
            .replace("{container}", &wt.container)
            .replace("{root}", &root.to_string_lossy())
            .replace("{branch}", &wt.branch)
    }

    /// Get the lifecycle command for an action + worktree type.
    /// Checks config first, then stack defaults. Returns None to use built-in fallback.
    pub fn lifecycle_cmd(
        &self,
        wt_type: &crate::worktree::WorktreeType,
        action: &str,
    ) -> Option<&str> {
        if let Some(ref lc) = self.dash.lifecycle {
            let cmds = match wt_type {
                crate::worktree::WorktreeType::Docker => &lc.docker,
                crate::worktree::WorktreeType::Local => &lc.local,
            };
            let cmd = match action {
                "start" => cmds.start.as_deref(),
                "stop" => cmds.stop.as_deref(),
                "restart" => cmds.restart.as_deref(),
                "logs" => cmds.logs.as_deref(),
                _ => None,
            };
            if cmd.is_some() {
                return cmd;
            }
        }

        if let Some(ref stack) = self.stack {
            let defaults = stack_defaults(stack, wt_type);
            let cmd = match action {
                "start" => defaults.start,
                "stop" => defaults.stop,
                "restart" => defaults.restart,
                "logs" => defaults.logs,
                _ => None,
            };
            if cmd.is_some() {
                return cmd;
            }
        }

        None
    }

    /// Check if lifecycle config exists (either explicit or via stack).
    pub fn has_lifecycle(&self) -> bool {
        self.dash.lifecycle.is_some() || self.stack.is_some()
    }
}

/// Stack-specific default lifecycle commands.
fn stack_defaults<'a>(
    stack: &str,
    wt_type: &crate::worktree::WorktreeType,
) -> LifecycleDefaults<'a> {
    use crate::worktree::WorktreeType;

    match (stack, wt_type) {
        ("node", WorktreeType::Local) => LifecycleDefaults {
            start: Some("npm start"), ..Default::default()
        },
        ("node", WorktreeType::Docker) | ("docker-compose", WorktreeType::Docker) | (_, WorktreeType::Docker) => LifecycleDefaults {
            start: Some("docker compose up -d --build"),
            stop: Some("docker compose down"),
            logs: Some("docker compose logs -f --tail 100"),
            ..Default::default()
        },
        ("nextjs", WorktreeType::Local) => LifecycleDefaults {
            start: Some("npx next dev"), ..Default::default()
        },
        ("nuxt", WorktreeType::Local) => LifecycleDefaults {
            start: Some("npx nuxt dev"), ..Default::default()
        },
        ("python" | "flask", WorktreeType::Local) => LifecycleDefaults {
            start: Some("flask run"), ..Default::default()
        },
        ("django", WorktreeType::Local) => LifecycleDefaults {
            start: Some("python manage.py runserver"), ..Default::default()
        },
        ("rails", WorktreeType::Local) => LifecycleDefaults {
            start: Some("rails server"), ..Default::default()
        },
        ("rust", WorktreeType::Local) => LifecycleDefaults {
            start: Some("cargo run"), ..Default::default()
        },
        ("go", WorktreeType::Local) => LifecycleDefaults {
            start: Some("go run ."), ..Default::default()
        },
        _ => LifecycleDefaults::default(),
    }
}

#[derive(Default)]
struct LifecycleDefaults<'a> {
    start: Option<&'a str>,
    stop: Option<&'a str>,
    restart: Option<&'a str>,
    logs: Option<&'a str>,
}

// ── Utilities ───────────────────────────────────────────────────────────

fn resolve_path(base: &Path, rel: &str) -> PathBuf {
    let rel_path = Path::new(rel);
    if rel_path.is_absolute() {
        rel_path.to_path_buf()
    } else {
        base.join(rel)
    }
}

fn expand_tilde(p: &str) -> String {
    if let Some(rest) = p.strip_prefix("~/") {
        if let Some(home) = dirs_home() {
            return format!("{}/{}", home, rest);
        }
    }
    p.to_string()
}

fn dirs_home() -> Option<String> {
    std::env::var("HOME").ok()
}
