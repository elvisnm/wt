const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');
const { SERVICE_PORTS, compute_ports, format_port_table, find_free_offset, VALID_SERVICE_MODES } = require('./service-ports');
const { get_lan_ip, build_lan_domain } = require('./lan-ip');
const {
  config, config_mod, run, auto_alias, has_ref, compute_auto_offset,
  resolve_worktrees_dir, update_env_key, remove_env_key,
} = require('./lib/utils');
const {
  apply_skip_worktree: apply_skip_worktree_config,
  copy_setup_files,
} = require('./lib/skip-worktree');
const { find_pm2, pm2_home, pm2_start, pm2_cleanup, pm2_process_name, load_aws_env } = require('./lib/pm2');
const { generate_config, OUTPUT_FILENAME } = require('./generate-ecosystem-config');
const { generate_workspace } = require('./generate-workspace-config');
const { write_traefik_config, is_traefik_routing } = require('./generate-docker-compose');

const scripts_dir = __dirname;

// ── Arg parsing ─────────────────────────────────────────────────────────

function parse_args(argv) {
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
    no_traefik: false,
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
    if (arg === '--no-traefik') { options.no_traefik = true; continue; }

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

function read_stored_offset(env_file_path) {
  if (!fs.existsSync(env_file_path)) return null;
  const content = fs.readFileSync(env_file_path, 'utf8');
  const offset_var = config ? config_mod.worktree_var(config, 'portOffset') : 'WORKTREE_PORT_OFFSET';
  const match = content.match(new RegExp(`^${offset_var}=(.+)$`, 'm'));
  return match ? parseInt(match[1].trim(), 10) : null;
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

// ── Setup symlinks (all strategies) ──────────────────────────────────────

function ensure_setup_symlinks(repo_root, worktree_path) {
  const symlinks = config && config.setup && config.setup.symlinks
    ? config.setup.symlinks
    : [];

  if (symlinks.length === 0) return;

  let created = 0;
  for (const entry of symlinks) {
    const src = path.isAbsolute(entry.src)
      ? entry.src
      : entry.src.startsWith('~/')
        ? path.join(os.homedir(), entry.src.slice(2))
        : path.resolve(repo_root, entry.src);
    const dst = path.join(worktree_path, entry.dst);

    if (!fs.existsSync(src)) {
      console.warn(`Warning: symlink source does not exist: ${entry.src}`);
      continue;
    }

    try {
      fs.rmSync(dst, { recursive: true, force: true });
      fs.mkdirSync(path.dirname(dst), { recursive: true });
      fs.symlinkSync(src, dst);
      created++;
    } catch (e) {
      console.warn(`Warning: Could not symlink ${entry.src} -> ${entry.dst}: ${e.message}`);
    }
  }
  if (created > 0) {
    console.log(`Created ${created} setup symlink(s).`);
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
    } else {
      console.warn(`Warning: env file not found: ${rel}`);
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
    const base_ports = Object.values(SERVICE_PORTS);
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
  update_env_key(env_file, offset_var, offset);
}

function ensure_host_build(env_file, worktree_path, repo_root, enable) {
  const host_build_var = config ? config_mod.worktree_var(config, 'hostBuild') : 'WORKTREE_HOST_BUILD';
  if (enable) {
    update_env_key(env_file, host_build_var, 'true');

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
    remove_env_key(env_file, host_build_var);
    console.log('Disabled host-build mode.');
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
  const options = parse_args(process.argv.slice(2));
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

  const valid_modes = VALID_SERVICE_MODES;
  if (!options.no_docker && !valid_modes.includes(options.mode)) {
    console.error(`Invalid service mode: ${options.mode}. Valid modes: ${valid_modes.join(', ')}`);
    process.exit(1);
  }

  const repo_root = run('git rev-parse --show-toplevel');
  const worktrees_dir = resolve_worktrees_dir(repo_root);

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
      ensure_setup_symlinks(repo_root, worktree_path);
      copy_setup_files(repo_root, worktree_path);
      const config_src = path.join(repo_root, 'workflow.config.js');
      if (fs.existsSync(config_src)) {
        fs.copyFileSync(config_src, path.join(worktree_path, 'workflow.config.js'));
      }

      // Restart PM2 services if localDev is enabled
      const use_local_dev = config && config_mod.feature_enabled(config, 'localDev')
        && config.services.pm2 && config.services.pm2.ecosystemConfig;
      if (use_local_dev) {
        const home = pm2_home(worktree_path);
        const pm2_bin = find_pm2(repo_root);

        // Stop existing PM2 daemon first
        pm2_cleanup(pm2_bin, home);

        // Regenerate ecosystem config
        const env_file_local = path.join(worktree_path, config.env.filename || '.env.worktree');
        const port_offset = read_stored_offset(env_file_local) || find_free_offset(compute_auto_offset(worktree_path));
        const mode = options.mode || config.services.defaultMode || 'full';
        const active_services = config_mod.resolve_services(config, mode);
        const passthrough = config.services.pm2.envPassthrough || [];
        const env_overrides = { SKULABS_ENV: 'development', NODE_ENV: 'development', ...load_aws_env() };
        if (fs.existsSync(env_file_local)) {
          const content = fs.readFileSync(env_file_local, 'utf8');
          for (const line of content.split('\n')) {
            const trimmed = line.trim();
            if (!trimmed || trimmed.startsWith('#')) continue;
            const idx = trimmed.indexOf('=');
            if (idx === -1) continue;
            const key = trimmed.slice(0, idx).trim();
            if (passthrough.includes(key) || key.startsWith('WORKTREE_') || key === 'PM2_HOME') {
              env_overrides[key] = trimmed.slice(idx + 1).trim();
            }
          }
        }
        const ecosystem_content = generate_config(
          config, worktree_path, target_branch.replace(/\//g, '-'),
          port_offset, active_services, env_overrides,
        );
        const ecosystem_path = path.join(worktree_path, OUTPUT_FILENAME);
        fs.writeFileSync(ecosystem_path, ecosystem_content, 'utf8');

        if (!options.no_traefik) {
          write_traefik_config(alias, config_mod.domain_for(config, alias), port_offset);
        }

        if (process.env.WT_INNER !== '1') {
          console.log('Starting PM2 services...');
          pm2_start({ pm2: pm2_bin, pm2_home: home, ecosystem_config: ecosystem_path, env: load_aws_env(), cwd: worktree_path });

          // Restart frontend build watcher if buildScript is configured
          const build_script_r = config && config.paths && config.paths.buildScript;
          if (build_script_r) {
            const build_script_path_r = path.join(worktree_path, build_script_r);
            if (fs.existsSync(build_script_path_r)) {
              const wt_suffix_r = target_branch.replace(/\//g, '-');
              const build_name_r = pm2_process_name('build', wt_suffix_r);
              const prefix_r = `PM2_HOME="${home}" `;
              try { execSync(`${prefix_r}${pm2_bin} delete "${build_name_r}"`, { stdio: 'pipe', cwd: worktree_path }); } catch {}
              const start_cmd_r = `${prefix_r}${pm2_bin} start "node ${build_script_r} develop --watch" --name "${build_name_r}" --cwd "${worktree_path}" --no-autorestart`;
              try {
                execSync(start_cmd_r, { stdio: 'inherit', cwd: worktree_path, env: { ...process.env, ...load_aws_env(), PM2_HOME: home } });
              } catch {
                console.warn('Warning: Frontend build watcher failed to start.');
              }
            }
          }

          console.log('PM2 services restarted.');
        }
      }

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
  apply_skip_worktree_config(worktree_path);

  const compose_strategy = config ? config.docker.composeStrategy : 'generate';
  const is_shared_compose = compose_strategy !== 'generate' && config && config.docker._composeFileResolved;

  if (options.no_docker) {
    console.log(`Worktree created at: ${worktree_path}`);
    copy_env_files(repo_root, worktree_path);
    ensure_setup_symlinks(repo_root, worktree_path);
    copy_setup_files(repo_root, worktree_path);

    // Copy workflow.config.js so wt commands work inside the worktree
    const config_src = path.join(repo_root, 'workflow.config.js');
    if (fs.existsSync(config_src)) {
      fs.copyFileSync(config_src, path.join(worktree_path, 'workflow.config.js'));
    }

    // Install dependencies if node project
    const pkg_path = path.join(worktree_path, 'package.json');
    if (fs.existsSync(pkg_path)) {
      console.log('Rebuilding native modules...');
      execSync('pnpm rebuild', { cwd: worktree_path, stdio: 'inherit' });
      console.log('Installing dependencies...');
      const pnpm_ver = execSync('pnpm --version', { cwd: worktree_path, encoding: 'utf8' }).trim();
      const pnpm_major = parseInt(pnpm_ver.split('.')[0], 10);
      const install_cmd = pnpm_major >= 10
        ? 'pnpm install --force --dangerously-allow-all-builds'
        : 'pnpm install --force';
      execSync(install_cmd, { cwd: worktree_path, stdio: 'inherit' });
    }

    // Build library packages (e.g. packages/db) so dist/ exists for dev servers
    if (fs.existsSync(path.join(worktree_path, 'turbo.json'))) {
      try {
        execSync('pnpm turbo build --filter="./packages/*"', { cwd: worktree_path, stdio: 'inherit' });
      } catch (e) {
        // Non-fatal: no packages to build
      }
    }

    // Generate .env.worktree for local worktree
    const use_local_dev = config && config_mod.feature_enabled(config, 'localDev')
      && config.services.pm2 && config.services.pm2.ecosystemConfig;

    if (use_local_dev) {
      const home = pm2_home(worktree_path);
      const env_filename_local = config ? config.env.filename : '.env.worktree';
      const env_file_local = path.join(worktree_path, env_filename_local);

      // Generate the env file if it doesn't exist
      if (!fs.existsSync(env_file_local)) {
        const create_env_script = path.join(scripts_dir, 'create-worktree-env.js');
        execSync(`node "${create_env_script}" --auto --dir "${worktree_path}"`, { stdio: 'inherit' });
      }

      // Ensure WORKTREE_ALIAS is in the env file (the alias from --alias flag
      // or auto_alias may differ from the directory name)
      const alias_var_local = config ? config_mod.worktree_var(config, 'alias') : 'WORKTREE_ALIAS';
      if (alias_var_local && alias) {
        const env_content = fs.existsSync(env_file_local) ? fs.readFileSync(env_file_local, 'utf8') : '';
        if (!env_content.includes(`${alias_var_local}=`)) {
          fs.appendFileSync(env_file_local, `${alias_var_local}=${alias}\n`);
        }
      }

      // Read port offset from the env file that was just generated (ensures ecosystem,
      // Traefik, and env file all use the same offset). Fall back to computing if not found.
      const port_offset = read_stored_offset(env_file_local) || find_free_offset(compute_auto_offset(worktree_path));
      const mode = options.mode || config.services.defaultMode || 'full';
      const active_services = config_mod.resolve_services(config, mode);

      // Build env overrides from passthrough keys + env file
      const passthrough = config.services.pm2.envPassthrough || [];
      const env_overrides = { SKULABS_ENV: 'development', NODE_ENV: 'development', ...load_aws_env() };
      if (fs.existsSync(env_file_local)) {
        const content = fs.readFileSync(env_file_local, 'utf8');
        for (const line of content.split('\n')) {
          const trimmed = line.trim();
          if (!trimmed || trimmed.startsWith('#')) continue;
          const idx = trimmed.indexOf('=');
          if (idx === -1) continue;
          const key = trimmed.slice(0, idx).trim();
          if (passthrough.includes(key) || key.startsWith('WORKTREE_') || key === 'PM2_HOME') {
            env_overrides[key] = trimmed.slice(idx + 1).trim();
          }
        }
      }

      const ecosystem_content = generate_config(
        config, worktree_path, target_branch.replace(/\//g, '-'),
        port_offset, active_services, env_overrides,
      );
      const ecosystem_path = path.join(worktree_path, OUTPUT_FILENAME);
      fs.writeFileSync(ecosystem_path, ecosystem_content, 'utf8');
      console.log(`Generated ${OUTPUT_FILENAME}`);

      // Generate VS Code workspace file
      generate_workspace(worktree_path, config, port_offset, active_services, env_overrides);
      console.log('Generated devbox.code-workspace');

      // Print port summary
      const ports = config_mod.compute_ports(config, port_offset);
      const domain = config_mod.domain_for(config, alias);
      console.log('');
      console.log(`Service mode: ${mode}`);
      console.log('Service Ports:');
      for (const svc of active_services) {
        if (ports[svc]) console.log(`  ${svc.padEnd(22)} ${ports[svc]}`);
      }

      // Write domain to env file so worktree processes know their URL
      if (domain) {
        const domain_var = config ? (config_mod.worktree_var(config, 'domain') || 'WORKTREE_DOMAIN') : 'WORKTREE_DOMAIN';
        const env_file_local = path.join(worktree_path, config ? config.env.filename : '.env.worktree');
        update_env_key(env_file_local, domain_var, domain);
      }

      if (!options.no_traefik) {
        const traefik_written = write_traefik_config(alias, domain, port_offset);
        if (traefik_written && domain) {
          is_traefik_routing().then((routing) => {
            if (routing) {
              console.log(`Domain: http://${domain}/`);
            } else {
              console.log(`Domain: http://${domain}/ (will activate when Traefik starts)`);
            }
          });
        }
      }
    }

    if (options.open) open_in_cursor(worktree_path);

    // When running inside the dashboard (WT_INNER=1), skip the dev server —
    // the dashboard will open a Dev tab after creation completes.
    if (process.env.WT_INNER === '1') {
      console.log('Worktree ready (dashboard will start dev server).');
      return;
    }

    // Start services
    if (use_local_dev) {
      const home = pm2_home(worktree_path);
      const pm2_bin = find_pm2(repo_root);
      const ecosystem_path = path.join(worktree_path, OUTPUT_FILENAME);
      console.log('Starting PM2 services...');
      pm2_start({
        pm2: pm2_bin,
        pm2_home: home,
        ecosystem_config: ecosystem_path,
        env: load_aws_env(),
        cwd: worktree_path,
      });

      // Start frontend build watcher if buildScript is configured
      const build_script = config && config.paths && config.paths.buildScript;
      if (build_script) {
        const build_script_path = path.join(worktree_path, build_script);
        if (fs.existsSync(build_script_path)) {
          const wt_suffix = target_branch.replace(/\//g, '-');
          const build_name = pm2_process_name('build', wt_suffix);
          const prefix = `PM2_HOME="${home}" `;
          const start_cmd = `${prefix}${pm2_bin} start "node ${build_script} develop --watch" --name "${build_name}" --cwd "${worktree_path}" --no-autorestart`;
          try {
            execSync(start_cmd, { stdio: 'inherit', cwd: worktree_path, env: { ...process.env, ...load_aws_env(), PM2_HOME: home } });
          } catch {
            console.warn('Warning: Frontend build watcher failed to start.');
          }
        }
      }

      console.log('PM2 services started.');
    } else {
      // Fallback: start dev server directly (standalone CLI usage)
      const path_var = config ? config_mod.env_var(config, 'projectPath') : 'PROJECT_PATH';
      const dev_cmd = config && config.dash && config.dash.localDevCommand ? config.dash.localDevCommand : 'pnpm dev';
      console.log(`Starting dev server (${dev_cmd})...`);
      execSync(dev_cmd, { cwd: worktree_path, stdio: 'inherit', env: { ...process.env, [path_var]: worktree_path } });
    }
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

  // Clean up leftover directory from a failed previous create (e.g., stale .pm2/)
  if (fs.existsSync(worktree_path)) {
    try {
      execSync(`git -C "${worktree_path}" rev-parse --git-dir`, { stdio: 'pipe' });
    } catch {
      console.log(`Removing stale directory: ${worktree_path}`);
      fs.rmSync(worktree_path, { recursive: true, force: true });
    }
  }

  // Fetch the base ref so we branch from the latest remote state
  if (options.from) {
    const remote_match = options.from.match(/^(\w+)\/(.+)$/);
    if (remote_match) {
      const [, remote, branch] = remote_match;
      try {
        console.log(`Fetching ${remote}/${branch}...`);
        execSync(`git -C "${repo_root}" fetch ${remote} ${branch}`, { stdio: 'pipe' });
      } catch {
        // fetch may fail (offline, etc.) — continue with local ref
      }
    }
  }

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
    // Detect if --from points to the same branch on the remote (e.g. --from=origin/feat-xyz
    // with target_branch=feat-xyz). In this case, check out the remote-tracking branch
    // instead of creating a new one.
    const is_remote_checkout = options.from === `origin/${target_branch}`
      || options.from === `refs/remotes/origin/${target_branch}`;

    if (is_remote_checkout) {
      if (branch_exists_locally) {
        // Local branch already exists — reset it to match the remote
        console.log(`Resetting local "${target_branch}" to match ${options.from}...`);
        try {
          execSync(`git -C "${repo_root}" branch -D "${target_branch}"`, { stdio: 'pipe' });
        } catch {
          // branch -D fails if checked out elsewhere — try force update
          console.log(`Could not delete branch, using existing "${target_branch}"...`);
          execSync(
            `git -C "${repo_root}" worktree add "${worktree_path}" "${target_branch}"`,
            { stdio: 'inherit' },
          );
          return;
        }
      }
      // git worktree add <path> <branch> auto-creates a tracking branch from origin/<branch>
      console.log(`Checking out remote branch "${target_branch}" from ${options.from}...`);
      execSync(
        `git -C "${repo_root}" worktree add "${worktree_path}" "${target_branch}"`,
        { stdio: 'inherit' },
      );
    } else if (branch_exists_locally) {
      // Delete stale local branch and recreate from the specified base ref
      console.log(`Branch "${target_branch}" exists locally, recreating from ${options.from}...`);
      try {
        execSync(`git -C "${repo_root}" branch -D "${target_branch}"`, { stdio: 'pipe' });
        execSync(
          `git -C "${repo_root}" worktree add -b "${target_branch}" "${worktree_path}" "${options.from}"`,
          { stdio: 'inherit' },
        );
      } catch {
        // branch -D fails if checked out elsewhere — fall back to using existing branch
        console.log(`Could not reset branch, using existing "${target_branch}"...`);
        execSync(
          `git -C "${repo_root}" worktree add "${worktree_path}" "${target_branch}"`,
          { stdio: 'inherit' },
        );
      }
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
  ensure_setup_symlinks(repo_root, worktree_path);

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
  ensure_setup_symlinks(repo_root, worktree_path);
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

  const ports = compute_ports(restart_offset);
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
  ensure_setup_symlinks(repo_root, worktree_path);

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

  const ports = compute_ports(port_offset);
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
