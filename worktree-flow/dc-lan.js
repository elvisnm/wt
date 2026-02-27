const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;
const { get_lan_ip, build_lan_domain } = require('./lan-ip');
const { generate_traefik_config, find_traefik_dir, sanitize_name } = require('./generate-docker-compose');

function run(command, opts = {}) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8', ...opts }).trim();
}

function resolve_worktree_path(repo_root, name) {
  const worktrees_dir = config && config.repo._worktreesDirResolved
    ? config.repo._worktreesDirResolved
    : path.join(path.dirname(repo_root), `${path.basename(repo_root)}-worktrees`);
  return path.join(worktrees_dir, name.replace(/\//g, '-'));
}

function read_env(env_path, key) {
  if (!fs.existsSync(env_path)) return null;
  const content = fs.readFileSync(env_path, 'utf8');
  const match = content.match(new RegExp(`^${key}=(.+)$`, 'm'));
  return match ? match[1].trim() : null;
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

function update_env_key(env_path, key, value) {
  let content = fs.readFileSync(env_path, 'utf8');
  const regex = new RegExp(`^${key}=.+$`, 'm');
  if (regex.test(content)) {
    content = content.replace(regex, `${key}=${value}`);
  } else {
    content = content.trimEnd() + `\n${key}=${value}\n`;
  }
  fs.writeFileSync(env_path, content, 'utf8');
}

function remove_env_key(env_path, key) {
  if (!fs.existsSync(env_path)) return;
  let content = fs.readFileSync(env_path, 'utf8');
  content = content.replace(new RegExp(`^${key}=.+\\n?`, 'm'), '');
  fs.writeFileSync(env_path, content, 'utf8');
}

function main() {
  const args = process.argv.slice(2);
  const off_mode = args.includes('--off');
  const name = args.find((a) => !a.startsWith('--'));

  if (!name) {
    console.log('Usage:');
    console.log('  pnpm dc:lan <name>        Enable LAN access for a worktree');
    console.log('  pnpm dc:lan <name> --off   Disable LAN access (revert to .localhost)');
    process.exit(1);
  }

  const repo_root = run('git rev-parse --show-toplevel');
  const worktree_path = resolve_worktree_path(repo_root, name);

  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    process.exit(1);
  }

  const env_path = path.join(worktree_path, '.env.worktree');
  if (!fs.existsSync(env_path)) {
    console.error(`No .env.worktree found at: ${worktree_path}`);
    process.exit(1);
  }

  const container_prefix = config ? config.name + '-' : '';
  const alias = read_env(env_path, 'WORKTREE_ALIAS') || read_container_name(worktree_path)?.replace(new RegExp(`^${container_prefix.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`), '') || path.basename(worktree_path);
  const safe_name = sanitize_name(alias);
  const offset_str = read_env(env_path, 'WORKTREE_HOST_PORT_OFFSET') || read_env(env_path, 'WORKTREE_PORT_OFFSET');
  const port_offset = offset_str ? Number.parseInt(offset_str, 10) : 0;
  const container_name = config ? config_mod.container_name(config, safe_name) : safe_name;
  const localhost_domain = config ? config_mod.domain_for(config, safe_name) : `${safe_name}.localhost`;

  const local_ip_key = config ? config_mod.env_var(config, 'localIp') : 'LOCAL_IP';
  const app_url_key = config ? config_mod.env_var(config, 'appUrl') : 'APP_URL';
  const lan_domain_key = config ? config_mod.env_var(config, 'lanDomain') : 'LAN_DOMAIN';

  if (off_mode) {
    if (local_ip_key) update_env_key(env_path, local_ip_key, localhost_domain);
    if (app_url_key) update_env_key(env_path, app_url_key, `http://${localhost_domain}/`);
    if (lan_domain_key) remove_env_key(env_path, lan_domain_key);

    const traefik_dir = find_traefik_dir();
    if (traefik_dir) {
      const traefik_yaml = generate_traefik_config(safe_name, localhost_domain, container_name, port_offset);
      fs.writeFileSync(path.join(traefik_dir, `${safe_name}.yml`), traefik_yaml, 'utf8');
    }

    console.log('LAN access disabled. Reverting to .localhost domain.');
    console.log(`Recreating ${container_name} to reload env vars...`);
    const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
    execSync(`docker compose -f "${compose_file}" up -d --force-recreate`, { stdio: 'inherit' });
    console.log(`\nDomain: http://${localhost_domain}/`);
    return;
  }

  const ip = get_lan_ip();
  if (!ip) {
    console.error('Could not detect LAN IP address. Are you connected to a network?');
    process.exit(1);
  }

  const lan_domain = build_lan_domain(safe_name, ip);

  if (local_ip_key) update_env_key(env_path, local_ip_key, lan_domain);
  if (app_url_key) update_env_key(env_path, app_url_key, `http://${lan_domain}/`);
  if (lan_domain_key) update_env_key(env_path, lan_domain_key, lan_domain);

  const traefik_dir = find_traefik_dir();
  if (traefik_dir) {
    const traefik_yaml = generate_traefik_config(safe_name, localhost_domain, container_name, port_offset, lan_domain);
    fs.writeFileSync(path.join(traefik_dir, `${safe_name}.yml`), traefik_yaml, 'utf8');
    console.log(`Updated Traefik config with LAN domain.`);
  }

  console.log(`Detected LAN IP: ${ip}`);
  console.log(`LAN domain: ${lan_domain}`);
  console.log(`\nRecreating ${container_name} to reload env vars...`);
  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');
  execSync(`docker compose -f "${compose_file}" up -d --force-recreate`, { stdio: 'inherit' });

  console.log(`\nShare this URL with anyone on your network:`);
  console.log(`  http://${lan_domain}/`);
}

main();
