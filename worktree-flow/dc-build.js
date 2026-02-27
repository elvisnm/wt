const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

function resolve_worktrees_dir() {
  if (config && config.repo._worktreesDirResolved) {
    return config.repo._worktreesDirResolved;
  }
  const repo_root = execSync('git rev-parse --show-toplevel', {
    stdio: 'pipe',
    encoding: 'utf8',
  }).trim();
  const project_name = path.basename(repo_root);
  const parent_dir = path.dirname(repo_root);
  return path.join(parent_dir, `${project_name}-worktrees`);
}

function find_worktree(name) {
  const worktrees_dir = resolve_worktrees_dir();
  const direct = path.join(worktrees_dir, name.replace(/\//g, '-'));
  if (fs.existsSync(direct)) return direct;

  if (!fs.existsSync(worktrees_dir)) return null;

  function scan(dir) {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      if (!entry.isDirectory()) continue;
      const full = path.join(dir, entry.name);
      if (entry.name.includes(name) && fs.existsSync(path.join(full, '.env.worktree'))) {
        return full;
      }
      const nested = scan(full);
      if (nested) return nested;
    }
    return null;
  }

  return scan(worktrees_dir);
}

function load_env_file(file_path) {
  const vars = {};
  if (!fs.existsSync(file_path)) return vars;
  const content = fs.readFileSync(file_path, 'utf8');
  for (const line of content.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;
    const idx = trimmed.indexOf('=');
    if (idx === -1) continue;
    const key = trimmed.slice(0, idx).trim();
    let value = trimmed.slice(idx + 1).trim();
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1);
    }
    vars[key] = value;
  }
  return vars;
}

function apply_overrides(worktree_path) {
  const overrides_dir = path.join(worktree_path, '.docker-overrides', 'frontend');
  if (!fs.existsSync(overrides_dir)) return [];

  const patched = [];
  function walk(dir, rel) {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      const rel_path = path.join(rel, entry.name);
      if (entry.isDirectory()) {
        walk(full, rel_path);
      } else {
        const target = path.join(worktree_path, 'frontend', rel_path);
        const backup = target + '.host-build-backup';
        if (fs.existsSync(target) && !fs.existsSync(backup)) {
          fs.copyFileSync(target, backup);
        }
        fs.copyFileSync(full, target);
        patched.push({ target, backup, had_original: fs.existsSync(backup) });
      }
    }
  }
  walk(overrides_dir, '');

  if (patched.length > 0) {
    console.log(`Applied ${patched.length} frontend override(s) from .docker-overrides/`);
  }
  return patched;
}

function restore_overrides(patched) {
  for (const { target, backup, had_original } of patched) {
    if (had_original && fs.existsSync(backup)) {
      fs.renameSync(backup, target);
    } else if (fs.existsSync(backup)) {
      fs.unlinkSync(backup);
    }
  }
  if (patched.length > 0) {
    console.log(`Restored ${patched.length} frontend file(s)`);
  }
}

function main() {
  const args = process.argv.slice(2);
  const name = args.find((a) => !a.startsWith('-'));

  if (!name) {
    console.log('Usage:');
    console.log('  pnpm dc:build <name>         Run esbuild watch on host for a worktree');
    console.log('');
    console.log('This runs the frontend build (esbuild --watch) on the host machine');
    console.log('instead of inside the Docker container for faster rebuilds.');
    console.log('');
    console.log('The worktree must have been created with --host-build flag.');
    process.exit(1);
  }

  const worktree_path = find_worktree(name);
  if (!worktree_path) {
    console.error(`Worktree not found: ${name}`);
    process.exit(1);
  }

  const env_file = path.join(worktree_path, '.env.worktree');
  if (!fs.existsSync(env_file)) {
    console.error(`No .env.worktree found at: ${worktree_path}`);
    process.exit(1);
  }

  const env_vars = load_env_file(env_file);

  if (env_vars.WORKTREE_HOST_BUILD !== 'true') {
    console.error('This worktree was not created with --host-build.');
    console.error('Recreate it with: pnpm dc:up ' + name + ' --host-build');
    process.exit(1);
  }

  const nm_path = path.join(worktree_path, 'node_modules');
  if (!fs.existsSync(nm_path)) {
    console.error('node_modules symlink not found. Recreate with: pnpm dc:up ' + name + ' --host-build');
    process.exit(1);
  }

  const env_key = config ? config_mod.env_var(config, 'environment') : 'APP_ENV';
  const path_key = config ? config_mod.env_var(config, 'projectPath') : 'PROJECT_PATH';
  const env = {
    ...process.env,
    NODE_ENV: 'development',
    WORKTREE_HOST_BUILD: 'true',
  };
  if (env_key) env[env_key] = 'development';
  if (path_key) env[path_key] = worktree_path;

  const host_offset = env_vars.WORKTREE_HOST_PORT_OFFSET || env_vars.WORKTREE_PORT_OFFSET;
  if (host_offset) {
    env.WORKTREE_PORT_OFFSET = host_offset;
  }
  if (env_vars.WORKTREE_PORT_BASE) {
    env.WORKTREE_PORT_BASE = env_vars.WORKTREE_PORT_BASE;
  }
  if (env_vars.WORKTREE_NAME) {
    env.WORKTREE_NAME = env_vars.WORKTREE_NAME;
  }
  const local_ip_key = config ? config_mod.env_var(config, 'localIp') : 'LOCAL_IP';
  const app_url_key = config ? config_mod.env_var(config, 'appUrl') : 'APP_URL';
  if (local_ip_key && env_vars[local_ip_key]) {
    env[local_ip_key] = env_vars[local_ip_key];
  }
  if (app_url_key && env_vars[app_url_key]) {
    env[app_url_key] = env_vars[app_url_key];
  }

  const alias = path.basename(worktree_path);
  console.log(`Starting host build for: ${alias}`);
  console.log(`Worktree: ${worktree_path}`);
  console.log(`Port offset: ${host_offset || env_vars.WORKTREE_PORT_BASE || '0'}`);
  console.log('');

  const repo_root = execSync('git rev-parse --show-toplevel', {
    stdio: 'pipe',
    encoding: 'utf8',
  }).trim();
  const build_script = config && config.paths._buildScriptResolved
    ? config.paths._buildScriptResolved
    : path.join(repo_root, 'scripts', 'deployment_scripts', 'build.js');

  const patched = apply_overrides(worktree_path);

  function cleanup() {
    restore_overrides(patched);
  }
  process.on('SIGINT', () => { cleanup(); process.exit(0); });
  process.on('SIGTERM', () => { cleanup(); process.exit(0); });

  try {
    execSync(`node "${build_script}" develop --watch`, {
      stdio: 'inherit',
      cwd: worktree_path,
      env,
    });
  } catch (error) {
    cleanup();
    process.exit(error.status || 1);
  }
}

main();
