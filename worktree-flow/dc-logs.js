const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const { config, config_mod, resolve_worktree_path, get_container_name } = require('./lib/utils');

function parse_args(argv) {
  const options = {
    name: null,
    follow: false,
    tail: 100,
    service: null,
  };

  const remaining = [...argv];
  while (remaining.length) {
    const arg = remaining.shift();

    if (!options.name && !arg.startsWith('-')) {
      options.name = arg;
      continue;
    }

    if (arg === '--follow' || arg === '-f' || arg === '--l' || arg === '-l') {
      options.follow = true;
      continue;
    }

    if (arg === '--tail' || arg === '-t') {
      options.tail = Number.parseInt(remaining.shift(), 10) || 100;
      continue;
    }
    if (arg.startsWith('--tail=')) {
      options.tail = Number.parseInt(arg.split('=')[1], 10) || 100;
      continue;
    }
    if (arg.startsWith('--t:')) {
      options.tail = Number.parseInt(arg.split(':')[1], 10) || 100;
      continue;
    }
    if (arg.startsWith('-t:')) {
      options.tail = Number.parseInt(arg.split(':')[1], 10) || 100;
      continue;
    }

    if (arg === '--service' || arg === '-s') {
      options.service = remaining.shift();
      continue;
    }
    if (arg.startsWith('--service=')) {
      options.service = arg.split('=')[1];
      continue;
    }

    console.error(`Unknown argument: ${arg}`);
    return null;
  }

  return options;
}

function main() {
  const options = parse_args(process.argv.slice(2));
  if (!options || !options.name) {
    console.log('Usage:');
    console.log('  pnpm dc:logs <name>                        Last 100 lines');
    console.log('  pnpm dc:logs <name> -f                     Follow (live)');
    console.log('  pnpm dc:logs <name> --tail=200             Last 200 lines');
    console.log('  pnpm dc:logs <name> --t:200                Last 200 lines (shorthand)');
    console.log('  pnpm dc:logs <name> -s app                 Logs for a specific PM2 service');
    console.log('  pnpm dc:logs <name> -s api -f              Follow specific service');
    console.log('');
    console.log('Services: app, api, socket_server, admin_server, ship_server,');
    console.log('          job_server, combined_sync, listings_sync, cache_server,');
    console.log('          insights_server, order_table_server, inventory_table_server');
    process.exit(1);
  }

  const worktree_path = resolve_worktree_path(options.name);

  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    process.exit(1);
  }

  const shared = config ? config_mod.get_compose_info(config, worktree_path) : null;
  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');

  if (!shared && !fs.existsSync(compose_file)) {
    console.error(`No docker-compose file found for: ${worktree_path}`);
    process.exit(1);
  }

  if (options.service && !shared) {
    // PM2 service logs (generate strategy only — shared compose has native services)
    const container = get_container_name(worktree_path);
    if (!container) {
      console.error('Container is not running. Start it with: pnpm dc:up ' + options.name);
      process.exit(1);
    }

    const lines_flag = options.follow ? '' : ` --lines ${options.tail} --nostream`;
    const cmd = `docker exec -it ${container} pm2 logs ${options.service}${lines_flag}`;

    try {
      execSync(cmd, { stdio: 'inherit' });
    } catch {
      process.exit(1);
    }
    return;
  }

  const follow_flag = options.follow ? ' -f' : '';
  const tail_flag = ` --tail=${options.tail}`;
  const service_filter = options.service ? ` ${options.service}` : '';

  if (shared) {
    const cmd = `docker compose -f "${shared.compose_file}" -p "${shared.project}" logs${follow_flag}${tail_flag}${service_filter}`;
    try {
      execSync(cmd, { stdio: 'inherit', env: { ...process.env, ...shared.env } });
    } catch {
      process.exit(1);
    }
  } else {
    const cmd = `docker compose -f docker-compose.worktree.yml logs${follow_flag}${tail_flag}`;
    try {
      execSync(cmd, { stdio: 'inherit', cwd: worktree_path });
    } catch {
      process.exit(1);
    }
  }
}

main();
