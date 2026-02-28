#!/usr/bin/env node

/**
 * workflow-init.js — Smart project scanner to generate workflow.config.js.
 *
 * Usage:
 *   wt init [directory]       Scan project and generate workflow.config.js
 *   wt init --personalize     Update user-specific values in existing config
 *
 * Flags:
 *   --force                   Overwrite existing workflow.config.js
 *   --dry-run                 Print what would be generated, don't write
 *   --personalize             Detect and update user-specific values (claude path)
 *
 * Default directory is CWD.
 */

const fs = require('fs');
const os = require('os');
const path = require('path');
const { execSync } = require('child_process');

// ── Constants ────────────────────────────────────────────────────────────────

const CONFIG_FILENAME = 'workflow.config.js';

const DB_DEFAULTS = {
  mongodb: { host: 'localhost', port: 27017, defaultDb: 'db' },
  postgres: { host: 'localhost', port: 5432, defaultDb: 'postgres' },
  supabase: { host: null, port: null, defaultDb: null },
  mysql: { host: 'localhost', port: 3306, defaultDb: 'mydb' },
};

const DEV_COMMANDS = {
  node: 'pnpm dev',
  go: 'go run .',
  rust: 'cargo run',
  python: 'python -m flask run',
  generic: 'make dev',
};

// Images that are infrastructure, not user services
const INFRA_IMAGES = ['mongo', 'postgres', 'redis', 'mysql', 'mariadb', 'traefik', 'nginx', 'haproxy', 'mailhog', 'adminer', 'pgadmin', 'phpmyadmin', 'zookeeper', 'kafka', 'rabbitmq', 'elasticsearch', 'minio', 'localstack', 'memcached'];

// Env var patterns that hint at specific database types
const DB_ENV_PATTERNS = [
  { pattern: /SUPABASE_URL/i, type: 'supabase' },
  { pattern: /DATABASE_URL.*postgres/i, type: 'postgres' },
  { pattern: /POSTGRES/i, type: 'postgres' },
  { pattern: /MONGO_URI|MONGODB_URI|MONGO_URL/i, type: 'mongodb' },
  { pattern: /MYSQL/i, type: 'mysql' },
];

// Env var patterns that hint at shared strategy variables (wt-specific)
const SHARED_STRATEGY_VARS = ['BRANCH_SLUG', 'PROJECT_ROOT', 'REPO_ROOT'];

// Generic env vars that do NOT indicate shared strategy
const GENERIC_ENV_VARS = ['NODE_ENV', 'PORT', 'HOME', 'USER', 'PATH', 'PWD'];

// ── Utility Helpers ──────────────────────────────────────────────────────────

function file_exists(filepath) {
  try {
    fs.accessSync(filepath, fs.constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

function read_json(filepath) {
  try {
    return JSON.parse(fs.readFileSync(filepath, 'utf8'));
  } catch {
    return null;
  }
}

function read_text(filepath) {
  try {
    return fs.readFileSync(filepath, 'utf8');
  } catch {
    return null;
  }
}

function dir_exists(dirpath) {
  try {
    return fs.statSync(dirpath).isDirectory();
  } catch {
    return false;
  }
}

// ── Phase 1: Foundation Detectors ────────────────────────────────────────────

/**
 * Detect project type, name, package manager, and dev command.
 */
function detect_project(dir) {
  const result = {
    type: 'generic',
    name: path.basename(dir),
    packageManager: null,
    devCommand: DEV_COMMANDS.generic,
    pkg: null,
  };

  // Node.js
  const pkg_path = path.join(dir, 'package.json');
  if (file_exists(pkg_path)) {
    const pkg = read_json(pkg_path);
    result.type = 'node';
    result.pkg = pkg;
    if (pkg && pkg.name) {
      result.name = pkg.name.replace(/^@[^/]+\//, ''); // strip scope
    }

    // Detect package manager
    if (file_exists(path.join(dir, 'pnpm-lock.yaml'))) {
      result.packageManager = 'pnpm';
      result.devCommand = 'pnpm dev';
    } else if (file_exists(path.join(dir, 'yarn.lock'))) {
      result.packageManager = 'yarn';
      result.devCommand = 'yarn dev';
    } else if (file_exists(path.join(dir, 'bun.lockb')) || file_exists(path.join(dir, 'bun.lock'))) {
      result.packageManager = 'bun';
      result.devCommand = 'bun dev';
    } else {
      result.packageManager = 'npm';
      result.devCommand = 'npm run dev';
    }
    return result;
  }

  // Go
  const gomod_path = path.join(dir, 'go.mod');
  if (file_exists(gomod_path)) {
    result.type = 'go';
    const gomod = read_text(gomod_path);
    if (gomod) {
      const m = gomod.match(/^module\s+(\S+)/m);
      if (m) result.name = m[1].split('/').pop();
    }
    result.devCommand = DEV_COMMANDS.go;
    return result;
  }

  // Rust
  const cargo_path = path.join(dir, 'Cargo.toml');
  if (file_exists(cargo_path)) {
    result.type = 'rust';
    const cargo = read_text(cargo_path);
    if (cargo) {
      const m = cargo.match(/^\s*name\s*=\s*"([^"]+)"/m);
      if (m) result.name = m[1];
    }
    result.devCommand = DEV_COMMANDS.rust;
    return result;
  }

  // Python
  if (file_exists(path.join(dir, 'pyproject.toml'))) {
    result.type = 'python';
    const pyproj = read_text(path.join(dir, 'pyproject.toml'));
    if (pyproj) {
      const m = pyproj.match(/^\s*name\s*=\s*"([^"]+)"/m);
      if (m) result.name = m[1];
    }
    result.devCommand = DEV_COMMANDS.python;
    return result;
  }
  if (file_exists(path.join(dir, 'setup.py'))) {
    result.type = 'python';
    result.devCommand = DEV_COMMANDS.python;
    return result;
  }

  return result;
}

/**
 * Find docker-compose files in the target directory.
 */
function find_compose_files(dir) {
  const patterns = [
    'docker-compose.yml',
    'docker-compose.yaml',
    'docker-compose.dev.yml',
    'docker-compose.dev.yaml',
    'docker-compose.override.yml',
    'docker-compose.override.yaml',
    'compose.yml',
    'compose.yaml',
    'compose.dev.yml',
    'compose.dev.yaml',
  ];
  const subdirs = ['docker', '.docker'];
  const found = [];

  for (const p of patterns) {
    if (file_exists(path.join(dir, p))) found.push(p);
  }
  for (const sub of subdirs) {
    const sub_dir = path.join(dir, sub);
    if (dir_exists(sub_dir)) {
      for (const p of patterns) {
        if (file_exists(path.join(sub_dir, p))) found.push(`${sub}/${p}`);
      }
    }
  }
  return found;
}

/**
 * Detect .env files at root and in common app directories.
 */
function detect_env_files(dir) {
  const names = ['.env', '.env.example', '.env.local', '.env.development'];
  const found = [];

  // Root level
  for (const name of names) {
    if (file_exists(path.join(dir, name))) found.push(name);
  }

  // apps/*/ and packages/*/
  for (const sub of ['apps', 'packages']) {
    const sub_dir = path.join(dir, sub);
    if (!dir_exists(sub_dir)) continue;
    try {
      const entries = fs.readdirSync(sub_dir, { withFileTypes: true });
      for (const entry of entries) {
        if (!entry.isDirectory()) continue;
        for (const name of names) {
          const p = path.join(sub, entry.name, name);
          if (file_exists(path.join(dir, p))) found.push(p);
        }
      }
    } catch { /* skip */ }
  }

  return found;
}

/**
 * Detect git remote refs (origin branches).
 */
function detect_git_refs(dir) {
  try {
    const output = execSync('git for-each-ref --format="%(refname:short)" refs/remotes/origin', {
      cwd: dir,
      encoding: 'utf8',
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    const refs = output.trim().split('\n').filter(Boolean);
    // Find main/master/develop
    const base_refs = [];
    for (const ref of refs) {
      const short = ref.replace('origin/', '');
      if (['main', 'master', 'develop'].includes(short)) {
        base_refs.push(ref);
      }
    }
    return base_refs;
  } catch {
    return [];
  }
}

/**
 * Detect an existing Traefik instance on the Docker host.
 * Returns { detected: true, network: string } or null.
 */
function detect_traefik() {
  try {
    // Find any container using a traefik image (running or stopped)
    const ps_output = execSync(
      'docker ps -a --filter "ancestor=traefik" --format "{{.ID}}"',
      { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }
    ).trim();

    if (!ps_output) {
      // Also try with traefik:* pattern
      const ps_output2 = execSync(
        'docker ps -a --format json',
        { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }
      ).trim();
      if (!ps_output2) return null;

      const lines = ps_output2.split('\n').filter(Boolean);
      for (const line of lines) {
        try {
          const info = JSON.parse(line);
          if (info.Image && info.Image.startsWith('traefik')) {
            return inspect_traefik_container(info.ID);
          }
        } catch { /* skip invalid json */ }
      }
      return null;
    }

    const container_id = ps_output.split('\n')[0].trim();
    return inspect_traefik_container(container_id);
  } catch {
    return null;
  }
}

function inspect_traefik_container(container_id) {
  try {
    const inspect_output = execSync(
      `docker inspect --format '{{json .NetworkSettings.Networks}}' "${container_id}"`,
      { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }
    ).trim();

    const networks = JSON.parse(inspect_output);
    const network_names = Object.keys(networks).filter(n => n !== 'bridge' && n !== 'host' && n !== 'none');
    const network = network_names[0] || 'web';

    return { detected: true, network };
  } catch {
    return { detected: true, network: 'web' };
  }
}

/**
 * Detect monorepo tooling (turbo or nx).
 */
function detect_monorepo(dir) {
  if (file_exists(path.join(dir, 'turbo.json'))) {
    return { type: 'turbo', devCommand: 'turbo dev' };
  }
  if (file_exists(path.join(dir, 'nx.json'))) {
    return { type: 'nx', devCommand: 'nx serve' };
  }
  return null;
}

// ── Phase 2: Parsing ─────────────────────────────────────────────────────────

/**
 * Parse docker-compose YAML using a line-by-line state machine.
 * No npm YAML dependency needed — compose files have predictable structure.
 *
 * Returns:
 *   { services: { [name]: { image, ports, containerName } },
 *     databases: [{ name, type }],
 *     redis: { name, containerHost, port } | null,
 *     has_env_substitution: boolean,
 *     shared_strategy_vars: string[],
 *     container_prefix: string | null }
 */
function parse_compose_services(text) {
  const result = {
    services: {},
    databases: [],
    redis: null,
    has_env_substitution: false,
    shared_strategy_vars: [],
    container_prefix: null,
  };

  if (!text) return result;

  const lines = text.split('\n');
  let in_services = false;
  let current_service = null;
  let in_ports = false;
  let service_indent = -1;

  // Track all ${VAR} references
  const var_refs = new Set();
  const var_re = /\$\{([A-Z_]+)(?::?-[^}]*)?\}/g;

  // Scan entire file for variable references
  for (const line of lines) {
    let m;
    while ((m = var_re.exec(line)) !== null) {
      var_refs.add(m[1]);
    }
  }

  // Check for shared strategy variables
  for (const v of SHARED_STRATEGY_VARS) {
    if (var_refs.has(v)) {
      result.shared_strategy_vars.push(v);
    }
  }
  result.has_env_substitution = var_refs.size > 0;

  // Port regex: handles "3000:3000", "${WEB_PORT:-3000}:3000", "${WEB_PORT}:3000"
  const port_re = /^"?(?:\$\{([A-Z_]+)(?::?-(\d+))?\}|(\d+)):?(\d+)?"?$/;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trimEnd();
    if (!trimmed || trimmed.startsWith('#')) continue;

    // Measure indent
    const indent = line.length - line.trimStart().length;
    const content = line.trimStart();

    // Top-level section detection
    if (indent === 0 && content.startsWith('services:')) {
      in_services = true;
      current_service = null;
      in_ports = false;
      continue;
    }
    if (indent === 0 && !content.startsWith(' ') && !content.startsWith('#')) {
      // New top-level section (volumes:, networks:, etc.)
      if (content.match(/^[a-z]/)) {
        in_services = false;
        current_service = null;
        in_ports = false;
      }
      continue;
    }

    if (!in_services) continue;

    // Service name detection (indent 2)
    if (indent === 2 && content.match(/^[a-z_][a-z0-9_-]*:$/i)) {
      const name = content.replace(':', '');
      current_service = name;
      service_indent = indent;
      in_ports = false;
      result.services[name] = { image: null, ports: [], containerName: null };
      continue;
    }

    if (!current_service) continue;

    // Properties within a service (indent 4+)
    if (indent <= service_indent) {
      // Back to service level or above
      if (indent <= 2 && content.match(/^[a-z_][a-z0-9_-]*:$/i)) {
        const name = content.replace(':', '');
        current_service = name;
        in_ports = false;
        result.services[name] = { image: null, ports: [], containerName: null };
      } else {
        current_service = null;
        in_ports = false;
      }
      continue;
    }

    // Key-value parsing within a service
    if (content.startsWith('image:')) {
      const val = content.replace('image:', '').trim();
      result.services[current_service].image = val;
      in_ports = false;
      continue;
    }

    if (content.startsWith('container_name:')) {
      const val = content.replace('container_name:', '').trim();
      result.services[current_service].containerName = val;
      in_ports = false;
      continue;
    }

    if (content === 'ports:') {
      in_ports = true;
      continue;
    }

    // Port array entries
    if (in_ports && content.startsWith('- ')) {
      const port_str = content.replace(/^-\s*/, '').trim().replace(/^["']|["']$/g, '');
      const pm = port_str.match(port_re);
      if (pm) {
        const host_port = pm[3] ? parseInt(pm[3], 10) : (pm[2] ? parseInt(pm[2], 10) : null);
        const container_port = pm[4] ? parseInt(pm[4], 10) : host_port;
        const env_var = pm[1] || null;
        result.services[current_service].ports.push({
          host: host_port,
          container: container_port,
          envVar: env_var,
        });
      }
      continue;
    }

    // Any other key ends the ports array
    if (in_ports && !content.startsWith('- ')) {
      in_ports = false;
    }
  }

  // Post-process: classify services
  for (const [name, svc] of Object.entries(result.services)) {
    const image_lower = (svc.image || '').toLowerCase();

    // Check if this is a database
    if (image_lower.match(/^(mongo|postgres|mysql|mariadb)/)) {
      const type = image_lower.startsWith('mongo') ? 'mongodb'
        : image_lower.startsWith('postgres') ? 'postgres'
        : 'mysql';
      result.databases.push({ name, type, containerHost: name });
    }

    // Check if this is redis
    if (image_lower.startsWith('redis')) {
      const port = svc.ports.length > 0 ? (svc.ports[0].container || 6379) : 6379;
      result.redis = { name, containerHost: name, port };
    }

    // Extract container prefix from container_name pattern
    if (svc.containerName) {
      // Pattern: prefix-${BRANCH_SLUG}-service or prefix-${BRANCH_SLUG:-dev}-service
      const prefix_match = svc.containerName.match(/^([a-z0-9]+)-\$\{/i);
      if (prefix_match && !result.container_prefix) {
        result.container_prefix = prefix_match[1].toLowerCase();
      }
    }
  }

  return result;
}

/**
 * Scan .env.example files for database type hints and AWS detection.
 */
function detect_env_hints(dir, env_files) {
  const result = { dbType: null, awsDetected: false };

  // Prefer .env.example files for hints
  const example_files = env_files.filter(f => f.includes('.example'));
  const files_to_scan = example_files.length > 0 ? example_files : env_files;

  for (const f of files_to_scan) {
    const content = read_text(path.join(dir, f));
    if (!content) continue;

    // Database hints
    for (const { pattern, type } of DB_ENV_PATTERNS) {
      if (pattern.test(content) && !result.dbType) {
        result.dbType = type;
      }
    }

    // AWS detection
    if (/AWS_ACCESS_KEY|AWS_SECRET|AWS_REGION/i.test(content)) {
      result.awsDetected = true;
    }
  }

  return result;
}

// ── Phase 3: Assembly ────────────────────────────────────────────────────────

/**
 * Determine compose strategy based on compose files and parsed content.
 */
function detect_strategy(compose_files, parsed) {
  if (compose_files.length === 0) {
    return { strategy: 'generate', composeFile: null };
  }

  // If compose has BRANCH_SLUG or PROJECT_ROOT → shared strategy
  const has_shared_vars = parsed.shared_strategy_vars.some(
    v => !GENERIC_ENV_VARS.includes(v)
  );

  if (has_shared_vars) {
    // Prefer *.dev.* variant, then first found
    const dev_file = compose_files.find(f => f.includes('.dev.'));
    const chosen = dev_file || compose_files[0];
    return { strategy: chosen, composeFile: chosen };
  }

  return { strategy: 'generate', composeFile: compose_files[0] || null };
}

/**
 * Detect services from compose parsing and package.json scripts.
 * Returns { ports: { name: port }, primary: string }.
 */
function detect_services(parsed, pkg) {
  const ports = {};
  const service_names = [];

  // From compose: filter out infra services
  for (const [name, svc] of Object.entries(parsed.services)) {
    const image_lower = (svc.image || '').toLowerCase();
    const is_infra = INFRA_IMAGES.some(infra => image_lower.startsWith(infra));
    if (is_infra) continue;

    // Get port from compose mapping
    let port = null;
    if (svc.ports.length > 0) {
      port = svc.ports[0].host || svc.ports[0].container;
    }
    if (port) {
      ports[name] = port;
      service_names.push(name);
    }
  }

  // From package.json scripts (if no compose services found)
  if (service_names.length === 0 && pkg && pkg.scripts) {
    const port_re = /--port[= ](\d+)|PORT=(\d+)|-p[= ](\d+)/;
    for (const [script_name, cmd] of Object.entries(pkg.scripts)) {
      if (script_name.startsWith('dev:') || script_name.startsWith('start:')) {
        const svc_name = script_name.split(':')[1];
        const m = cmd.match(port_re);
        const port = m ? parseInt(m[1] || m[2] || m[3], 10) : null;
        if (svc_name && port) {
          ports[svc_name] = port;
          service_names.push(svc_name);
        }
      }
    }
  }

  // Determine primary: prefer 'web', then 'app', then 'frontend', then first
  const primary = service_names.includes('web') ? 'web'
    : service_names.includes('app') ? 'app'
    : service_names.includes('frontend') ? 'frontend'
    : service_names[0] || null;

  return { ports, primary };
}

/**
 * Detect database from compose parsing and env hints.
 */
function detect_database(parsed, env_hints) {
  // Compose images take priority
  if (parsed.databases.length > 0) {
    const db = parsed.databases[0];
    const defaults = DB_DEFAULTS[db.type] || {};
    return {
      type: db.type,
      source: 'compose',
      host: defaults.host,
      containerHost: db.containerHost,
      port: defaults.port,
      defaultDb: defaults.defaultDb,
      extras: parsed.databases.length > 1
        ? parsed.databases.slice(1).map(d => d.type)
        : null,
    };
  }

  // Fall back to env hints
  if (env_hints.dbType) {
    const defaults = DB_DEFAULTS[env_hints.dbType] || {};
    return {
      type: env_hints.dbType,
      source: 'env',
      host: defaults.host,
      port: defaults.port,
      defaultDb: defaults.defaultDb,
      containerHost: null,
      extras: null,
    };
  }

  return { type: null, source: null };
}

/**
 * Detect redis from compose parsing.
 */
function detect_redis(parsed) {
  return parsed.redis || null;
}

/**
 * Detect the claude CLI binary path.
 */
function detect_claude() {
  try {
    const p = execSync('which claude', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }).trim();
    if (p) return p;
  } catch { /* not in PATH */ }

  const home = os.homedir();

  const local_path = path.join(home, '.claude', 'local', 'claude');
  if (file_exists(local_path)) return local_path;

  const nvm_dir = path.join(home, '.nvm', 'versions', 'node');
  if (dir_exists(nvm_dir)) {
    try {
      const entries = fs.readdirSync(nvm_dir).sort().reverse();
      for (const entry of entries) {
        const p = path.join(nvm_dir, entry, 'bin', 'claude');
        if (file_exists(p)) return p;
      }
    } catch { /* skip */ }
  }

  const cursor_dir = path.join(home, '.cursor', 'extensions');
  if (dir_exists(cursor_dir)) {
    try {
      const entries = fs.readdirSync(cursor_dir).sort().reverse();
      for (const entry of entries) {
        if (entry.startsWith('anthropic.claude-code')) {
          const p = path.join(cursor_dir, entry, 'resources', 'native-binary', 'claude');
          if (file_exists(p)) return p;
        }
      }
    } catch { /* skip */ }
  }

  for (const p of ['/opt/homebrew/bin/claude', '/usr/local/bin/claude']) {
    if (file_exists(p)) return p;
  }

  return null;
}

// ── Phase 4: Output ──────────────────────────────────────────────────────────

/**
 * Assemble the full config object from all detections.
 * Only includes non-default values.
 */
function assemble_config(detections) {
  const { project, compose_files, parsed, strategy, services, database, redis, env_files, env_hints, git_refs, monorepo, traefik } = detections;

  // Use container prefix from compose as name if available
  const name = (parsed.container_prefix || project.name)
    .toLowerCase()
    .replace(/[^a-z0-9_-]/g, '-');

  const config = { name };

  // Dev command: monorepo tool overrides project default
  const devCommand = monorepo ? monorepo.devCommand : project.devCommand;

  // Docker section
  const docker = {};
  if (strategy.strategy !== 'generate') {
    docker.composeStrategy = strategy.strategy;
    docker.composeFile = strategy.composeFile;
    docker.generate = null;
  }

  // Env files: only .env files (not .example)
  const actual_env_files = env_files.filter(f => !f.includes('.example') && !f.includes('.local'));
  if (actual_env_files.length > 0) {
    docker.envFiles = actual_env_files;
  }

  if (traefik && traefik.detected) {
    docker.proxy = { type: 'traefik', domainTemplate: '{alias}.localhost' };
    docker.sharedInfra = { network: traefik.network };
  } else {
    docker.proxy = { type: 'ports' };
  }

  if (Object.keys(docker).length > 0) {
    config.docker = docker;
  }

  // Services
  if (Object.keys(services.ports).length > 0) {
    config.services = {
      ports: services.ports,
      primary: services.primary,
      quickLinks: Object.keys(services.ports).map(name => ({
        label: name.charAt(0).toUpperCase() + name.slice(1),
        service: name,
        pathPrefix: '',
      })),
    };
  }

  // Database
  if (database.type) {
    const db_config = { type: database.type };
    if (database.type === 'supabase') {
      // Supabase is cloud-hosted, no local config needed
    } else {
      if (database.host) db_config.host = database.host;
      if (database.containerHost) db_config.containerHost = database.containerHost;
      if (database.port) db_config.port = database.port;
      if (database.defaultDb) db_config.defaultDb = database.defaultDb;
    }
    config.database = db_config;
  } else {
    config.database = { type: null };
  }

  // Redis
  if (redis) {
    config.redis = {
      containerHost: redis.containerHost,
      port: redis.port,
    };
  }

  // Dash
  const claude_path = detect_claude();
  config.dash = {
    commands: {
      shell: { label: 'Shell', cmd: 'bash' },
      claude: { label: 'Claude', cmd: claude_path || 'claude' },
      dev: { label: 'Dev', cmd: devCommand },
    },
    localDevCommand: devCommand,
  };

  // dash.services: for shared compose strategy, services are separate containers
  // (no pm2 inside), so use static discovery with devTab running check.
  // For generate strategy, pm2 is the default — omit to keep config minimal.
  if (strategy.strategy !== 'generate' && Object.keys(services.ports).length > 0) {
    config.dash.services = {
      manager: 'static',
      list: Object.entries(services.ports).map(([name, port]) => ({ name, port })),
      runningCheck: 'devTab',
    };
  }

  return config;
}

/**
 * Generate the workflow.config.js file content as a JS source string.
 */
function generate_config_file(config, detections) {
  const lines = [];
  const { strategy, compose_files, database, monorepo, env_hints } = detections;

  lines.push('// workflow.config.js \u2014 Workflow configuration');
  lines.push(`// Generated by: wt init on ${new Date().toISOString().split('T')[0]}`);
  if (strategy.composeFile) {
    lines.push(`// Detected from: ${strategy.composeFile}`);
  }
  lines.push('');
  lines.push('module.exports = {');

  // name
  const name_comment = detections.parsed.container_prefix
    ? '  // from container_name prefix in compose'
    : '';
  lines.push(`  name: '${config.name}',${name_comment}`);
  lines.push('');

  // docker
  if (config.docker) {
    lines.push('  docker: {');
    if (config.docker.composeStrategy) {
      lines.push(`    composeStrategy: '${config.docker.composeStrategy}',`);
      lines.push(`    composeFile: '${config.docker.composeFile}',`);
      lines.push('    generate: null,');
    }
    if (config.docker.envFiles) {
      const files_str = config.docker.envFiles.map(f => `'${f}'`).join(', ');
      lines.push(`    envFiles: [${files_str}],`);
    }
    if (config.docker.proxy) {
      if (config.docker.proxy.domainTemplate) {
        lines.push('    proxy: {');
        lines.push(`      type: '${config.docker.proxy.type}',`);
        lines.push(`      domainTemplate: '${config.docker.proxy.domainTemplate}',`);
        lines.push('    },');
      } else {
        lines.push(`    proxy: { type: '${config.docker.proxy.type}' },`);
      }
    }
    if (config.docker.sharedInfra) {
      lines.push('    sharedInfra: {');
      lines.push(`      network: '${config.docker.sharedInfra.network}',`);
      lines.push('    },');
    }
    lines.push('  },');
    lines.push('');
  }

  // services
  if (config.services) {
    lines.push('  services: {');
    lines.push('    ports: {');
    for (const [name, port] of Object.entries(config.services.ports)) {
      lines.push(`      ${name}: ${port},`);
    }
    lines.push('    },');
    if (config.services.primary) {
      lines.push(`    primary: '${config.services.primary}',`);
    }
    if (config.services.quickLinks && config.services.quickLinks.length > 0) {
      lines.push('    quickLinks: [');
      for (const link of config.services.quickLinks) {
        lines.push(`      { label: '${link.label}', service: '${link.service}', pathPrefix: '${link.pathPrefix}' },`);
      }
      lines.push('    ],');
    }
    lines.push('  },');
    lines.push('');
  }

  // database
  if (config.database) {
    if (config.database.type) {
      const source_comment = database.source === 'env'
        ? `  // detected from ${env_hints.dbType ? '.env.example' : 'env files'}`
        : database.source === 'compose' ? '  // detected from compose' : '';
      const db_parts = [`type: '${config.database.type}'`];
      if (config.database.host) db_parts.push(`host: '${config.database.host}'`);
      if (config.database.containerHost) db_parts.push(`containerHost: '${config.database.containerHost}'`);
      if (config.database.port) db_parts.push(`port: ${config.database.port}`);
      if (config.database.defaultDb) db_parts.push(`defaultDb: '${config.database.defaultDb}'`);

      if (db_parts.length <= 2) {
        lines.push(`  database: { ${db_parts.join(', ')} },${source_comment}`);
      } else {
        lines.push(`  database: {${source_comment}`);
        for (const part of db_parts) {
          lines.push(`    ${part},`);
        }
        lines.push('  },');
      }
      if (database.extras) {
        lines.push(`  // also detected in compose: ${database.extras.join(', ')}`);
      }
    } else {
      lines.push('  database: { type: null },');
    }
    lines.push('');
  }

  // redis
  if (config.redis) {
    lines.push(`  redis: { containerHost: '${config.redis.containerHost}', port: ${config.redis.port} },`);
  } else {
    lines.push('  redis: null,');
  }
  lines.push('');

  // dash
  if (config.dash) {
    lines.push('  dash: {');
    lines.push('    commands: {');
    for (const [key, val] of Object.entries(config.dash.commands)) {
      const pad = key.length < 6 ? ' '.repeat(6 - key.length) : '';
      lines.push(`      ${key}:${pad} { label: '${val.label}', cmd: '${val.cmd}' },`);
    }
    lines.push('    },');
    lines.push(`    localDevCommand: '${config.dash.localDevCommand}',`);
    if (config.dash.services) {
      const svc = config.dash.services;
      lines.push('    services: {');
      lines.push(`      manager: '${svc.manager}',`);
      if (svc.list && svc.list.length > 0) {
        lines.push('      list: [');
        for (const entry of svc.list) {
          lines.push(`        { name: '${entry.name}', port: ${entry.port} },`);
        }
        lines.push('      ],');
      }
      lines.push(`      runningCheck: '${svc.runningCheck}',`);
      lines.push('    },');
    }
    lines.push('  },');
  }

  lines.push('};');
  lines.push('');

  return lines.join('\n');
}

/**
 * Print a styled summary of the detected configuration using @clack/prompts.
 */
async function print_summary(config, detections) {
  const p = await import('@clack/prompts');
  const { project, strategy, services, database, redis, monorepo, git_refs, compose_files, env_files, parsed, traefik } = detections;

  const strategy_label = strategy.strategy === 'generate'
    ? 'generate'
    : `shared \u2192 ${strategy.composeFile}`;

  const services_label = Object.keys(services.ports).length > 0
    ? Object.entries(services.ports).map(([n, p]) => `${n}:${p}`).join(', ')
    : 'none detected';

  const db_label = database.type
    ? `${database.type}${database.source === 'env' ? ' (from .env.example)' : ' (from compose)'}`
    : 'not detected';

  const redis_label = redis ? `${redis.containerHost}:${redis.port}` : 'not detected';

  const monorepo_label = monorepo ? monorepo.type : 'none';

  const refs_label = git_refs.length > 0 ? git_refs.join(', ') : 'not detected';

  const dev_label = config.dash ? config.dash.localDevCommand : project.devCommand;

  const pm_label = project.packageManager ? `${project.type}, ${project.packageManager}` : project.type;

  const proxy_label = traefik && traefik.detected
    ? `traefik (${traefik.network} network)`
    : 'ports';

  const summary_lines = [
    `  Project      ${config.name} (${pm_label})`,
    `  Compose      ${strategy_label}`,
    `  Services     ${services_label}`,
    `  Primary      ${services.primary || 'none'}`,
    `  Proxy        ${proxy_label}`,
    `  Database     ${db_label}`,
    `  Redis        ${redis_label}`,
    `  Monorepo     ${monorepo_label}`,
    `  Base refs    ${refs_label}`,
    `  Dev command  ${dev_label}`,
  ];

  p.note(summary_lines.join('\n'), `Scanned ${detections.dir}`);
}

// ── Personalize Mode ─────────────────────────────────────────────────────────

/**
 * Update user-specific values in an existing workflow.config.js.
 * Reads the file as text and does surgical replacements to preserve
 * formatting, comments, and all non-personalized values.
 */
async function personalize(target_dir) {
  const p = await import('@clack/prompts');

  const config_path = path.join(target_dir, CONFIG_FILENAME);
  if (!file_exists(config_path)) {
    p.log.error(`${CONFIG_FILENAME} not found. Copy the template first, then run --personalize.`);
    process.exit(1);
  }

  p.intro('wt init --personalize');

  // Load existing config to read current values
  let config;
  try {
    delete require.cache[require.resolve(config_path)];
    config = require(config_path);
  } catch (err) {
    p.log.error(`Failed to load ${CONFIG_FILENAME}: ${err.message}`);
    process.exit(1);
  }

  // Read file as text for surgical edits
  let config_text = read_text(config_path);
  if (!config_text) {
    p.log.error(`Failed to read ${CONFIG_FILENAME}`);
    process.exit(1);
  }

  const changes = [];

  // ── Claude binary path ──────────────────────────────────────────────
  const claude_path = detect_claude();
  const current_claude = config && config.dash && config.dash.commands
    && config.dash.commands.claude && config.dash.commands.claude.cmd;

  if (claude_path && claude_path !== current_claude) {
    // Match the claude command line: `claude: { ... cmd: 'value' ... }`
    // Handles single-line format with single or double quotes
    const claude_re = /(claude:\s*\{[^}]*?cmd:\s*)(["'])([^"']*?)\2/;
    if (claude_re.test(config_text)) {
      config_text = config_text.replace(claude_re, `$1$2${claude_path}$2`);
      changes.push(`claude cmd: '${current_claude || 'claude'}' \u2192 '${claude_path}'`);
    }
  } else if (!claude_path) {
    p.log.warn('Could not detect claude binary. Skipping claude path update.');
  }

  if (changes.length === 0) {
    p.log.info('No changes needed \u2014 config is already personalized.');
    p.outro('Done');
    return;
  }

  // Show changes
  p.note(changes.map(c => `  ${c}`).join('\n'), 'Detected changes');

  // Write updated config
  fs.writeFileSync(config_path, config_text, 'utf8');
  p.log.success(`Updated ${CONFIG_FILENAME}`);
  p.outro('Done');
}

// ── CLI Entry Point ──────────────────────────────────────────────────────────

async function main() {
  // Parse CLI args
  const args = process.argv.slice(2);
  let target_dir = null;
  let force = false;
  let dry_run = false;
  let do_personalize = false;

  for (const arg of args) {
    if (arg === '--force') { force = true; continue; }
    if (arg === '--dry-run') { dry_run = true; continue; }
    if (arg === '--personalize') { do_personalize = true; continue; }
    if (!arg.startsWith('-') && !target_dir) { target_dir = arg; continue; }
  }

  target_dir = path.resolve(target_dir || process.cwd());

  // Personalize mode: update user-specific values in existing config
  if (do_personalize) {
    return personalize(target_dir);
  }

  const p = await import('@clack/prompts');

  p.intro('wt init');

  // Verify target directory exists
  if (!dir_exists(target_dir)) {
    p.log.error(`Directory does not exist: ${target_dir}`);
    process.exit(1);
  }

  // Check for existing config
  const config_path = path.join(target_dir, CONFIG_FILENAME);
  if (file_exists(config_path) && !force && !dry_run) {
    p.log.error(`${CONFIG_FILENAME} already exists. Use --force to overwrite.`);
    process.exit(1);
  }

  // ── Phase 1: Foundation ────────────────────────────────────────────────
  const project = detect_project(target_dir);
  const compose_files = find_compose_files(target_dir);
  const env_files = detect_env_files(target_dir);
  const git_refs = detect_git_refs(target_dir);
  const monorepo = detect_monorepo(target_dir);
  const traefik = detect_traefik();

  // ── Phase 2: Parsing ──────────────────────────────────────────────────
  // Pick best compose file to parse: prefer *.dev.*, then first found
  let compose_text = null;
  let compose_to_parse = null;
  if (compose_files.length > 0) {
    compose_to_parse = compose_files.find(f => f.includes('.dev.')) || compose_files[0];
    compose_text = read_text(path.join(target_dir, compose_to_parse));
  }
  const parsed = parse_compose_services(compose_text);
  const env_hints = detect_env_hints(target_dir, env_files);

  // ── Phase 3: Assembly ─────────────────────────────────────────────────
  const strategy = detect_strategy(compose_files, parsed);
  const services = detect_services(parsed, project.pkg);
  const database = detect_database(parsed, env_hints);
  const redis = detect_redis(parsed);

  const detections = {
    dir: target_dir,
    project,
    compose_files,
    parsed,
    strategy,
    services,
    database,
    redis,
    env_files,
    env_hints,
    git_refs,
    monorepo,
    traefik,
  };

  const config = assemble_config(detections);

  // ── Phase 4: Output ───────────────────────────────────────────────────
  await print_summary(config, detections);

  const config_content = generate_config_file(config, detections);

  if (dry_run) {
    p.log.info('Dry run — would generate:');
    console.log('');
    console.log(config_content);
    p.outro('Dry run complete. No files written.');
    return;
  }

  fs.writeFileSync(config_path, config_content, 'utf8');

  p.log.success(`Generated ${CONFIG_FILENAME}`);
  p.note(
    [
      `  1. Review the config: cat ${CONFIG_FILENAME}`,
      '  2. Create your first worktree: wt create',
    ].join('\n'),
    'Next steps'
  );
  p.outro('Done');
}

// ── Exports (for future wizard reuse) ────────────────────────────────────────

module.exports = {
  detect_project,
  find_compose_files,
  parse_compose_services,
  detect_strategy,
  detect_services,
  detect_database,
  detect_redis,
  detect_env_files,
  detect_env_hints,
  detect_git_refs,
  detect_monorepo,
  detect_traefik,
  detect_claude,
  assemble_config,
  generate_config_file,
  personalize,
};

// Run CLI when executed directly
if (require.main === module) {
  main().catch((err) => {
    console.error(`\n  Error: ${err.message}`);
    process.exit(1);
  });
}
