const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

/**
 * Generic PM2 helpers for local (non-Docker) worktree management.
 *
 * All behavior is driven by workflow.config.js — no hardcoded project values.
 * Each worktree gets its own PM2 daemon via PM2_HOME isolation.
 */

// ── PM2 binary discovery ─────────────────────────────────────────────────

/**
 * Find the pm2 binary. Checks local node_modules first, then global.
 * @param {string} [repo_root] - Path to the main repo root
 * @returns {string} Path to pm2 binary (quoted if needed)
 */
function find_pm2(repo_root) {
  if (repo_root) {
    const local = path.join(repo_root, 'node_modules', '.bin', 'pm2');
    if (fs.existsSync(local)) return `"${local}"`;
  }
  return 'pm2';
}

// ── PM2_HOME resolution ──────────────────────────────────────────────────

/**
 * Get the PM2_HOME directory for a worktree.
 * @param {string} worktree_path
 * @returns {string}
 */
function pm2_home(worktree_path) {
  return path.join(worktree_path, '.pm2');
}

/**
 * Build a shell env prefix string that sets PM2_HOME.
 * @param {string} home_dir
 * @returns {string}
 */
function pm2_env_prefix(home_dir) {
  return home_dir ? `PM2_HOME="${home_dir}" ` : '';
}

// ── PM2 lifecycle ────────────────────────────────────────────────────────

/**
 * Start PM2 with an ecosystem config file.
 * @param {object} options
 * @param {string} options.pm2 - Path to pm2 binary
 * @param {string} options.pm2_home - PM2_HOME directory
 * @param {string} options.ecosystem_config - Path to ecosystem config file
 * @param {object} [options.env] - Extra env vars to pass
 * @param {string} [options.cwd] - Working directory
 */
function pm2_start({ pm2, pm2_home: home, ecosystem_config, env, cwd }) {
  const prefix = pm2_env_prefix(home);
  const cmd = `${prefix}${pm2} start "${ecosystem_config}"`;
  try {
    execSync(cmd, {
      stdio: 'inherit',
      cwd,
      env: { ...process.env, ...env, PM2_HOME: home },
    });
  } catch {
    // PM2 returns non-zero even on partial success (e.g. some apps launched).
    // Don't throw — caller can check pm2_list for actual status.
  }
}

/**
 * Kill the PM2 daemon for an isolated PM2_HOME.
 * Safe to call even if no daemon is running.
 * @param {string} pm2_bin - Path to pm2 binary
 * @param {string} home - PM2_HOME directory
 */
function pm2_kill(pm2_bin, home) {
  if (!home) return;
  try {
    execSync(`${pm2_env_prefix(home)}${pm2_bin} kill`, { stdio: 'pipe' });
  } catch {
    // daemon not running — expected
  }
}

/**
 * Delete PM2 processes matching a name pattern (for shared PM2 daemon fallback).
 * @param {string} pm2_bin - Path to pm2 binary
 * @param {string[]} process_names - Names to delete
 */
function pm2_delete_by_names(pm2_bin, process_names) {
  if (!process_names.length) return;
  const escaped = process_names.map((n) => n.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
  const pattern = escaped.join('|');
  try {
    execSync(`${pm2_bin} delete "/(${pattern})/"`, { stdio: 'pipe' });
  } catch {
    // no matching processes
  }
}

/**
 * Clean up PM2 processes for a worktree.
 * With PM2_HOME isolation: kills the entire daemon.
 * Without: deletes processes by namespaced name pattern.
 *
 * @param {string} pm2_bin - Path to pm2 binary
 * @param {string} home - PM2_HOME directory (null for shared daemon)
 * @param {string[]} [namespaced_names] - Process names to delete (shared daemon only)
 */
function pm2_cleanup(pm2_bin, home, namespaced_names) {
  if (home) {
    pm2_kill(pm2_bin, home);
    return;
  }
  if (namespaced_names && namespaced_names.length) {
    pm2_delete_by_names(pm2_bin, namespaced_names);
  }
}

/**
 * Get PM2 process list as JSON.
 * @param {string} pm2_bin
 * @param {string} home - PM2_HOME directory
 * @returns {Array} Process list
 */
function pm2_list(pm2_bin, home) {
  const prefix = pm2_env_prefix(home);
  try {
    const output = execSync(`${prefix}${pm2_bin} jlist`, {
      stdio: ['pipe', 'pipe', 'pipe'],
      encoding: 'utf8',
      env: { ...process.env, PM2_HOME: home },
    }).trim();
    return JSON.parse(output);
  } catch {
    return [];
  }
}

/**
 * Run a PM2 action (restart, stop, start) on a service.
 * @param {string} pm2_bin
 * @param {string} home - PM2_HOME directory
 * @param {string} action - 'start', 'stop', 'restart'
 * @param {string} service_name - PM2 process name
 * @param {object} [options]
 * @param {string} [options.ecosystem_config] - Ecosystem file (needed for 'start')
 */
function pm2_action(pm2_bin, home, action, service_name, options = {}) {
  const prefix = pm2_env_prefix(home);
  const env_opts = { ...process.env, PM2_HOME: home };

  if (action === 'start' && options.ecosystem_config) {
    execSync(
      `${prefix}${pm2_bin} start "${options.ecosystem_config}" --only "${service_name}" --update-env`,
      { stdio: 'inherit', env: env_opts },
    );
  } else {
    execSync(
      `${prefix}${pm2_bin} ${action} "${service_name}"`,
      { stdio: 'inherit', env: env_opts },
    );
  }
}

/**
 * Build the namespaced PM2 process name for a service in a worktree.
 * @param {string} base_name - Base service name (e.g. 'app')
 * @param {string} worktree_suffix - Worktree name suffix (e.g. 'my-feature')
 * @returns {string} Namespaced name (e.g. 'app-my-feature')
 */
function pm2_process_name(base_name, worktree_suffix) {
  if (!worktree_suffix) return base_name;
  const safe_suffix = worktree_suffix.replace(/[^a-zA-Z0-9._-]/g, '-');
  return `${base_name}-${safe_suffix}`;
}

module.exports = {
  find_pm2,
  pm2_home,
  pm2_env_prefix,
  pm2_start,
  pm2_kill,
  pm2_delete_by_names,
  pm2_cleanup,
  pm2_list,
  pm2_action,
  pm2_process_name,
};
