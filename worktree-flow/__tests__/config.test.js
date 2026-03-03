/**
 * config.test.js — Tests for the worktree-flow config loader.
 *
 * Covers: load_config, find_config, container_name, volume_prefix,
 * compose_project, compute_offset, compute_ports, db_name, domain_for,
 * env_var, worktree_var, services_for_mode, feature_enabled, get_compose_info.
 */

const fs = require('fs');
const path = require('path');
const os = require('os');

// Re-require config module fresh to avoid DEFAULTS mutation between tests.
// deep_merge uses shallow copy, so nested objects (env, repo) share references
// across calls within the same process. jest.resetModules() clears Jest's
// module registry so require() returns a fresh instance.
function fresh_config_mod() {
  jest.resetModules();
  return require('../config');
}

let config_mod = fresh_config_mod();

// ── Helpers ──────────────────────────────────────────────────────────────

/**
 * Create a temp directory with a workflow.config.js file.
 * Returns { dir, config_path, cleanup }.
 */
function create_temp_config(config_obj) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'wt-config-test-'));
  const config_path = path.join(dir, 'workflow.config.js');
  const content = `module.exports = ${JSON.stringify(config_obj, null, 2)};`;
  fs.writeFileSync(config_path, content, 'utf8');

  const cleanup = () => {
    fs.rmSync(dir, { recursive: true, force: true });
  };

  return { dir, config_path, cleanup };
}

/**
 * Create a nested directory structure and return the leaf directory.
 * Useful for testing config walk-up discovery.
 */
function create_nested_dirs(base, ...segments) {
  const target = path.join(base, ...segments);
  fs.mkdirSync(target, { recursive: true });
  return target;
}

/**
 * Minimal valid config for most tests.
 */
function minimal_config(overrides = {}) {
  return {
    name: 'testapp',
    ...overrides,
  };
}

// ── Test suites ──────────────────────────────────────────────────────────

describe('find_config', () => {
  let tmp;

  afterEach(() => {
    if (tmp && tmp.cleanup) tmp.cleanup();
    tmp = null;
  });

  test('finds config in the given directory', () => {
    tmp = create_temp_config(minimal_config());
    const result = config_mod.find_config(tmp.dir);
    expect(result).not.toBeNull();
    expect(result.configPath).toBe(tmp.config_path);
    expect(result.repoRoot).toBe(tmp.dir);
  });

  test('walks up to find config in a parent directory', () => {
    tmp = create_temp_config(minimal_config());
    const nested = create_nested_dirs(tmp.dir, 'src', 'deep', 'nested');

    const result = config_mod.find_config(nested);
    expect(result).not.toBeNull();
    expect(result.configPath).toBe(tmp.config_path);
    expect(result.repoRoot).toBe(tmp.dir);
  });

  test('returns null when no config exists', () => {
    const empty_dir = fs.mkdtempSync(path.join(os.tmpdir(), 'wt-no-config-'));
    try {
      const result = config_mod.find_config(empty_dir);
      expect(result).toBeNull();
    } finally {
      fs.rmSync(empty_dir, { recursive: true, force: true });
    }
  });

  test('stops at closest config file (does not continue walking up)', () => {
    tmp = create_temp_config(minimal_config({ name: 'outer' }));

    const inner_dir = create_nested_dirs(tmp.dir, 'projects', 'inner');
    const inner_config_path = path.join(inner_dir, 'workflow.config.js');
    const inner_content = `module.exports = ${JSON.stringify(minimal_config({ name: 'inner' }))};`;
    fs.writeFileSync(inner_config_path, inner_content, 'utf8');

    const deep = create_nested_dirs(inner_dir, 'src');

    const result = config_mod.find_config(deep);
    expect(result).not.toBeNull();
    expect(result.repoRoot).toBe(inner_dir);
    expect(result.configPath).toBe(inner_config_path);
  });
});

describe('load_config', () => {
  let tmp;

  beforeEach(() => {
    config_mod = fresh_config_mod();
  });

  afterEach(() => {
    if (tmp && tmp.cleanup) tmp.cleanup();
    tmp = null;
  });

  test('loads a minimal config and applies defaults', () => {
    tmp = create_temp_config(minimal_config());
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.name).toBe('testapp');
    expect(cfg._repoRoot).toBe(tmp.dir);
    expect(cfg._configPath).toBe(tmp.config_path);
  });

  test('throws when config is not found and required=true', () => {
    const empty_dir = fs.mkdtempSync(path.join(os.tmpdir(), 'wt-missing-'));
    try {
      expect(() => {
        config_mod.load_config({ cwd: empty_dir, required: true });
      }).toThrow(/Could not find workflow\.config\.js/);
    } finally {
      fs.rmSync(empty_dir, { recursive: true, force: true });
    }
  });

  test('returns null when config is not found and required=false', () => {
    const empty_dir = fs.mkdtempSync(path.join(os.tmpdir(), 'wt-missing2-'));
    try {
      const result = config_mod.load_config({ cwd: empty_dir, required: false });
      expect(result).toBeNull();
    } finally {
      fs.rmSync(empty_dir, { recursive: true, force: true });
    }
  });

  test('throws when name is missing', () => {
    tmp = create_temp_config({});
    expect(() => {
      config_mod.load_config({ cwd: tmp.dir });
    }).toThrow(/"name" is required/);
  });

  test('throws when name is not a string', () => {
    tmp = create_temp_config({ name: 123 });
    expect(() => {
      config_mod.load_config({ cwd: tmp.dir });
    }).toThrow(/"name" is required and must be a string/);
  });

  test('throws on malformed config file (syntax error)', () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'wt-malformed-'));
    const config_path = path.join(dir, 'workflow.config.js');
    fs.writeFileSync(config_path, 'module.exports = { name: "test", broken )', 'utf8');

    tmp = { dir, config_path, cleanup: () => fs.rmSync(dir, { recursive: true, force: true }) };

    expect(() => {
      config_mod.load_config({ cwd: dir });
    }).toThrow(/Failed to load/);
  });

  // ── Computed defaults ──────────────────────────────────────────────

  test('computes worktreesDir when not provided', () => {
    tmp = create_temp_config(minimal_config());
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.repo.worktreesDir).toBe('../testapp-worktrees');
    expect(cfg.repo._worktreesDirResolved).toBe(
      path.resolve(tmp.dir, '../testapp-worktrees')
    );
  });

  test('preserves explicit worktreesDir', () => {
    tmp = create_temp_config(minimal_config({
      repo: { worktreesDir: '../custom-dir' },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.repo.worktreesDir).toBe('../custom-dir');
  });

  test('computes env.prefix from name when not provided', () => {
    tmp = create_temp_config(minimal_config());
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.env.prefix).toBe('TESTAPP');
  });

  test('converts name with hyphens to uppercase prefix', () => {
    tmp = create_temp_config(minimal_config({ name: 'my-cool-app' }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.env.prefix).toBe('MY_COOL_APP');
  });

  test('preserves explicit env.prefix', () => {
    tmp = create_temp_config(minimal_config({
      env: { prefix: 'CUSTOM' },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.env.prefix).toBe('CUSTOM');
  });

  // ── Deep merge ─────────────────────────────────────────────────────

  test('deep merges user config with defaults', () => {
    tmp = create_temp_config(minimal_config({
      docker: {
        baseImage: 'custom:latest',
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.docker.baseImage).toBe('custom:latest');
    expect(cfg.docker.composeStrategy).toBe('generate');
    expect(cfg.docker.proxy.type).toBe('ports');
  });

  test('arrays in user config override default arrays (not merged)', () => {
    tmp = create_temp_config(minimal_config({
      repo: {
        branchPrefixes: ['feature', 'bugfix'],
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.repo.branchPrefixes).toEqual(['feature', 'bugfix']);
  });

  test('null values in user config override defaults', () => {
    tmp = create_temp_config(minimal_config({
      redis: null,
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.redis).toBeNull();
  });

  test('nested objects are deeply merged', () => {
    tmp = create_temp_config(minimal_config({
      services: {
        ports: { web: 3000, api: 4000 },
        primary: 'web',
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.services.ports).toEqual({ web: 3000, api: 4000 });
    expect(cfg.services.primary).toBe('web');
    expect(cfg.services.modes).toEqual({});
    expect(cfg.services.defaultMode).toBeNull();
  });

  // ── Env var template resolution ────────────────────────────────────

  test('resolves {PREFIX} templates in env vars', () => {
    tmp = create_temp_config(minimal_config({
      env: {
        prefix: 'MYAPP',
        vars: {
          dbConnection: '{PREFIX}_MONGO_URL',
          redisHost: '{PREFIX}_REDIS_HOST',
        },
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.env.vars.dbConnection).toBe('MYAPP_MONGO_URL');
    expect(cfg.env.vars.redisHost).toBe('MYAPP_REDIS_HOST');
  });

  test('resolves {PREFIX} using computed prefix when none set', () => {
    tmp = create_temp_config(minimal_config({
      name: 'cool-app',
      env: {
        vars: {
          dbConnection: '{PREFIX}_DB',
        },
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.env.prefix).toBe('COOL_APP');
    expect(cfg.env.vars.dbConnection).toBe('COOL_APP_DB');
  });

  test('handles env vars with multiple {PREFIX} occurrences', () => {
    tmp = create_temp_config(minimal_config({
      env: {
        prefix: 'APP',
        vars: {
          weird: '{PREFIX}_{PREFIX}_THING',
        },
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.env.vars.weird).toBe('APP_APP_THING');
  });

  test('leaves non-string env var values unchanged', () => {
    tmp = create_temp_config(minimal_config({
      env: {
        prefix: 'APP',
        vars: {
          nullVar: null,
          numVar: 42,
        },
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.env.vars.nullVar).toBeNull();
    expect(cfg.env.vars.numVar).toBe(42);
  });

  // ── Path resolution ────────────────────────────────────────────────

  test('resolves composeFile relative to repoRoot', () => {
    tmp = create_temp_config(minimal_config({
      docker: {
        composeFile: 'docker/docker-compose.dev.yml',
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.docker._composeFileResolved).toBe(
      path.resolve(tmp.dir, 'docker/docker-compose.dev.yml')
    );
  });

  test('resolves flowScripts relative to repoRoot', () => {
    tmp = create_temp_config(minimal_config({
      paths: {
        flowScripts: 'scripts/worktree',
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.paths._flowScriptsResolved).toBe(
      path.resolve(tmp.dir, 'scripts/worktree')
    );
  });

  test('resolves dockerOverrides relative to repoRoot', () => {
    tmp = create_temp_config(minimal_config({
      paths: {
        dockerOverrides: '.docker-overrides',
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.paths._dockerOverridesResolved).toBe(
      path.resolve(tmp.dir, '.docker-overrides')
    );
  });

  test('resolves buildScript relative to repoRoot', () => {
    tmp = create_temp_config(minimal_config({
      paths: {
        buildScript: 'scripts/build.js',
      },
    }));
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.paths._buildScriptResolved).toBe(
      path.resolve(tmp.dir, 'scripts/build.js')
    );
  });

  test('does not set resolved paths for null values', () => {
    tmp = create_temp_config(minimal_config());
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.docker._composeFileResolved).toBeUndefined();
    expect(cfg.paths._flowScriptsResolved).toBeUndefined();
    expect(cfg.paths._dockerOverridesResolved).toBeUndefined();
    expect(cfg.paths._buildScriptResolved).toBeUndefined();
  });

  // ── Metadata ───────────────────────────────────────────────────────

  test('attaches _repoRoot and _configPath metadata', () => {
    tmp = create_temp_config(minimal_config());
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg._repoRoot).toBe(tmp.dir);
    expect(cfg._configPath).toBe(tmp.config_path);
  });

  // ── require cache clearing ─────────────────────────────────────────

  test('picks up config changes on re-load (cache busting)', () => {
    tmp = create_temp_config(minimal_config({ name: 'first' }));

    const cfg1 = config_mod.load_config({ cwd: tmp.dir });
    expect(cfg1.name).toBe('first');

    const updated = `module.exports = ${JSON.stringify(minimal_config({ name: 'second' }))};`;
    fs.writeFileSync(tmp.config_path, updated, 'utf8');

    // Jest has its own module registry — get a fresh config_mod to pick up changes
    const fresh = fresh_config_mod();
    const cfg2 = fresh.load_config({ cwd: tmp.dir });
    expect(cfg2.name).toBe('second');
  });
});

describe('container_name', () => {
  test('formats as {name}-{alias}', () => {
    const cfg = { name: 'myapp' };
    expect(config_mod.container_name(cfg, 'feature-login')).toBe('myapp-feature-login');
  });

  test('works with short names', () => {
    const cfg = { name: 'bc' };
    expect(config_mod.container_name(cfg, 'fix-bug')).toBe('bc-fix-bug');
  });
});

describe('volume_prefix', () => {
  test('formats as {name}_{alias} with underscores', () => {
    const cfg = { name: 'myapp' };
    expect(config_mod.volume_prefix(cfg, 'feature-login')).toBe('myapp_feature-login');
  });
});

describe('compose_project', () => {
  test('formats as {name}-{alias}', () => {
    const cfg = { name: 'myapp' };
    expect(config_mod.compose_project(cfg, 'test-slug')).toBe('myapp-test-slug');
  });

  test('matches container_name format', () => {
    const cfg = { name: 'app' };
    const alias = 'my-branch';
    expect(config_mod.compose_project(cfg, alias)).toBe(config_mod.container_name(cfg, alias));
  });
});

describe('compute_offset', () => {
  describe('sha256 algorithm', () => {
    const sha_config = {
      portOffset: { algorithm: 'sha256', min: 100, range: 2000 },
    };

    test('returns a number within [min, min + range)', () => {
      const offset = config_mod.compute_offset(sha_config, 'test-input');
      expect(offset).toBeGreaterThanOrEqual(100);
      expect(offset).toBeLessThan(2100);
    });

    test('is deterministic (same input produces same output)', () => {
      const offset1 = config_mod.compute_offset(sha_config, 'my-worktree');
      const offset2 = config_mod.compute_offset(sha_config, 'my-worktree');
      expect(offset1).toBe(offset2);
    });

    test('different inputs produce different offsets (usually)', () => {
      const offset_a = config_mod.compute_offset(sha_config, 'worktree-alpha');
      const offset_b = config_mod.compute_offset(sha_config, 'worktree-beta');
      expect(offset_a).not.toBe(offset_b);
    });

    test('always returns integer', () => {
      const offset = config_mod.compute_offset(sha_config, 'some-path');
      expect(Number.isInteger(offset)).toBe(true);
    });

    test('respects custom min and range', () => {
      const custom_config = {
        portOffset: { algorithm: 'sha256', min: 500, range: 100 },
      };
      for (const input of ['a', 'bb', 'ccc', 'dddd', 'long-worktree-name']) {
        const offset = config_mod.compute_offset(custom_config, input);
        expect(offset).toBeGreaterThanOrEqual(500);
        expect(offset).toBeLessThan(600);
      }
    });
  });

  describe('cksum algorithm', () => {
    const cksum_config = {
      portOffset: { algorithm: 'cksum', min: 1, range: 99 },
    };

    test('returns a number within [min, min + range)', () => {
      const offset = config_mod.compute_offset(cksum_config, 'test');
      expect(offset).toBeGreaterThanOrEqual(1);
      expect(offset).toBeLessThan(100);
    });

    test('is deterministic', () => {
      const offset1 = config_mod.compute_offset(cksum_config, 'my-worktree');
      const offset2 = config_mod.compute_offset(cksum_config, 'my-worktree');
      expect(offset1).toBe(offset2);
    });

    test('always returns integer', () => {
      const offset = config_mod.compute_offset(cksum_config, 'input');
      expect(Number.isInteger(offset)).toBe(true);
    });

    test('respects custom min and range', () => {
      const custom_config = {
        portOffset: { algorithm: 'cksum', min: 10, range: 50 },
      };
      for (const input of ['x', 'yy', 'zzz', 'worktree']) {
        const offset = config_mod.compute_offset(custom_config, input);
        expect(offset).toBeGreaterThanOrEqual(10);
        expect(offset).toBeLessThan(60);
      }
    });
  });

  test('throws on unknown algorithm', () => {
    const bad_config = {
      portOffset: { algorithm: 'md5', min: 0, range: 100 },
    };
    expect(() => {
      config_mod.compute_offset(bad_config, 'test');
    }).toThrow(/Unknown port offset algorithm: md5/);
  });

  test('sha256 and cksum produce different values for same input', () => {
    const sha_cfg = { portOffset: { algorithm: 'sha256', min: 0, range: 10000 } };
    const ck_cfg = { portOffset: { algorithm: 'cksum', min: 0, range: 10000 } };
    const sha_offset = config_mod.compute_offset(sha_cfg, 'my-worktree-path');
    const ck_offset = config_mod.compute_offset(ck_cfg, 'my-worktree-path');
    expect(sha_offset).not.toBe(ck_offset);
  });
});

describe('compute_ports', () => {
  test('applies offset to all service ports', () => {
    const cfg = {
      services: {
        ports: { web: 3000, api: 4000, admin: 5000 },
      },
    };
    const result = config_mod.compute_ports(cfg, 150);
    expect(result).toEqual({ web: 3150, api: 4150, admin: 5150 });
  });

  test('returns empty object for empty ports', () => {
    const cfg = { services: { ports: {} } };
    const result = config_mod.compute_ports(cfg, 100);
    expect(result).toEqual({});
  });

  test('works with zero offset', () => {
    const cfg = {
      services: { ports: { web: 3000 } },
    };
    const result = config_mod.compute_ports(cfg, 0);
    expect(result).toEqual({ web: 3000 });
  });

  test('preserves service names as keys', () => {
    const cfg = {
      services: {
        ports: { socket_server: 3000, cache_server: 3008 },
      },
    };
    const result = config_mod.compute_ports(cfg, 64);
    expect(Object.keys(result)).toEqual(['socket_server', 'cache_server']);
    expect(result.socket_server).toBe(3064);
    expect(result.cache_server).toBe(3072);
  });
});

describe('db_name', () => {
  test('returns prefix + sanitized alias', () => {
    const cfg = {
      database: { type: 'mongodb', dbNamePrefix: 'db_' },
    };
    expect(config_mod.db_name(cfg, 'feature-login')).toBe('db_feature_login');
  });

  test('returns null when database type is null', () => {
    const cfg = {
      database: { type: null, dbNamePrefix: 'db_' },
    };
    expect(config_mod.db_name(cfg, 'my-alias')).toBeNull();
  });

  test('returns null when database is not configured', () => {
    const cfg = { database: null };
    expect(config_mod.db_name(cfg, 'alias')).toBeNull();
  });

  test('handles alias with special characters', () => {
    const cfg = {
      database: { type: 'mongodb', dbNamePrefix: 'db_' },
    };
    expect(config_mod.db_name(cfg, 'fix/weird.chars!')).toBe('db_fix_weird_chars_');
  });

  test('handles empty dbNamePrefix', () => {
    const cfg = {
      database: { type: 'mongodb', dbNamePrefix: '' },
    };
    expect(config_mod.db_name(cfg, 'myalias')).toBe('myalias');
  });

  test('handles undefined dbNamePrefix', () => {
    const cfg = {
      database: { type: 'mongodb' },
    };
    expect(config_mod.db_name(cfg, 'myalias')).toBe('myalias');
  });

  test('only allows alphanumeric and underscores in alias part', () => {
    const cfg = {
      database: { type: 'postgres', dbNamePrefix: 'pg_' },
    };
    const result = config_mod.db_name(cfg, 'a-b.c/d@e');
    expect(result).toBe('pg_a_b_c_d_e');
    expect(result).toMatch(/^[a-zA-Z0-9_]+$/);
  });
});

describe('domain_for', () => {
  test('replaces {alias} in domain template', () => {
    const cfg = {
      docker: {
        proxy: { domainTemplate: '{alias}.localhost' },
      },
    };
    expect(config_mod.domain_for(cfg, 'feature-login')).toBe('feature-login.localhost');
  });

  test('handles multiple {alias} placeholders', () => {
    const cfg = {
      docker: {
        proxy: { domainTemplate: '{alias}.{alias}.test' },
      },
    };
    expect(config_mod.domain_for(cfg, 'myalias')).toBe('myalias.myalias.test');
  });

  test('returns null when proxy is not configured', () => {
    const cfg = {
      docker: { proxy: null },
    };
    expect(config_mod.domain_for(cfg, 'alias')).toBeNull();
  });

  test('returns null when domainTemplate is not set', () => {
    const cfg = {
      docker: {
        proxy: { type: 'ports', domainTemplate: null },
      },
    };
    expect(config_mod.domain_for(cfg, 'alias')).toBeNull();
  });
});

describe('env_var', () => {
  test('returns the resolved env var name for a key', () => {
    const cfg = {
      env: {
        vars: {
          dbConnection: 'MYAPP_MONGO_URL',
          redisHost: 'MYAPP_REDIS_HOST',
        },
      },
    };
    expect(config_mod.env_var(cfg, 'dbConnection')).toBe('MYAPP_MONGO_URL');
    expect(config_mod.env_var(cfg, 'redisHost')).toBe('MYAPP_REDIS_HOST');
  });

  test('returns null for unknown key', () => {
    const cfg = { env: { vars: {} } };
    expect(config_mod.env_var(cfg, 'nonexistent')).toBeNull();
  });
});

describe('worktree_var', () => {
  test('returns the worktree var name for a key', () => {
    const cfg = {
      env: {
        worktreeVars: {
          name: 'WORKTREE_NAME',
          alias: 'WORKTREE_ALIAS',
        },
      },
    };
    expect(config_mod.worktree_var(cfg, 'name')).toBe('WORKTREE_NAME');
    expect(config_mod.worktree_var(cfg, 'alias')).toBe('WORKTREE_ALIAS');
  });

  test('returns null for unknown key', () => {
    const cfg = { env: { worktreeVars: {} } };
    expect(config_mod.worktree_var(cfg, 'missing')).toBeNull();
  });
});

describe('services_for_mode', () => {
  const cfg = {
    services: {
      modes: {
        minimal: ['web', 'api'],
        full: null,
      },
      defaultMode: 'minimal',
    },
  };

  test('returns service list for a named mode', () => {
    expect(config_mod.services_for_mode(cfg, 'minimal')).toEqual(['web', 'api']);
  });

  test('returns null for a mode with null value (all services)', () => {
    expect(config_mod.services_for_mode(cfg, 'full')).toBeNull();
  });

  test('uses defaultMode when mode argument is falsy', () => {
    expect(config_mod.services_for_mode(cfg, null)).toEqual(['web', 'api']);
    expect(config_mod.services_for_mode(cfg, undefined)).toEqual(['web', 'api']);
    expect(config_mod.services_for_mode(cfg, '')).toEqual(['web', 'api']);
  });

  test('returns null for unknown mode name', () => {
    expect(config_mod.services_for_mode(cfg, 'nonexistent')).toBeNull();
  });

  test('returns null when no modes are defined and no default', () => {
    const empty_cfg = {
      services: { modes: {}, defaultMode: null },
    };
    expect(config_mod.services_for_mode(empty_cfg, null)).toBeNull();
  });
});

describe('feature_enabled', () => {
  test('returns true for boolean true', () => {
    const cfg = { features: { hostBuild: true } };
    expect(config_mod.feature_enabled(cfg, 'hostBuild')).toBe(true);
  });

  test('returns false for boolean false', () => {
    const cfg = { features: { hostBuild: false } };
    expect(config_mod.feature_enabled(cfg, 'hostBuild')).toBe(false);
  });

  test('returns false for null', () => {
    const cfg = { features: { devHeap: null } };
    expect(config_mod.feature_enabled(cfg, 'devHeap')).toBe(false);
  });

  test('returns false for undefined', () => {
    const cfg = { features: {} };
    expect(config_mod.feature_enabled(cfg, 'missing')).toBe(false);
  });

  test('returns true for object with enabled: true', () => {
    const cfg = { features: { admin: { enabled: true, defaultUserId: '123' } } };
    expect(config_mod.feature_enabled(cfg, 'admin')).toBe(true);
  });

  test('returns false for object with enabled: false', () => {
    const cfg = { features: { admin: { enabled: false } } };
    expect(config_mod.feature_enabled(cfg, 'admin')).toBe(false);
  });

  test('returns true for object without enabled field (defaults to not false)', () => {
    const cfg = { features: { admin: { defaultUserId: '123' } } };
    expect(config_mod.feature_enabled(cfg, 'admin')).toBe(true);
  });

  test('returns true for truthy non-boolean values (number)', () => {
    const cfg = { features: { devHeap: 2048 } };
    expect(config_mod.feature_enabled(cfg, 'devHeap')).toBe(true);
  });

  test('returns true for truthy string values', () => {
    const cfg = { features: { custom: 'yes' } };
    expect(config_mod.feature_enabled(cfg, 'custom')).toBe(true);
  });
});

describe('get_compose_info', () => {
  let tmp;

  afterEach(() => {
    if (tmp && tmp.cleanup) tmp.cleanup();
    tmp = null;
  });

  test('returns null for generate strategy', () => {
    const cfg = {
      docker: { composeStrategy: 'generate', _composeFileResolved: '/some/path' },
    };
    expect(config_mod.get_compose_info(cfg, '/some/worktree')).toBeNull();
  });

  test('returns null when config is null', () => {
    expect(config_mod.get_compose_info(null, '/some/worktree')).toBeNull();
  });

  test('returns null when compose file is not resolved', () => {
    const cfg = {
      docker: { composeStrategy: 'shared' },
    };
    expect(config_mod.get_compose_info(cfg, '/some/worktree')).toBeNull();
  });

  test('returns null when .env.worktree does not exist', () => {
    const cfg = {
      name: 'testapp',
      docker: {
        composeStrategy: 'shared',
        _composeFileResolved: '/path/to/compose.yml',
      },
      env: { filename: '.env.worktree' },
      services: { ports: {} },
    };
    const nonexistent = path.join(os.tmpdir(), 'wt-nonexistent-dir-' + Date.now());
    expect(config_mod.get_compose_info(cfg, nonexistent)).toBeNull();
  });

  test('returns null when .env.worktree has no BRANCH_SLUG', () => {
    const wt_dir = fs.mkdtempSync(path.join(os.tmpdir(), 'wt-compose-'));
    fs.writeFileSync(path.join(wt_dir, '.env.worktree'), 'SOME_VAR=value\n', 'utf8');

    tmp = { cleanup: () => fs.rmSync(wt_dir, { recursive: true, force: true }) };

    const cfg = {
      name: 'testapp',
      docker: {
        composeStrategy: 'shared',
        _composeFileResolved: '/path/to/compose.yml',
      },
      env: { filename: '.env.worktree' },
      services: { ports: {} },
      _repoRoot: '/repo',
    };

    expect(config_mod.get_compose_info(cfg, wt_dir)).toBeNull();
  });

  test('parses slug and port vars from .env.worktree', () => {
    const wt_dir = fs.mkdtempSync(path.join(os.tmpdir(), 'wt-compose-'));
    const env_content = [
      'BRANCH_SLUG=my-feature',
      'WEB_PORT=3150',
      'API_PORT=4150',
    ].join('\n');
    fs.writeFileSync(path.join(wt_dir, '.env.worktree'), env_content, 'utf8');

    tmp = { cleanup: () => fs.rmSync(wt_dir, { recursive: true, force: true }) };

    const cfg = {
      name: 'testapp',
      docker: {
        composeStrategy: 'shared',
        _composeFileResolved: '/path/to/compose.yml',
      },
      env: { filename: '.env.worktree' },
      services: { ports: { web: 3000, api: 4000 } },
      _repoRoot: '/repo',
    };

    const result = config_mod.get_compose_info(cfg, wt_dir);

    expect(result).not.toBeNull();
    expect(result.compose_file).toBe('/path/to/compose.yml');
    expect(result.project).toBe('testapp-my-feature');
    expect(result.slug).toBe('my-feature');
    expect(result.env.BRANCH_SLUG).toBe('my-feature');
    expect(result.env.REPO_ROOT).toBe('/repo');
    expect(result.env.PROJECT_ROOT).toBe(wt_dir);
    expect(result.env.WEB_PORT).toBe('3150');
    expect(result.env.API_PORT).toBe('4150');
  });

  test('uses custom env filename from config', () => {
    const wt_dir = fs.mkdtempSync(path.join(os.tmpdir(), 'wt-compose-'));
    const env_content = 'BRANCH_SLUG=custom-slug\n';
    fs.writeFileSync(path.join(wt_dir, '.env.custom'), env_content, 'utf8');

    tmp = { cleanup: () => fs.rmSync(wt_dir, { recursive: true, force: true }) };

    const cfg = {
      name: 'testapp',
      docker: {
        composeStrategy: 'shared',
        _composeFileResolved: '/path/to/compose.yml',
      },
      env: { filename: '.env.custom' },
      services: { ports: {} },
      _repoRoot: '/repo',
    };

    const result = config_mod.get_compose_info(cfg, wt_dir);
    expect(result).not.toBeNull();
    expect(result.slug).toBe('custom-slug');
  });
});

describe('CONFIG_FILENAME', () => {
  test('is workflow.config.js', () => {
    expect(config_mod.CONFIG_FILENAME).toBe('workflow.config.js');
  });
});

describe('full integration: realistic config scenarios', () => {
  let tmp;

  beforeEach(() => {
    config_mod = fresh_config_mod();
  });

  afterEach(() => {
    if (tmp && tmp.cleanup) tmp.cleanup();
    tmp = null;
  });

  test('generate strategy config loads correctly', () => {
    const config_obj = {
      name: 'myapp',
      docker: {
        baseImage: 'myapp-dev:latest',
        composeStrategy: 'generate',
        generate: {
          containerWorkdir: '/app',
          entrypoint: 'pnpm dev',
        },
        sharedInfra: {
          network: 'myapp-infra_default',
        },
        proxy: {
          type: 'traefik',
          domainTemplate: '{alias}.localhost',
        },
      },
      services: {
        ports: { web: 3000, api: 3001, admin: 3002 },
        modes: {
          minimal: ['web', 'api'],
          full: null,
        },
        defaultMode: 'minimal',
        primary: 'web',
      },
      portOffset: {
        algorithm: 'sha256',
        min: 100,
        range: 2000,
      },
      database: {
        type: 'mongodb',
        dbNamePrefix: 'db_',
      },
      env: {
        prefix: 'MYAPP',
        vars: {
          dbConnection: '{PREFIX}_MONGO_URL',
          redisHost: '{PREFIX}_REDIS_HOST',
        },
      },
      features: {
        hostBuild: true,
        lan: true,
        admin: { enabled: true, defaultUserId: 'user-123' },
      },
    };

    tmp = create_temp_config(config_obj);
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.name).toBe('myapp');
    expect(cfg.docker.composeStrategy).toBe('generate');
    expect(cfg.docker.generate.containerWorkdir).toBe('/app');
    expect(cfg.env.prefix).toBe('MYAPP');
    expect(cfg.env.vars.dbConnection).toBe('MYAPP_MONGO_URL');
    expect(cfg.env.vars.redisHost).toBe('MYAPP_REDIS_HOST');

    expect(config_mod.container_name(cfg, 'feat-login')).toBe('myapp-feat-login');
    expect(config_mod.compose_project(cfg, 'feat-login')).toBe('myapp-feat-login');
    expect(config_mod.db_name(cfg, 'feat-login')).toBe('db_feat_login');
    expect(config_mod.domain_for(cfg, 'feat-login')).toBe('feat-login.localhost');

    const offset = config_mod.compute_offset(cfg, '/path/to/worktree');
    expect(offset).toBeGreaterThanOrEqual(100);
    expect(offset).toBeLessThan(2100);

    const ports = config_mod.compute_ports(cfg, offset);
    expect(ports.web).toBe(3000 + offset);
    expect(ports.api).toBe(3001 + offset);
    expect(ports.admin).toBe(3002 + offset);

    expect(config_mod.services_for_mode(cfg, 'minimal')).toEqual(['web', 'api']);
    expect(config_mod.services_for_mode(cfg, 'full')).toBeNull();

    expect(config_mod.feature_enabled(cfg, 'hostBuild')).toBe(true);
    expect(config_mod.feature_enabled(cfg, 'lan')).toBe(true);
    expect(config_mod.feature_enabled(cfg, 'admin')).toBe(true);
    expect(config_mod.feature_enabled(cfg, 'awsCredentials')).toBe(false);
  });

  test('shared strategy config (build-check style) loads correctly', () => {
    const config_obj = {
      name: 'bc',
      docker: {
        composeStrategy: 'shared',
        composeFile: 'docker/docker-compose.dev.yml',
        proxy: {
          type: 'ports',
          domainTemplate: null,
        },
      },
      services: {
        ports: { web: 3000, api: 4000 },
        modes: { default: null },
        defaultMode: 'default',
        primary: 'web',
      },
      portOffset: {
        algorithm: 'cksum',
        min: 1,
        range: 99,
      },
      database: {
        type: 'supabase',
      },
      redis: null,
      env: {
        prefix: 'BC',
        vars: {
          projectPath: null,
        },
      },
      features: {
        hostBuild: false,
        lan: false,
        admin: { enabled: false },
      },
    };

    tmp = create_temp_config(config_obj);
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.name).toBe('bc');
    expect(cfg.env.prefix).toBe('BC');
    expect(cfg.redis).toBeNull();
    expect(cfg.docker.composeStrategy).toBe('shared');
    expect(cfg.docker._composeFileResolved).toBe(
      path.resolve(tmp.dir, 'docker/docker-compose.dev.yml')
    );

    expect(config_mod.container_name(cfg, 'fix-bug')).toBe('bc-fix-bug');
    expect(config_mod.domain_for(cfg, 'anything')).toBeNull();
    expect(config_mod.db_name(cfg, 'fix-bug')).toBe('db_fix_bug');

    const offset = config_mod.compute_offset(cfg, 'some-path');
    expect(offset).toBeGreaterThanOrEqual(1);
    expect(offset).toBeLessThan(100);

    expect(config_mod.feature_enabled(cfg, 'hostBuild')).toBe(false);
    expect(config_mod.feature_enabled(cfg, 'admin')).toBe(false);
  });

  test('minimal config gets all defaults applied', () => {
    tmp = create_temp_config({ name: 'bare' });
    const cfg = config_mod.load_config({ cwd: tmp.dir });

    expect(cfg.name).toBe('bare');
    expect(cfg.repo.worktreesDir).toBe('../bare-worktrees');
    expect(cfg.repo.branchPrefixes).toEqual(['feat', 'fix', 'ops', 'hotfix', 'release', 'chore']);
    expect(cfg.docker.composeStrategy).toBe('generate');
    expect(cfg.docker.proxy.type).toBe('ports');
    expect(cfg.services.ports).toEqual({});
    expect(cfg.portOffset.algorithm).toBe('sha256');
    expect(cfg.portOffset.min).toBe(100);
    expect(cfg.portOffset.range).toBe(2000);
    expect(cfg.database.dbNamePrefix).toBe('db_');
    expect(cfg.database.type).toBeNull();
    expect(cfg.redis).toBeNull();
    expect(cfg.env.prefix).toBe('BARE');
    expect(cfg.env.filename).toBe('.env.worktree');
    expect(cfg.features.hostBuild).toBe(false);
    expect(cfg.features.autostop).toBe(true);
    expect(cfg.dash.commands.shell).toEqual({ label: 'Shell', cmd: 'bash' });
    expect(cfg.dash.localDevCommand).toBe('pnpm dev');
    expect(cfg.git.skipWorktree).toEqual([]);
  });
});

describe('edge cases', () => {
  let tmp;

  beforeEach(() => {
    config_mod = fresh_config_mod();
  });

  afterEach(() => {
    if (tmp && tmp.cleanup) tmp.cleanup();
    tmp = null;
  });

  test('config with undefined values does not override defaults', () => {
    tmp = create_temp_config({
      name: 'testapp',
      docker: {
        baseImage: undefined,
      },
    });
    const cfg = config_mod.load_config({ cwd: tmp.dir });
    expect(cfg.docker.baseImage).toBeNull();
  });

  test('config with empty services.ports', () => {
    tmp = create_temp_config({
      name: 'testapp',
      services: { ports: {} },
    });
    const cfg = config_mod.load_config({ cwd: tmp.dir });
    const ports = config_mod.compute_ports(cfg, 100);
    expect(ports).toEqual({});
  });

  test('compute_offset with empty string input', () => {
    const cfg = {
      portOffset: { algorithm: 'sha256', min: 0, range: 1000 },
    };
    const offset = config_mod.compute_offset(cfg, '');
    expect(typeof offset).toBe('number');
    expect(offset).toBeGreaterThanOrEqual(0);
    expect(offset).toBeLessThan(1000);
  });

  test('compute_offset with very long input', () => {
    const cfg = {
      portOffset: { algorithm: 'sha256', min: 100, range: 2000 },
    };
    const long_input = 'a'.repeat(10000);
    const offset = config_mod.compute_offset(cfg, long_input);
    expect(offset).toBeGreaterThanOrEqual(100);
    expect(offset).toBeLessThan(2100);
  });

  test('db_name with empty alias', () => {
    const cfg = {
      database: { type: 'mongodb', dbNamePrefix: 'db_' },
    };
    expect(config_mod.db_name(cfg, '')).toBe('db_');
  });

  test('container_name with empty alias', () => {
    const cfg = { name: 'app' };
    expect(config_mod.container_name(cfg, '')).toBe('app-');
  });

  test('load_config from a deeply nested subdirectory', () => {
    tmp = create_temp_config(minimal_config({ name: 'deep-test' }));
    const deep = create_nested_dirs(tmp.dir, 'a', 'b', 'c', 'd', 'e', 'f');
    const cfg = config_mod.load_config({ cwd: deep });
    expect(cfg.name).toBe('deep-test');
    expect(cfg._repoRoot).toBe(tmp.dir);
  });
});
