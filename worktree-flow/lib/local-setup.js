/**
 * local-setup.js — Shared logic for local (non-Docker) worktree setup.
 *
 * Extracted from dc-worktree-up.js to eliminate duplication between
 * the local create and local restart paths.
 */

const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const { config, config_mod, compute_auto_offset, update_env_key, read_offset } = require('./utils');
const { find_pm2, pm2_home, pm2_start, pm2_cleanup, pm2_process_name } = require('./pm2');
const { refresh_credentials } = require('./aws');
const { find_free_offset } = require('../service-ports');
const { generate_config, OUTPUT_FILENAME } = require('../generate-ecosystem-config');
const { write_traefik_config } = require('../generate-docker-compose');

/**
 * Check if localDev PM2 management is enabled and configured.
 */
function is_local_dev_enabled() {
  return config && config_mod.feature_enabled(config, 'localDev')
    && config.services.pm2 && config.services.pm2.ecosystemConfig;
}

/**
 * Resolve the env filename from config or default.
 */
function env_filename() {
  return config ? config.env.filename || '.env.worktree' : '.env.worktree';
}

/**
 * Read port offset from env file, or compute a new one.
 */
function resolve_offset(worktree_path) {
  return read_offset(worktree_path) || find_free_offset(compute_auto_offset(worktree_path));
}

/**
 * Build env overrides map from passthrough keys + env file contents.
 * These are passed to the ecosystem config generator.
 */
function build_env_overrides(worktree_path) {
  const passthrough = config.services.pm2.envPassthrough || [];
  const overrides = { SKULABS_ENV: 'development', NODE_ENV: 'development', ...refresh_credentials(config) };
  const env_file = path.join(worktree_path, env_filename());

  if (fs.existsSync(env_file)) {
    const content = fs.readFileSync(env_file, 'utf8');
    for (const line of content.split('\n')) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith('#')) continue;
      const idx = trimmed.indexOf('=');
      if (idx === -1) continue;
      const key = trimmed.slice(0, idx).trim();
      if (passthrough.includes(key) || key.startsWith('WORKTREE_') || key === 'PM2_HOME') {
        overrides[key] = trimmed.slice(idx + 1).trim();
      }
    }
  }
  return overrides;
}

/**
 * Generate ecosystem config file and write it to worktree.
 * Returns the path to the generated file.
 */
function generate_ecosystem(worktree_path, branch, port_offset, mode) {
  const active_services = config_mod.resolve_services(config, mode);
  const env_overrides = build_env_overrides(worktree_path);
  const branch_slug = branch.replace(/\//g, '-');

  const ecosystem_content = generate_config(
    config, worktree_path, branch_slug,
    port_offset, active_services, env_overrides,
  );
  const ecosystem_path = path.join(worktree_path, OUTPUT_FILENAME);
  fs.writeFileSync(ecosystem_path, ecosystem_content, 'utf8');

  return { ecosystem_path, active_services, env_overrides };
}

/**
 * Write traefik config for the worktree (unless --no-traefik).
 */
function setup_traefik(alias, port_offset, options) {
  if (options.no_traefik) return;
  const domain = config_mod.domain_for(config, alias);
  write_traefik_config(alias, domain, port_offset);
}

/**
 * Start PM2 services and optional frontend build watcher.
 */
function start_services(worktree_path, branch, repo_root) {
  const home = pm2_home(worktree_path);
  const pm2_bin = find_pm2(repo_root);
  const ecosystem_path = path.join(worktree_path, OUTPUT_FILENAME);

  console.log('Starting PM2 services...');
  pm2_start({
    pm2: pm2_bin,
    pm2_home: home,
    ecosystem_config: ecosystem_path,
    env: refresh_credentials(config),
    cwd: worktree_path,
  });

  start_build_watcher(worktree_path, branch, home, pm2_bin);
}

/**
 * Stop existing PM2 daemon for the worktree.
 */
function stop_services(worktree_path, repo_root) {
  const home = pm2_home(worktree_path);
  const pm2_bin = find_pm2(repo_root);
  pm2_cleanup(pm2_bin, home);
}

/**
 * Start the frontend build watcher if buildScript is configured.
 */
function start_build_watcher(worktree_path, branch, home, pm2_bin) {
  const build_script = config && config.paths && config.paths.buildScript;
  if (!build_script) return;

  const build_script_path = path.join(worktree_path, build_script);
  if (!fs.existsSync(build_script_path)) return;

  const wt_suffix = branch.replace(/\//g, '-');
  const build_name = pm2_process_name('build', wt_suffix);
  const prefix = `PM2_HOME="${home}" `;

  // Delete any existing build watcher
  try { execSync(`${prefix}${pm2_bin} delete "${build_name}"`, { stdio: 'pipe', cwd: worktree_path }); } catch {}

  const start_cmd = `${prefix}${pm2_bin} start "node ${build_script} develop --watch" --name "${build_name}" --cwd "${worktree_path}" --no-autorestart`;
  try {
    execSync(start_cmd, { stdio: 'inherit', cwd: worktree_path, env: { ...process.env, ...refresh_credentials(config), PM2_HOME: home } });
  } catch {
    console.warn('Warning: Frontend build watcher failed to start.');
  }
}

/**
 * Write WORKTREE_TYPE to env file for explicit discovery.
 */
function write_worktree_type(worktree_path) {
  const env_file = path.join(worktree_path, env_filename());
  update_env_key(env_file, 'WORKTREE_TYPE', 'local');
}

module.exports = {
  is_local_dev_enabled,
  env_filename,
  resolve_offset,
  build_env_overrides,
  generate_ecosystem,
  setup_traefik,
  start_services,
  stop_services,
  start_build_watcher,
  write_worktree_type,
};
