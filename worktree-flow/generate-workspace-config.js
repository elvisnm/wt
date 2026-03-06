/**
 * generate-workspace-config.js — Generate a VS Code / Cursor workspace file for a local worktree.
 *
 * Reads debug ports, heap sizes, and service definitions from workflow.config.js.
 * Generates devbox.code-workspace with:
 *   - Debug launch configs for each active service (attach to inspect port + offset)
 *   - Terminal env settings (PM2_HOME, relevant worktree vars)
 *   - Task definitions for common workflows
 */

const fs = require('fs');
const path = require('path');
const { config, config_mod, read_env_multi } = require('./lib/utils');
const { pm2_home } = require('./lib/pm2');

const WORKSPACE_FILENAME = 'devbox.code-workspace';

/**
 * Build VS Code debug launch configurations for each service.
 * @param {object} config - Resolved workflow config
 * @param {number} offset - Port offset
 * @param {Set<string>} active_services - Active service names
 * @param {string} [app_url] - App URL for browser launcher
 * @returns {Array} Launch configurations
 */
function build_launch_configs(config, offset, active_services, app_url) {
  const debug_ports = (config.services.pm2 && config.services.pm2.debugPorts) || {};
  const skip_files = ['<node_internals>/**'];
  const configs = [];

  for (const service_name of active_services) {
    const default_port = debug_ports[service_name];
    if (!default_port) continue;

    const port = default_port + offset;
    configs.push({
      name: `Debug ${service_name} (${port})`,
      port,
      request: 'attach',
      restart: true,
      skipFiles: skip_files,
      type: 'node',
    });
  }

  // Frontend browser launcher
  const primary = config.services.primary;
  const primary_port = primary && config.services.ports[primary]
    ? config.services.ports[primary] + offset
    : null;
  const url = app_url
    ? app_url.replace(/\/$/, '')
    : (primary_port ? `http://localhost:${primary_port}` : null);

  if (url) {
    configs.push({
      type: 'chrome',
      name: 'Frontend (Chrome)',
      request: 'launch',
      url,
      webRoot: '${workspaceFolder}',
    });
  }

  return configs;
}

/**
 * Generate and write the workspace config file.
 * @param {string} worktree_path
 * @param {object} config - Resolved workflow config
 * @param {number} offset - Port offset
 * @param {Set<string>} active_services - Active service names
 * @param {object} env_vars - Env vars read from .env.worktree
 */
function generate_workspace(worktree_path, config, offset, active_services, env_vars) {
  const home = pm2_home(worktree_path);

  // Terminal env: forward key worktree vars to integrated terminal
  const terminal_env = {};
  if (home) terminal_env.PM2_HOME = home;

  const passthrough = (config.services.pm2 && config.services.pm2.envPassthrough) || [];
  for (const key of passthrough) {
    if (env_vars[key]) terminal_env[key] = env_vars[key];
  }

  const app_url_var = config_mod.env_var(config, 'appUrl');
  const app_url = app_url_var ? env_vars[app_url_var] : null;

  const launch_configs = build_launch_configs(config, offset, active_services, app_url);

  const workspace = {
    folders: [{ path: '.' }],
    settings: {
      'terminal.integrated.env.osx': {
        ...terminal_env,
      },
      'terminal.integrated.env.linux': {
        ...terminal_env,
      },
    },
    launch: {
      version: '0.2.0',
      configurations: launch_configs,
    },
  };

  const output_path = path.join(worktree_path, WORKSPACE_FILENAME);
  fs.writeFileSync(output_path, JSON.stringify(workspace, null, 2) + '\n', 'utf8');
  return output_path;
}

// ── CLI entry point ──────────────────────────────────────────────────────

function main() {
  if (!config) {
    console.error('No workflow.config.js found.');
    process.exit(1);
  }

  const worktree_path = path.resolve(process.argv[2] || process.cwd());

  // Read env vars
  const env_keys = [
    config_mod.worktree_var(config, 'portOffset') || 'WORKTREE_PORT_OFFSET',
    config_mod.worktree_var(config, 'portBase') || 'WORKTREE_PORT_BASE',
    ...(config.services.pm2 && config.services.pm2.envPassthrough || []),
    config_mod.env_var(config, 'appUrl'),
  ].filter(Boolean);

  const env_vals = read_env_multi(worktree_path, env_keys);

  const offset_key = config_mod.worktree_var(config, 'portOffset') || 'WORKTREE_PORT_OFFSET';
  const base_key = config_mod.worktree_var(config, 'portBase') || 'WORKTREE_PORT_BASE';
  const offset = env_vals[base_key]
    ? Number.parseInt(env_vals[base_key], 10) - 3000
    : Number.parseInt(env_vals[offset_key] || '0', 10);

  const mode = config.services.defaultMode || 'full';
  const active_services = config_mod.resolve_services(config, mode);

  const output = generate_workspace(worktree_path, config, offset, active_services, env_vals);
  console.log(`Wrote ${output}`);
}

if (require.main === module) {
  main();
}

module.exports = { generate_workspace, build_launch_configs, WORKSPACE_FILENAME };
