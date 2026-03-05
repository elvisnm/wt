const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const { config, resolve_worktree_path, get_container_name, read_env } = require('./lib/utils');
const { find_pm2, pm2_home, pm2_action, pm2_list } = require('./lib/pm2');
const { ALL_SERVICE_NAMES } = require('./service-ports');
const { OUTPUT_FILENAME } = require('./generate-ecosystem-config');

function print_usage() {
  console.log('Usage:');
  console.log('  wt service <worktree> start <service>   Start a service');
  console.log('  wt service <worktree> stop <service>    Stop a service');
  console.log('  wt service <worktree> restart <service> Restart a service');
  console.log('  wt service <worktree> list              Show running PM2 services');
  console.log('');
  console.log('Available services:');
  console.log(`  ${ALL_SERVICE_NAMES.join(', ')}`);
  console.log('');
  console.log('Examples:');
  console.log('  wt service my-branch start ship_server');
  console.log('  wt service my-branch stop insights_server');
  console.log('  wt service my-branch list');
}

/**
 * Detect whether a worktree is local (PM2) or Docker.
 * Returns 'local' | 'docker' | null.
 */
function detect_worktree_type(worktree_path) {
  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  if (fs.existsSync(compose_file)) return 'docker';

  const home = pm2_home(worktree_path);
  if (fs.existsSync(home)) return 'local';

  // Check for .env.worktree as a fallback indicator of a local worktree
  const env_filename = config ? config.env.filename : '.env.worktree';
  if (fs.existsSync(path.join(worktree_path, env_filename))) return 'local';

  return null;
}

function handle_docker(worktree_path, name, action, service) {
  const container = get_container_name(worktree_path);
  if (!container) {
    console.error('Container is not running. Start it with: wt up ' + name);
    process.exit(1);
  }

  try {
    if (action === 'list') {
      execFileSync('docker', ['exec', '-it', container, 'pm2', 'list'], { stdio: 'inherit' });
      return;
    }

    if (action === 'start') {
      execFileSync(
        'docker',
        ['exec', '-it', container, 'pm2', 'start', 'ecosystem.dev.config.js', '--only', service, '--update-env'],
        { stdio: 'inherit' },
      );
    } else {
      execFileSync(
        'docker',
        ['exec', '-it', container, 'pm2', action, service],
        { stdio: 'inherit' },
      );
    }

    console.log(`\n${action === 'start' ? 'Started' : action === 'stop' ? 'Stopped' : 'Restarted'}: ${service}`);
  } catch (error) {
    process.exit(error.status || 1);
  }
}

function handle_local(worktree_path, name, action, service) {
  const home = pm2_home(worktree_path);
  const repo_root = require('./lib/utils').run('git rev-parse --show-toplevel');
  const pm2_bin = find_pm2(repo_root);

  if (action === 'list') {
    const processes = pm2_list(pm2_bin, home);
    if (!processes.length) {
      console.log('No PM2 processes running.');
      return;
    }

    const name_w = Math.max(7, ...processes.map((p) => (p.name || '').length));
    console.log(`${'Service'.padEnd(name_w)}  Status     CPU    Memory`);
    console.log(`${'-'.repeat(name_w)}  ---------  -----  ------`);
    for (const p of processes) {
      const env = p.pm2_env || {};
      const status = (env.status || 'unknown').padEnd(9);
      const cpu = `${p.monit && p.monit.cpu !== undefined ? p.monit.cpu + '%' : '-'}`.padEnd(5);
      const mem = p.monit && p.monit.memory
        ? `${Math.round(p.monit.memory / 1024 / 1024)}MB`
        : '-';
      console.log(`${(p.name || '').padEnd(name_w)}  ${status}  ${cpu}  ${mem}`);
    }
    return;
  }

  const ecosystem_config = path.join(worktree_path, OUTPUT_FILENAME);
  pm2_action(pm2_bin, home, action, service, {
    ecosystem_config: fs.existsSync(ecosystem_config) ? ecosystem_config : undefined,
  });

  console.log(`\n${action === 'start' ? 'Started' : action === 'stop' ? 'Stopped' : 'Restarted'}: ${service}`);
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

  const worktree_path = resolve_worktree_path(name);

  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    process.exit(1);
  }

  const wt_type = detect_worktree_type(worktree_path);

  if (wt_type === 'docker') {
    handle_docker(worktree_path, name, action, service);
  } else if (wt_type === 'local') {
    handle_local(worktree_path, name, action, service);
  } else {
    console.error(`No Docker or local PM2 environment found at: ${worktree_path}`);
    process.exit(1);
  }
}

main();
