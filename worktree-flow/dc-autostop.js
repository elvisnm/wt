const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

function run(command, opts = {}) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8', ...opts }).trim();
}

function get_running_containers() {
  const container_prefix = config ? config.name + '-' : '';
  const infra_prefix = config ? config.name + '-docker-' : '';
  try {
    const lines = run(`docker ps --filter "name=${container_prefix}" --format json`).split('\n').filter(Boolean);
    const containers = [];
    for (const line of lines) {
      try {
        const data = JSON.parse(line);
        const name = data.Names || data.Name || data.name;
        if (name && name.startsWith(container_prefix) && !name.startsWith(infra_prefix)) {
          containers.push(name);
        }
      } catch {
        continue;
      }
    }
    return containers;
  } catch {
    return [];
  }
}

function get_cpu_usage(container_name) {
  try {
    const output = run(`docker stats --no-stream --format "{{.CPUPerc}}" "${container_name}"`);
    return Number.parseFloat(output.replace('%', '')) || 0;
  } catch {
    return -1;
  }
}

function get_started_at(container_name) {
  try {
    const started = run(`docker inspect --format={{.State.StartedAt}} "${container_name}"`);
    return new Date(started);
  } catch {
    return null;
  }
}

function find_worktree_for_container(container_name, worktrees_dir) {
  if (!fs.existsSync(worktrees_dir)) return null;
  for (const entry of fs.readdirSync(worktrees_dir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const compose_file = path.join(worktrees_dir, entry.name, 'docker-compose.worktree.yml');
    if (!fs.existsSync(compose_file)) continue;
    const content = fs.readFileSync(compose_file, 'utf8');
    if (content.includes(`container_name: ${container_name}`)) {
      return entry.name;
    }
  }
  return null;
}

function main() {
  const dry_run = process.argv.includes('--dry-run');
  const idle_hours_arg = process.argv.find((a) => a.startsWith('--hours='));
  const idle_hours = idle_hours_arg ? Number.parseFloat(idle_hours_arg.split('=')[1]) : 2;
  const cpu_threshold = 1.0;

  const repo_root = run('git rev-parse --show-toplevel');
  const worktrees_dir = config && config.repo._worktreesDirResolved
    ? config.repo._worktreesDirResolved
    : path.join(path.dirname(repo_root), `${path.basename(repo_root)}-worktrees`);

  const containers = get_running_containers();
  if (containers.length === 0) {
    console.log('No running worktree containers found.');
    return;
  }

  console.log(`Checking ${containers.length} running worktree container(s) (idle threshold: ${idle_hours}h, CPU <${cpu_threshold}%)...\n`);

  const to_stop = [];

  for (const name of containers) {
    const started = get_started_at(name);
    if (!started) continue;

    const uptime_hours = (Date.now() - started.getTime()) / 3600000;
    if (uptime_hours < idle_hours) {
      console.log(`  ${name}: up ${uptime_hours.toFixed(1)}h — too recent, skipping`);
      continue;
    }

    const cpu = get_cpu_usage(name);
    if (cpu < 0) {
      console.log(`  ${name}: could not read CPU, skipping`);
      continue;
    }

    if (cpu >= cpu_threshold) {
      console.log(`  ${name}: CPU ${cpu.toFixed(1)}% — active, skipping`);
      continue;
    }

    const wt_name = find_worktree_for_container(name, worktrees_dir) || name;
    console.log(`  ${name}: CPU ${cpu.toFixed(1)}%, up ${uptime_hours.toFixed(1)}h — idle`);
    to_stop.push({ container: name, worktree: wt_name });
  }

  console.log('');

  if (to_stop.length === 0) {
    console.log('No idle containers to stop.');
    return;
  }

  if (dry_run) {
    console.log(`Would stop ${to_stop.length} idle container(s):`);
    for (const { container } of to_stop) {
      console.log(`  ${container}`);
    }
    console.log('\nDry run — no containers stopped.');
    return;
  }

  for (const { container, worktree } of to_stop) {
    console.log(`Stopping ${container}...`);
    try {
      execSync(`docker stop "${container}"`, { stdio: 'inherit' });
      console.log(`Stopped. Restart with: pnpm dc:up ${worktree}`);
    } catch {
      console.warn(`Warning: Could not stop ${container}.`);
    }
  }

  console.log(`\nStopped ${to_stop.length} idle container(s).`);
}

main();
