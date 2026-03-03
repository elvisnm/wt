const { execSync } = require('child_process');
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');

/**
 * Shared utilities for worktree-flow scripts.
 *
 * Config is loaded once and cached. Functions that depend on config
 * have safe fallbacks for when no workflow.config.js is present.
 */

const config_mod = require('../config');
const config = config_mod.load_config({ required: false }) || null;

function get_env_filename() {
  return config ? config.env.filename : '.env.worktree';
}

// ── Shell execution ──────────────────────────────────────────────────────

function run(command, opts = {}) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8', ...opts }).trim();
}

// ── Worktree path resolution ─────────────────────────────────────────────

/**
 * Resolve the worktrees base directory (the parent that holds all worktree dirs).
 * Accepts an optional repo_root; if omitted, detects via git.
 */
function resolve_worktrees_dir(repo_root) {
  if (config && config.repo._worktreesDirResolved) {
    return config.repo._worktreesDirResolved;
  }
  if (!repo_root) {
    repo_root = run('git rev-parse --show-toplevel');
  }
  const project_name = path.basename(repo_root);
  const parent_dir = path.dirname(repo_root);
  return path.join(parent_dir, `${project_name}-worktrees`);
}

/**
 * Resolve the full path to a specific worktree by branch/name.
 * Two signatures supported:
 *   resolve_worktree_path(name)            - auto-detects repo_root
 *   resolve_worktree_path(repo_root, name) - explicit repo_root
 */
function resolve_worktree_path(repo_root_or_name, name) {
  let repo_root;
  if (name === undefined) {
    name = repo_root_or_name;
    repo_root = run('git rev-parse --show-toplevel');
  } else {
    repo_root = repo_root_or_name;
  }
  const worktrees_dir = resolve_worktrees_dir(repo_root);
  return path.join(worktrees_dir, name.replace(/\//g, '-'));
}

// ── Docker worktree discovery ────────────────────────────────────────────

/**
 * Find all docker worktrees under a base directory.
 * Returns an array of { name, path } objects.
 * Scans recursively; stops at directories that contain a compose file or env file.
 */
function find_docker_worktrees(base_dir) {
  const results = [];
  const env_filename = get_env_filename();

  function scan(dir, prefix) {
    const entries = fs.readdirSync(dir, { withFileTypes: true });
    for (const entry of entries) {
      if (!entry.isDirectory()) continue;
      const full_path = path.join(dir, entry.name);
      const rel_name = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (fs.existsSync(path.join(full_path, 'docker-compose.worktree.yml')) ||
          fs.existsSync(path.join(full_path, env_filename))) {
        results.push({ name: rel_name, path: full_path });
      } else {
        scan(full_path, rel_name);
      }
    }
  }

  scan(base_dir, '');
  return results;
}

// ── Env file reading ─────────────────────────────────────────────────────

/**
 * Read a single key from a worktree env file.
 * Uses config.env.filename if config is available, otherwise '.env.worktree'.
 */
function read_env(worktree_path, key) {
  const env_path = path.join(worktree_path, get_env_filename());
  try {
    const content = fs.readFileSync(env_path, 'utf8');
    const match = content.match(new RegExp(`^${key}=(.+)$`, 'm'));
    return match ? match[1].trim() : null;
  } catch {
    return null;
  }
}

// ── Port offset calculation ──────────────────────────────────────────────

/**
 * Compute a deterministic port offset from a seed string (typically the worktree path).
 * Uses config.compute_offset if config is available, otherwise sha256 hash.
 */
function compute_auto_offset(seed) {
  if (config) return config_mod.compute_offset(config, seed);
  const hash = crypto.createHash('sha256').update(seed).digest('hex');
  const hash_int = Number.parseInt(hash.slice(0, 8), 16);
  return (hash_int % 2000) + 100;
}

/**
 * Read the port offset for a worktree.
 * Checks WORKTREE_HOST_PORT_OFFSET, WORKTREE_PORT_OFFSET, WORKTREE_PORT_BASE
 * in the env file, then falls back to the compose file, then computes automatically.
 */
function read_offset(worktree_path) {
  const env_path = path.join(worktree_path, get_env_filename());
  try {
    const content = fs.readFileSync(env_path, 'utf8');
    const host_offset_match = content.match(/^WORKTREE_HOST_PORT_OFFSET=(\d+)/m);
    if (host_offset_match) return Number.parseInt(host_offset_match[1], 10);

    const offset_match = content.match(/^WORKTREE_PORT_OFFSET=(\d+)/m);
    if (offset_match) return Number.parseInt(offset_match[1], 10);

    const base_match = content.match(/^WORKTREE_PORT_BASE=(\d+)/m);
    if (base_match) return Number.parseInt(base_match[1], 10) - 3000;
  } catch { /* env file doesn't exist */ }

  const compose_path = path.join(worktree_path, 'docker-compose.worktree.yml');
  try {
    const content = fs.readFileSync(compose_path, 'utf8');
    const port_match = content.match(/"(\d+):3001"/);
    if (port_match) {
      const host_port = Number.parseInt(port_match[1], 10);
      if (host_port !== 3001) return host_port - 3001;
    }
  } catch { /* compose file doesn't exist */ }

  return compute_auto_offset(worktree_path);
}

// ── Container name helpers ───────────────────────────────────────────────

/**
 * Get the running container name by querying docker compose ps.
 * Works for the generate strategy (single-container).
 */
function get_container_name(worktree_path) {
  try {
    const output = execSync('docker compose -f docker-compose.worktree.yml ps --format json', {
      stdio: 'pipe',
      encoding: 'utf8',
      cwd: worktree_path,
    }).trim();

    if (!output) return null;

    const lines = output.split('\n').filter(Boolean);
    for (const line of lines) {
      try {
        const data = JSON.parse(line);
        return data.Name || data.name || null;
      } catch {
        continue;
      }
    }
  } catch {
    return null;
  }

  return null;
}

/**
 * Read the container_name from docker-compose.worktree.yml (static, no docker query).
 */
function read_container_name(worktree_path) {
  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  try {
    const content = fs.readFileSync(compose_file, 'utf8');
    const match = content.match(/container_name:\s*(\S+)/);
    return match ? match[1] : null;
  } catch {
    return null;
  }
}

// ── Service mode ─────────────────────────────────────────────────────────

/**
 * Read the service mode for a worktree.
 * Checks shared compose env first, then falls back to compose file WORKTREE_SERVICES.
 */
function read_service_mode(worktree_path) {
  const shared = config ? config_mod.get_compose_info(config, worktree_path) : null;
  if (shared) {
    const svc_var = config.env.worktreeVars.services || 'WORKTREE_SERVICES';
    const mode = read_env(worktree_path, svc_var);
    return mode || (config.services.defaultMode || 'default');
  }
  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  try {
    const content = fs.readFileSync(compose_file, 'utf8');
    const match = content.match(/WORKTREE_SERVICES=(\w+)/);
    const fallback = config && config.services.defaultMode ? config.services.defaultMode : 'default';
    return match ? match[1] : fallback;
  } catch {
    return config && config.services.defaultMode ? config.services.defaultMode : 'default';
  }
}

// ── Alias reading ────────────────────────────────────────────────────────

/**
 * Read the WORKTREE_ALIAS from the env file.
 */
function read_alias(worktree_path) {
  const env_path = path.join(worktree_path, get_env_filename());
  try {
    const content = fs.readFileSync(env_path, 'utf8');
    const match = content.match(/^WORKTREE_ALIAS=(.+)$/m);
    return match ? match[1].trim() : null;
  } catch {
    return null;
  }
}

// ── Mongo container ──────────────────────────────────────────────────────

/**
 * Find a running MongoDB container by name pattern.
 */
function find_mongo_container() {
  try {
    const output = execSync('docker ps --format "{{.Names}}"', {
      stdio: 'pipe',
      encoding: 'utf8',
    }).trim();
    const names = output.split('\n').filter(Boolean);
    const project_name = config ? config.name : 'project';
    return names.find((n) => n.includes(project_name) && n.includes('mongo')) || null;
  } catch {
    return null;
  }
}

// ── Branch helpers ───────────────────────────────────────────────────────

/**
 * Derive a short alias from a branch name (strips common prefixes, takes first two parts).
 */
function auto_alias(branch_name) {
  const prefixes = config ? config.repo.branchPrefixes : ['feat', 'fix', 'ops', 'hotfix', 'release', 'chore'];
  const prefix_pattern = new RegExp(`^(${prefixes.join('|')})\\/`, 'i');
  const stripped = branch_name.replace(prefix_pattern, '');
  const clean = stripped.replace(/\//g, '-').replace(/[^a-zA-Z0-9-]/g, '-').toLowerCase();
  const parts = clean.split('-').filter(Boolean);
  return parts.slice(0, 2).join('-') || clean.slice(0, 20);
}

/**
 * Check if a git ref exists in the repo.
 */
function has_ref(repo_root, ref) {
  try {
    execSync(`git -C "${repo_root}" show-ref --verify --quiet "${ref}"`);
    return true;
  } catch {
    return false;
  }
}

// ── Exports ──────────────────────────────────────────────────────────────

module.exports = {
  config,
  config_mod,
  get_env_filename,
  run,
  resolve_worktrees_dir,
  resolve_worktree_path,
  find_docker_worktrees,
  read_env,
  compute_auto_offset,
  read_offset,
  get_container_name,
  read_container_name,
  read_service_mode,
  read_alias,
  find_mongo_container,
  auto_alias,
  has_ref,
};
