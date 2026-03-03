const fs = require('fs');
const path = require('path');
const {
  config, config_mod, run, resolve_worktrees_dir, find_docker_worktrees,
  read_env, read_container_name, read_service_mode,
} = require('./lib/utils');

function get_container_stats() {
  const stats = new Map();
  try {
    const lines = run('docker stats --no-stream --format json').split('\n').filter(Boolean);
    for (const line of lines) {
      try {
        const data = JSON.parse(line);
        const name = data.Name || data.name;
        const container_prefix = config ? config.name + '-' : '';
        if (name && name.startsWith(container_prefix)) {
          stats.set(name, {
            cpu: data.CPUPerc || '0%',
            mem: data.MemUsage || '',
            mem_pct: data.MemPerc || '0%',
          });
        }
      } catch {
        continue;
      }
    }
  } catch {
    // docker not running
  }
  return stats;
}

/**
 * Aggregate stats for containers matching a project prefix.
 * For shared compose, multiple containers (e.g. bc-test-workflow-api, bc-test-workflow-web)
 * belong to a single worktree. Sums CPU and memory across all.
 */
function aggregate_project_stats(all_stats, project_prefix) {
  let total_cpu = 0;
  let total_mem_bytes = 0;
  let found = false;

  for (const [name, s] of all_stats) {
    if (!name.startsWith(project_prefix)) continue;
    found = true;
    total_cpu += parseFloat(s.cpu) || 0;
    // Parse mem like "123.4MiB" or "1.2GiB"
    const mem_match = (s.mem || '').match(/([\d.]+)\s*(GiB|MiB|KiB|B)/i);
    if (mem_match) {
      const val = parseFloat(mem_match[1]);
      const unit = mem_match[2].toLowerCase();
      if (unit === 'gib') total_mem_bytes += val * 1024 * 1024 * 1024;
      else if (unit === 'mib') total_mem_bytes += val * 1024 * 1024;
      else if (unit === 'kib') total_mem_bytes += val * 1024;
      else total_mem_bytes += val;
    }
  }

  if (!found) return null;

  let mem_str;
  if (total_mem_bytes >= 1024 * 1024 * 1024) {
    mem_str = `${(total_mem_bytes / (1024 * 1024 * 1024)).toFixed(1)}GiB`;
  } else {
    mem_str = `${(total_mem_bytes / (1024 * 1024)).toFixed(1)}MiB`;
  }

  return { cpu: `${total_cpu.toFixed(2)}%`, mem: mem_str };
}

/**
 * For shared compose: check container status via docker compose ps.
 */
function get_project_container_info(shared_info) {
  try {
    const output = run(
      `docker compose -f "${shared_info.compose_file}" -p "${shared_info.project}" ps --format json`,
      { env: { ...process.env, ...shared_info.env } }
    );
    if (!output) return { running: false, health: null, started: null };

    const lines = output.split('\n').filter(Boolean);
    let any_running = false;
    let earliest_started = null;
    let overall_health = null;

    for (const line of lines) {
      try {
        const data = JSON.parse(line);
        const state = (data.State || '').toLowerCase();
        if (state === 'running') {
          any_running = true;
          // Health from compose ps
          const health = (data.Health || '').toLowerCase();
          if (health === 'healthy') overall_health = overall_health || 'healthy';
          else if (health === 'starting') overall_health = 'starting';
          else if (health === 'unhealthy') overall_health = 'unhealthy';
        }
      } catch { continue; }
    }

    // Get started time from docker inspect on one container
    if (any_running) {
      try {
        const containers = run(`docker compose -f "${shared_info.compose_file}" -p "${shared_info.project}" ps -q`,
          { env: { ...process.env, ...shared_info.env } }
        );
        const first_id = containers.split('\n').filter(Boolean)[0];
        if (first_id) {
          const inspect = run(`docker inspect --format json "${first_id}"`);
          const info = JSON.parse(inspect);
          const arr = Array.isArray(info) ? info[0] : info;
          earliest_started = arr.State?.StartedAt || null;
        }
      } catch {}
    }

    return { running: any_running, health: overall_health, started: earliest_started };
  } catch {
    return { running: false, health: null, started: null };
  }
}

function get_container_info(container_name) {
  try {
    const json = run(`docker inspect --format json "${container_name}"`);
    const data = JSON.parse(json);
    const info = Array.isArray(data) ? data[0] : data;
    const state = info.State || {};
    return {
      running: state.Running || false,
      health: state.Health ? state.Health.Status : null,
      started: state.StartedAt || null,
    };
  } catch {
    return { running: false, health: null, started: null };
  }
}

function format_uptime(started_at) {
  if (!started_at) return '';
  const start = new Date(started_at);
  const now = new Date();
  const diff_ms = now - start;
  if (diff_ms < 0) return '';

  const mins = Math.floor(diff_ms / 60000);
  if (mins < 60) return `${mins}m`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ${mins % 60}m`;
  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h`;
}

function pad(str, len) {
  return str.length >= len ? str : str + ' '.repeat(len - str.length);
}

function main() {
  const repo_root = run('git rev-parse --show-toplevel');
  const worktrees_dir = resolve_worktrees_dir(repo_root);

  if (!fs.existsSync(worktrees_dir)) {
    console.log('No worktrees directory found.');
    return;
  }

  const docker_worktrees = find_docker_worktrees(worktrees_dir);
  if (docker_worktrees.length === 0) {
    console.log('No Docker worktrees found.');
    return;
  }

  const container_stats = get_container_stats();

  const rows = [];
  for (const wt of docker_worktrees) {
    const wt_path = wt.path;
    const name_prefix = config ? config.name + '-' : '';
    const shared = config ? config_mod.get_compose_info(config, wt_path) : null;

    let alias, info, stats_data;

    if (shared) {
      // Shared compose strategy: multiple containers per worktree
      alias = read_env(wt_path, 'WORKTREE_ALIAS') || shared.slug;
      info = get_project_container_info(shared);
      const agg = aggregate_project_stats(container_stats, `${shared.project}-`);
      stats_data = agg ? { mem: agg.mem, cpu: agg.cpu } : null;
    } else {
      // Generate strategy: single container per worktree
      const container_name = read_container_name(wt_path) || `${name_prefix}${path.basename(wt_path)}`;
      alias = read_env(wt_path, 'WORKTREE_ALIAS') || container_name.replace(new RegExp(`^${name_prefix.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`), '');
      info = get_container_info(container_name);
      const raw_stats = container_stats.get(container_name);
      stats_data = raw_stats ? { mem: raw_stats.mem.split('/')[0].trim(), cpu: raw_stats.cpu } : null;
    }

    const offset = read_env(wt_path, 'WORKTREE_HOST_PORT_OFFSET') || read_env(wt_path, 'WORKTREE_PORT_OFFSET') || '?';
    const mode = read_service_mode(wt_path);

    let status;
    if (!info.running) {
      status = '\x1b[31mstopped\x1b[0m';
    } else if (info.health === 'healthy') {
      status = '\x1b[32mhealthy\x1b[0m';
    } else if (info.health === 'starting') {
      status = '\x1b[33mstarting\x1b[0m';
    } else {
      status = '\x1b[32mrunning\x1b[0m';
    }

    const uptime = info.running ? format_uptime(info.started) : '';
    const mem = stats_data ? stats_data.mem : '';
    const cpu = stats_data ? stats_data.cpu : '';
    const primary_base = config ? config.services.ports[config.services.primary] : 3001;
    const app_port = offset !== '?' ? primary_base + Number(offset) : '?';
    const domain_str = config ? (config_mod.domain_for(config, alias) || alias) : `${alias}.localhost`;

    rows.push({ alias, status, mode, uptime, mem, cpu, app_port, domain: domain_str });
  }

  const col = {
    alias: Math.max(5, ...rows.map((r) => r.alias.length)),
    status: 9,
    mode: Math.max(4, ...rows.map((r) => r.mode.length)),
    uptime: Math.max(6, ...rows.map((r) => r.uptime.length)),
    mem: Math.max(6, ...rows.map((r) => r.mem.length)),
    cpu: Math.max(3, ...rows.map((r) => r.cpu.length)),
    domain: Math.max(6, ...rows.map((r) => r.domain.length)),
  };

  const header = `  ${pad('ALIAS', col.alias)}  ${pad('STATUS', col.status)}  ${pad('MODE', col.mode)}  ${pad('UPTIME', col.uptime)}  ${pad('MEMORY', col.mem)}  ${pad('CPU', col.cpu)}  ${pad('DOMAIN', col.domain)}`;
  const divider = '  ' + '-'.repeat(header.length - 2);

  console.log('');
  console.log(`Docker worktrees (${docker_worktrees.length}):`);
  console.log('');
  console.log(header);
  console.log(divider);

  for (const r of rows) {
    const status_padded = r.status + ' '.repeat(Math.max(0, col.status - r.status.replace(/\x1b\[[0-9;]*m/g, '').length));
    console.log(`  ${pad(r.alias, col.alias)}  ${status_padded}  ${pad(r.mode, col.mode)}  ${pad(r.uptime, col.uptime)}  ${pad(r.mem, col.mem)}  ${pad(r.cpu, col.cpu)}  ${pad(r.domain, col.domain)}`);
  }

  console.log('');
}

main();
