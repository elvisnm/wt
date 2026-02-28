const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const { format_port_table, compute_ports, MINIMAL_SERVICES } = require('./service-ports');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

function parseArgs(argv) {
  const options = {
    name: null,
    all: false,
  };

  const remaining = [...argv];
  while (remaining.length) {
    const arg = remaining.shift();
    if (!options.name && !arg.startsWith('--')) {
      options.name = arg;
      continue;
    }

    if (arg === '--all') {
      options.all = true;
      continue;
    }

    console.error(`Unknown argument: ${arg}`);
    return null;
  }

  return options;
}

function run(command, opts = {}) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8', ...opts }).trim();
}

function resolve_worktrees_dir(repo_root) {
  if (config && config.repo._worktreesDirResolved) {
    return config.repo._worktreesDirResolved;
  }
  const project_name = path.basename(repo_root);
  const parent_dir = path.dirname(repo_root);
  return path.join(parent_dir, `${project_name}-worktrees`);
}

function find_docker_worktrees(base_dir) {
  const results = [];
  const env_filename = config ? config.env.filename : '.env.worktree';

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

function compute_auto_offset(seed) {
  if (config) return config_mod.compute_offset(config, seed);
  const crypto = require('crypto');
  const hash = crypto.createHash('sha256').update(seed).digest('hex');
  const hash_int = Number.parseInt(hash.slice(0, 8), 16);
  return (hash_int % 2000) + 100;
}

function read_offset(worktree_path) {
  const env_path = path.join(worktree_path, '.env.worktree');
  if (fs.existsSync(env_path)) {
    const content = fs.readFileSync(env_path, 'utf8');
    const host_offset_match = content.match(/^WORKTREE_HOST_PORT_OFFSET=(\d+)/m);
    if (host_offset_match) return Number.parseInt(host_offset_match[1], 10);

    const offset_match = content.match(/^WORKTREE_PORT_OFFSET=(\d+)/m);
    if (offset_match) return Number.parseInt(offset_match[1], 10);

    const base_match = content.match(/^WORKTREE_PORT_BASE=(\d+)/m);
    if (base_match) return Number.parseInt(base_match[1], 10) - 3000;
  }

  const compose_path = path.join(worktree_path, 'docker-compose.worktree.yml');
  if (fs.existsSync(compose_path)) {
    const content = fs.readFileSync(compose_path, 'utf8');
    const port_match = content.match(/"(\d+):3001"/);
    if (port_match) {
      const host_port = Number.parseInt(port_match[1], 10);
      if (host_port !== 3001) return host_port - 3001;
    }
  }

  return compute_auto_offset(worktree_path);
}

function get_container_status(worktree_path) {
  try {
    const output = run('docker compose -f docker-compose.worktree.yml ps --format json', {
      cwd: worktree_path,
    });

    if (!output) return null;

    const lines = output.split('\n').filter(Boolean);
    for (const line of lines) {
      try {
        const data = JSON.parse(line);
        return {
          container: data.Name || data.name || 'unknown',
          state: data.State || data.state || 'unknown',
          status_detail: data.Status || data.status || '',
        };
      } catch {
        continue;
      }
    }
  } catch {
    return null;
  }

  return null;
}

function read_service_mode(worktree_path) {
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

function read_alias(worktree_path) {
  const env_path = path.join(worktree_path, '.env.worktree');
  if (fs.existsSync(env_path)) {
    const content = fs.readFileSync(env_path, 'utf8');
    const match = content.match(/^WORKTREE_ALIAS=(.+)$/m);
    if (match) return match[1].trim();
  }
  return null;
}

function format_info(worktree_name, worktree_path, { compact = false } = {}) {
  const shared = config ? config_mod.get_compose_info(config, worktree_path) : null;
  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  if (!shared && !fs.existsSync(compose_file)) return false;

  const offset = read_offset(worktree_path);
  const alias = read_alias(worktree_path);

  let container_info;
  if (shared) {
    // Shared compose: use docker compose ps
    try {
      const output = run(
        `docker compose -f "${shared.compose_file}" -p "${shared.project}" ps --format json`,
        { env: { ...process.env, ...shared.env } }
      );
      const lines = (output || '').split('\n').filter(Boolean);
      const containers = [];
      for (const line of lines) {
        try { containers.push(JSON.parse(line)); } catch { continue; }
      }
      const any_running = containers.some((c) => (c.State || '').toLowerCase() === 'running');
      container_info = {
        container: containers.map((c) => c.Name || c.name).join(', ') || 'none',
        state: any_running ? 'running' : (containers.length > 0 ? containers[0].State : 'not running'),
        status_detail: containers.map((c) => `${c.Name || c.name}: ${c.Status || c.State || ''}`).join('; '),
      };
    } catch {
      container_info = null;
    }
  } else {
    container_info = get_container_status(worktree_path);
  }

  const service_mode = shared
    ? (config.services.defaultMode || 'default')
    : read_service_mode(worktree_path);

  const is_running = container_info && container_info.state === 'running';
  const icon = is_running ? '\x1b[32m●\x1b[0m' : '\x1b[31m○\x1b[0m';
  const state_text = container_info ? container_info.state : 'not running';

  console.log(`${icon} ${worktree_name}`);

  if (compact) {
    if (config) {
      const cfg_ports = config_mod.compute_ports(config, offset);
      const summary = Object.entries(cfg_ports).map(([s, p]) => `${s}:${p}`).join('  ');
      console.log(`  ${state_text}  offset=${offset}  ${summary}`);
    } else {
      const ports = compute_ports(offset);
      const summary = MINIMAL_SERVICES.map((s) => `${s}:${ports[s]}`).join('  ');
      console.log(`  ${state_text}  offset=${offset}  ${summary}`);
    }
    console.log('');
    return true;
  }

  const domain = alias
    ? (config ? config_mod.domain_for(config, alias) : `${alias}.localhost`)
    : null;

  console.log(`  Path:      ${worktree_path}`);
  console.log(`  Container: ${container_info ? container_info.container : 'none'}`);
  if (alias) console.log(`  Alias:     ${alias}`);
  if (domain) console.log(`  Domain:    ${domain}`);
  console.log(`  State:     ${state_text}`);
  console.log(`  Offset:    ${offset}`);
  console.log('');

  if (config) {
    const cfg_ports = config_mod.compute_ports(config, offset);
    console.log('  Quick Links:');
    for (const ql of (config.services.quickLinks || [])) {
      const svc_port = cfg_ports[ql.service] || '?';
      console.log(`    ${ql.label}: http://localhost:${svc_port}${ql.pathPrefix || ''}`);
    }
    if (!config.services.quickLinks || config.services.quickLinks.length === 0) {
      const primary_port = cfg_ports[config.services.primary] || Object.values(cfg_ports)[0];
      if (domain) console.log(`    Web:   http://${domain}/`);
      else console.log(`    Web:   http://localhost:${primary_port}`);
    }
    console.log('');
    console.log(`  Service Ports (${service_mode}):`);
    for (const [svc, port] of Object.entries(cfg_ports)) {
      console.log(`    ${svc.padEnd(20)} ${port}`);
    }
  } else {
    const ports = compute_ports(offset);
    console.log('  Quick Links:');
    if (domain) console.log(`    Web:   http://${domain}/`);
    else console.log(`    Web:   http://localhost:${ports.app}/`);
    console.log(`    API:   http://localhost:${ports.api}/`);
    console.log(`    Admin: http://localhost:${ports.admin_server}/`);
    console.log('');
    console.log(`  Service Ports (${service_mode}):`);
    console.log(format_port_table(offset, { mode: service_mode }));
  }
  console.log('');

  return true;
}

function main() {
  const options = parseArgs(process.argv.slice(2));
  if (!options || (!options.name && !options.all)) {
    console.log('Usage:');
    console.log('  pnpm dc:info <name>    Show container info for a specific worktree');
    console.log('  pnpm dc:info --all     Show all active Docker worktree containers');
    process.exit(1);
  }

  const repo_root = run('git rev-parse --show-toplevel');
  const worktrees_dir = resolve_worktrees_dir(repo_root);

  if (options.all) {
    if (!fs.existsSync(worktrees_dir)) {
      console.log('No worktrees directory found.');
      return;
    }

    const docker_worktrees = find_docker_worktrees(worktrees_dir);

    if (docker_worktrees.length === 0) {
      console.log('No Docker worktrees found.');
      return;
    }

    console.log(`\nDocker worktrees (${docker_worktrees.length}):\n`);
    for (const wt of docker_worktrees) {
      format_info(wt.name, wt.path, { compact: true });
    }
    return;
  }

  const worktree_path = path.join(worktrees_dir, options.name.replace(/\//g, '-'));
  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    process.exit(1);
  }

  console.log('');
  const found = format_info(options.name, worktree_path);
  if (!found) {
    console.error(`No Docker environment found for: ${options.name}`);
    console.error('This worktree was not created with Docker.');
    process.exit(1);
  }
}

main();
