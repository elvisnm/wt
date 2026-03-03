const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const { resolve_worktree_path, get_container_name } = require('./lib/utils');

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
