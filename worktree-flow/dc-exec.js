const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');

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
  const args = process.argv.slice(2);
  const name = args[0];
  const command_args = args.slice(1);

  if (!name || name.startsWith('-') || command_args.length === 0) {
    console.log('Usage:');
    console.log('  pnpm dc:exec <name> <command...>');
    console.log('');
    console.log('Examples:');
    console.log('  pnpm dc:exec feature/abc pm2 list');
    console.log('  pnpm dc:exec feature/abc pm2 restart app');
    console.log('  pnpm dc:exec feature/abc node -e "console.log(1)"');
    console.log('  pnpm dc:exec feature/abc cat .env.worktree');
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
    const { execFileSync } = require('child_process');
    execFileSync('docker', ['exec', '-it', container, ...command_args], { stdio: 'inherit' });
  } catch (error) {
    process.exit(error.status || 1);
  }
}

main();
