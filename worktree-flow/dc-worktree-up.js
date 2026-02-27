const { execSync } = require('child_process');
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const { SERVICE_PORTS, compute_ports, format_port_table, find_free_offset, VALID_SERVICE_MODES } = require('./service-ports');
const { get_lan_ip, build_lan_domain } = require('./lan-ip');
const config_mod = require('./config');

const config = config_mod.load_config({ required: false }) || null;
const scripts_dir = __dirname;

// ── Arg parsing ─────────────────────────────────────────────────────────

function parseArgs(argv) {
  const options = {
    name: null,
    branch: null,
    from: null,
    alias: null,
    open: false,
    rebuild: false,
    shared_db: false,
    seed: false,
    poll: false,
    lan: false,
    host_build: false,
    no_host_build: false,
    no_docker: false,
    mode: config && config.services.defaultMode ? config.services.defaultMode : 'full',
  };

  const remaining = [...argv];
  while (remaining.length) {
    const arg = remaining.shift();
    if (!options.name && !arg.startsWith('--')) {
      options.name = arg;
      continue;
    }

    if (arg === '--open') { options.open = true; continue; }
    if (arg === '--rebuild') { options.rebuild = true; continue; }
    if (arg === '--shared-db') { options.shared_db = true; continue; }
    if (arg === '--seed') { options.seed = true; continue; }
    if (arg === '--poll') { options.poll = true; continue; }
    if (arg === '--lan') { options.lan = true; continue; }
    if (arg === '--host-build') { options.host_build = true; continue; }
    if (arg === '--no-host-build') { options.no_host_build = true; continue; }
    if (arg === '--no-docker') { options.no_docker = true; continue; }

    if (arg === '--branch') { options.branch = remaining.shift(); continue; }
    if (arg.startsWith('--branch=')) { options.branch = arg.split('=')[1]; continue; }
    if (arg === '--from') { options.from = remaining.shift(); continue; }
    if (arg.startsWith('--from=')) { options.from = arg.split('=')[1]; continue; }
    if (arg === '--alias') { options.alias = remaining.shift(); continue; }
    if (arg.startsWith('--alias=')) { options.alias = arg.split('=')[1]; continue; }
    if (arg === '--mode') { options.mode = remaining.shift(); continue; }
    if (arg.startsWith('--mode=')) { options.mode = arg.split('=')[1]; continue; }

    console.error(`Unknown argument: ${arg}`);
    return null;
  }

  return options;
}

// ── Helpers ──────────────────────────────────────────────────────────────

function run(command, opts = {}) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8', ...opts }).trim();
}

function auto_alias(branch_name) {
  const prefixes = config ? config.repo.branchPrefixes : ['feat', 'fix', 'ops', 'hotfix', 'release', 'chore'];
  const prefix_pattern = new RegExp(`^(${prefixes.join('|')})\\/`, 'i');
  const stripped = branch_name.replace(prefix_pattern, '');
  const clean = stripped.replace(/\//g, '-').replace(/[^a-zA-Z0-9-]/g, '-').toLowerCase();
  const parts = clean.split('-').filter(Boolean);
  return parts.slice(0, 2).join('-') || clean.slice(0, 20);
}

function read_stored_mode(worktree_path) {
  const compose_path = path.join(worktree_path, 'docker-compose.worktree.yml');
  if (!fs.existsSync(compose_path)) return null;
  const content = fs.readFileSync(compose_path, 'utf8');
  const services_var = config ? config_mod.worktree_var(config, 'services') : 'WORKTREE_SERVICES';
  const match = content.match(new RegExp(`${services_var}=(\\w+)`));
  return match ? match[1] : null;
}

function read_stored_alias(env_file_path) {
  if (!fs.existsSync(env_file_path)) return null;
  const content = fs.readFileSync(env_file_path, 'utf8');
  const alias_var = config ? config_mod.worktree_var(config, 'alias') : 'WORKTREE_ALIAS';
  const match = content.match(new RegExp(`^${alias_var}=(.+)$`, 'm'));
  return match ? match[1].trim() : null;
}

function has_ref(repo_root, ref) {
  try {
    execSync(`git -C "${repo_root}" show-ref --verify --quiet "${ref}"`);
    return true;
  } catch {
    return false;
  }
}

function compute_auto_offset(seed) {
  if (config) return config_mod.compute_offset(config, seed);
  const hash = crypto.createHash('sha256').update(seed).digest('hex');
  const hash_int = Number.parseInt(hash.slice(0, 8), 16);
  return (hash_int % 2000) + 100;
}

function get_container_name(alias) {
  if (config) return config_mod.container_name(config, alias);
  return alias; // no config = just use alias
}

function get_domain(alias) {
  if (config) return config_mod.domain_for(config, alias);
  return `${alias}.localhost`;
}

function get_db_name(alias) {
  if (config) return config_mod.db_name(config, alias);
  return null;
}

function has_local_db() {
  return config && config.database && (config.database.host || config.database.containerHost);
}

// ── Git exclude ─────────────────────────────────────────────────────────

function ensure_git_exclude(worktree_path) {
  const raw_common_dir = run(`git -C "${worktree_path}" rev-parse --git-common-dir`);
  const common_dir = path.resolve(worktree_path, raw_common_dir);
  const exclude_dir = path.join(common_dir, 'info');
  const exclude_file = path.join(exclude_dir, 'exclude');

  const env_filename = config ? config.env.filename : '.env.worktree';
  const patterns = [env_filename, 'docker-compose.worktree.yml', 'docker-compose.traefik.yml'];
  if (config && config.paths.dockerOverrides) {
    patterns.push(config.paths.dockerOverrides + '/');
  } else {
    patterns.push('.docker-overrides/');
  }

  fs.mkdirSync(exclude_dir, { recursive: true });

  let content = '';
  if (fs.existsSync(exclude_file)) {
    content = fs.readFileSync(exclude_file, 'utf8');
  }

  const missing = patterns.filter((p) => !content.includes(p));
  if (missing.length === 0) return;

  const separator = content && !content.endsWith('\n') ? '\n' : '';
  fs.appendFileSync(exclude_file, separator + missing.join('\n') + '\n');
}

// ── Override files (generate strategy) ──────────────────────────────────

function ensure_override_files(repo_root, worktree_path) {
  const override_files = config && config.docker.generate && config.docker.generate.overrideFiles
    ? config.docker.generate.overrideFiles
    : [];

  if (override_files.length === 0) return;

  let copied = 0;
  for (const entry of override_files) {
    const src = path.join(repo_root, entry.src);
    const dst = path.join(worktree_path, entry.dst);
    try {
      fs.mkdirSync(path.dirname(dst), { recursive: true });
      fs.copyFileSync(src, dst);
      copied++;
    } catch {
      console.warn(`Warning: Could not copy ${entry.src}`);
    }
  }
  if (copied > 0) {
    console.log(`Copied ${copied} override file(s).`);
  }
}

// ── Env file copying (shared strategy) ──────────────────────────────────

function copy_env_files(repo_root, worktree_path) {
  const env_files = config && config.docker.envFiles ? config.docker.envFiles : [];

  for (const rel of env_files) {
    const src = path.join(repo_root, rel);
    const dst = path.join(worktree_path, rel);
    if (fs.existsSync(src)) {
      fs.mkdirSync(path.dirname(dst), { recursive: true });
      fs.copyFileSync(src, dst);
    }
  }
}

// ── Editor ──────────────────────────────────────────────────────────────

function open_in_cursor(worktree_path) {
  try {
    execSync(`cursor -n "${worktree_path}"`, { stdio: 'ignore' });
    return;
  } catch { }

  if (process.platform === 'darwin') {
    try {
      execSync(`open -na "Cursor" --args "${worktree_path}"`, { stdio: 'ignore' });
    } catch { }
  }
}

// ── Port conflict detection ─────────────────────────────────────────────

function find_conflicting_containers(exclude_name) {
  try {
    const lines = run('docker ps --format json').split('\n').filter(Boolean);
    const base_ports = config ? Object.values(config.services.ports) : Object.values(SERVICE_PORTS);
    const port_patterns = base_ports.map((p) => `:${p}->`);
    const conflicts = [];

    for (const line of lines) {
      const info = JSON.parse(line);
      if (info.Names === exclude_name) continue;
      if (port_patterns.some((pat) => (info.Ports || '').includes(pat))) {
        conflicts.push(info.Names);
      }
    }
    return conflicts;
  } catch {
    return [];
  }
}

function prompt_stop_or_cancel(conflicts) {
  console.log('');
  console.log('Port conflict detected!');
  console.log('The following environment(s) are already running and would cause port conflicts:\n');
  for (const name of conflicts) {
    console.log(`  - ${name}`);
  }
  console.log('');
  console.log('  1) Stop now  - stop the running instance(s) and continue');
  console.log("  2) Cancel    - I'll stop it myself");
  console.log('');

  process.stdout.write('Choice [1/2]: ');
  const buf = Buffer.alloc(256);
  const bytes = fs.readSync(0, buf, 0, 256);
  return buf.toString('utf8', 0, bytes).trim() === '1';
}

function stop_containers(names) {
  for (const name of names) {
    console.log(`Stopping ${name}...`);
    try {
      execSync(`docker stop "${name}"`, { stdio: 'inherit' });
    } catch {
      console.warn(`Warning: Could not stop ${name}.`);
    }
  }
}

function check_port_conflicts(container_name) {
  const conflicts = find_conflicting_containers(container_name);
  if (conflicts.length === 0) return;

  const should_stop = prompt_stop_or_cancel(conflicts);
  if (should_stop) {
    stop_containers(conflicts);
    console.log('');
  } else {
    console.log('Cancelled. Stop the running instance(s) first, then try again.');
    process.exit(0);
  }
}

// ── Pre-baked image (generate strategy) ─────────────────────────────────

function ensure_prebaked_image() {
  const base_image = config ? config.docker.baseImage : null;
  if (!base_image) return;

  try {
    const result = run(`docker image inspect ${base_image} --format={{.Id}}`);
    if (result) return;
  } catch { }

  console.log(`Pre-baked image not found. Building ${base_image}...`);
  console.log('(this is a one-time operation)\n');
  const rebuild_script = path.join(scripts_dir, 'dc-rebuild-base.js');
  execSync(`node "${rebuild_script}"`, { stdio: 'inherit' });
  console.log('');
}

// ── Health check ────────────────────────────────────────────────────────

function wait_for_healthy(container_name, timeout_ms = 300000) {
  const start = Date.now();
  let dots = 0;

  while (Date.now() - start < timeout_ms) {
    try {
      const status = run(`docker inspect --format={{.State.Health.Status}} "${container_name}"`);
      if (status === 'healthy') {
        process.stdout.write('\n');
        return true;
      }
      if (status === 'unhealthy') {
        process.stdout.write('\n');
        console.warn('Container reported unhealthy. Check logs with: wt logs <name>');
        return false;
      }
    } catch { }

    dots = (dots + 1) % 4;
    process.stdout.write(`\rWaiting for app to start${'.'.repeat(dots)}${' '.repeat(3 - dots)}`);
    execSync('sleep 3', { stdio: 'ignore' });
  }

  process.stdout.write('\n');
  console.warn('Timed out waiting for container health. Check logs with: wt logs <name>');
  return false;
}

// ── LAN ─────────────────────────────────────────────────────────────────

function resolve_lan_domain(alias, env_file, enable_lan) {
  if (enable_lan) {
    const ip = get_lan_ip();
    if (!ip) {
      console.warn('Warning: Could not detect LAN IP. Skipping LAN mode.');
      return null;
    }
    const domain = build_lan_domain(alias, ip);
    return { ip, domain };
  }
  if (fs.existsSync(env_file)) {
    const content = fs.readFileSync(env_file, 'utf8');
    const lan_domain_var = config ? config_mod.env_var(config, 'lanDomain') : null;
    if (lan_domain_var) {
      const match = content.match(new RegExp(`^${lan_domain_var}=(.+)$`, 'm'));
      if (match) {
        const domain = match[1].trim();
        const ip_match = domain.match(/\.(\d+\.\d+\.\d+\.\d+)\.nip\.io$/);
        return { ip: ip_match ? ip_match[1] : null, domain };
      }
    }
  }
  return null;
}

function update_env_lan(env_file, lan_domain) {
  if (!lan_domain || !config) return;
  const lan_var = config_mod.env_var(config, 'lanDomain');
  const ip_var = config_mod.env_var(config, 'localIp');
  const app_url_var = config_mod.env_var(config, 'appUrl');
  if (!lan_var) return;

  let content = fs.readFileSync(env_file, 'utf8');
  if (content.includes(`${lan_var}=`)) {
    content = content.replace(new RegExp(`^${lan_var}=.+$`, 'm'), `${lan_var}=${lan_domain}`);
  } else {
    content = content.trimEnd() + `\n${lan_var}=${lan_domain}\n`;
  }
  if (ip_var) {
    content = content.replace(new RegExp(`^${ip_var}=.+$`, 'm'), `${ip_var}=${lan_domain}`);
  }
  if (app_url_var) {
    content = content.replace(new RegExp(`^${app_url_var}=.+$`, 'm'), `${app_url_var}=http://${lan_domain}/`);
  }
  fs.writeFileSync(env_file, content, 'utf8');
}

// ── Env file helpers ────────────────────────────────────────────────────

function ensure_env_defaults(env_file, alias) {
  if (!config) return;
  const content = fs.readFileSync(env_file, 'utf8');
  const domain = get_domain(alias);
  let updated = content;

  const ip_var = config_mod.env_var(config, 'localIp');
  const app_url_var = config_mod.env_var(config, 'appUrl');
  const dev_heap_var = config_mod.worktree_var(config, 'devHeap');
  const dev_heap_val = config.features.devHeap;

  if (ip_var && !content.includes(`${ip_var}=`)) {
    updated = updated.trimEnd() + `\n${ip_var}=${domain}\n`;
  }
  if (app_url_var && !content.includes(`${app_url_var}=`)) {
    updated = updated.trimEnd() + `\n${app_url_var}=http://${domain}/\n`;
  }
  if (dev_heap_var && dev_heap_val && !content.includes(`${dev_heap_var}=`)) {
    updated = updated.trimEnd() + `\n${dev_heap_var}=${dev_heap_val}\n`;
  }

  if (updated !== content) {
    fs.writeFileSync(env_file, updated, 'utf8');
  }
}

function ensure_env_offset(env_file, offset) {
  const offset_var = config ? config_mod.worktree_var(config, 'hostPortOffset') : 'WORKTREE_HOST_PORT_OFFSET';
  let content = fs.readFileSync(env_file, 'utf8');
  if (content.includes(`${offset_var}=`)) {
    content = content.replace(new RegExp(`^${offset_var}=.+$`, 'm'), `${offset_var}=${offset}`);
  } else {
    content = content.trimEnd() + `\n${offset_var}=${offset}\n`;
  }
  fs.writeFileSync(env_file, content, 'utf8');
}

function ensure_host_build(env_file, worktree_path, repo_root, enable) {
  const host_build_var = config ? config_mod.worktree_var(config, 'hostBuild') : 'WORKTREE_HOST_BUILD';
  let content = fs.readFileSync(env_file, 'utf8');
  if (enable) {
    if (!content.includes(`${host_build_var}=`)) {
      content = content.trimEnd() + `\n${host_build_var}=true\n`;
    } else {
      content = content.replace(new RegExp(`^${host_build_var}=.+$`, 'm'), `${host_build_var}=true`);
    }
    fs.writeFileSync(env_file, content, 'utf8');

    const nm_link = path.join(worktree_path, 'node_modules');
    const nm_target = path.join(repo_root, 'node_modules');
    try {
      const stat = fs.lstatSync(nm_link);
      if (stat.isSymbolicLink()) {
        const current = fs.readlinkSync(nm_link);
        if (current === nm_target) return;
        fs.unlinkSync(nm_link);
      } else if (stat.isDirectory()) {
        fs.rmSync(nm_link, { recursive: true });
      }
    } catch { }
    if (!fs.existsSync(nm_link)) {
      fs.symlinkSync(nm_target, nm_link);
      console.log(`Symlinked node_modules -> ${nm_target}`);
    }
  } else {
    if (content.includes(`${host_build_var}=`)) {
      content = content.replace(new RegExp(`^${host_build_var}=.+\\n?`, 'm'), '');
      fs.writeFileSync(env_file, content, 'utf8');
      console.log('Disabled host-build mode.');
    }
    const nm_link = path.join(worktree_path, 'node_modules');
    try {
      const stat = fs.lstatSync(nm_link);
      if (stat.isSymbolicLink()) {
        fs.unlinkSync(nm_link);
        console.log('Removed node_modules symlink.');
      }
    } catch {}
  }
}

// ── Output helpers ──────────────────────────────────────────────────────

function print_quick_links(ports, alias, lan) {
  const quick_links = config ? (config.services.quickLinks || []) : [];
  const domain = get_domain(alias);

  console.log('Quick Links:');
  if (quick_links.length > 0) {
    for (const ql of quick_links) {
      const svc_port = ports[ql.service] || '?';
      const url = domain ? `http://${domain}${ql.pathPrefix || ''}` : `http://localhost:${svc_port}${ql.pathPrefix || ''}`;
      console.log(`  ${ql.label}: ${url}`);
    }
  } else {
    const primary = config ? config.services.primary : null;
    const primary_port = primary && ports[primary] ? ports[primary] : Object.values(ports)[0];
    console.log(`  http://localhost:${primary_port}`);
  }
  if (lan) console.log(`  LAN:   http://${lan.domain}/`);
}

function print_service_ports(ports) {
  console.log('Service Ports:');
  for (const [name, port] of Object.entries(ports)) {
    console.log(`  ${name.padEnd(20)} ${port}`);
  }
}

// ── Traefik override (shared compose strategy) ──────────────────────────

function generate_traefik_override(worktree_path, alias, network_name) {
  if (!config || !config.services || !config.services.ports) return null;

  const svc_ports = config.services.ports;
  const primary = config.services.primary || Object.keys(svc_ports)[0];
  const domain = get_domain(alias);
  if (!domain) return null;

  const project_name = config.name;
  const safe_alias = alias.replace(/[^a-zA-Z0-9-]/g, '-').toLowerCase();

  const lines = ['services:'];

  const service_names = Object.keys(svc_ports);
  for (const svc_name of service_names) {
    const container_port = svc_ports[svc_name];
    const router_name = `${project_name}-${safe_alias}-${svc_name}`;

    let rule;
    if (svc_name === primary) {
      rule = `Host(\`${domain}\`)`;
    } else {
      rule = `Host(\`${domain}\`) && PathPrefix(\`/${svc_name}\`)`;
    }

    lines.push(`  ${svc_name}:`);
    lines.push('    labels:');
    lines.push('      - "traefik.enable=true"');
    lines.push(`      - "traefik.http.routers.${router_name}.rule=${rule}"`);
    lines.push(`      - "traefik.http.routers.${router_name}.entrypoints=web"`);
    lines.push(`      - "traefik.http.services.${router_name}.loadbalancer.server.port=${container_port}"`);
    if (svc_name !== primary) {
      // Higher priority for path-prefixed routes so they match before the catch-all Host rule
      lines.push(`      - "traefik.http.routers.${router_name}.priority=200"`);
    }
    lines.push('    networks:');
    lines.push(`      - ${network_name}`);
  }

  lines.push('networks:');
  lines.push(`  ${network_name}:`);
  lines.push('    external: true');
  lines.push('');

  const override_path = path.join(worktree_path, 'docker-compose.traefik.yml');
  fs.writeFileSync(override_path, lines.join('\n'), 'utf8');
  return override_path;
}

// ── Main ────────────────────────────────────────────────────────────────

function main() {
  const options = parseArgs(process.argv.slice(2));
  if (!options || !options.name) {
    console.log('Usage:');
    console.log('  wt up <name> --from=origin/branch');
    console.log('  wt up <name> --branch=<new>');
    console.log('');
    console.log('Options:');
    console.log('  --from=<ref>     Base ref to create the branch from');
    console.log('  --branch=<new>   Create a new branch name using <name> as source');
    console.log('  --open           Open the worktree in Cursor');
    console.log('  --rebuild        Rebuild the Docker image and recreate the container');
    console.log('  --alias=<name>   Short name for container (auto-derived if omitted)');
    console.log('  --shared-db      Use the shared db instead of an isolated per-worktree database');
    console.log('  --seed           Seed the isolated database from the shared db after creation');
    console.log('  --mode=<mode>    Service mode (from config)');
    console.log('  --host-build     Run frontend build on host instead of in container');
    console.log('  --no-host-build  Disable host-build mode');
    console.log('  --no-docker      Create worktree without Docker');
    process.exit(1);
  }

  const valid_modes = config ? Object.keys(config.services.modes) : VALID_SERVICE_MODES;
  if (!options.no_docker && !valid_modes.includes(options.mode)) {
    console.error(`Invalid service mode: ${options.mode}. Valid modes: ${valid_modes.join(', ')}`);
    process.exit(1);
  }

  const repo_root = run('git rev-parse --show-toplevel');
  const worktrees_dir = config && config.repo._worktreesDirResolved
    ? config.repo._worktreesDirResolved
    : path.join(path.dirname(repo_root), `${path.basename(repo_root)}-worktrees`);

  fs.mkdirSync(worktrees_dir, { recursive: true });

  const target_branch = options.branch || options.name;
  const worktree_path = path.join(worktrees_dir, target_branch.replace(/\//g, '-'));
  const env_filename = config ? config.env.filename : '.env.worktree';
  const env_file = path.join(worktree_path, env_filename);
  const alias = options.alias || read_stored_alias(env_file) || auto_alias(target_branch);
  const container_name = get_container_name(alias);

  options.mode_explicit = process.argv.some((a) => a === '--mode' || a.startsWith('--mode='));

  // ── Restart existing worktree ───────────────────────────────────────
  if (fs.existsSync(worktree_path)) {
    const compose_strategy = config ? config.docker.composeStrategy : 'generate';
    const is_shared_compose = compose_strategy !== 'generate' && config && config.docker._composeFileResolved;
    const worktree_compose = path.join(worktree_path, 'docker-compose.worktree.yml');
    const worktree_env = path.join(worktree_path, env_filename);
    const has_worktree_marker = fs.existsSync(worktree_compose) || (is_shared_compose && fs.existsSync(worktree_env));

    if (options.no_docker) {
      console.log(`Worktree already exists at: ${worktree_path} (no-docker)`);
      ensure_git_exclude(worktree_path);
      if (options.open) open_in_cursor(worktree_path);
      return;
    }

    if (has_worktree_marker) {
      console.log(`Worktree already exists at: ${worktree_path}`);
      ensure_git_exclude(worktree_path);

      if (is_shared_compose) {
        return restart_shared(repo_root, worktree_path, alias, options);
      } else {
        return restart_generate(repo_root, worktree_path, target_branch, alias, env_file, options);
      }
    }
    // Path exists but no compose — treat as bare worktree that needs setup
    console.log(`Worktree exists without Docker setup: ${worktree_path}`);
    console.log('Setting up Docker environment...');
  } else {
    // ── Create new worktree ─────────────────────────────────────────────
    create_git_worktree(repo_root, worktree_path, target_branch, options);
  }

  ensure_git_exclude(worktree_path);

  const compose_strategy = config ? config.docker.composeStrategy : 'generate';
  const is_shared_compose = compose_strategy !== 'generate' && config && config.docker._composeFileResolved;

  if (options.no_docker) {
    console.log(`Worktree created at: ${worktree_path}`);
    copy_env_files(repo_root, worktree_path);

    // Install dependencies if node project
    const pkg_path = path.join(worktree_path, 'package.json');
    if (fs.existsSync(pkg_path)) {
      console.log('Rebuilding native modules...');
      execSync('pnpm rebuild', { cwd: worktree_path, stdio: 'inherit' });
      console.log('Installing dependencies...');
      execSync('pnpm install --force --dangerously-allow-all-builds', { cwd: worktree_path, stdio: 'inherit' });
    }

    if (options.open) open_in_cursor(worktree_path);

    // Start dev server
    const path_var = config ? config_mod.env_var(config, 'projectPath') : 'PROJECT_PATH';
    const dev_cmd = config && config.dash && config.dash.localDevCommand ? config.dash.localDevCommand : 'pnpm dev';
    console.log(`Starting dev server (${dev_cmd})...`);
    execSync(dev_cmd, { cwd: worktree_path, stdio: 'inherit', env: { ...process.env, [path_var]: worktree_path } });
    return;
  }

  if (is_shared_compose) {
    create_shared(repo_root, worktree_path, target_branch, alias, options);
  } else {
    create_generate(repo_root, worktree_path, target_branch, alias, env_file, options);
  }
}

// ── Git worktree creation ───────────────────────────────────────────────

function create_git_worktree(repo_root, worktree_path, target_branch, options) {
  execSync(`git -C "${repo_root}" worktree prune`, { stdio: 'pipe' });

  const branch_exists_locally = has_ref(repo_root, `refs/heads/${target_branch}`);

  if (options.branch) {
    if (!has_ref(repo_root, `refs/heads/${options.name}`)) {
      console.error(`Source branch does not exist locally: ${options.name}`);
      process.exit(1);
    }
    if (branch_exists_locally) {
      console.error(`Target branch already exists locally: ${target_branch}`);
      process.exit(1);
    }
    execSync(
      `git -C "${repo_root}" worktree add -b "${target_branch}" "${worktree_path}" "${options.name}"`,
      { stdio: 'inherit' },
    );
  } else if (options.from) {
    if (branch_exists_locally) {
      console.log(`Branch "${target_branch}" exists locally, checking out into worktree...`);
      execSync(
        `git -C "${repo_root}" worktree add "${worktree_path}" "${target_branch}"`,
        { stdio: 'inherit' },
      );
    } else {
      console.log(`Creating branch "${target_branch}" from ${options.from}...`);
      execSync(
        `git -C "${repo_root}" worktree add -b "${target_branch}" "${worktree_path}" "${options.from}"`,
        { stdio: 'inherit' },
      );
    }
  } else {
    if (branch_exists_locally) {
      console.log(`Branch "${target_branch}" exists locally, checking out into worktree...`);
      execSync(
        `git -C "${repo_root}" worktree add "${worktree_path}" "${target_branch}"`,
        { stdio: 'inherit' },
      );
    } else {
      // Auto-detect base ref
      const configured_refs = config && config.repo.baseRefs ? config.repo.baseRefs : [];
      const candidates = configured_refs.length > 0
        ? configured_refs.map((r) => `refs/remotes/${r.replace(/^refs\/remotes\//, '')}`)
        : [
            'refs/remotes/origin/main',
            'refs/heads/main',
            'refs/remotes/origin/master',
            'refs/heads/master',
          ];
      const base_ref = candidates.find((ref) => has_ref(repo_root, ref));
      if (!base_ref) {
        console.error('Could not find a base branch. Configure repo.baseRefs in workflow.config.js');
        process.exit(1);
      }
      execSync(
        `git -C "${repo_root}" worktree add -b "${target_branch}" "${worktree_path}" "${base_ref}"`,
        { stdio: 'inherit' },
      );
    }
  }

  // Set upstream tracking
  execSync(
    `git -C "${worktree_path}" config "branch.${target_branch}.remote" origin`,
    { stdio: 'inherit' },
  );
  execSync(
    `git -C "${worktree_path}" config "branch.${target_branch}.merge" "refs/heads/${target_branch}"`,
    { stdio: 'inherit' },
  );
}

// ── Shared compose: restart ─────────────────────────────────────────────

function restart_shared(repo_root, worktree_path, alias, options) {
  const port_offset = find_free_offset(compute_auto_offset(worktree_path));
  const svc_ports = config.services.ports;
  const compose_env = { ...process.env, REPO_ROOT: repo_root, PROJECT_ROOT: worktree_path };
  const slug = alias.replace(/[^a-zA-Z0-9]/g, '-').toLowerCase();
  compose_env.BRANCH_SLUG = slug;
  for (const [name, base] of Object.entries(svc_ports)) {
    compose_env[`${name.toUpperCase()}_PORT`] = String(base + port_offset);
  }

  const compose_file = config.docker._composeFileResolved;
  const project_name = config_mod.compose_project(config, slug);
  const recreate_flag = options.rebuild ? ' --force-recreate' : '';

  // Generate Traefik override if proxy.type is traefik
  let traefik_flag = '';
  if (config.docker.proxy && config.docker.proxy.type === 'traefik') {
    const network = (config.docker.sharedInfra && config.docker.sharedInfra.network) || 'web';
    const override = generate_traefik_override(worktree_path, alias, network);
    if (override) {
      traefik_flag = ` -f "${override}"`;
      console.log(`Traefik override: ${override}`);
    }
  }

  console.log('Starting Docker containers...');
  execSync(
    `docker compose -f "${compose_file}"${traefik_flag} -p "${project_name}" up --build -d${recreate_flag}`,
    { stdio: 'inherit', env: compose_env },
  );

  const ports = config_mod.compute_ports(config, port_offset);
  const domain = get_domain(alias);
  console.log(`Alias: ${alias}`);
  if (domain) console.log(`Domain: ${domain}`);
  console.log('');
  print_quick_links(ports, alias, null);
  console.log('');
  print_service_ports(ports);
}

// ── Shared compose: create ──────────────────────────────────────────────

function create_shared(repo_root, worktree_path, target_branch, alias, options) {
  const port_offset = find_free_offset(compute_auto_offset(worktree_path));
  const svc_ports = config.services.ports;
  const port_env_pairs = Object.entries(svc_ports).map(([name, base]) => {
    const env_name = `${name.toUpperCase()}_PORT`;
    return { env_name, port: base + port_offset };
  });

  // Write env file
  const env_filename = config.env.filename || '.env.worktree';
  const env_path = path.join(worktree_path, env_filename);
  const alias_var = config_mod.worktree_var(config, 'alias') || 'WORKTREE_ALIAS';
  const name_var = config_mod.worktree_var(config, 'name') || 'WORKTREE_NAME';
  const offset_var = config_mod.worktree_var(config, 'hostPortOffset') || 'WORKTREE_HOST_PORT_OFFSET';

  const slug = alias.replace(/[^a-zA-Z0-9]/g, '-').toLowerCase();
  const env_lines = [
    `${name_var}=${target_branch}`,
    `${alias_var}=${alias}`,
    `${offset_var}=${port_offset}`,
    `BRANCH_SLUG=${slug}`,
  ];
  for (const { env_name, port } of port_env_pairs) {
    env_lines.push(`${env_name}=${port}`);
  }
  env_lines.push(`REPO_ROOT=${repo_root}`);
  env_lines.push(`PROJECT_ROOT=${worktree_path}`);
  env_lines.push('');
  fs.writeFileSync(env_path, env_lines.join('\n'));

  // Copy env files from config
  copy_env_files(repo_root, worktree_path);

  // Set env vars for docker compose
  const compose_env = {
    ...process.env,
    REPO_ROOT: repo_root,
    PROJECT_ROOT: worktree_path,
    BRANCH_SLUG: slug,
  };
  for (const { env_name, port } of port_env_pairs) {
    compose_env[env_name] = String(port);
  }

  const compose_file = config.docker._composeFileResolved;
  const project_name = config_mod.compose_project(config, slug);

  // Generate Traefik override if proxy.type is traefik
  let traefik_flag = '';
  if (config.docker.proxy && config.docker.proxy.type === 'traefik') {
    const network = (config.docker.sharedInfra && config.docker.sharedInfra.network) || 'web';
    const override = generate_traefik_override(worktree_path, alias, network);
    if (override) {
      traefik_flag = ` -f "${override}"`;
      console.log(`Traefik override: ${override}`);
    }
  }

  console.log('Starting Docker containers...');
  execSync(
    `docker compose -f "${compose_file}"${traefik_flag} -p "${project_name}" up --build -d`,
    { stdio: 'inherit', env: compose_env },
  );

  if (options.open) open_in_cursor(worktree_path);

  const ports = config_mod.compute_ports(config, port_offset);
  const domain = get_domain(alias);

  console.log('');
  console.log(`Worktree created at: ${worktree_path}`);
  console.log(`Port offset: ${port_offset}`);
  console.log(`Alias: ${alias}`);
  if (domain) console.log(`Domain: ${domain}`);
  console.log('');
  print_quick_links(ports, alias, null);
  console.log('');
  print_service_ports(ports);
  console.log('');
  console.log('Commands:');
  console.log(`  Logs:    wt logs ${alias}`);
  console.log(`  Stop:    wt down ${alias}`);
  console.log(`  Status:  wt status`);
}

// ── Generate strategy: restart ──────────────────────────────────────────

function restart_generate(repo_root, worktree_path, target_branch, alias, env_file, options) {
  // Preserve the mode from the existing compose file unless explicitly overridden
  if (!options.mode_explicit) {
    const stored_mode = read_stored_mode(worktree_path);
    if (stored_mode) {
      options.mode = stored_mode;
    }
  }

  if (options.poll) {
    const poll_var = config ? config_mod.worktree_var(config, 'poll') : 'WORKTREE_POLL';
    const env_content = fs.readFileSync(env_file, 'utf8');
    if (!env_content.includes(`${poll_var}=`)) {
      fs.appendFileSync(env_file, `${poll_var}=true\n`);
    }
  }

  const restart_offset = find_free_offset(compute_auto_offset(worktree_path));

  ensure_override_files(repo_root, worktree_path);
  ensure_env_defaults(env_file, alias);
  ensure_env_offset(env_file, restart_offset);

  let host_build;
  if (options.no_host_build) {
    host_build = false;
    ensure_host_build(env_file, worktree_path, repo_root, false);
  } else {
    const hb_var = config ? config_mod.worktree_var(config, 'hostBuild') : 'WORKTREE_HOST_BUILD';
    host_build = options.host_build || fs.readFileSync(env_file, 'utf8').includes(`${hb_var}=true`);
    if (host_build) {
      const nm_path = path.join(worktree_path, 'node_modules');
      try {
        const stat = fs.lstatSync(nm_path);
        if (stat.isDirectory() && !stat.isSymbolicLink()) {
          console.log('Stopping container to replace node_modules directory with symlink...');
          execSync(`docker compose -f docker-compose.worktree.yml down`, { stdio: 'inherit', cwd: worktree_path });
        }
      } catch {}
      ensure_host_build(env_file, worktree_path, repo_root, true);
    }
  }

  const lan = resolve_lan_domain(alias, env_file, options.lan);
  const lan_flag = lan ? ` --lan-domain "${lan.domain}"` : '';
  const host_build_flag = host_build ? ' --host-build' : '';

  const compose_script = path.join(scripts_dir, 'generate-docker-compose.js');
  execSync(
    `node "${compose_script}" --path "${worktree_path}" --name "${target_branch}" --alias "${alias}" --offset ${restart_offset} --mode ${options.mode}${lan_flag}${host_build_flag}`,
    { stdio: 'inherit' },
  );

  if (lan) update_env_lan(env_file, lan.domain);

  ensure_prebaked_image();
  check_port_conflicts(get_container_name(alias));

  const recreate_flag = (options.rebuild || options.lan || host_build || options.no_host_build) ? ' --force-recreate' : '';
  console.log('Starting Docker container...');
  execSync(`docker compose -f docker-compose.worktree.yml up -d${recreate_flag}`, {
    stdio: 'inherit',
    cwd: worktree_path,
  });
  wait_for_healthy(get_container_name(alias));

  const ports = config ? config_mod.compute_ports(config, restart_offset) : compute_ports(restart_offset);
  const domain = get_domain(alias);

  console.log(`Alias: ${alias}`);
  if (domain) console.log(`Domain: ${domain}`);
  if (lan) console.log(`LAN:    ${lan.domain}`);
  console.log(`Service mode: ${options.mode}`);
  console.log('');
  print_quick_links(ports, alias, lan);
  console.log('');
  if (config) {
    print_service_ports(ports);
  } else {
    console.log('Service Ports:');
    console.log(format_port_table(restart_offset, { mode: options.mode }));
  }
}

// ── Generate strategy: create ───────────────────────────────────────────

function create_generate(repo_root, worktree_path, target_branch, alias, env_file, options) {
  const env_script = path.join(scripts_dir, 'create-worktree-env.js');
  const shared_db_flag = options.shared_db ? ' --shared-db' : '';
  const force_flag = fs.existsSync(env_file) ? ' --force' : '';
  execSync(`node "${env_script}" --auto --docker --alias "${alias}"${shared_db_flag}${force_flag} --dir "${worktree_path}"`, { stdio: 'inherit' });

  const port_offset = find_free_offset(compute_auto_offset(worktree_path));
  const offset_var = config ? config_mod.worktree_var(config, 'hostPortOffset') : 'WORKTREE_HOST_PORT_OFFSET';
  fs.appendFileSync(env_file, `${offset_var}=${port_offset}\n`);
  if (options.poll) {
    const poll_var = config ? config_mod.worktree_var(config, 'poll') : 'WORKTREE_POLL';
    fs.appendFileSync(env_file, `${poll_var}=true\n`);
  }

  if (options.host_build) ensure_host_build(env_file, worktree_path, repo_root, true);

  const lan = resolve_lan_domain(alias, env_file, options.lan);
  if (lan) update_env_lan(env_file, lan.domain);
  const lan_flag = lan ? ` --lan-domain "${lan.domain}"` : '';
  const host_build_flag = options.host_build ? ' --host-build' : '';

  ensure_override_files(repo_root, worktree_path);

  const compose_script = path.join(scripts_dir, 'generate-docker-compose.js');
  execSync(
    `node "${compose_script}" --path "${worktree_path}" --name "${target_branch}" --alias "${alias}" --offset ${port_offset} --mode ${options.mode}${lan_flag}${host_build_flag}`,
    { stdio: 'inherit' },
  );

  ensure_prebaked_image();
  check_port_conflicts(get_container_name(alias));

  console.log('Starting Docker container...');
  execSync('docker compose -f docker-compose.worktree.yml up -d', {
    stdio: 'inherit',
    cwd: worktree_path,
  });

  if (options.open) open_in_cursor(worktree_path);

  wait_for_healthy(get_container_name(alias));

  const ports = config ? config_mod.compute_ports(config, port_offset) : compute_ports(port_offset);
  const domain = get_domain(alias);

  console.log(`Worktree created at: ${worktree_path}`);
  console.log(`Port offset: ${port_offset}`);
  console.log(`Alias: ${alias}`);
  if (domain) console.log(`Domain: ${domain}`);
  if (lan) console.log(`LAN:    ${lan.domain}`);
  console.log(`Service mode: ${options.mode}`);
  console.log('');
  print_quick_links(ports, alias, lan);
  console.log('');
  if (config) {
    print_service_ports(ports);
  } else {
    console.log('Service Ports:');
    console.log(format_port_table(port_offset, { mode: options.mode }));
  }

  // Database info
  if (has_local_db() && !options.shared_db) {
    const db_name_str = get_db_name(alias);
    if (options.seed) {
      console.log('');
      console.log('Seeding database...');
      const seed_script = path.join(scripts_dir, 'dc-seed.js');
      execSync(`node "${seed_script}" "${options.name}"`, { stdio: 'inherit' });
    } else if (db_name_str) {
      console.log('');
      console.log(`Database: ${db_name_str} (empty)`);
      console.log(`Run \`wt seed ${options.name}\` to populate from the shared db.`);
    }
  }
}

main();
