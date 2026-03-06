const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const { config, config_mod, run, has_ref, resolve_worktree_path, read_env, read_env_multi, sanitize_name } = require('./lib/utils');
const { find_pm2, pm2_home, pm2_cleanup } = require('./lib/pm2');

function parse_args(argv) {
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

function remove_traefik_config(alias) {
  if (!alias) return;
  const traefik_dir = config && config.docker && config.docker.proxy
    && config.docker.proxy._dynamicDirResolved
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
  const options = parse_args(process.argv.slice(2));
  if (!options || !options.name) {
    console.log('Usage:');
    console.log('  pnpm dc:down <name>                          Stop the Docker container');
    console.log('  pnpm dc:down <name> --remove                 Stop container, remove volumes and worktree');
    console.log('  pnpm dc:down <name> --remove --delete-branch Also delete the local branch');
    console.log('  pnpm dc:down <name> --remove --force         Force remove with uncommitted changes');
    process.exit(1);
  }

  let repo_root;
  if (config && config._repoRoot) {
    repo_root = config._repoRoot;
  } else {
    try {
      repo_root = run('git rev-parse --show-toplevel');
    } catch {
      console.error('Could not determine repository root. Run this from inside a git repo or ensure workflow.config.js is accessible.');
      process.exit(1);
    }
  }
  const worktree_path = resolve_worktree_path(repo_root, options.name);

  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    process.exit(1);
  }

  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  const env_filename = config ? config.env.filename : '.env.worktree';
  const shared = config ? config_mod.get_compose_info(config, worktree_path) : null;
  const is_docker = fs.existsSync(compose_file) || !!shared;
  const alias = is_docker ? read_env(worktree_path, 'WORKTREE_ALIAS') : null;

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
  } else {
    // Local (non-Docker) worktree — stop PM2 if running
    const home = pm2_home(worktree_path);
    if (fs.existsSync(home)) {
      const pm2_bin = find_pm2(repo_root);
      console.log('Stopping local PM2 services...');
      pm2_cleanup(pm2_bin, home);
      console.log('PM2 services stopped.');
    }
    remove_traefik_config(alias);

    if (!options.remove) {
      console.log(`Local worktree preserved at: ${worktree_path}`);
      console.log(`To restart: wt up ${options.name}`);
      return;
    }
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
