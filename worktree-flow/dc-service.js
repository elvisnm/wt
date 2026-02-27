const { execSync, execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

const ALL_SERVICE_NAMES = config
  ? Object.keys(config.services.ports)
  : [
    'app',
    'api',
    'socket_server',
    'serviceHostServer',
    'combined_sync',
    'listings_sync',
    'admin_server',
    'ship_server',
    'job_server',
    'insights_server',
    'cache_server',
    'order_table_server',
    'inventory_table_server',
  ];

function resolve_worktree_path(name) {
  const repo_root = execSync('git rev-parse --show-toplevel', {
    stdio: 'pipe',
    encoding: 'utf8',
  }).trim();
  const worktrees_dir = config && config.repo._worktreesDirResolved
    ? config.repo._worktreesDirResolved
    : path.join(path.dirname(repo_root), `${path.basename(repo_root)}-worktrees`);
  return path.join(worktrees_dir, name.replace(/\//g, '-'));
}

function get_container_name(worktree_path) {
  try {
    const output = execSync('docker compose -f docker-compose.worktree.yml ps --format json', {
      stdio: 'pipe',
      encoding: 'utf8',
      cwd: worktree_path,
    }).trim();

    if (!output) return null;

    const lines = output.split('\n').filter(Boolean);
    for (const line of lines) {
      try {
        const data = JSON.parse(line);
        return data.Name || data.name || null;
      } catch {
        continue;
      }
    }
  } catch {
    return null;
  }

  return null;
}

function print_usage() {
  console.log('Usage:');
  console.log('  pnpm dc:service <worktree> start <service>   Start a service');
  console.log('  pnpm dc:service <worktree> stop <service>    Stop a service');
  console.log('  pnpm dc:service <worktree> restart <service> Restart a service');
  console.log('  pnpm dc:service <worktree> list              Show running PM2 services');
  console.log('');
  console.log('Available services:');
  console.log(`  ${ALL_SERVICE_NAMES.join(', ')}`);
  console.log('');
  console.log('Examples:');
  console.log('  pnpm dc:service my-branch start ship_server');
  console.log('  pnpm dc:service my-branch stop insights_server');
  console.log('  pnpm dc:service my-branch list');
}

function main() {
  const args = process.argv.slice(2);
  const name = args[0];
  const action = args[1];
  const service = args[2];

  if (!name || !action || name.startsWith('-')) {
    print_usage();
    process.exit(1);
  }

  const valid_actions = ['start', 'stop', 'restart', 'list'];
  if (!valid_actions.includes(action)) {
    console.error(`Invalid action: ${action}. Valid actions: ${valid_actions.join(', ')}`);
    process.exit(1);
  }

  if (action !== 'list' && !service) {
    console.error('Service name is required for start/stop/restart.');
    print_usage();
    process.exit(1);
  }

  if (service && !ALL_SERVICE_NAMES.includes(service)) {
    console.error(`Unknown service: ${service}`);
    console.error(`Available: ${ALL_SERVICE_NAMES.join(', ')}`);
    process.exit(1);
  }

  const worktree_path = resolve_worktree_path(name);

  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    process.exit(1);
  }

  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  if (!fs.existsSync(compose_file)) {
    console.error(`No docker-compose.worktree.yml found at: ${worktree_path}`);
    process.exit(1);
  }

  const container = get_container_name(worktree_path);
  if (!container) {
    console.error('Container is not running. Start it with: pnpm dc:up ' + name);
    process.exit(1);
  }

  try {
    if (action === 'list') {
      execFileSync('docker', ['exec', '-it', container, 'pm2', 'list'], { stdio: 'inherit' });
      return;
    }

    const pm2_action = action === 'start' ? 'start' : action === 'stop' ? 'stop' : 'restart';

    if (action === 'start') {
      execFileSync(
        'docker',
        ['exec', '-it', container, 'pm2', 'start', 'ecosystem.dev.config.js', '--only', service, '--update-env'],
        { stdio: 'inherit' },
      );
    } else {
      execFileSync(
        'docker',
        ['exec', '-it', container, 'pm2', pm2_action, service],
        { stdio: 'inherit' },
      );
    }

    console.log(`\n${action === 'start' ? 'Started' : action === 'stop' ? 'Stopped' : 'Restarted'}: ${service}`);
  } catch (error) {
    process.exit(error.status || 1);
  }
}

main();
