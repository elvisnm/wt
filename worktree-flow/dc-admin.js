const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const {
  config, config_mod, run, resolve_worktrees_dir, find_docker_worktrees, read_container_name,
} = require('./lib/utils');

const ADMIN_KEY = config ? config_mod.env_var(config, 'adminAccounts') : 'ADMIN_ACCOUNTS';
const DEFAULT_ADMIN_ID = config && config.features.admin && config.features.admin.defaultUserId
  ? config.features.admin.defaultUserId
  : null;

function is_running(worktree_path) {
  try {
    const output = run('docker compose -f docker-compose.worktree.yml ps --format json', { cwd: worktree_path });
    if (!output) return false;
    for (const line of output.split('\n').filter(Boolean)) {
      const info = JSON.parse(line);
      if (info.State === 'running') return true;
    }
  } catch { }
  return false;
}

function update_env(env_path, set_admin, admin_id) {
  let content = fs.readFileSync(env_path, 'utf8');
  const regex = new RegExp(`^${ADMIN_KEY}=.+\\n?`, 'm');

  if (set_admin) {
    if (regex.test(content)) {
      content = content.replace(regex, `${ADMIN_KEY}=${admin_id}\n`);
    } else {
      content = content.trimEnd() + `\n${ADMIN_KEY}=${admin_id}\n`;
    }
  } else {
    content = content.replace(regex, '');
  }

  fs.writeFileSync(env_path, content, 'utf8');
}

function main() {
  const args = process.argv.slice(2);
  const action = args.find((a) => !a.startsWith('--'));
  const target = args.find((a) => a.startsWith('--name='))?.split('=')[1];
  const admin_id = args.find((a) => a.startsWith('--user-id='))?.split('=')[1] || DEFAULT_ADMIN_ID;

  if (!action || !['set', 'unset'].includes(action)) {
    console.log('Usage:');
    console.log('  pnpm admin:set                        Set admin on all running containers');
    console.log('  pnpm admin:set --name=<wt>             Set admin on a specific worktree');
    console.log('  pnpm admin:set --user-id=<id>          Set a specific user as admin');
    console.log('  pnpm admin:unset                       Remove admin from all running containers');
    console.log('  pnpm admin:unset --name=<wt>           Remove admin from a specific worktree');
    process.exit(1);
  }

  const set_admin = action === 'set';
  const worktrees_dir = resolve_worktrees_dir();
  if (!fs.existsSync(worktrees_dir)) {
    console.error('No Docker worktrees found.');
    process.exit(1);
  }
  const worktrees = find_docker_worktrees(worktrees_dir);

  if (worktrees.length === 0) {
    console.error('No Docker worktrees found.');
    process.exit(1);
  }

  let updated = 0;

  for (const wt of worktrees) {
    if (target && !wt.name.includes(target)) continue;

    const env_filename = config ? config.env.filename : '.env.worktree';
    const env_path = path.join(wt.path, env_filename);
    if (!fs.existsSync(env_path)) continue;

    const running = is_running(wt.path);
    if (!target && !running) continue;

    update_env(env_path, set_admin, admin_id);
    const container = read_container_name(wt.path);

    if (running && container) {
      console.log(`${set_admin ? 'Setting' : 'Unsetting'} admin on ${container}...`);
      execSync(`docker compose -f docker-compose.worktree.yml up -d --force-recreate`, {
        stdio: 'inherit',
        cwd: wt.path,
      });
      updated++;
    } else {
      console.log(`Updated ${wt.name} .env.worktree (container not running)`);
      updated++;
    }
  }

  if (updated === 0) {
    console.log(target ? `No worktree matching "${target}" found.` : 'No running worktrees found.');
  } else {
    console.log(`\nDone. ${set_admin ? 'Admin enabled' : 'Admin disabled'} on ${updated} worktree(s).`);
  }
}

main();
