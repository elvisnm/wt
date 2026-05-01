/**
 * service-ports.test.js — Tests for config-driven service port management.
 *
 * Covers: SERVICE_PORTS, ALL_SERVICE_NAMES, compute_ports, format_port_table,
 * find_free_offset.
 */

/**
 * Re-require service-ports module fresh.
 * service-ports.js loads config at require-time, so jest.resetModules()
 * is needed to get a clean instance between tests that need isolation.
 */
function fresh_service_ports() {
  jest.resetModules();
  return require('../service-ports');
}

// ── No-config behavior (wt repo has no wt.config.js) ───────────────

describe('without config', () => {
  let sp;
  beforeAll(() => { sp = fresh_service_ports(); });

  test('SERVICE_PORTS is an empty object', () => {
    expect(typeof sp.SERVICE_PORTS).toBe('object');
    expect(Object.keys(sp.SERVICE_PORTS).length).toBe(0);
  });

  test('ALL_SERVICE_NAMES is an empty array', () => {
    expect(Array.isArray(sp.ALL_SERVICE_NAMES)).toBe(true);
    expect(sp.ALL_SERVICE_NAMES.length).toBe(0);
  });

  test('compute_ports throws without config', () => {
    expect(() => sp.compute_ports(100)).toThrow(/wt\.config\.(cjs|js)/);
  });

  test('find_free_offset returns initial offset when no ports defined', () => {
    expect(sp.find_free_offset(50)).toBe(50);
  });
});

// ── With-config behavior (mocked) ────────────────────────────────────────

describe('with config', () => {
  let sp;

  const mock_config = {
    services: {
      ports: {
        web: 3000,
        api: 3001,
        worker: 3002,
        admin: 3050,
      },
    },
  };

  beforeAll(() => {
    jest.resetModules();
    jest.doMock('../lib/utils', () => {
      const config_mod = require('../config');
      return {
        config: mock_config,
        config_mod: { ...config_mod, compute_ports: config_mod.compute_ports },
      };
    });
    sp = require('../service-ports');
  });

  afterAll(() => {
    jest.restoreAllMocks();
  });

  test('SERVICE_PORTS matches config', () => {
    expect(sp.SERVICE_PORTS).toEqual(mock_config.services.ports);
  });

  test('all port values are positive integers', () => {
    for (const [, port] of Object.entries(sp.SERVICE_PORTS)) {
      expect(Number.isInteger(port)).toBe(true);
      expect(port).toBeGreaterThan(0);
    }
  });

  test('all keys are snake_case strings', () => {
    for (const name of Object.keys(sp.SERVICE_PORTS)) {
      expect(typeof name).toBe('string');
      expect(name).toMatch(/^[a-z][a-z0-9_]*$/);
    }
  });

  test('has no duplicate port values', () => {
    const ports = Object.values(sp.SERVICE_PORTS);
    const unique = new Set(ports);
    expect(unique.size).toBe(ports.length);
  });

  test('ALL_SERVICE_NAMES matches config port keys', () => {
    expect(sp.ALL_SERVICE_NAMES).toEqual(['web', 'api', 'worker', 'admin']);
  });

  // ── compute_ports ──────────────────────────────────────────────────────

  describe('compute_ports', () => {
    test('applies offset to all service ports', () => {
      const result = sp.compute_ports(150);
      for (const [name, base] of Object.entries(sp.SERVICE_PORTS)) {
        expect(result[name]).toBe(base + 150);
      }
    });

    test('returns same number of entries as SERVICE_PORTS', () => {
      const result = sp.compute_ports(50);
      expect(Object.keys(result).length).toBe(Object.keys(sp.SERVICE_PORTS).length);
    });

    test('preserves all service names as keys', () => {
      const result = sp.compute_ports(10);
      expect(Object.keys(result).sort()).toEqual(Object.keys(sp.SERVICE_PORTS).sort());
    });

    test('works with zero offset', () => {
      const result = sp.compute_ports(0);
      expect(result).toEqual(sp.SERVICE_PORTS);
    });

    test('works with large offset', () => {
      const result = sp.compute_ports(5000);
      for (const [name, base] of Object.entries(sp.SERVICE_PORTS)) {
        expect(result[name]).toBe(base + 5000);
      }
    });

    test('returns a new object (not a reference to SERVICE_PORTS)', () => {
      const result = sp.compute_ports(0);
      expect(result).not.toBe(sp.SERVICE_PORTS);
      expect(result).toEqual(sp.SERVICE_PORTS);
    });
  });

  // ── format_port_table ──────────────────────────────────────────────────

  describe('format_port_table', () => {
    const offset = 100;

    test('returns a string', () => {
      expect(typeof sp.format_port_table(offset)).toBe('string');
    });

    test('contains all port numbers with offset applied', () => {
      const result = sp.format_port_table(offset);
      const ports = sp.compute_ports(offset);

      for (const [name, port] of Object.entries(ports)) {
        expect(result).toContain(String(port));
        expect(result).toContain(name);
      }
    });

    test('includes all services', () => {
      const result = sp.format_port_table(offset);
      const lines = result.split('\n');
      expect(lines.length).toBe(Object.keys(sp.SERVICE_PORTS).length);
      for (const svc of Object.keys(sp.SERVICE_PORTS)) {
        expect(result).toContain(svc);
      }
    });

    test('each line is indented with two leading spaces', () => {
      const result = sp.format_port_table(offset);
      for (const line of result.split('\n')) {
        expect(line).toMatch(/^  /);
      }
    });

    test('service names are padded to align port numbers', () => {
      const result = sp.format_port_table(offset);
      const lines = result.split('\n');
      const positions = lines.map((l) => {
        const m = l.match(/\d+/);
        return m ? m.index : -1;
      });
      const unique = new Set(positions.filter((p) => p >= 0));
      expect(unique.size).toBe(1);
    });
  });
});

// ── find_free_offset (uses real SERVICE_PORTS from whatever config is loaded) ─

describe('find_free_offset', () => {
  function mock_exec_sync(return_value) {
    jest.resetModules();
    const child_process = require('child_process');
    jest.spyOn(child_process, 'execSync').mockReturnValue(return_value);
    jest.doMock('../lib/utils', () => {
      const config_mod = require('../config');
      return {
        config: {
          services: {
            ports: { web: 3000, api: 3001 },
          },
        },
        config_mod: { ...config_mod, compute_ports: config_mod.compute_ports },
      };
    });
    return require('../service-ports');
  }

  function mock_exec_sync_throw() {
    jest.resetModules();
    const child_process = require('child_process');
    jest.spyOn(child_process, 'execSync').mockImplementation(() => {
      throw new Error('Command failed');
    });
    jest.doMock('../lib/utils', () => {
      const config_mod = require('../config');
      return {
        config: {
          services: {
            ports: { web: 3000, api: 3001 },
          },
        },
        config_mod: { ...config_mod, compute_ports: config_mod.compute_ports },
      };
    });
    return require('../service-ports');
  }

  afterEach(() => {
    jest.restoreAllMocks();
  });

  test('returns initial offset when no ports conflict', () => {
    const mod = mock_exec_sync('');
    expect(mod.find_free_offset(50)).toBe(50);
  });

  test('increments offset when initial offset has conflict', () => {
    const mod = mock_exec_sync(
      `node 12345 user 20u IPv4 0x12345 0t0 TCP *:${3000 + 50} (LISTEN)\n`
    );
    const spy = jest.spyOn(console, 'log').mockImplementation(() => {});
    const result = mod.find_free_offset(50);
    expect(result).toBe(51);
    expect(spy).toHaveBeenCalledWith(expect.stringContaining('Port conflict at offset 50'));
  });

  test('skips multiple conflicting offsets', () => {
    const mod = mock_exec_sync(
      `node 1 user 20u IPv4 0x1 0t0 TCP *:${3000 + 10} (LISTEN)\n` +
      `node 2 user 21u IPv4 0x2 0t0 TCP *:${3000 + 11} (LISTEN)\n`
    );
    const spy = jest.spyOn(console, 'log').mockImplementation(() => {});
    const result = mod.find_free_offset(10);
    expect(result).toBe(12);
    expect(spy).toHaveBeenCalledWith(expect.stringContaining('using 12 instead'));
  });

  test('handles execSync failure gracefully (returns initial offset)', () => {
    const mod = mock_exec_sync_throw();
    expect(mod.find_free_offset(75)).toBe(75);
  });

  test('does not log when initial offset is free', () => {
    const mod = mock_exec_sync('');
    const spy = jest.spyOn(console, 'log').mockImplementation(() => {});
    mod.find_free_offset(50);
    expect(spy).not.toHaveBeenCalled();
  });

  test('parses port numbers from both lsof and ss output formats', () => {
    const lsof_line = `node 1 user 20u IPv4 0x1 0t0 TCP *:${3000 + 30} (LISTEN)`;
    const ss_line = `LISTEN 0 128 0.0.0.0:${3000 + 31} 0.0.0.0:*`;
    const mod = mock_exec_sync(`${lsof_line}\n${ss_line}\n`);
    const spy = jest.spyOn(console, 'log').mockImplementation(() => {});
    const result = mod.find_free_offset(30);
    expect(result).toBeGreaterThanOrEqual(32);
  });
});

// ── Cross-function integration ───────────────────────────────────────────

describe('integration (with config)', () => {
  let sp;

  beforeAll(() => {
    jest.resetModules();
    jest.doMock('../lib/utils', () => {
      const config_mod = require('../config');
      return {
        config: {
          services: {
            ports: { web: 3000, api: 3001, worker: 3002 },
          },
        },
        config_mod: { ...config_mod, compute_ports: config_mod.compute_ports },
      };
    });
    sp = require('../service-ports');
  });

  afterAll(() => {
    jest.restoreAllMocks();
  });

  test('compute_ports + format_port_table consistency', () => {
    const offset = 64;
    const ports = sp.compute_ports(offset);
    const table = sp.format_port_table(offset);
    for (const [name, port] of Object.entries(ports)) {
      expect(table).toContain(name);
      expect(table).toContain(String(port));
    }
  });
});
