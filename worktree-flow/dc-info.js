const fs = require('fs');
const path = require('path');
const { format_port_table, compute_ports, MINIMAL_SERVICES } = require('./service-ports');
const {
  config, config_mod, run, resolve_worktrees_dir, find_docker_worktrees,
  read_offset, read_service_mode, read_env,
} = require('./lib/utils');

function parse_args(argv) {
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

function format_info(worktree_name, worktree_path, { compact = false } = {}) {
  const shared = config ? config_mod.get_compose_info(config, worktree_path) : null;
  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  if (!shared && !fs.existsSync(compose_file)) return false;

  const offset = read_offset(worktree_path);
  const alias = read_env(worktree_path, 'WORKTREE_ALIAS');

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
      if (Object.keys(require('./service-ports').SERVICE_PORTS).length > 0) {
        const ports = compute_ports(offset);
        const summary = Object.entries(ports).slice(0, 5).map(([s, p]) => `${s}:${p}`).join('  ');
        console.log(`  ${state_text}  offset=${offset}  ${summary}`);
      } else {
        console.log(`  ${state_text}  offset=${offset}`);
      }
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
  const options = parse_args(process.argv.slice(2));
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
