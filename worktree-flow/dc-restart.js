const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const { format_port_table } = require('./service-ports');
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

function compute_auto_offset(seed) {
  const crypto = require('crypto');
  const hash = crypto.createHash('sha256').update(seed).digest('hex');
  const hash_int = Number.parseInt(hash.slice(0, 8), 16);
  return (hash_int % 2000) + 100;
}

function read_offset(worktree_path) {
  const env_path = path.join(worktree_path, '.env.worktree');
  if (fs.existsSync(env_path)) {
    const content = fs.readFileSync(env_path, 'utf8');
    const host_offset_match = content.match(/^WORKTREE_HOST_PORT_OFFSET=(\d+)/m);
    if (host_offset_match) return Number.parseInt(host_offset_match[1], 10);

    const offset_match = content.match(/^WORKTREE_PORT_OFFSET=(\d+)/m);
    if (offset_match) return Number.parseInt(offset_match[1], 10);

    const base_match = content.match(/^WORKTREE_PORT_BASE=(\d+)/m);
    if (base_match) return Number.parseInt(base_match[1], 10) - 3000;
  }

  return compute_auto_offset(worktree_path);
}

function main() {
  const name = process.argv[2];

  if (!name || name.startsWith('-')) {
    console.log('Usage:');
    console.log('  pnpm dc:restart <name>    Restart the Docker container');
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

  console.log(`Restarting ${name}...`);

  if (shared) {
    execSync(
      `docker compose -f "${shared.compose_file}" -p "${shared.project}" restart`,
      { stdio: 'inherit', env: { ...process.env, ...shared.env } },
    );
    const offset = read_offset(worktree_path);
    const ports = config_mod.compute_ports(config, offset);
    console.log('\nService Ports:');
    for (const [svc, port] of Object.entries(ports)) {
      console.log(`  ${svc.padEnd(20)} ${port}`);
    }
  } else {
    execSync('docker compose -f docker-compose.worktree.yml restart', {
      stdio: 'inherit',
      cwd: worktree_path,
    });
    const offset = read_offset(worktree_path);
    console.log('\nService Ports:');
    console.log(format_port_table(offset));
  }
}

main();
