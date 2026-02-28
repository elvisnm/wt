const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

function parseArgs(argv) {
  const options = {
    name: null,
    remove: false,
    force: false,
    delete_branch: false,
  };

  const remaining = [...argv];
  while (remaining.length) {
    const arg = remaining.shift();
    if (!options.name && !arg.startsWith('--')) {
      options.name = arg;
      continue;
    }

    if (arg === '--remove') {
      options.remove = true;
      continue;
    }

    if (arg === '--force') {
      options.force = true;
      continue;
    }

    if (arg === '--delete-branch') {
      options.delete_branch = true;
      continue;
    }

    console.error(`Unknown argument: ${arg}`);
    return null;
  }

  return options;
}

function run(command, opts = {}) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8', ...opts }).trim();
}

function has_ref(repo_root, ref) {
  try {
    execSync(`git -C "${repo_root}" show-ref --verify --quiet "${ref}"`);
    return true;
  } catch {
    return false;
  }
}

function resolve_worktree_path(repo_root, name) {
  const worktrees_dir = config && config.repo._worktreesDirResolved
    ? config.repo._worktreesDirResolved
    : path.join(path.dirname(repo_root), `${path.basename(repo_root)}-worktrees`);
  return path.join(worktrees_dir, name.replace(/\//g, '-'));
}

function read_alias(worktree_path) {
  const env_path = path.join(worktree_path, '.env.worktree');
  if (fs.existsSync(env_path)) {
    const content = fs.readFileSync(env_path, 'utf8');
    const match = content.match(/^WORKTREE_ALIAS=(.+)$/m);
    if (match) return match[1].trim();
  }
  return null;
}

function sanitize_name(name) {
  return name.replace(/[^a-zA-Z0-9_-]/g, '-').toLowerCase();
}

function remove_traefik_config(alias) {
  if (!alias) return;
  const traefik_dir = config && config.docker.proxy._dynamicDirResolved
    ? config.docker.proxy._dynamicDirResolved
    : null;
  if (!traefik_dir) return;
  const safe_name = sanitize_name(alias);
  const traefik_file = path.join(traefik_dir, `${safe_name}.yml`);
  if (fs.existsSync(traefik_file)) {
    fs.unlinkSync(traefik_file);
    console.log(`Removed Traefik config: ${traefik_file}`);
  }
}

function main() {
  const options = parseArgs(process.argv.slice(2));
  if (!options || !options.name) {
    console.log('Usage:');
    console.log('  pnpm dc:down <name>                          Stop the Docker container');
    console.log('  pnpm dc:down <name> --remove                 Stop container, remove volumes and worktree');
    console.log('  pnpm dc:down <name> --remove --delete-branch Also delete the local branch');
    console.log('  pnpm dc:down <name> --remove --force         Force remove with uncommitted changes');
    process.exit(1);
  }

  const repo_root = run('git rev-parse --show-toplevel');
  const worktree_path = resolve_worktree_path(repo_root, options.name);

  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    process.exit(1);
  }

  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  const env_filename = config ? config.env.filename : '.env.worktree';
  const shared = config ? config_mod.get_compose_info(config, worktree_path) : null;
  const is_docker = fs.existsSync(compose_file) || !!shared;
  const alias = is_docker ? read_alias(worktree_path) : null;

  if (is_docker) {
    if (shared) {
      // Shared compose strategy
      const traefik_override = path.join(worktree_path, 'docker-compose.traefik.yml');
      const traefik_flag = fs.existsSync(traefik_override) ? ` -f "${traefik_override}"` : '';
      const compose_cmd = `docker compose -f "${shared.compose_file}"${traefik_flag} -p "${shared.project}"`;
      const compose_opts = { stdio: 'inherit', env: { ...process.env, ...shared.env } };
      if (options.remove) {
        console.log('Stopping Docker containers and removing volumes...');
        try { execSync(`${compose_cmd} down -v`, compose_opts); }
        catch { console.warn('Warning: Failed to stop Docker containers (may not be running).'); }
        remove_traefik_config(alias);
      } else {
        console.log('Stopping Docker containers (volumes preserved)...');
        try { execSync(`${compose_cmd} stop`, compose_opts); }
        catch { console.warn('Warning: Failed to stop Docker containers (may not be running).'); }
        remove_traefik_config(alias);
        console.log(`Docker containers stopped. Worktree preserved at: ${worktree_path}`);
        console.log(`To restart: pnpm dc:up ${options.name}`);
        return;
      }
    } else if (options.remove) {
      console.log('Stopping Docker container and removing volumes...');
      try {
        execSync('docker compose -f docker-compose.worktree.yml down -v', {
          stdio: 'inherit',
          cwd: worktree_path,
        });
      } catch {
        console.warn('Warning: Failed to stop Docker container (may not be running).');
      }
      remove_traefik_config(alias);
    } else {
      console.log('Stopping Docker container (volumes preserved)...');
      try {
        execSync('docker compose -f docker-compose.worktree.yml stop', {
          stdio: 'inherit',
          cwd: worktree_path,
        });
      } catch {
        console.warn('Warning: Failed to stop Docker container (may not be running).');
      }
      remove_traefik_config(alias);
      console.log(`Docker container stopped. Worktree preserved at: ${worktree_path}`);
      console.log(`To restart: pnpm dc:up ${options.name}`);
      return;
    }
  } else if (!options.remove) {
    console.log('This is a local worktree (no Docker). Use --remove to delete it.');
    return;
  }

  const remove_cmd = options.force ? 'worktree remove --force' : 'worktree remove';
  try {
    execSync(`git -C "${repo_root}" ${remove_cmd} "${worktree_path}"`, { stdio: 'inherit' });
  } catch (error) {
    if (!options.force) {
      console.error('Failed to remove worktree. There may be uncommitted changes.');
      console.error('Use --force to remove anyway.');
      process.exit(1);
    }
    throw error;
  }

  if (options.delete_branch && has_ref(repo_root, `refs/heads/${options.name}`)) {
    const delete_flag = options.force ? '-D' : '-d';
    try {
      execSync(`git -C "${repo_root}" branch ${delete_flag} "${options.name}"`, { stdio: 'inherit' });
    } catch {
      console.warn(`Warning: Could not delete branch "${options.name}" (may not be fully merged).`);
      if (!options.force) {
        console.warn('Use --force to force-delete the branch.');
      }
    }
  }

  execSync(`git -C "${repo_root}" worktree prune`, { stdio: 'inherit' });

  if (fs.existsSync(worktree_path)) {
    fs.rmSync(worktree_path, { recursive: true, force: true });
  }

  console.log(`Removed worktree and Docker environment: ${options.name}`);
}

main();
