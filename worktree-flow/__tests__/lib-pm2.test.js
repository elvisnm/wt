/**
 * lib-pm2.test.js — Tests for worktree-flow/lib/pm2.js
 */

const fs = require('fs');
const path = require('path');
const os = require('os');

// Mock child_process at the module level so pm2.js picks up the same mock
jest.mock('child_process', () => ({
  execSync: jest.fn(),
}));

const { execSync } = require('child_process');
const pm2 = require('../lib/pm2');

function make_tmp_dir(prefix = 'wt-pm2-test-') {
  return fs.mkdtempSync(path.join(os.tmpdir(), prefix));
}

beforeEach(() => {
  execSync.mockReset();
});

// ── find_pm2 ──────────────────────────────────────────────────────────────

describe('find_pm2', () => {
  test('returns quoted local path when node_modules/.bin/pm2 exists', () => {
    const tmp = make_tmp_dir();
    const bin_dir = path.join(tmp, 'node_modules', '.bin');
    fs.mkdirSync(bin_dir, { recursive: true });
    fs.writeFileSync(path.join(bin_dir, 'pm2'), '#!/bin/sh\n');

    const result = pm2.find_pm2(tmp);
    expect(result).toBe(`"${path.join(bin_dir, 'pm2')}"`);

    fs.rmSync(tmp, { recursive: true });
  });

  test('returns "pm2" when no local binary exists', () => {
    const tmp = make_tmp_dir();
    expect(pm2.find_pm2(tmp)).toBe('pm2');
    fs.rmSync(tmp, { recursive: true });
  });

  test('returns "pm2" when repo_root is undefined', () => {
    expect(pm2.find_pm2(undefined)).toBe('pm2');
  });
});

// ── pm2_home ──────────────────────────────────────────────────────────────

describe('pm2_home', () => {
  test('returns .pm2 inside worktree path', () => {
    expect(pm2.pm2_home('/tmp/my-worktree')).toBe('/tmp/my-worktree/.pm2');
  });
});

// ── pm2_env_prefix ───────────────────────────────────────────────────────

describe('pm2_env_prefix', () => {
  test('returns PM2_HOME prefix when home_dir is set', () => {
    expect(pm2.pm2_env_prefix('/tmp/.pm2')).toBe('PM2_HOME="/tmp/.pm2" ');
  });

  test('returns empty string when home_dir is falsy', () => {
    expect(pm2.pm2_env_prefix(null)).toBe('');
    expect(pm2.pm2_env_prefix('')).toBe('');
  });
});

// ── pm2_process_name ─────────────────────────────────────────────────────

describe('pm2_process_name', () => {
  test('appends worktree suffix', () => {
    expect(pm2.pm2_process_name('app', 'my-feature')).toBe('app-my-feature');
  });

  test('returns base name when no suffix', () => {
    expect(pm2.pm2_process_name('app', '')).toBe('app');
    expect(pm2.pm2_process_name('app', null)).toBe('app');
  });

  test('sanitizes special characters in suffix', () => {
    expect(pm2.pm2_process_name('api', 'feat/new-thing')).toBe('api-feat-new-thing');
  });

  test('preserves dots and underscores in suffix', () => {
    expect(pm2.pm2_process_name('app', 'v1.2_test')).toBe('app-v1.2_test');
  });
});

// ── pm2_kill ─────────────────────────────────────────────────────────────

describe('pm2_kill', () => {
  test('does nothing when home is falsy', () => {
    pm2.pm2_kill('pm2', null);
    expect(execSync).not.toHaveBeenCalled();
  });

  test('calls pm2 kill with PM2_HOME prefix', () => {
    execSync.mockReturnValue('');
    pm2.pm2_kill('pm2', '/tmp/.pm2');
    expect(execSync).toHaveBeenCalledWith('PM2_HOME="/tmp/.pm2" pm2 kill', { stdio: 'pipe' });
  });

  test('swallows errors from pm2 kill', () => {
    execSync.mockImplementation(() => { throw new Error('no daemon'); });
    expect(() => pm2.pm2_kill('pm2', '/tmp/.pm2')).not.toThrow();
  });
});

// ── pm2_delete_by_names ──────────────────────────────────────────────────

describe('pm2_delete_by_names', () => {
  test('calls pm2 delete with regex pattern', () => {
    execSync.mockReturnValue('');
    pm2.pm2_delete_by_names('pm2', ['app-feat', 'api-feat']);
    expect(execSync).toHaveBeenCalledWith(
      'pm2 delete "/(app-feat|api-feat)/"',
      { stdio: 'pipe' },
    );
  });

  test('does nothing for empty names array', () => {
    pm2.pm2_delete_by_names('pm2', []);
    expect(execSync).not.toHaveBeenCalled();
  });

  test('escapes regex special characters in names', () => {
    execSync.mockReturnValue('');
    pm2.pm2_delete_by_names('pm2', ['app.server']);
    expect(execSync).toHaveBeenCalledWith(
      'pm2 delete "/(app\\.server)/"',
      { stdio: 'pipe' },
    );
  });
});

// ── pm2_cleanup ──────────────────────────────────────────────────────────

describe('pm2_cleanup', () => {
  test('kills daemon when pm2_home is set', () => {
    execSync.mockReturnValue('');
    pm2.pm2_cleanup('pm2', '/tmp/.pm2');
    expect(execSync).toHaveBeenCalledWith('PM2_HOME="/tmp/.pm2" pm2 kill', { stdio: 'pipe' });
  });

  test('deletes by name pattern when no pm2_home', () => {
    execSync.mockReturnValue('');
    pm2.pm2_cleanup('pm2', null, ['app-feat', 'api-feat']);
    expect(execSync).toHaveBeenCalledWith(
      expect.stringContaining('pm2 delete'),
      { stdio: 'pipe' },
    );
  });

  test('does nothing when no pm2_home and no names', () => {
    pm2.pm2_cleanup('pm2', null);
    expect(execSync).not.toHaveBeenCalled();
  });
});

// ── pm2_list ─────────────────────────────────────────────────────────────

describe('pm2_list', () => {
  test('returns parsed JSON from pm2 jlist', () => {
    const mock_data = [{ name: 'app', pm2_env: { status: 'online' } }];
    execSync.mockReturnValue(JSON.stringify(mock_data));
    const result = pm2.pm2_list('pm2', '/tmp/.pm2');
    expect(result).toEqual(mock_data);
  });

  test('returns empty array on error', () => {
    execSync.mockImplementation(() => { throw new Error('fail'); });
    expect(pm2.pm2_list('pm2', '/tmp/.pm2')).toEqual([]);
  });

  test('passes PM2_HOME in env', () => {
    execSync.mockReturnValue('[]');
    pm2.pm2_list('pm2', '/tmp/.pm2');
    expect(execSync).toHaveBeenCalledWith(
      'PM2_HOME="/tmp/.pm2" pm2 jlist',
      expect.objectContaining({
        env: expect.objectContaining({ PM2_HOME: '/tmp/.pm2' }),
      }),
    );
  });
});

// ── pm2_action ───────────────────────────────────────────────────────────

describe('pm2_action', () => {
  test('restart calls pm2 restart with service name', () => {
    execSync.mockReturnValue('');
    pm2.pm2_action('pm2', '/tmp/.pm2', 'restart', 'app-feat');
    expect(execSync).toHaveBeenCalledWith(
      'PM2_HOME="/tmp/.pm2" pm2 restart "app-feat"',
      expect.objectContaining({ stdio: 'inherit' }),
    );
  });

  test('start with ecosystem config uses --only flag', () => {
    execSync.mockReturnValue('');
    pm2.pm2_action('pm2', '/tmp/.pm2', 'start', 'app-feat', {
      ecosystem_config: '/path/to/ecosystem.config.js',
    });
    expect(execSync).toHaveBeenCalledWith(
      'PM2_HOME="/tmp/.pm2" pm2 start "/path/to/ecosystem.config.js" --only "app-feat" --update-env',
      expect.objectContaining({ stdio: 'inherit' }),
    );
  });

  test('stop calls pm2 stop', () => {
    execSync.mockReturnValue('');
    pm2.pm2_action('pm2', '/tmp/.pm2', 'stop', 'api-feat');
    expect(execSync).toHaveBeenCalledWith(
      'PM2_HOME="/tmp/.pm2" pm2 stop "api-feat"',
      expect.objectContaining({ stdio: 'inherit' }),
    );
  });
});
