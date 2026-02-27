const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

function run(command, opts = {}) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8', ...opts }).trim();
}

function main() {
  const dry_run = process.argv.includes('--dry-run');
  const repo_root = run('git rev-parse --show-toplevel');
  const worktrees_dir = config && config.repo._worktreesDirResolved
    ? config.repo._worktreesDirResolved
    : path.join(path.dirname(repo_root), `${path.basename(repo_root)}-worktrees`);

  const volume_prefix = config ? config.name + '_' : '';
  const volume_regex = config
    ? new RegExp(`^[a-z0-9-]+_${config.name}_`)
    : /^$/;
  const volumes = run('docker volume ls --format {{.Name}}')
    .split('\n')
    .filter((v) => v.startsWith(volume_prefix) || v.match(volume_regex));

  if (volumes.length === 0) {
    console.log(`No ${config ? config.name : 'project'} Docker volumes found.`);
    return;
  }

  const active_aliases = new Set();
  if (fs.existsSync(worktrees_dir)) {
    for (const entry of fs.readdirSync(worktrees_dir)) {
      const env_path = path.join(worktrees_dir, entry, '.env.worktree');
      if (fs.existsSync(env_path)) {
        const content = fs.readFileSync(env_path, 'utf8');
        const match = content.match(/^WORKTREE_ALIAS=(.+)$/m);
        if (match) active_aliases.add(match[1].trim());
      }
    }
  }

  const running_containers = new Set();
  try {
    const lines = run('docker ps --format {{.Names}}').split('\n').filter(Boolean);
    for (const name of lines) {
      running_containers.add(name);
    }
  } catch {
    // docker not running
  }

  const config_name = config ? config.name : 'project';
  const vol_pattern = new RegExp(`${config_name}[_-]([a-z0-9_-]+?)_(?:node_modules|pnpm_store)`, 'i');
  const container_prefix = config ? config.name + '-' : '';
  const orphaned = volumes.filter((vol) => {
    const container_match = vol.match(vol_pattern);
    if (!container_match) return false;
    const alias = container_match[1];
    if (active_aliases.has(alias)) return false;
    if (running_containers.has(`${container_prefix}${alias}`)) return false;
    return true;
  });

  if (orphaned.length === 0) {
    console.log('No orphaned volumes found.');
    return;
  }

  console.log(`Found ${orphaned.length} orphaned volume(s):\n`);
  for (const vol of orphaned) {
    console.log(`  ${vol}`);
  }
  console.log('');

  if (dry_run) {
    console.log('Dry run — no volumes removed.');
    return;
  }

  let removed = 0;
  for (const vol of orphaned) {
    try {
      run(`docker volume rm "${vol}"`);
      console.log(`Removed: ${vol}`);
      removed += 1;
    } catch {
      console.warn(`Could not remove: ${vol} (may be in use by a stopped container)`);
    }
  }

  console.log(`\nRemoved ${removed}/${orphaned.length} orphaned volume(s).`);
  if (removed < orphaned.length) {
    console.log('Tip: Remove stopped containers first with `docker rm <container>`, then re-run.');
  }
}

main();
