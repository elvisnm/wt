const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

const ADMIN_KEY = config ? config_mod.env_var(config, 'adminAccounts') : 'ADMIN_ACCOUNTS';
const DEFAULT_ADMIN_ID = config && config.features.admin && config.features.admin.defaultUserId
  ? config.features.admin.defaultUserId
  : null;

function run(command, opts = {}) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8', ...opts }).trim();
}

function resolve_worktrees_dir() {
  if (config && config.repo._worktreesDirResolved) {
    return config.repo._worktreesDirResolved;
  }
  const repo_root = run('git rev-parse --show-toplevel');
  const project_name = path.basename(repo_root);
  const parent_dir = path.dirname(repo_root);
  return path.join(parent_dir, `${project_name}-worktrees`);
}

function find_docker_worktrees() {
  const worktrees_dir = resolve_worktrees_dir();
  if (!fs.existsSync(worktrees_dir)) return [];
  const results = [];
  function scan(dir, prefix) {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      if (!entry.isDirectory()) continue;
      const full_path = path.join(dir, entry.name);
      const rel_name = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (fs.existsSync(path.join(full_path, 'docker-compose.worktree.yml'))) {
        results.push({ name: rel_name, path: full_path });
      } else {
        scan(full_path, rel_name);
      }
    }
  }
  scan(worktrees_dir, '');
  return results;
}

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

function read_container_name(worktree_path) {
  try {
    const content = fs.readFileSync(path.join(worktree_path, 'docker-compose.worktree.yml'), 'utf8');
    const match = content.match(/container_name:\s*(\S+)/);
    return match ? match[1] : null;
  } catch {
    return null;
  }
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
  const worktrees = find_docker_worktrees();

  if (worktrees.length === 0) {
    console.error('No Docker worktrees found.');
    process.exit(1);
  }

  let updated = 0;

  for (const wt of worktrees) {
    if (target && !wt.name.includes(target)) continue;

    const env_path = path.join(wt.path, '.env.worktree');
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
