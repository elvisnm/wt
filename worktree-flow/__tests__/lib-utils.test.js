/**
 * lib-utils.test.js — Tests for worktree-flow/lib/utils.js
 *
 * Covers: get_env_filename, run, resolve_worktrees_dir, resolve_worktree_path,
 * find_docker_worktrees, read_env, read_env_multi, compute_auto_offset,
 * read_offset, get_container_name, read_container_name, read_service_mode,
 * find_mongo_container, auto_alias, has_ref, sanitize_name.
 */

const fs = require('fs');
const path = require('path');
const os = require('os');
const child_process = require('child_process');

// ── Test helper ──────────────────────────────────────────────────────────

function make_tmp_dir(prefix = 'wt-utils-test-') {
  return fs.mkdtempSync(path.join(os.tmpdir(), prefix));
}

// ── Fresh module loader ──────────────────────────────────────────────────

// lib/utils.js loads config on require(). We need a fresh require to test
// without config (no workflow.config.js in the test environment).
function fresh_utils() {
  jest.resetModules();
  return require('../lib/utils');
}

// ── sanitize_name ────────────────────────────────────────────────────────

describe('sanitize_name', () => {
  const { sanitize_name } = fresh_utils();

  test('lowercases input', () => {
    expect(sanitize_name('MyBranch')).toBe('mybranch');
  });

  test('replaces non-alphanumeric characters with hyphens', () => {
    expect(sanitize_name('feat/my.branch')).toBe('feat-my-branch');
  });

  test('preserves hyphens and underscores', () => {
    expect(sanitize_name('my-branch_name')).toBe('my-branch_name');
  });

  test('handles empty string', () => {
    expect(sanitize_name('')).toBe('');
  });
});

// ── auto_alias ───────────────────────────────────────────────────────────

describe('auto_alias', () => {
  const { auto_alias } = fresh_utils();

  test('strips known prefixes', () => {
    expect(auto_alias('feat/my-feature')).toBe('my-feature');
    expect(auto_alias('fix/login-bug')).toBe('login-bug');
    expect(auto_alias('hotfix/urgent-patch')).toBe('urgent-patch');
  });

  test('takes first two parts', () => {
    expect(auto_alias('feat/add-user-authentication-flow')).toBe('add-user');
  });

  test('handles branch with no prefix', () => {
    expect(auto_alias('my-branch')).toBe('my-branch');
  });

  test('strips prefix case-insensitively', () => {
    expect(auto_alias('FEAT/upper-case')).toBe('upper-case');
  });

  test('handles slash-based nested branches', () => {
    expect(auto_alias('feat/scope/deep/nested')).toBe('scope-deep');
  });

  test('returns empty-derived alias for prefix-only branch', () => {
    const result = auto_alias('feat/');
    expect(typeof result).toBe('string');
    // after stripping "feat/" the remainder is empty, producing an empty alias
    expect(result).toBe('');
  });
});

// ── get_env_filename ─────────────────────────────────────────────────────

describe('get_env_filename', () => {
  test('returns default when no config', () => {
    const utils = fresh_utils();
    // Without a workflow.config.js in the test env, config is null
    expect(utils.get_env_filename()).toBe('.env.worktree');
  });
});

// ── run ──────────────────────────────────────────────────────────────────

describe('run', () => {
  const { run } = fresh_utils();

  test('executes a command and returns trimmed output', () => {
    const result = run('echo "  hello  "');
    expect(result).toBe('hello');
  });

  test('throws on command failure', () => {
    expect(() => run('false')).toThrow();
  });
});

// ── find_docker_worktrees ────────────────────────────────────────────────

describe('find_docker_worktrees', () => {
  const { find_docker_worktrees } = fresh_utils();
  let tmp;

  beforeEach(() => {
    tmp = make_tmp_dir();
  });

  afterEach(() => {
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  test('returns empty array for non-existent directory', () => {
    expect(find_docker_worktrees('/nonexistent/path')).toEqual([]);
  });

  test('returns empty array for empty directory', () => {
    expect(find_docker_worktrees(tmp)).toEqual([]);
  });

  test('finds worktrees with docker-compose.worktree.yml', () => {
    const wt_dir = path.join(tmp, 'my-feature');
    fs.mkdirSync(wt_dir);
    fs.writeFileSync(path.join(wt_dir, 'docker-compose.worktree.yml'), '');

    const result = find_docker_worktrees(tmp);
    expect(result).toEqual([{ name: 'my-feature', path: wt_dir }]);
  });

  test('finds worktrees with .env.worktree', () => {
    const wt_dir = path.join(tmp, 'shared-wt');
    fs.mkdirSync(wt_dir);
    fs.writeFileSync(path.join(wt_dir, '.env.worktree'), 'BRANCH_SLUG=test');

    const result = find_docker_worktrees(tmp);
    expect(result).toEqual([{ name: 'shared-wt', path: wt_dir }]);
  });

  test('returns multiple worktrees', () => {
    const dirs = ['alpha', 'beta', 'gamma'];
    for (const name of dirs) {
      const d = path.join(tmp, name);
      fs.mkdirSync(d);
      fs.writeFileSync(path.join(d, 'docker-compose.worktree.yml'), '');
    }

    const result = find_docker_worktrees(tmp);
    const names = result.map((r) => r.name).sort();
    expect(names).toEqual(['alpha', 'beta', 'gamma']);
  });

  test('scans recursively for nested worktrees', () => {
    const parent = path.join(tmp, 'nested');
    const child = path.join(parent, 'deep');
    fs.mkdirSync(child, { recursive: true });
    fs.writeFileSync(path.join(child, 'docker-compose.worktree.yml'), '');

    const result = find_docker_worktrees(tmp);
    expect(result).toEqual([{ name: 'nested/deep', path: child }]);
  });

  test('stops recursion at worktree directories', () => {
    const wt_dir = path.join(tmp, 'stop-here');
    fs.mkdirSync(wt_dir);
    fs.writeFileSync(path.join(wt_dir, 'docker-compose.worktree.yml'), '');

    // child inside a worktree should NOT be returned separately
    const child = path.join(wt_dir, 'subdir');
    fs.mkdirSync(child);
    fs.writeFileSync(path.join(child, 'docker-compose.worktree.yml'), '');

    const result = find_docker_worktrees(tmp);
    expect(result).toEqual([{ name: 'stop-here', path: wt_dir }]);
  });

  test('skips non-directory entries', () => {
    fs.writeFileSync(path.join(tmp, 'not-a-dir.txt'), 'content');
    expect(find_docker_worktrees(tmp)).toEqual([]);
  });
});

// ── read_env ─────────────────────────────────────────────────────────────

describe('read_env', () => {
  const { read_env } = fresh_utils();
  let tmp;

  beforeEach(() => {
    tmp = make_tmp_dir();
  });

  afterEach(() => {
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  test('reads a key from env file', () => {
    fs.writeFileSync(
      path.join(tmp, '.env.worktree'),
      'BRANCH_SLUG=my-feature\nWORKTREE_PORT_OFFSET=42\n',
    );
    expect(read_env(tmp, 'BRANCH_SLUG')).toBe('my-feature');
    expect(read_env(tmp, 'WORKTREE_PORT_OFFSET')).toBe('42');
  });

  test('returns null for missing key', () => {
    fs.writeFileSync(path.join(tmp, '.env.worktree'), 'BRANCH_SLUG=test\n');
    expect(read_env(tmp, 'NONEXISTENT')).toBeNull();
  });

  test('returns null when env file does not exist', () => {
    expect(read_env(tmp, 'ANYTHING')).toBeNull();
  });

  test('trims values', () => {
    fs.writeFileSync(path.join(tmp, '.env.worktree'), 'KEY=  value  \n');
    expect(read_env(tmp, 'KEY')).toBe('value');
  });
});

// ── read_env_multi ───────────────────────────────────────────────────────

describe('read_env_multi', () => {
  const { read_env_multi } = fresh_utils();
  let tmp;

  beforeEach(() => {
    tmp = make_tmp_dir();
  });

  afterEach(() => {
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  test('reads multiple keys in a single call', () => {
    fs.writeFileSync(
      path.join(tmp, '.env.worktree'),
      'A=1\nB=2\nC=3\n',
    );
    const result = read_env_multi(tmp, ['A', 'B', 'C']);
    expect(result).toEqual({ A: '1', B: '2', C: '3' });
  });

  test('returns null for missing keys', () => {
    fs.writeFileSync(path.join(tmp, '.env.worktree'), 'A=1\n');
    const result = read_env_multi(tmp, ['A', 'MISSING']);
    expect(result).toEqual({ A: '1', MISSING: null });
  });

  test('returns all nulls when file is missing', () => {
    const result = read_env_multi(tmp, ['X', 'Y']);
    expect(result).toEqual({ X: null, Y: null });
  });
});

// ── compute_auto_offset ──────────────────────────────────────────────────

describe('compute_auto_offset', () => {
  const { compute_auto_offset } = fresh_utils();

  test('returns a number', () => {
    const result = compute_auto_offset('/some/path');
    expect(typeof result).toBe('number');
  });

  test('returns deterministic result for same input', () => {
    const a = compute_auto_offset('/test/path');
    const b = compute_auto_offset('/test/path');
    expect(a).toBe(b);
  });

  test('returns different results for different inputs', () => {
    const a = compute_auto_offset('/path/one');
    const b = compute_auto_offset('/path/two');
    expect(a).not.toBe(b);
  });

  test('result is within expected range (100-2100) without config', () => {
    for (let i = 0; i < 50; i++) {
      const offset = compute_auto_offset(`/test/path-${i}`);
      expect(offset).toBeGreaterThanOrEqual(100);
      expect(offset).toBeLessThanOrEqual(2099);
    }
  });
});

// ── read_offset ──────────────────────────────────────────────────────────

describe('read_offset', () => {
  const { read_offset } = fresh_utils();
  let tmp;

  beforeEach(() => {
    tmp = make_tmp_dir();
  });

  afterEach(() => {
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  test('reads WORKTREE_HOST_PORT_OFFSET from env file (highest priority)', () => {
    fs.writeFileSync(
      path.join(tmp, '.env.worktree'),
      'WORKTREE_HOST_PORT_OFFSET=500\nWORKTREE_PORT_OFFSET=100\n',
    );
    expect(read_offset(tmp)).toBe(500);
  });

  test('reads WORKTREE_PORT_OFFSET when HOST variant missing', () => {
    fs.writeFileSync(path.join(tmp, '.env.worktree'), 'WORKTREE_PORT_OFFSET=200\n');
    expect(read_offset(tmp)).toBe(200);
  });

  test('reads WORKTREE_PORT_BASE and subtracts 3000', () => {
    fs.writeFileSync(path.join(tmp, '.env.worktree'), 'WORKTREE_PORT_BASE=3300\n');
    expect(read_offset(tmp)).toBe(300);
  });

  test('reads offset from compose file port mapping', () => {
    fs.writeFileSync(
      path.join(tmp, 'docker-compose.worktree.yml'),
      'services:\n  app:\n    ports:\n      - "3401:3001"\n',
    );
    expect(read_offset(tmp)).toBe(400);
  });

  test('falls back to compute_auto_offset when no sources found', () => {
    const offset = read_offset(tmp);
    expect(typeof offset).toBe('number');
    expect(offset).toBeGreaterThanOrEqual(100);
  });

  test('env file takes priority over compose file', () => {
    fs.writeFileSync(path.join(tmp, '.env.worktree'), 'WORKTREE_PORT_OFFSET=99\n');
    fs.writeFileSync(
      path.join(tmp, 'docker-compose.worktree.yml'),
      'services:\n  app:\n    ports:\n      - "3501:3001"\n',
    );
    expect(read_offset(tmp)).toBe(99);
  });
});

// ── read_container_name ──────────────────────────────────────────────────

describe('read_container_name', () => {
  const { read_container_name } = fresh_utils();
  let tmp;

  beforeEach(() => {
    tmp = make_tmp_dir();
  });

  afterEach(() => {
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  test('reads container_name from compose file', () => {
    fs.writeFileSync(
      path.join(tmp, 'docker-compose.worktree.yml'),
      'services:\n  app:\n    container_name: my-project-feature\n',
    );
    expect(read_container_name(tmp)).toBe('my-project-feature');
  });

  test('returns null when compose file missing', () => {
    expect(read_container_name(tmp)).toBeNull();
  });

  test('returns null when no container_name in compose file', () => {
    fs.writeFileSync(
      path.join(tmp, 'docker-compose.worktree.yml'),
      'services:\n  app:\n    image: node:20\n',
    );
    expect(read_container_name(tmp)).toBeNull();
  });
});

// ── update_env_key ───────────────────────────────────────────────────────

describe('update_env_key', () => {
  const { update_env_key } = fresh_utils();
  let tmp;

  beforeEach(() => {
    tmp = make_tmp_dir();
  });

  afterEach(() => {
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  test('updates existing key', () => {
    const env_path = path.join(tmp, '.env');
    fs.writeFileSync(env_path, 'KEY=old\nOTHER=keep\n');
    update_env_key(env_path, 'KEY', 'new');
    const content = fs.readFileSync(env_path, 'utf8');
    expect(content).toContain('KEY=new');
    expect(content).toContain('OTHER=keep');
    expect(content).not.toContain('KEY=old');
  });

  test('appends new key when not present', () => {
    const env_path = path.join(tmp, '.env');
    fs.writeFileSync(env_path, 'EXISTING=val\n');
    update_env_key(env_path, 'NEW_KEY', 'new_val');
    const content = fs.readFileSync(env_path, 'utf8');
    expect(content).toContain('EXISTING=val');
    expect(content).toContain('NEW_KEY=new_val');
  });
});

// ── remove_env_key ───────────────────────────────────────────────────────

describe('remove_env_key', () => {
  const { remove_env_key } = fresh_utils();
  let tmp;

  beforeEach(() => {
    tmp = make_tmp_dir();
  });

  afterEach(() => {
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  test('removes a key from env file', () => {
    const env_path = path.join(tmp, '.env');
    fs.writeFileSync(env_path, 'KEEP=1\nREMOVE=2\nALSO_KEEP=3\n');
    remove_env_key(env_path, 'REMOVE');
    const content = fs.readFileSync(env_path, 'utf8');
    expect(content).toContain('KEEP=1');
    expect(content).toContain('ALSO_KEEP=3');
    expect(content).not.toContain('REMOVE=2');
  });

  test('no-op when file does not exist', () => {
    expect(() => remove_env_key(path.join(tmp, 'missing'), 'KEY')).not.toThrow();
  });

  test('no-op when key does not exist', () => {
    const env_path = path.join(tmp, '.env');
    fs.writeFileSync(env_path, 'KEEP=1\n');
    remove_env_key(env_path, 'NONEXISTENT');
    expect(fs.readFileSync(env_path, 'utf8')).toBe('KEEP=1\n');
  });
});

// ── resolve_worktrees_dir (without config) ───────────────────────────────

describe('resolve_worktrees_dir', () => {
  const { resolve_worktrees_dir } = fresh_utils();

  test('computes worktrees dir from repo root', () => {
    const result = resolve_worktrees_dir('/home/user/apps/myproject');
    expect(result).toBe('/home/user/apps/myproject-worktrees');
  });

  test('handles nested repo paths', () => {
    const result = resolve_worktrees_dir('/a/b/c/repo');
    expect(result).toBe('/a/b/c/repo-worktrees');
  });
});

// ── resolve_worktree_path ────────────────────────────────────────────────

describe('resolve_worktree_path', () => {
  const { resolve_worktree_path } = fresh_utils();

  test('resolves path with explicit repo root', () => {
    const result = resolve_worktree_path('/home/user/apps/myproject', 'my-feature');
    expect(result).toBe('/home/user/apps/myproject-worktrees/my-feature');
  });

  test('replaces slashes with hyphens in name', () => {
    const result = resolve_worktree_path('/home/user/apps/myproject', 'feat/my-feature');
    expect(result).toBe('/home/user/apps/myproject-worktrees/feat-my-feature');
  });
});

// ── read_service_mode ────────────────────────────────────────────────────

describe('read_service_mode', () => {
  const { read_service_mode } = fresh_utils();
  let tmp;

  beforeEach(() => {
    tmp = make_tmp_dir();
  });

  afterEach(() => {
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  test('reads WORKTREE_SERVICES from compose file', () => {
    fs.writeFileSync(
      path.join(tmp, 'docker-compose.worktree.yml'),
      'environment:\n  - WORKTREE_SERVICES=minimal\n',
    );
    expect(read_service_mode(tmp)).toBe('minimal');
  });

  test('returns default when compose has no WORKTREE_SERVICES', () => {
    fs.writeFileSync(
      path.join(tmp, 'docker-compose.worktree.yml'),
      'services:\n  app:\n    image: node\n',
    );
    expect(read_service_mode(tmp)).toBe('default');
  });

  test('returns default when no compose file exists', () => {
    expect(read_service_mode(tmp)).toBe('default');
  });
});

// ── get_container_name (docker query, mocked) ────────────────────────────
// utils.js destructures execSync at require-time, so we must mock child_process
// before loading the module. We use a dedicated fresh_utils_with_mock() helper.

function fresh_utils_with_mock(mock_fn) {
  jest.resetModules();
  jest.doMock('child_process', () => {
    const actual = jest.requireActual('child_process');
    return { ...actual, execSync: mock_fn };
  });
  const utils = require('../lib/utils');
  jest.dontMock('child_process');
  return utils;
}

describe('get_container_name', () => {
  test('returns null when docker command fails', () => {
    const mock = jest.fn(() => { throw new Error('docker not running'); });
    const utils = fresh_utils_with_mock(mock);
    expect(utils.get_container_name('/fake/path')).toBeNull();
  });

  test('parses container name from JSON output', () => {
    const mock = jest.fn(() => '{"Name":"my-project-feature","State":"running"}\n');
    const utils = fresh_utils_with_mock(mock);
    expect(utils.get_container_name('/fake/path')).toBe('my-project-feature');
  });

  test('returns null on empty output', () => {
    const mock = jest.fn(() => '');
    const utils = fresh_utils_with_mock(mock);
    expect(utils.get_container_name('/fake/path')).toBeNull();
  });
});

// ── find_mongo_container (docker query, mocked) ──────────────────────────

describe('find_mongo_container', () => {
  test('returns null when docker fails', () => {
    const mock = jest.fn(() => { throw new Error('docker not running'); });
    const utils = fresh_utils_with_mock(mock);
    expect(utils.find_mongo_container()).toBeNull();
  });

  test('returns null when no mongo container found', () => {
    const mock = jest.fn(() => 'redis\nnginx\n');
    const utils = fresh_utils_with_mock(mock);
    expect(utils.find_mongo_container()).toBeNull();
  });
});

// ── has_ref (git query, mocked) ──────────────────────────────────────────

describe('has_ref', () => {
  test('returns true when ref exists', () => {
    const mock = jest.fn(() => '');
    const utils = fresh_utils_with_mock(mock);
    expect(utils.has_ref('/repo', 'refs/heads/main')).toBe(true);
  });

  test('returns false when ref does not exist', () => {
    const mock = jest.fn(() => { throw new Error('not a valid ref'); });
    const utils = fresh_utils_with_mock(mock);
    expect(utils.has_ref('/repo', 'refs/heads/nonexistent')).toBe(false);
  });
});
