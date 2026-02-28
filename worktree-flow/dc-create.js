const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');

const os = require('os');
const config = config_mod.load_config({ required: false }) || null;

const SENTINEL_PATH = path.join(os.tmpdir(), 'wt-create-done');

process.on('SIGINT', () => process.exit(0));

function run(command) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8' }).trim();
}

// ── Scripts directory resolution ────────────────────────────────────────

function resolve_scripts_dir() {
  // 1. Config-defined path
  if (config && config.paths._flowScriptsResolved) {
    return config.paths._flowScriptsResolved;
  }
  // 2. Same directory as this script (wt install)
  return __dirname;
}

const scripts_dir = resolve_scripts_dir();

// ── Worktree discovery ──────────────────────────────────────────────────

function find_existing_worktrees(base_dir) {
  const results = [];
  if (!fs.existsSync(base_dir)) return results;
  const env_filename = config ? config.env.filename : '.env.worktree';
  for (const entry of fs.readdirSync(base_dir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const full_path = path.join(base_dir, entry.name);
    // Check for env file (shared compose) or docker-compose.worktree.yml (generate)
    if (fs.existsSync(path.join(full_path, env_filename)) ||
        fs.existsSync(path.join(full_path, 'docker-compose.worktree.yml'))) {
      results.push(full_path);
    }
  }
  return results;
}

function read_env(worktree_path, key) {
  const env_filename = config ? config.env.filename : '.env.worktree';
  const env_path = path.join(worktree_path, env_filename);
  if (!fs.existsSync(env_path)) return null;
  const content = fs.readFileSync(env_path, 'utf8');
  const match = content.match(new RegExp(`^${key}=(.+)$`, 'm'));
  return match ? match[1].trim() : null;
}

function auto_alias(branch_name) {
  const prefixes = config ? config.repo.branchPrefixes : ['feat', 'fix', 'ops', 'hotfix', 'release', 'chore'];
  const prefix_pattern = new RegExp(`^(${prefixes.join('|')})\\/`, 'i');
  const stripped = branch_name.replace(prefix_pattern, '');
  const clean = stripped.replace(/\//g, '-').replace(/[^a-zA-Z0-9-]/g, '-').toLowerCase();
  const parts = clean.split('-').filter(Boolean);
  return parts.slice(0, 2).join('-') || clean.slice(0, 20);
}

function get_container_status(container_name) {
  try {
    return run(`docker inspect --format={{.State.Status}} "${container_name}"`);
  } catch {
    return 'not found';
  }
}

// ── Validation ──────────────────────────────────────────────────────────

function validate_alias(value, existing_aliases) {
  if (!value) return 'Alias is required';
  if (!/^[a-z0-9][a-z0-9-]*$/.test(value)) return 'Must be lowercase alphanumeric with hyphens';
  if (value.length > 30) return 'Must be 30 characters or less';
  if (existing_aliases.includes(value)) return `Alias "${value}" is already in use by another worktree`;
  return undefined;
}

function validate_branch(value) {
  if (!value) return 'Branch name is required';
  if (/[^a-zA-Z0-9/_-]/.test(value)) return 'Branch name contains invalid characters';
  return undefined;
}

// ── Base ref detection ──────────────────────────────────────────────────

function detect_base_refs() {
  const candidates = ['main', 'master'];
  const found = [];
  for (const name of candidates) {
    try {
      run(`git rev-parse --verify origin/${name}`);
      found.push(`origin/${name}`);
    } catch {
      // branch doesn't exist on remote
    }
  }
  return found.length > 0 ? found : ['origin/main'];
}

function get_base_ref_options() {
  let refs;
  if (config && config.repo.baseRefs && config.repo.baseRefs.length > 0) {
    refs = config.repo.baseRefs;
  } else {
    refs = detect_base_refs();
  }

  const options = refs.map((ref, i) => ({
    value: ref,
    label: ref,
    hint: i === 0 ? 'default' : undefined,
  }));
  options.push({ value: '__custom', label: 'Custom ref' });
  return options;
}

// ── Pre-flight checks ───────────────────────────────────────────────────

function check_repo() {
  const repo_root = run('git rev-parse --show-toplevel');
  try {
    const common_dir = run(`git -C "${repo_root}" rev-parse --git-common-dir`);
    const git_dir = run(`git -C "${repo_root}" rev-parse --git-dir`);
    if (path.resolve(repo_root, common_dir) !== path.resolve(repo_root, git_dir)) {
      console.error('This command must be run from the main repo, not a worktree.');
      process.exit(1);
    }
  } catch {
    // not a worktree, fine
  }
  return repo_root;
}

function check_docker() {
  try {
    execSync('docker info', { stdio: 'pipe' });
  } catch {
    console.error('Docker is not running. Start Docker Desktop and try again.');
    process.exit(1);
  }

  const network_name = config ? config.docker.sharedInfra.network : null;
  if (network_name) {
    try {
      run(`docker network inspect ${network_name}`);
    } catch {
      console.error(`Docker network "${network_name}" not found.`);
      console.error('Start the shared infrastructure stack first.');
      process.exit(1);
    }
  }
}

// ── Additional options (config-driven) ──────────────────────────────────

function get_extra_options() {
  if (!config) return [];
  const options = [];
  // host_build is now a top-level environment option, not an extra
  if (config.features.lan) {
    options.push({ value: 'lan', label: 'LAN access', hint: 'nip.io domain' });
  }
  if (config.features.devHeap) {
    options.push({ value: 'poll', label: 'PM2 polling', hint: 'for slow file watchers' });
  }
  return options;
}

// ── Main flow ───────────────────────────────────────────────────────────

async function main() {
  const p = await import('@clack/prompts');

  // Remove stale sentinel
  try { fs.unlinkSync(SENTINEL_PATH); } catch {}

  const repo_root = check_repo();
  const worktrees_dir = config && config.repo._worktreesDirResolved
    ? config.repo._worktreesDirResolved
    : path.join(path.dirname(repo_root), `${path.basename(repo_root)}-worktrees`);

  p.intro('Create a worktree');

  const existing = find_existing_worktrees(worktrees_dir);
  const alias_var = config ? (config.env.worktreeVars.alias || 'WORKTREE_ALIAS') : 'WORKTREE_ALIAS';
  const existing_aliases = existing.map((wt) => read_env(wt, alias_var)).filter(Boolean);
  const existing_info = existing.map((wt) => {
    const alias = read_env(wt, alias_var);
    const name = path.basename(wt);
    const container = alias && config ? config_mod.container_name(config, alias) : null;
    const status = container ? get_container_status(container) : 'unknown';
    return { path: wt, name, alias, container, status };
  });

  // Step 1: What do you want to do?
  const stopped = existing_info.filter((w) => w.status === 'exited' || w.status === 'created');
  const action_options = [
    { value: 'new', label: 'Create a new branch', hint: 'fork from a base ref' },
    { value: 'existing', label: 'Check out a remote branch', hint: 'existing branch on origin' },
  ];
  if (stopped.length > 0) {
    action_options.push({
      value: 'restart',
      label: 'Restart a stopped worktree',
      hint: `${stopped.length} stopped`,
    });
  }

  const action = await p.select({
    message: 'What do you want to do?',
    options: action_options,
  });
  if (p.isCancel(action)) { p.cancel('Cancelled.'); process.exit(0); }

  // Restart path
  if (action === 'restart') {
    check_docker();
    const restart_target = await p.select({
      message: 'Which worktree?',
      options: stopped.map((w) => ({
        value: w.name,
        label: w.name,
        hint: w.alias ? `alias: ${w.alias}` : undefined,
      })),
    });
    if (p.isCancel(restart_target)) { p.cancel('Cancelled.'); process.exit(0); }

    const up_script = path.join(scripts_dir, 'dc-worktree-up.js');
    p.log.info(`Restarting ${restart_target}...`);
    execSync(`node "${up_script}" "${restart_target}"`, {
      stdio: 'inherit',
      cwd: repo_root,
    });
    p.outro('Done!');
    return;
  }

  // New branch path
  let branch_name;
  let from_ref;

  if (action === 'new') {
    const prefixes = config ? config.repo.branchPrefixes : ['feat', 'fix', 'ops', 'hotfix', 'release', 'chore'];
    branch_name = await p.text({
      message: `Branch name? (suggested: ${prefixes.map((p) => p + '/').join(', ')})`,
      placeholder: 'feat/my-feature',
      validate: validate_branch,
    });
    if (p.isCancel(branch_name)) { p.cancel('Cancelled.'); process.exit(0); }

    const ref_options = get_base_ref_options();
    const base_ref = await p.select({
      message: 'Base ref?',
      options: ref_options,
    });
    if (p.isCancel(base_ref)) { p.cancel('Cancelled.'); process.exit(0); }

    if (base_ref === '__custom') {
      from_ref = await p.text({
        message: 'Enter the base ref:',
        placeholder: 'origin/some-branch',
        validate: (v) => (v ? undefined : 'Ref is required'),
      });
      if (p.isCancel(from_ref)) { p.cancel('Cancelled.'); process.exit(0); }
    } else {
      from_ref = base_ref;
    }
  }

  // Existing remote branch path
  if (action === 'existing') {
    const spin = p.spinner();
    spin.start('Fetching remote branches...');
    try {
      execSync('git fetch origin --prune', { stdio: 'pipe' });
    } catch {
      // fetch may partially fail, continue
    }
    spin.stop('Remote branches fetched.');

    const search_term = await p.text({
      message: 'Search branches (min 2 chars):',
      placeholder: 'feat/my',
      validate: (v) => (v && v.length >= 2 ? undefined : 'Enter at least 2 characters'),
    });
    if (p.isCancel(search_term)) { p.cancel('Cancelled.'); process.exit(0); }

    let branches;
    try {
      const raw = run(`git branch -r --list "origin/*" --format="%(refname:short)"`);
      branches = raw.split('\n')
        .filter((b) => b && !b.includes('HEAD') && b.toLowerCase().includes(search_term.toLowerCase()))
        .slice(0, 30);
    } catch {
      branches = [];
    }

    if (branches.length === 0) {
      p.log.error(`No remote branches match "${search_term}".`);
      p.cancel('No matches.');
      process.exit(0);
    }

    const selected = await p.select({
      message: 'Select a branch:',
      options: branches.map((b) => ({ value: b, label: b })),
    });
    if (p.isCancel(selected)) { p.cancel('Cancelled.'); process.exit(0); }

    branch_name = selected.replace(/^origin\//, '');
    from_ref = selected;
  }

  // Step 3: Environment
  const local_dev_cmd = config && config.dash && config.dash.localDevCommand ? config.dash.localDevCommand : 'pnpm dev';
  const has_host_build = config && config.features.hostBuild;
  const env_choice = await p.select({
    message: 'Environment?',
    options: [
      { value: 'docker', label: 'Docker', hint: 'run services in container' },
      ...(has_host_build
        ? [{ value: 'host_build', label: 'Docker + Host Build', hint: 'container + esbuild on host' }]
        : []),
      { value: 'local', label: 'Local', hint: `plain worktree, run with ${local_dev_cmd}` },
    ],
  });
  if (p.isCancel(env_choice)) { p.cancel('Cancelled.'); process.exit(0); }

  const is_host_build = env_choice === 'host_build';
  const use_docker = env_choice !== 'local';

  const default_mode = config && config.services.defaultMode ? config.services.defaultMode : Object.keys(config.services.modes)[0];
  let actual_mode = default_mode;
  if (use_docker && config && Object.keys(config.services.modes).length > 1) {
    const mode_keys = Object.keys(config.services.modes);
    mode_keys.sort((a, b) => (a === default_mode ? -1 : b === default_mode ? 1 : 0));
    const mode_choice = await p.select({
      message: 'Services?',
      options: mode_keys.map((m) => {
        const services = config.services.modes[m];
        const hint = services ? `${services.length} services` : 'all services';
        return { value: m, label: m.charAt(0).toUpperCase() + m.slice(1), hint };
      }),
    });
    if (p.isCancel(mode_choice)) { p.cancel('Cancelled.'); process.exit(0); }
    actual_mode = mode_choice;
  }

  let final_alias = '';
  let db_choice = null;
  let seed_db = false;
  let selected_extras = [];

  // Database: only for projects with a local database
  const has_local_db = config
    ? config.database && (config.database.host || config.database.containerHost)
    : false;
  const db_prefix = config && config.database ? (config.database.dbNamePrefix || 'db_') : 'db_';
  const db_default = config && config.database ? (config.database.defaultDb || 'db') : 'db';

  if (use_docker) {
    check_docker();

    // Step 4: Alias
    const default_alias = auto_alias(branch_name);
    const alias = await p.text({
      message: 'Alias (short name for container):',
      defaultValue: default_alias,
      placeholder: default_alias,
      validate: (v) => validate_alias(v || default_alias, existing_aliases),
    });
    if (p.isCancel(alias)) { p.cancel('Cancelled.'); process.exit(0); }
    final_alias = alias || default_alias;

    // Step 5: Database (only for projects with a local database)
    if (has_local_db) {
      const isolated_name = config ? config_mod.db_name(config, final_alias) : `${db_prefix}${final_alias.replace(/[^a-zA-Z0-9_]/g, '_')}`;
      db_choice = await p.select({
        message: 'Database?',
        options: [
          { value: 'isolated', label: `Isolated (${isolated_name})`, hint: 'recommended' },
          { value: 'shared', label: `Shared (${db_default})`, hint: 'use main database' },
        ],
      });
      if (p.isCancel(db_choice)) { p.cancel('Cancelled.'); process.exit(0); }

      const has_seed = config ? !!config.database.seedCommand : false;
      if (db_choice === 'isolated' && has_seed) {
        seed_db = await p.confirm({
          message: `Seed from ${db_default}?`,
          initialValue: true,
        });
        if (p.isCancel(seed_db)) { p.cancel('Cancelled.'); process.exit(0); }
      }
    }

    // Step 6: Additional options (config-driven)
    const extra_options = get_extra_options();
    if (extra_options.length > 0) {
      const extras = await p.multiselect({
        message: 'Additional options:',
        options: extra_options,
        required: false,
      });
      if (p.isCancel(extras)) { p.cancel('Cancelled.'); process.exit(0); }
      selected_extras = extras || [];
    }
  }

  // Summary + confirm
  if (use_docker) {
    const flags = [];
    flags.push(`--from=${from_ref}`);
    flags.push(`--alias=${final_alias}`);
    flags.push(`--mode=${actual_mode}`);
    if (db_choice === 'shared') flags.push('--shared-db');
    if (seed_db) flags.push('--seed');
    if (is_host_build || selected_extras.includes('host_build')) flags.push('--host-build');
    if (selected_extras.includes('lan')) flags.push('--lan');
    if (selected_extras.includes('poll')) flags.push('--poll');

    const domain = config ? config_mod.domain_for(config, final_alias) : null;
    const isolated_name = config ? config_mod.db_name(config, final_alias) : `${db_prefix}${final_alias.replace(/[^a-zA-Z0-9_]/g, '_')}`;

    const mode_display = is_host_build ? `${actual_mode} + host build` : actual_mode;
    const summary_lines = [
      `Branch:   ${branch_name}`,
      `Base:     ${from_ref}`,
      `Alias:    ${final_alias}`,
      domain ? `Domain:   ${domain}` : null,
      `Mode:     ${mode_display}`,
      has_local_db ? `Database: ${db_choice === 'shared' ? `shared (${db_default})` : `isolated (${isolated_name})`}` : null,
      seed_db ? 'Seed DB:  yes' : null,
      selected_extras.length > 0 ? `Extras:   ${selected_extras.join(', ')}` : null,
    ].filter(Boolean);

    const cmd_line = `wt up ${branch_name} ${flags.join(' ')}`;
    summary_lines.push('', `Command:  ${cmd_line}`);

    p.note(summary_lines.join('\n'), 'Summary');

    const confirmed = await p.confirm({ message: 'Create this worktree?' });
    if (p.isCancel(confirmed) || !confirmed) { p.cancel('Cancelled.'); process.exit(0); }

    const up_script = path.join(scripts_dir, 'dc-worktree-up.js');
    const args = [branch_name, ...flags];
    execSync(`node "${up_script}" ${args.map((a) => `"${a}"`).join(' ')}`, {
      stdio: 'inherit',
      cwd: repo_root,
    });
  } else {
    const summary_lines = [
      `Branch:   ${branch_name}`,
      `Base:     ${from_ref}`,
      `Mode:     local (${local_dev_cmd})`,
    ];

    const cmd_line = `wt up ${branch_name} --from=${from_ref} --no-docker`;
    summary_lines.push('', `Command:  ${cmd_line}`);

    p.note(summary_lines.join('\n'), 'Summary');

    const confirmed = await p.confirm({ message: 'Create this worktree?' });
    if (p.isCancel(confirmed) || !confirmed) { p.cancel('Cancelled.'); process.exit(0); }

    const up_script = path.join(scripts_dir, 'dc-worktree-up.js');
    try {
      execSync(`node "${up_script}" "${branch_name}" "--from=${from_ref}" --no-docker`, {
        stdio: 'inherit',
        cwd: repo_root,
      });
    } catch (e) {
      if (e.signal === 'SIGINT') {
        // Dev server stopped by user — normal exit
        process.exit(0);
      }
      throw e;
    }
  }

  // Signal dashboard that creation finished (alias on second line)
  fs.writeFileSync(SENTINEL_PATH, `0\n${final_alias || ''}`);
  p.outro('Worktree created!');
}

main().catch((err) => {
  console.error(err);
  fs.writeFileSync(SENTINEL_PATH, '1');
  process.exit(1);
});
