const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

const SENTINEL_PATH = path.join(os.tmpdir(), 'wt-skip-worktree-done');

function run(command, opts = {}) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8', ...opts }).trim();
}

function resolve_worktree_path(repo_root, name) {
  const worktrees_dir = config && config.repo._worktreesDirResolved
    ? config.repo._worktreesDirResolved
    : path.join(path.dirname(repo_root), `${path.basename(repo_root)}-worktrees`);
  return path.join(worktrees_dir, name.replace(/\//g, '-'));
}

function read_container_name(worktree_path) {
  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  try {
    const content = fs.readFileSync(compose_file, 'utf8');
    const match = content.match(/container_name:\s*(\S+)/);
    return match ? match[1] : null;
  } catch {
    return null;
  }
}

function get_skip_paths() {
  const paths = config && config.git && Array.isArray(config.git.skipWorktree)
    ? config.git.skipWorktree
    : [];
  if (paths.length === 0) {
    console.log('No skip-worktree paths configured.');
    console.log('Add git.skipWorktree to your workflow.config.js:');
    console.log('');
    console.log("  git: {");
    console.log("    skipWorktree: ['build/', '.beads/', 'CLAUDE.md', 'pnpm-lock.yaml'],");
    console.log("  },");
  }
  return paths;
}

function is_docker_worktree(worktree_path) {
  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  if (fs.existsSync(compose_file)) return true;
  if (config) {
    const shared = config_mod.get_compose_info(config, worktree_path);
    if (shared) return true;
  }
  return false;
}

function get_container(worktree_path) {
  if (config) {
    const shared = config_mod.get_compose_info(config, worktree_path);
    if (shared) {
      const primary = config.services.primary || Object.keys(config.services.ports)[0];
      return `${shared.project}-${primary}`;
    }
  }
  return read_container_name(worktree_path);
}

function git_cmd(worktree_path, args, container) {
  if (container) {
    return `docker exec ${container} git ${args}`;
  }
  return `git -C "${worktree_path}" ${args}`;
}

function action_apply(worktree_path, container) {
  const paths = get_skip_paths();
  if (paths.length === 0) return;

  console.log('Applying skip-worktree...\n');
  let total = 0;
  for (const p of paths) {
    try {
      const files = run(git_cmd(worktree_path, `ls-files -z "${p}"`, container))
        .split('\0')
        .filter(Boolean);
      if (files.length === 0) {
        console.log(`  ${p} (no tracked files)`);
        continue;
      }
      const file_args = files.map((f) => `"${f}"`).join(' ');
      run(git_cmd(worktree_path, `update-index --skip-worktree ${file_args}`, container));
      console.log(`  ${p} (${files.length} file${files.length > 1 ? 's' : ''})`);
      total += files.length;
    } catch {
      console.log(`  ${p} (skipped — not found or not tracked)`);
    }
  }
  console.log(`\nApplied skip-worktree to ${total} file(s).`);
}

function action_remove(worktree_path, container) {
  const paths = get_skip_paths();
  if (paths.length === 0) return;

  console.log('Removing skip-worktree...\n');
  let total = 0;
  for (const p of paths) {
    try {
      const files = run(git_cmd(worktree_path, `ls-files -z "${p}"`, container))
        .split('\0')
        .filter(Boolean);
      if (files.length === 0) continue;
      const file_args = files.map((f) => `"${f}"`).join(' ');
      run(git_cmd(worktree_path, `update-index --no-skip-worktree ${file_args}`, container));
      console.log(`  ${p} (${files.length} file${files.length > 1 ? 's' : ''})`);
      total += files.length;
    } catch {
      // skip silently
    }
  }
  console.log(`\nRemoved skip-worktree from ${total} file(s).`);
}

function action_list(worktree_path, container) {
  try {
    const output = run(git_cmd(worktree_path, 'ls-files -v', container));
    const skipped = output.split('\n').filter((line) => line.startsWith('S '));
    if (skipped.length === 0) {
      console.log('No files with skip-worktree set.');
      return;
    }
    console.log(`Skip-worktree files (${skipped.length}):\n`);
    for (const line of skipped) {
      console.log(`  ${line.slice(2)}`);
    }
  } catch (err) {
    console.error(`Failed to list files: ${err.message}`);
    process.exit(1);
  }
}

function main() {
  // Clean up any stale sentinel from a previous run
  try { fs.unlinkSync(SENTINEL_PATH); } catch {}

  const args = process.argv.slice(2);
  const action = args[0];
  const name = args[1];

  if (!action || !['apply', 'remove', 'list'].includes(action)) {
    console.log('Usage:');
    console.log('  node dc-skip-worktree.js apply <name>   Apply skip-worktree to configured paths');
    console.log('  node dc-skip-worktree.js remove <name>  Remove skip-worktree from configured paths');
    console.log('  node dc-skip-worktree.js list <name>    List files with skip-worktree set');
    fs.writeFileSync(SENTINEL_PATH, '1');
    process.exit(1);
  }

  if (!name) {
    console.error('Error: worktree name is required');
    fs.writeFileSync(SENTINEL_PATH, '1');
    process.exit(1);
  }

  const repo_root = run('git rev-parse --show-toplevel');
  const worktree_path = resolve_worktree_path(repo_root, name);

  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    fs.writeFileSync(SENTINEL_PATH, '1');
    process.exit(1);
  }

  let container = null;
  if (is_docker_worktree(worktree_path)) {
    container = get_container(worktree_path);
    if (container) {
      try {
        const state = run(`docker inspect --format={{.State.Status}} "${container}"`);
        if (state !== 'running') container = null;
      } catch {
        container = null;
      }
    }
  }

  switch (action) {
    case 'apply':
      action_apply(worktree_path, container);
      break;
    case 'remove':
      action_remove(worktree_path, container);
      break;
    case 'list':
      action_list(worktree_path, container);
      break;
  }

  fs.writeFileSync(SENTINEL_PATH, '0');
  process.exit(0);
}

main();
