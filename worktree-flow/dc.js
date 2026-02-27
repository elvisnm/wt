const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

process.on('SIGINT', () => process.exit(0));

const ALL_SERVICE_NAMES = config
  ? Object.keys(config.services.ports)
  : [
    'app', 'api', 'socket_server', 'serviceHostServer',
    'combined_sync', 'listings_sync', 'admin_server', 'ship_server',
    'job_server', 'insights_server', 'cache_server',
    'order_table_server', 'inventory_table_server',
  ];

function run(command) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8' }).trim();
}

function find_docker_worktrees(base_dir) {
  const results = [];
  if (!fs.existsSync(base_dir)) return results;
  const env_filename = config ? config.env.filename : '.env.worktree';
  for (const entry of fs.readdirSync(base_dir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const full = path.join(base_dir, entry.name);
    if (fs.existsSync(path.join(full, 'docker-compose.worktree.yml')) ||
        fs.existsSync(path.join(full, env_filename))) {
      results.push(full);
    }
  }
  return results;
}

function read_env(wt_path, key) {
  const env_filename = config ? config.env.filename : '.env.worktree';
  const env = path.join(wt_path, env_filename);
  if (!fs.existsSync(env)) return null;
  const content = fs.readFileSync(env, 'utf8');
  const m = content.match(new RegExp(`^${key}=(.+)$`, 'm'));
  return m ? m[1].trim() : null;
}

function get_container_status(name) {
  try { return run(`docker inspect --format={{.State.Status}} "${name}"`); }
  catch { return null; }
}

function get_shared_compose_status(shared_info) {
  try {
    const output = run(
      `docker compose -f "${shared_info.compose_file}" -p "${shared_info.project}" ps --format json`,
      { env: { ...process.env, ...shared_info.env } }
    );
    const lines = (output || '').split('\n').filter(Boolean);
    for (const line of lines) {
      try {
        const data = JSON.parse(line);
        if ((data.State || '').toLowerCase() === 'running') return 'running';
      } catch { continue; }
    }
    return lines.length > 0 ? 'exited' : null;
  } catch {
    return null;
  }
}

function read_container_name(wt_path) {
  const compose = path.join(wt_path, 'docker-compose.worktree.yml');
  try {
    const content = fs.readFileSync(compose, 'utf8');
    const m = content.match(/container_name:\s*(\S+)/);
    return m ? m[1] : null;
  } catch { return null; }
}

function get_worktree_list(worktrees_dir) {
  return find_docker_worktrees(worktrees_dir).map((wt) => {
    const alias = read_env(wt, 'WORKTREE_ALIAS');
    const name = path.basename(wt);
    const shared = config ? config_mod.get_compose_info(config, wt) : null;

    let container, status;
    if (shared) {
      // Shared compose: check project containers
      const primary = config.services.primary || Object.keys(config.services.ports)[0];
      container = `${shared.project}-${primary}`;
      status = get_shared_compose_status(shared);
    } else {
      container = alias
        ? (config ? config_mod.container_name(config, alias) : alias)
        : read_container_name(wt);
      status = container ? get_container_status(container) : null;
    }

    const host_build = read_env(wt, 'WORKTREE_HOST_BUILD') === 'true';
    const lan_domain_key = config ? config_mod.env_var(config, 'lanDomain') : 'LAN_DOMAIN';
    const lan_domain = lan_domain_key ? read_env(wt, lan_domain_key) : null;
    return { path: wt, name, alias, container, status, host_build, lan_domain };
  });
}

function preflight_checks() {
  try { execSync('docker info', { stdio: 'pipe' }); }
  catch {
    console.error('Docker is not running. Start Docker Desktop and try again.');
    process.exit(1);
  }
  const repo_root = run('git rev-parse --show-toplevel');
  try {
    const common = run(`git -C "${repo_root}" rev-parse --git-common-dir`);
    const gitdir = run(`git -C "${repo_root}" rev-parse --git-dir`);
    if (path.resolve(repo_root, common) !== path.resolve(repo_root, gitdir)) {
      console.error('Run this from the main repo, not a worktree.');
      process.exit(1);
    }
  } catch {}
  return repo_root;
}

function exec_script(repo_root, script, args = '', { ignore_exit } = {}) {
  const scripts_dir = config && config.paths._flowScriptsResolved
    ? config.paths._flowScriptsResolved
    : path.join(repo_root, 'scripts/worktree');
  const cmd = `node "${path.join(scripts_dir, script)}" ${args}`;
  try {
    execSync(cmd, { stdio: 'inherit', cwd: repo_root });
  } catch (err) {
    if (err.signal === 'SIGINT' || err.status === 130) process.exit(0);
    if (ignore_exit) return;
    throw err;
  }
}

// --- Worktree picker (reused across flows) ---

async function pick_worktree(p, worktrees, { message = 'Select a worktree:', filter } = {}) {
  const list = filter ? worktrees.filter(filter) : worktrees;
  if (list.length === 0) {
    p.log.warn('No worktrees match.');
    return null;
  }
  const choice = await p.select({
    message,
    options: list.map((w) => {
      const icon = w.status === 'running' ? '\x1b[32m●\x1b[0m' : '\x1b[90m○\x1b[0m';
      const parts = [w.status || 'no container'];
      if (w.lan_domain) parts.push('LAN');
      if (w.host_build) parts.push('host-build');
      return { value: w, label: `${icon} ${w.name}`, hint: `${w.alias} · ${parts.join(' · ')}` };
    }),
  });
  if (p.isCancel(choice)) { p.cancel('Cancelled.'); process.exit(0); }
  return choice;
}

// --- Flow: Create ---

async function flow_create(p, ctx) {
  exec_script(ctx.repo_root, 'dc-create.js');
}

// --- Flow: Manage ---

async function flow_manage(p, ctx) {
  if (ctx.worktrees.length === 0) {
    p.log.warn('No worktrees found. Create one first.');
    return;
  }

  const wt = await pick_worktree(p, ctx.worktrees, { message: 'Which worktree?' });
  if (!wt) return;

  const is_running = wt.status === 'running';
  const options = [];
  options.push({ value: 'info', label: 'Show info', hint: 'ports, URLs, status' });
  if (is_running) {
    options.push({ value: 'logs', label: 'View logs' });
    options.push({ value: 'restart', label: 'Restart container' });
    options.push({ value: 'stop', label: 'Stop container' });
    options.push({ value: 'bash', label: 'Open bash shell' });
    options.push({ value: 'services', label: 'Manage PM2 services' });
  } else {
    options.push({ value: 'logs', label: 'View logs', hint: 'from last run' });
    options.push({ value: 'start', label: 'Start container' });
  }
  options.push({ value: 'remove', label: 'Remove worktree', hint: 'destructive' });

  const action = await p.select({ message: `${wt.name}:`, options });
  if (p.isCancel(action)) { p.cancel('Cancelled.'); process.exit(0); }

  switch (action) {
    case 'info':
      exec_script(ctx.repo_root, 'dc-info.js', `"${wt.name}"`);
      return;
    case 'logs': return sub_logs(p, ctx, wt);
    case 'restart':
      exec_script(ctx.repo_root, 'dc-restart.js', `"${wt.name}"`);
      return;
    case 'stop':
      exec_script(ctx.repo_root, 'dc-worktree-down.js', `"${wt.name}"`);
      p.log.success(`Stopped ${wt.name}`);
      return;
    case 'start':
      exec_script(ctx.repo_root, 'dc-worktree-up.js', `"${wt.name}"`);
      return;
    case 'bash':
      exec_script(ctx.repo_root, 'dc-bash.js', `"${wt.name}"`);
      return;
    case 'services': return sub_services(p, ctx, wt);
    case 'remove': return sub_remove(p, ctx, wt);
  }
}

async function sub_logs(p, ctx, wt) {
  const service_options = [{ value: '__all', label: 'All services' }];

  if (wt.status === 'running') {
    try {
      const raw = run(`docker exec "${wt.container}" pm2 jlist`);
      const list = JSON.parse(raw);
      for (const s of list) {
        const base = ALL_SERVICE_NAMES.find((n) => s.name === n || s.name.startsWith(n + '-')) || s.name;
        const icon = s.pm2_env?.status === 'online' ? '●' : '○';
        service_options.push({ value: s.name, label: `${icon} ${base}`, hint: s.pm2_env?.status });
      }
    } catch {}
  }

  if (service_options.length === 1) {
    for (const name of ALL_SERVICE_NAMES) {
      service_options.push({ value: name, label: name });
    }
  }

  const service = await p.select({ message: 'Which service?', options: service_options });
  if (p.isCancel(service)) { p.cancel('Cancelled.'); process.exit(0); }

  const follow = await p.confirm({ message: 'Follow (live)?', initialValue: true });
  if (p.isCancel(follow)) { p.cancel('Cancelled.'); process.exit(0); }

  let args = `"${wt.name}"`;
  if (service !== '__all') args += ` -s ${service}`;
  if (follow) args += ' -f';

  exec_script(ctx.repo_root, 'dc-logs.js', args, { ignore_exit: follow });
}

async function sub_services(p, ctx, wt) {
  let pm2_services = [];
  try {
    const raw = run(`docker exec "${wt.container}" pm2 jlist`);
    pm2_services = JSON.parse(raw).map((s) => {
      const base = ALL_SERVICE_NAMES.find((n) => s.name === n || s.name.startsWith(n + '-'));
      return {
        name: s.name,
        base_name: base || s.name,
        status: s.pm2_env?.status || 'unknown',
        memory: s.monit?.memory || 0,
      };
    });
  } catch {
    p.log.error('Could not query PM2. Is the container running?');
    return;
  }

  if (pm2_services.length > 0) {
    const lines = pm2_services.map((s) => {
      const icon = s.status === 'online' ? '●' : '○';
      const mem = (s.memory / 1024 / 1024).toFixed(0);
      return `${icon} ${s.base_name.padEnd(26)} ${s.status.padEnd(10)} ${mem} MB`;
    });
    p.note(lines.join('\n'), 'PM2 Services');
  }

  const action = await p.select({
    message: 'Action?',
    options: [
      { value: 'restart_all', label: 'Restart all services' },
      { value: 'restart', label: 'Restart a service' },
      { value: 'stop', label: 'Stop a service' },
      { value: 'start', label: 'Start a service' },
    ],
  });
  if (p.isCancel(action)) { p.cancel('Cancelled.'); process.exit(0); }

  if (action === 'restart_all') {
    execSync(`docker exec "${wt.container}" pm2 restart all`, { stdio: 'inherit' });
    p.log.success('Restarted all services');
    return;
  }

  let service_choices;
  if (action === 'start') {
    const online_bases = new Set(pm2_services.filter((s) => s.status === 'online').map((s) => s.base_name));
    const startable = ALL_SERVICE_NAMES.filter((n) => !online_bases.has(n));
    if (startable.length === 0) {
      p.log.info('All known services are already running.');
      return;
    }
    service_choices = startable.map((n) => ({ value: n, label: n }));
  } else {
    const online = pm2_services.filter((s) => s.status === 'online');
    if (online.length === 0) {
      p.log.warn('No services are running.');
      return;
    }
    service_choices = online.map((s) => ({ value: s.name, label: s.base_name }));
  }

  const target = await p.select({ message: `Service to ${action}:`, options: service_choices });
  if (p.isCancel(target)) { p.cancel('Cancelled.'); process.exit(0); }

  if (action === 'start') {
    exec_script(ctx.repo_root, 'dc-service.js', `"${wt.name}" start ${target}`);
  } else {
    execSync(`docker exec "${wt.container}" pm2 ${action} "${target}"`, { stdio: 'inherit' });
  }
  const display = ALL_SERVICE_NAMES.find((n) => target === n || target.startsWith(n + '-')) || target;
  p.log.success(`${action === 'start' ? 'Started' : action === 'stop' ? 'Stopped' : 'Restarted'} ${display}`);
}

async function sub_remove(p, ctx, wt) {
  let has_changes = false;
  try {
    const status = run(`git -C "${wt.path}" status --porcelain`);
    has_changes = status.length > 0;
  } catch {}

  if (has_changes) {
    p.log.warn('This worktree has uncommitted changes!');
  }

  const delete_branch = await p.confirm({
    message: 'Also delete the local git branch?',
    initialValue: false,
  });
  if (p.isCancel(delete_branch)) { p.cancel('Cancelled.'); process.exit(0); }

  const summary = [
    `Worktree:   ${wt.name}`,
    `Directory:  ${wt.path}`,
    `Container:  ${wt.container || 'none'} + volumes will be removed`,
    `Branch:     ${delete_branch ? 'will be deleted' : 'kept'}`,
    has_changes ? '\nUncommitted changes will be lost!' : null,
  ].filter(Boolean);
  p.note(summary.join('\n'), 'Will remove');

  const confirmed = await p.confirm({ message: 'Proceed?' });
  if (p.isCancel(confirmed) || !confirmed) { p.cancel('Cancelled.'); process.exit(0); }

  let flags = '--remove';
  if (delete_branch) flags += ' --delete-branch';
  if (has_changes) flags += ' --force';

  exec_script(ctx.repo_root, 'dc-worktree-down.js', `"${wt.name}" ${flags}`);
  p.log.success(`Removed ${wt.name}`);
}

// --- Flow: Database ---

async function flow_database(p, ctx) {
  const action = await p.select({
    message: 'Database operation?',
    options: [
      { value: 'seed', label: 'Seed', hint: 'copy shared db into worktree db' },
      { value: 'reset', label: 'Reset', hint: 'drop + re-seed (fresh copy)' },
      { value: 'drop', label: 'Drop', hint: 'delete the worktree database' },
      { value: 'fix_images', label: 'Fix image URLs', hint: 'absolute fakes3 URLs to relative' },
    ],
  });
  if (p.isCancel(action)) { p.cancel('Cancelled.'); process.exit(0); }

  if (action === 'fix_images') {
    const shared_db = config ? config.database.defaultDb : 'db';
    const db_prefix = config ? (config.database.dbNamePrefix || '') : 'db_';
    const db_options = [{ value: shared_db, label: `Shared (${shared_db})` }];
    for (const w of ctx.worktrees.filter((w) => w.alias)) {
      const db = config
        ? config_mod.db_name(config, w.alias)
        : `db_${w.alias.replace(/[^a-zA-Z0-9_]/g, '_')}`;
      db_options.push({ value: db, label: w.name, hint: db });
    }

    const target = await p.select({ message: 'Which database?', options: db_options });
    if (p.isCancel(target)) { p.cancel('Cancelled.'); process.exit(0); }

    const dry = await p.confirm({ message: 'Dry run first?', initialValue: true });
    if (p.isCancel(dry)) { p.cancel('Cancelled.'); process.exit(0); }

    exec_script(ctx.repo_root, 'dc-images-fix.js', `--db=${target}${dry ? ' --dry-run' : ''}`);

    if (dry) {
      const proceed = await p.confirm({ message: 'Run for real?' });
      if (p.isCancel(proceed) || !proceed) { p.cancel('Cancelled.'); process.exit(0); }
      exec_script(ctx.repo_root, 'dc-images-fix.js', `--db=${target}`);
    }
    return;
  }

  const wt = await pick_worktree(p, ctx.worktrees, {
    message: 'Which worktree?',
    filter: (w) => w.alias,
  });
  if (!wt) return;

  const db_name = config
    ? config_mod.db_name(config, wt.alias)
    : `db_${wt.alias.replace(/[^a-zA-Z0-9_]/g, '_')}`;

  if (action === 'drop' || action === 'reset') {
    const verb = action === 'drop' ? 'permanently delete' : 'drop and re-seed';
    const confirmed = await p.confirm({ message: `This will ${verb} "${db_name}". Continue?` });
    if (p.isCancel(confirmed) || !confirmed) { p.cancel('Cancelled.'); process.exit(0); }
  }

  const flag = action === 'seed' ? '' : `--${action}`;
  exec_script(ctx.repo_root, 'dc-seed.js', `${flag} "${wt.name}"`.trim());
  p.log.success(`Database ${action} complete for ${wt.name}`);
}

// --- Flow: Admin & Config ---

async function flow_admin(p, ctx) {
  const action = await p.select({
    message: 'What to configure?',
    options: [
      { value: 'admin_set', label: 'Enable admin access' },
      { value: 'admin_unset', label: 'Disable admin access' },
      { value: 'lan_on', label: 'Enable LAN access', hint: 'nip.io domain' },
      { value: 'lan_off', label: 'Disable LAN access', hint: 'revert to .localhost' },
    ],
  });
  if (p.isCancel(action)) { p.cancel('Cancelled.'); process.exit(0); }

  const running = ctx.worktrees.filter((w) => w.status === 'running');

  if (action === 'admin_set' || action === 'admin_unset') {
    if (running.length === 0) {
      p.log.warn('No running worktrees.');
      return;
    }

    const scope = await p.select({
      message: 'Apply to?',
      options: [
        { value: 'all', label: 'All running worktrees', hint: `${running.length} container(s)` },
        { value: 'pick', label: 'Select a specific worktree' },
      ],
    });
    if (p.isCancel(scope)) { p.cancel('Cancelled.'); process.exit(0); }

    let name_flag = '';
    if (scope === 'pick') {
      const wt = await pick_worktree(p, running, { message: 'Which worktree?' });
      if (!wt) return;
      name_flag = `--name="${wt.name}"`;
    }

    const verb = action === 'admin_set' ? 'set' : 'unset';
    exec_script(ctx.repo_root, 'dc-admin.js', `${verb} ${name_flag}`.trim());
    return;
  }

  // LAN toggle
  if (running.length === 0) {
    p.log.warn('No running worktrees.');
    return;
  }

  const hint_key = action === 'lan_on' ? 'no LAN' : 'has LAN';
  const filter_fn = action === 'lan_on'
    ? (w) => w.status === 'running' && !w.lan_domain
    : (w) => w.status === 'running' && w.lan_domain;

  const candidates = running.filter(filter_fn);
  if (candidates.length === 0) {
    const msg = action === 'lan_on'
      ? 'All running worktrees already have LAN enabled.'
      : 'No running worktrees have LAN enabled.';
    p.log.info(msg);
    return;
  }

  const wt = await pick_worktree(p, candidates, { message: 'Which worktree?' });
  if (!wt) return;

  const off_flag = action === 'lan_off' ? ' --off' : '';
  exec_script(ctx.repo_root, 'dc-lan.js', `"${wt.name}"${off_flag}`);
}

// --- Flow: Maintenance ---

async function flow_maintenance(p, ctx) {
  const action = await p.select({
    message: 'Maintenance task?',
    options: [
      { value: 'prune', label: 'Prune orphaned volumes', hint: 'clean up deleted worktrees' },
      { value: 'autostop', label: 'Stop idle containers', hint: 'CPU < 1% for N hours' },
      { value: 'rebuild', label: 'Rebuild base image', hint: config ? config.docker.baseImage : 'dev:prebaked' },
    ],
  });
  if (p.isCancel(action)) { p.cancel('Cancelled.'); process.exit(0); }

  if (action === 'prune') {
    p.log.info('Scanning for orphaned volumes (dry run)...');
    exec_script(ctx.repo_root, 'dc-prune.js', '--dry-run');

    const confirmed = await p.confirm({ message: 'Remove these volumes?' });
    if (p.isCancel(confirmed) || !confirmed) { p.cancel('Cancelled.'); process.exit(0); }

    exec_script(ctx.repo_root, 'dc-prune.js');
    return;
  }

  if (action === 'autostop') {
    const hours = await p.text({
      message: 'Idle threshold (hours):',
      defaultValue: '2',
      placeholder: '2',
      validate: (v) => {
        const n = Number(v);
        return Number.isFinite(n) && n > 0 ? undefined : 'Must be a positive number';
      },
    });
    if (p.isCancel(hours)) { p.cancel('Cancelled.'); process.exit(0); }

    p.log.info('Checking for idle containers (dry run)...');
    exec_script(ctx.repo_root, 'dc-autostop.js', `--hours=${hours} --dry-run`);

    const confirmed = await p.confirm({ message: 'Stop these containers?' });
    if (p.isCancel(confirmed) || !confirmed) { p.cancel('Cancelled.'); process.exit(0); }

    exec_script(ctx.repo_root, 'dc-autostop.js', `--hours=${hours}`);
    return;
  }

  if (action === 'rebuild') {
    const confirmed = await p.confirm({
      message: 'Rebuild base image? (2-3 min, shared by all worktrees)',
    });
    if (p.isCancel(confirmed) || !confirmed) { p.cancel('Cancelled.'); process.exit(0); }

    exec_script(ctx.repo_root, 'dc-rebuild-base.js');
  }
}

// --- Main ---

async function main() {
  const p = await import('@clack/prompts');
  const repo_root = preflight_checks();
  const worktrees_dir = config
    ? config.repo._worktreesDirResolved
    : path.join(path.dirname(repo_root), `${path.basename(repo_root)}-worktrees`);
  const worktrees = get_worktree_list(worktrees_dir);
  const ctx = { repo_root, worktrees_dir, worktrees };

  p.intro('Docker Worktrees');

  if (worktrees.length > 0) {
    const lines = worktrees.map((w) => {
      const icon = w.status === 'running' ? '\x1b[32m●\x1b[0m' : '\x1b[90m○\x1b[0m';
      const domain = w.alias
        ? (config ? config_mod.domain_for(config, w.alias) : `${w.alias}.localhost`)
        : '';
      const extras = [];
      if (w.lan_domain) extras.push('LAN');
      if (w.host_build) extras.push('host-build');
      const suffix = extras.length ? ` [${extras.join(', ')}]` : '';
      return `${icon} ${w.name.padEnd(30)} ${(w.status || '').padEnd(10)} ${domain}${suffix}`;
    });
    p.note(lines.join('\n'), `${worktrees.length} worktree(s)`);
  }

  const has_worktrees = worktrees.length > 0;
  const menu_options = [
    { value: 'create', label: 'Create a new worktree' },
  ];
  if (has_worktrees) {
    menu_options.push({ value: 'manage', label: 'Manage a worktree', hint: 'info, logs, restart, stop, bash, services' });
    menu_options.push({ value: 'database', label: 'Database', hint: 'seed, drop, reset, fix images' });
    menu_options.push({ value: 'admin', label: 'Admin & config', hint: 'admin access, LAN toggle' });
  }
  menu_options.push({ value: 'maintenance', label: 'Maintenance', hint: 'prune volumes, stop idle, rebuild image' });

  const action = await p.select({ message: 'What do you want to do?', options: menu_options });
  if (p.isCancel(action)) { p.cancel('Cancelled.'); process.exit(0); }

  switch (action) {
    case 'create': return flow_create(p, ctx);
    case 'manage': return flow_manage(p, ctx);
    case 'database': return flow_database(p, ctx);
    case 'admin': return flow_admin(p, ctx);
    case 'maintenance': return flow_maintenance(p, ctx);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
