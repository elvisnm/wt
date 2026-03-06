/**
 * config.js — Workflow configuration loader for worktree-flow.
 *
 * Searches upward from CWD (or a given path) for `workflow.config.js`,
 * parses it, applies defaults, and exports a resolved config object.
 *
 * Usage:
 *   const config = require('./config');
 *   // config.name, config.docker.baseImage, config.services.ports, etc.
 */

const fs = require('fs');
const path = require('path');
const os = require('os');

const CONFIG_FILENAME = 'workflow.config.js';

// ── Defaults ────────────────────────────────────────────────────────────

const DEFAULTS = {
  repo: {
    worktreesDir: null, // computed from name if not set
    branchPrefixes: ['feat', 'fix', 'ops', 'hotfix', 'release', 'chore'],
    baseRefs: null, // auto-detected from git if not set (e.g. ['origin/main', 'origin/develop'])
  },

  docker: {
    baseImage: null,
    composeStrategy: 'generate',
    composeFile: null,

    generate: {
      containerWorkdir: '/app',
      entrypoint: 'pnpm dev',
      extraMounts: [],
      extraEnv: {},
      overrideFiles: [], // files to copy into .docker-overrides/ on create/restart
    },

    envFiles: [], // env files to copy from main repo to worktree (e.g. ['.env', 'apps/api/.env'])

    sharedInfra: {
      network: null,
      composePath: null,
    },

    proxy: {
      type: 'ports',
      dynamicDir: 'traefik/dynamic',
      domainTemplate: '{alias}.localhost',
    },
  },

  services: {
    ports: {},
    modes: {},
    defaultMode: null,
    primary: null,
    quickLinks: [],
    groups: null, // { core: ['app', 'api'], sync: ['sync_server'], ... }
    pm2: {
      ecosystemConfig: null, // path to ecosystem config (e.g. 'ecosystem.dev.config.js')
      debugPorts: null,      // { app: 9225, api: 9229, ... }
      heapSizes: null,       // { app: 512, api: 512, ... } (MB)
      envPassthrough: null,  // ['MY_PATH', 'MY_MONGO', ...] env keys to forward to PM2
    },
  },

  portOffset: {
    algorithm: 'sha256',
    min: 100,
    range: 2000,
    autoResolve: true,
  },

  database: {
    type: null,
    host: null,
    containerHost: null,
    port: null,
    defaultDb: null,
    replicaSet: null,
    dbNamePrefix: 'db_',
    seedCommand: null,
    dropCommand: null,
  },

  redis: null,

  env: {
    prefix: null, // computed from name if not set
    filename: '.env.worktree',
    vars: {},
    worktreeVars: {
      name: 'WORKTREE_NAME',
      alias: 'WORKTREE_ALIAS',
      hostBuild: 'WORKTREE_HOST_BUILD',
      services: 'WORKTREE_SERVICES',
      hostPortOffset: 'WORKTREE_HOST_PORT_OFFSET',
      portOffset: 'WORKTREE_PORT_OFFSET',
      portBase: 'WORKTREE_PORT_BASE',
      poll: 'WORKTREE_POLL',
      devHeap: 'WORKTREE_DEV_HEAP',
    },
    // Extra env vars to generate in .env.worktree for local (non-Docker) worktrees.
    // Values support templates: {name}, {alias}, {offset}, {path}, {domain}, {appPort}
    localDefaults: null, // e.g. { 'MY_REDIS_PREFIX': 'wt:{name}:', 'MY_APP_URL': 'http://{domain}:{appPort}/' }
  },

  setup: {
    symlinks: [], // paths to symlink into each worktree on creation: [{ src, dst }]
    copyFiles: [], // files to copy from repo root into each worktree (relative paths)
  },

  git: {
    skipWorktree: [],
  },

  features: {
    hostBuild: false,
    lan: false,
    admin: { enabled: false, defaultUserId: null },
    awsCredentials: false,
    autostop: true,
    prune: true,
    imagesFix: false,
    rebuildBase: false,
    devHeap: null,
    localDev: false, // enable PM2-based local (non-Docker) worktree management
  },

  dash: {
    commands: {
      shell: { label: 'Shell', cmd: 'bash' },
      claude: { label: 'Claude', cmd: 'claude' },
    },
    localDevCommand: 'pnpm dev',
  },

  paths: {
    flowScripts: null,
    dockerOverrides: null,
    buildScript: null,
  },
};

// ── Deep merge utility ──────────────────────────────────────────────────

function is_plain_object(val) {
  return val !== null && typeof val === 'object' && !Array.isArray(val);
}

/**
 * Deep merge source into target. Arrays and null values in source
 * override target (not merged). Only plain objects are recursively merged.
 */
function deep_merge(target, source) {
  const result = { ...target };
  for (const key of Object.keys(source)) {
    const src_val = source[key];
    const tgt_val = result[key];

    if (src_val === undefined) continue;

    if (is_plain_object(src_val) && is_plain_object(tgt_val)) {
      result[key] = deep_merge(tgt_val, src_val);
    } else {
      result[key] = src_val;
    }
  }
  return result;
}

// ── Path resolution ─────────────────────────────────────────────────────

/**
 * Walk upward from `start_dir` looking for CONFIG_FILENAME.
 * Returns { configPath, repoRoot } or null.
 */
function find_config(start_dir) {
  let dir = path.resolve(start_dir);
  const root = path.parse(dir).root;

  while (dir !== root) {
    const candidate = path.join(dir, CONFIG_FILENAME);
    if (fs.existsSync(candidate)) {
      return { configPath: candidate, repoRoot: resolve_repo_root(dir) };
    }
    dir = path.dirname(dir);
  }
  return null;
}

/**
 * If `dir` is a git worktree (.git is a file pointing to the main repo),
 * return the main repo root. Otherwise return `dir` as-is.
 */
function resolve_repo_root(dir) {
  const git_path = path.join(dir, '.git');
  try {
    const stat = fs.statSync(git_path);
    if (stat.isFile()) {
      // .git file contains: gitdir: /path/to/main/.git/worktrees/<name>
      const content = fs.readFileSync(git_path, 'utf8').trim();
      const match = content.match(/^gitdir:\s*(.+)$/);
      if (match) {
        // Walk up from .git/worktrees/<name> to find the main repo root
        const git_dir = path.resolve(dir, match[1]);
        const worktrees_dir = path.dirname(git_dir);
        if (path.basename(worktrees_dir) === 'worktrees') {
          return path.dirname(path.dirname(worktrees_dir));
        }
      }
    }
  } catch {}
  return dir;
}

// ── Env var template resolver ───────────────────────────────────────────

/**
 * Replace `{PREFIX}` in env var name templates with the actual prefix.
 */
function resolve_env_vars(vars, prefix) {
  if (!vars || !prefix) return vars;
  const resolved = {};
  for (const [key, val] of Object.entries(vars)) {
    if (typeof val === 'string') {
      resolved[key] = val.replace(/\{PREFIX\}/g, prefix);
    } else {
      resolved[key] = val;
    }
  }
  return resolved;
}

// ── Tilde expansion ─────────────────────────────────────────────────────

function expand_tilde(p) {
  if (!p || typeof p !== 'string') return p;
  if (p.startsWith('~/')) {
    return path.join(os.homedir(), p.slice(2));
  }
  return p;
}

// ── Main loader ─────────────────────────────────────────────────────────

/**
 * Load and resolve the workflow config.
 *
 * @param {object} [options]
 * @param {string} [options.cwd]   - Starting directory (default: process.cwd())
 * @param {boolean} [options.required] - Throw if config not found (default: true)
 * @returns {object|null} Resolved config, or null if not found and not required.
 */
function load_config(options = {}) {
  const { cwd = process.cwd(), required = true } = options;

  const found = find_config(cwd);
  if (!found) {
    if (required) {
      throw new Error(
        `Could not find ${CONFIG_FILENAME} in ${cwd} or any parent directory.\n` +
        `Run "workflow init" to create one, or create it manually.`
      );
    }
    return null;
  }

  const { configPath, repoRoot } = found;

  // Load the user config (CommonJS module)
  let user_config;
  try {
    // Clear require cache so re-reads pick up changes
    delete require.cache[require.resolve(configPath)];
    user_config = require(configPath);
  } catch (err) {
    throw new Error(`Failed to load ${configPath}: ${err.message}`);
  }

  // Validate required fields
  if (!user_config.name || typeof user_config.name !== 'string') {
    throw new Error(`${CONFIG_FILENAME}: "name" is required and must be a string.`);
  }

  // Deep merge with defaults
  const merged = deep_merge(DEFAULTS, user_config);

  // ── Computed defaults ──────────────────────────────────────────────

  // repo.worktreesDir: default to "../{name}-worktrees"
  if (!merged.repo.worktreesDir) {
    merged.repo.worktreesDir = `../${merged.name}-worktrees`;
  }

  // env.prefix: default to uppercase project name
  if (!merged.env.prefix) {
    merged.env.prefix = merged.name.toUpperCase().replace(/[^A-Z0-9]/g, '_');
  }

  // Resolve env var templates ({PREFIX} -> actual prefix)
  merged.env.vars = resolve_env_vars(merged.env.vars, merged.env.prefix);

  // ── Path resolution (relative to repoRoot) ────────────────────────

  // Resolve worktreesDir relative to repoRoot
  merged.repo._worktreesDirResolved = path.resolve(repoRoot, merged.repo.worktreesDir);

  // Resolve shared infra composePath
  if (merged.docker.sharedInfra && merged.docker.sharedInfra.composePath) {
    merged.docker.sharedInfra._composePathResolved = path.resolve(
      expand_tilde(merged.docker.sharedInfra.composePath)
    );
  }

  // Resolve proxy dynamicDir (relative to sharedInfra composePath)
  if (merged.docker.proxy && merged.docker.proxy.type === 'traefik' &&
      merged.docker.sharedInfra && merged.docker.sharedInfra._composePathResolved) {
    merged.docker.proxy._dynamicDirResolved = path.join(
      merged.docker.sharedInfra._composePathResolved,
      merged.docker.proxy.dynamicDir
    );
  }

  // Resolve composeFile path
  if (merged.docker.composeFile) {
    merged.docker._composeFileResolved = path.resolve(repoRoot, merged.docker.composeFile);
  }

  // Resolve flow scripts path
  if (merged.paths.flowScripts) {
    merged.paths._flowScriptsResolved = path.resolve(repoRoot, merged.paths.flowScripts);
  }

  // Resolve docker overrides
  if (merged.paths.dockerOverrides) {
    merged.paths._dockerOverridesResolved = path.resolve(repoRoot, merged.paths.dockerOverrides);
  }

  // Resolve build script
  if (merged.paths.buildScript) {
    merged.paths._buildScriptResolved = path.resolve(repoRoot, merged.paths.buildScript);
  }

  // Resolve PM2 ecosystem config path
  if (merged.services.pm2 && merged.services.pm2.ecosystemConfig) {
    merged.services.pm2._ecosystemConfigResolved = path.resolve(
      repoRoot, merged.services.pm2.ecosystemConfig
    );
  }

  // ── Attach metadata ────────────────────────────────────────────────

  merged._repoRoot = repoRoot;
  merged._configPath = configPath;

  return merged;
}

// ── Convenience: derived values ─────────────────────────────────────────

/**
 * Get the container name for a given worktree alias.
 */
function container_name(config, alias) {
  return `${config.name}-${alias}`;
}

/**
 * Get the volume name prefix for a given worktree alias.
 */
function volume_prefix(config, alias) {
  return `${config.name}_${alias}`;
}

/**
 * Get the compose project name for a worktree.
 */
function compose_project(config, alias) {
  return `${config.name}-${alias}`;
}

/**
 * Compute port offset using the configured algorithm.
 */
function compute_offset(config, input_string) {
  const { algorithm, min, range } = config.portOffset;

  if (algorithm === 'sha256') {
    const crypto = require('crypto');
    const hash = crypto.createHash('sha256').update(input_string).digest();
    const num = hash.readUInt32BE(0);
    return (num % range) + min;
  }

  if (algorithm === 'cksum') {
    // Simple cksum-like: sum of char codes
    let sum = 0;
    for (let i = 0; i < input_string.length; i++) {
      sum = ((sum >> 16) + (sum << 16) + input_string.charCodeAt(i)) >>> 0;
    }
    return (sum % range) + min;
  }

  throw new Error(`Unknown port offset algorithm: ${algorithm}`);
}

/**
 * Compute all service ports for a given offset.
 */
function compute_ports(config, offset) {
  const result = {};
  for (const [name, base] of Object.entries(config.services.ports)) {
    result[name] = base + offset;
  }
  return result;
}

/**
 * Get the database name for a worktree alias.
 */
function db_name(config, alias) {
  if (!config.database || !config.database.type) return null;
  const safe_alias = alias.replace(/[^a-zA-Z0-9_]/g, '_');
  return `${config.database.dbNamePrefix || ''}${safe_alias}`;
}

/**
 * Get the domain for a worktree alias.
 */
function domain_for(config, alias) {
  if (!config.docker.proxy || !config.docker.proxy.domainTemplate) return null;
  return config.docker.proxy.domainTemplate.replace(/\{alias\}/g, alias);
}

/**
 * Get the resolved env var name for a logical key.
 * e.g. env_var(config, 'dbConnection') -> 'MYAPP_MONGO_URL'
 */
function env_var(config, key) {
  return config.env.vars[key] || null;
}

/**
 * Get the worktree env var name for a logical key.
 * e.g. worktree_var(config, 'alias') -> 'WORKTREE_ALIAS'
 */
function worktree_var(config, key) {
  return config.env.worktreeVars[key] || null;
}

/**
 * Get the list of services for a given mode.
 * Returns null for "all services" (when mode value is null).
 */
function services_for_mode(config, mode) {
  if (!mode) mode = config.services.defaultMode;
  if (!mode || !config.services.modes[mode]) return null;
  return config.services.modes[mode]; // null = all
}

/**
 * Resolve a service mode/group spec into a flat set of service names.
 *
 * Accepts:
 *   - 'full' or null  -> all services (from services.ports keys)
 *   - 'minimal'       -> services.modes.minimal list
 *   - any mode name   -> services.modes[mode] list
 *   - comma-separated -> expand each token as group name or service name
 *
 * When services.groups is defined, group names are expanded to their members.
 * If a 'core' group exists, its services are always included.
 *
 * @param {object} config
 * @param {string} [mode] - Mode name, preset, or comma-separated groups/services
 * @returns {Set<string>} Set of service names
 */
function resolve_services(config, mode) {
  const all_names = Object.keys(config.services.ports);
  const groups = config.services.groups;

  if (!mode || mode === 'full') {
    return new Set(all_names);
  }

  // Check if it's a named mode first
  if (config.services.modes && config.services.modes[mode]) {
    const mode_list = config.services.modes[mode];
    if (mode_list === null) return new Set(all_names);
    const result = new Set(mode_list);
    // Always include core group if defined
    if (groups && groups.core) {
      for (const name of groups.core) result.add(name);
    }
    return result;
  }

  // Comma-separated group/service names
  const tokens = mode.split(',').map((s) => s.trim()).filter(Boolean);
  const result = new Set();

  for (const token of tokens) {
    if (groups && groups[token]) {
      for (const name of groups[token]) result.add(name);
    } else if (all_names.includes(token)) {
      result.add(token);
    }
  }

  // Always include core group if defined
  if (groups && groups.core) {
    for (const name of groups.core) result.add(name);
  }

  return result;
}

/**
 * Check if a feature is enabled.
 */
function feature_enabled(config, feature_name) {
  const val = config.features[feature_name];
  if (val === null || val === undefined || val === false) return false;
  if (is_plain_object(val)) return val.enabled !== false;
  return true;
}

// ── Shared compose info ─────────────────────────────────────────────────

/**
 * For worktrees using the "shared" compose strategy, return the compose file,
 * project name, slug, and env vars needed to run docker compose commands.
 * Returns null for "generate" strategy or when config is missing.
 */
function get_compose_info(config, worktree_path) {
  if (!config || config.docker.composeStrategy === 'generate' || !config.docker._composeFileResolved) {
    return null;
  }

  const env_filename = config.env.filename || '.env.worktree';
  const env_path = path.join(worktree_path, env_filename);
  if (!fs.existsSync(env_path)) return null;

  const content = fs.readFileSync(env_path, 'utf8');
  const slug_match = content.match(/^BRANCH_SLUG=(.+)$/m);
  if (!slug_match) return null;

  const slug = slug_match[1].trim();
  const compose_file = config.docker._composeFileResolved;
  const project = compose_project(config, slug);

  // Build env vars from .env.worktree
  const env = {};
  env.REPO_ROOT = config._repoRoot;
  env.PROJECT_ROOT = worktree_path;
  env.BRANCH_SLUG = slug;
  for (const name of Object.keys(config.services.ports)) {
    const key = `${name.toUpperCase()}_PORT`;
    const val_match = content.match(new RegExp(`^${key}=(.+)$`, 'm'));
    if (val_match) env[key] = val_match[1].trim();
  }

  return { compose_file, project, slug, env };
}

// ── Exports ─────────────────────────────────────────────────────────────

module.exports = {
  CONFIG_FILENAME,
  load_config,
  find_config,

  // Derived value helpers
  container_name,
  volume_prefix,
  compose_project,
  compute_offset,
  compute_ports,
  db_name,
  domain_for,
  env_var,
  worktree_var,
  services_for_mode,
  resolve_services,
  feature_enabled,
  get_compose_info,
};
