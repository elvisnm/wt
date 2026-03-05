/**
 * generate-ecosystem-config.js — Generate a PM2 ecosystem config for a local worktree.
 *
 * Reads all definitions from workflow.config.js:
 *   services.ports, services.groups, services.pm2.debugPorts,
 *   services.pm2.heapSizes, services.pm2.envPassthrough
 *
 * Generates ecosystem.worktree.config.js in the worktree directory with:
 *   - Namespaced PM2 process names (e.g. app-my-feature)
 *   - Offset debug inspect ports
 *   - Worktree env vars forwarded to each process
 *   - Service filtering by mode/groups
 */

const fs = require('fs');
const path = require('path');
const { config, config_mod, read_env_multi } = require('./lib/utils');
const { pm2_process_name } = require('./lib/pm2');

const OUTPUT_FILENAME = 'ecosystem.worktree.config.js';

// ── Arg parsing ──────────────────────────────────────────────────────────

function parse_args(argv) {
  const options = {
    dir: process.cwd(),
    mode: null,
  };

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--dir') { options.dir = argv[++i]; continue; }
    if (arg.startsWith('--dir=')) { options.dir = arg.split('=')[1]; continue; }
    if (arg === '--mode') { options.mode = argv[++i]; continue; }
    if (arg.startsWith('--mode=')) { options.mode = arg.split('=')[1]; continue; }
  }

  return options;
}

// ── Source ecosystem template reading ────────────────────────────────────

/**
 * Read the project's source ecosystem config to get per-service definitions
 * (script paths, cwd, watch patterns, args, etc.).
 * Returns a map of baseName -> service definition object.
 */
function read_source_ecosystem(config) {
  const ecosystem_path = config.services.pm2._ecosystemConfigResolved;
  if (!ecosystem_path || !fs.existsSync(ecosystem_path)) return null;

  // We need to evaluate the ecosystem config, but it may reference env vars
  // that aren't set. Load it in a sandboxed way by reading the file and
  // extracting the apps array structure.
  try {
    // Clear require cache
    delete require.cache[require.resolve(ecosystem_path)];
    const ecosystem = require(ecosystem_path);
    const apps = ecosystem.apps || [];
    const map = {};
    for (const app of apps) {
      const base = app.baseName || app.name;
      if (base) map[base] = app;
    }
    return map;
  } catch {
    return null;
  }
}

// ── Generator ────────────────────────────────────────────────────────────

/**
 * Generate ecosystem config content for a worktree.
 *
 * @param {object} config - Resolved workflow config
 * @param {string} worktree_path - Absolute path to the worktree
 * @param {string} worktree_name - Worktree name (for PM2 namespacing)
 * @param {number} port_offset - Port offset for this worktree
 * @param {Set<string>} active_services - Set of service names to include
 * @param {object} env_overrides - Env vars to pass to all processes
 * @returns {string} JavaScript content for the ecosystem config file
 */
function generate_config(config, worktree_path, worktree_name, port_offset, active_services, env_overrides) {
  const pm2_config = config.services.pm2 || {};
  const debug_ports = pm2_config.debugPorts || {};
  const heap_sizes = pm2_config.heapSizes || {};
  const source_apps = read_source_ecosystem(config);

  const apps = [];

  for (const service_name of active_services) {
    const base_port = config.services.ports[service_name];
    if (base_port === undefined) continue;

    const pm2_name = pm2_process_name(service_name, worktree_name);
    const debug_port = debug_ports[service_name];
    const heap = heap_sizes[service_name];

    // Build node_args
    const node_args_parts = [];
    if (debug_port) {
      node_args_parts.push(`--inspect=${debug_port + port_offset}`);
    }
    if (heap) {
      node_args_parts.push(`--max-old-space-size=${heap}`);
    }

    // Start with source app definition if available, otherwise build minimal
    const source = source_apps && source_apps[service_name];
    const app = {
      name: pm2_name,
    };

    if (source) {
      if (source.cwd) app.cwd = source.cwd;
      if (source.script) app.script = source.script;
      if (source.args) app.args = source.args;
      if (source.watch) app.watch = source.watch;
      if (source.ignore_watch) app.ignore_watch = source.ignore_watch;
      if (source.merge_logs !== undefined) app.merge_logs = source.merge_logs;

      // Merge node_args: take extra flags from source (like --require ts-node/register)
      // but replace inspect and heap with our computed values
      if (source.node_args) {
        const source_parts = source.node_args.split(/\s+/);
        for (const part of source_parts) {
          if (part.startsWith('--inspect')) continue;
          if (part.startsWith('--max-old-space-size')) continue;
          if (part.startsWith('--max-semi-space-size')) continue;
          node_args_parts.push(part);
        }
      }
    }

    if (node_args_parts.length) {
      app.node_args = node_args_parts.join(' ');
    }

    app.env = { ...env_overrides };

    // Copy env from source (like TS_NODE_PROJECT, SERVICE_HOST_INSTANCES)
    if (source && source.env) {
      for (const [k, v] of Object.entries(source.env)) {
        if (!(k in app.env)) app.env[k] = v;
      }
    }

    apps.push(app);
  }

  // Generate the JS file content
  const json = JSON.stringify({ apps }, null, 2);
  return `// Generated by wt — do not edit manually\nmodule.exports = ${json};\n`;
}

// ── Main ─────────────────────────────────────────────────────────────────

function main() {
  if (!config) {
    console.error('No workflow.config.js found. Cannot generate ecosystem config.');
    process.exit(1);
  }

  if (!config.services.pm2 || !config.services.pm2.ecosystemConfig) {
    console.error('services.pm2.ecosystemConfig not set in workflow.config.js');
    process.exit(1);
  }

  const options = parse_args(process.argv.slice(2));
  const worktree_path = path.resolve(options.dir);

  // Read worktree env vars
  const env_filename = config.env.filename || '.env.worktree';
  const env_keys_to_read = [
    config_mod.worktree_var(config, 'name') || 'WORKTREE_NAME',
    config_mod.worktree_var(config, 'portOffset') || 'WORKTREE_PORT_OFFSET',
    config_mod.worktree_var(config, 'portBase') || 'WORKTREE_PORT_BASE',
  ];
  const env_vals = read_env_multi(worktree_path, env_keys_to_read);

  const worktree_name = env_vals[env_keys_to_read[0]] || path.basename(worktree_path);
  const port_offset = env_vals[env_keys_to_read[2]]
    ? Number.parseInt(env_vals[env_keys_to_read[2]], 10) - 3000
    : Number.parseInt(env_vals[env_keys_to_read[1]] || '0', 10);

  // Resolve active services
  const mode = options.mode || config.services.defaultMode || 'full';
  const active_services = config_mod.resolve_services(config, mode);

  // Build env overrides from passthrough keys
  const passthrough_keys = (config.services.pm2 && config.services.pm2.envPassthrough) || [];
  const all_env_keys = [...new Set([...passthrough_keys, ...env_keys_to_read])];
  const all_env_vals = read_env_multi(worktree_path, all_env_keys);

  const env_overrides = {
    SKULABS_ENV: 'development',
    NODE_ENV: 'development',
  };
  for (const key of all_env_keys) {
    const val = all_env_vals[key] || process.env[key];
    if (val) env_overrides[key] = val;
  }

  const content = generate_config(
    config, worktree_path, worktree_name, port_offset, active_services, env_overrides,
  );

  const output_path = path.join(worktree_path, OUTPUT_FILENAME);
  fs.writeFileSync(output_path, content, 'utf8');
  console.log(`Wrote ${output_path}`);
}

if (require.main === module) {
  main();
}

module.exports = { generate_config, OUTPUT_FILENAME };
