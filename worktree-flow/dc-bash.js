const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

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

function main() {
  const name = process.argv[2];

  if (!name || name.startsWith('-')) {
    console.log('Usage:');
    console.log('  pnpm dc:bash <name>    Open a bash shell inside the container');
    process.exit(1);
  }

  const worktree_path = resolve_worktree_path(name);

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

  let container;
  if (shared) {
    // For shared compose: exec into the primary service container
    const primary = config.services.primary || Object.keys(config.services.ports)[0];
    container = `${shared.project}-${primary}`;
    // Verify it's running
    try {
      const state = execSync(`docker inspect --format={{.State.Status}} "${container}"`, { stdio: 'pipe', encoding: 'utf8' }).trim();
      if (state !== 'running') {
        console.error(`Container ${container} is ${state}. Start it with: pnpm dc:up ${name}`);
        process.exit(1);
      }
    } catch {
      console.error(`Container ${container} not found. Start it with: pnpm dc:up ${name}`);
      process.exit(1);
    }
  } else {
    container = get_container_name(worktree_path);
    if (!container) {
      console.error('Container is not running. Start it with: pnpm dc:up ' + name);
      process.exit(1);
    }
  }

  console.log(`Connecting to ${container}...\n`);

  try {
    execSync(`docker exec -it ${container} /bin/bash`, { stdio: 'inherit' });
  } catch (error) {
    if (error.status === 126 || error.status === 127) {
      execSync(`docker exec -it ${container} /bin/sh`, { stdio: 'inherit' });
    } else {
      process.exit(error.status || 1);
    }
  }
}

main();
