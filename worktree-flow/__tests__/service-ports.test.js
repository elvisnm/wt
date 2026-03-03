/**
 * service-ports.test.js — Tests for service port mappings, mode filters,
 * and utility functions used by Docker worktree port management.
 *
 * Covers: SERVICE_PORTS, SERVICE_MODE_FILTERS, MINIMAL_SERVICES,
 * VALID_SERVICE_MODES, DEFAULT_SERVICE_MODE, compute_ports, format_port_table,
 * find_free_offset, get_service_ports, get_minimal_services,
 * get_valid_service_modes, get_service_mode_filters.
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

let sp = fresh_service_ports();

// ── Exported constants (no-config fallback values) ───────────────────────

describe('SERVICE_PORTS', () => {
  test('is a non-empty object', () => {
    expect(typeof sp.SERVICE_PORTS).toBe('object');
    expect(sp.SERVICE_PORTS).not.toBeNull();
    expect(Object.keys(sp.SERVICE_PORTS).length).toBeGreaterThan(0);
  });

  test('all values are positive integers', () => {
    for (const [name, port] of Object.entries(sp.SERVICE_PORTS)) {
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

  test('contains expected core services (hardcoded defaults)', () => {
    const expected_services = [
      'socket_server', 'app', 'sync', 'ship_server', 'api',
      'job_server', 'www', 'cache_server', 'insights_server',
      'order_table_server', 'inventory_table_server', 'admin_server',
      'livereload',
    ];
    for (const svc of expected_services) {
      expect(sp.SERVICE_PORTS).toHaveProperty(svc);
    }
  });

  test('has no duplicate port values', () => {
    const ports = Object.values(sp.SERVICE_PORTS);
    const unique = new Set(ports);
    expect(unique.size).toBe(ports.length);
  });
});

describe('MINIMAL_SERVICES', () => {
  test('is a non-empty array of strings', () => {
    expect(Array.isArray(sp.MINIMAL_SERVICES)).toBe(true);
    expect(sp.MINIMAL_SERVICES.length).toBeGreaterThan(0);
    for (const svc of sp.MINIMAL_SERVICES) {
      expect(typeof svc).toBe('string');
    }
  });

  test('every minimal service exists in SERVICE_PORTS', () => {
    for (const svc of sp.MINIMAL_SERVICES) {
      expect(sp.SERVICE_PORTS).toHaveProperty(svc);
    }
  });

  test('is a strict subset of SERVICE_PORTS keys', () => {
    const all_services = Object.keys(sp.SERVICE_PORTS);
    expect(sp.MINIMAL_SERVICES.length).toBeLessThan(all_services.length);
  });
});

describe('SERVICE_MODE_FILTERS', () => {
  test('is a non-empty object', () => {
    expect(typeof sp.SERVICE_MODE_FILTERS).toBe('object');
    expect(sp.SERVICE_MODE_FILTERS).not.toBeNull();
    expect(Object.keys(sp.SERVICE_MODE_FILTERS).length).toBeGreaterThan(0);
  });

  test('has minimal and full modes in defaults', () => {
    expect(sp.SERVICE_MODE_FILTERS).toHaveProperty('minimal');
    expect(sp.SERVICE_MODE_FILTERS).toHaveProperty('full');
  });

  test('minimal mode references MINIMAL_SERVICES', () => {
    expect(sp.SERVICE_MODE_FILTERS.minimal).toEqual(sp.MINIMAL_SERVICES);
  });

  test('full mode is null (no filter = all services)', () => {
    expect(sp.SERVICE_MODE_FILTERS.full).toBeNull();
  });
});

describe('VALID_SERVICE_MODES', () => {
  test('is a non-empty array of strings', () => {
    expect(Array.isArray(sp.VALID_SERVICE_MODES)).toBe(true);
    expect(sp.VALID_SERVICE_MODES.length).toBeGreaterThan(0);
    for (const mode of sp.VALID_SERVICE_MODES) {
      expect(typeof mode).toBe('string');
    }
  });

  test('contains minimal and full in defaults', () => {
    expect(sp.VALID_SERVICE_MODES).toContain('minimal');
    expect(sp.VALID_SERVICE_MODES).toContain('full');
  });

  test('matches keys of SERVICE_MODE_FILTERS', () => {
    expect([...sp.VALID_SERVICE_MODES].sort()).toEqual(
      Object.keys(sp.SERVICE_MODE_FILTERS).sort()
    );
  });
});

describe('DEFAULT_SERVICE_MODE', () => {
  test('is a string', () => {
    expect(typeof sp.DEFAULT_SERVICE_MODE).toBe('string');
  });

  test('is the first valid mode', () => {
    expect(sp.DEFAULT_SERVICE_MODE).toBe(sp.VALID_SERVICE_MODES[0]);
  });

  test('exists in VALID_SERVICE_MODES', () => {
    expect(sp.VALID_SERVICE_MODES).toContain(sp.DEFAULT_SERVICE_MODE);
  });
});

// ── compute_ports ────────────────────────────────────────────────────────

describe('compute_ports', () => {
  test('applies offset to all service ports', () => {
    const offset = 150;
    const result = sp.compute_ports(offset);

    for (const [name, base] of Object.entries(sp.SERVICE_PORTS)) {
      expect(result[name]).toBe(base + offset);
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
    const offset = 5000;
    const result = sp.compute_ports(offset);
    for (const [name, base] of Object.entries(sp.SERVICE_PORTS)) {
      expect(result[name]).toBe(base + offset);
    }
  });

  test('works with negative offset (mathematical correctness)', () => {
    const offset = -100;
    const result = sp.compute_ports(offset);
    for (const [name, base] of Object.entries(sp.SERVICE_PORTS)) {
      expect(result[name]).toBe(base + offset);
    }
  });

  test('returns a new object (not a reference to SERVICE_PORTS)', () => {
    const result = sp.compute_ports(0);
    expect(result).not.toBe(sp.SERVICE_PORTS);
    expect(result).toEqual(sp.SERVICE_PORTS);
  });
});

// ── format_port_table ────────────────────────────────────────────────────

describe('format_port_table', () => {
  const offset = 100;

  test('returns a string', () => {
    const result = sp.format_port_table(offset);
    expect(typeof result).toBe('string');
  });

  test('contains port numbers with offset applied', () => {
    const result = sp.format_port_table(offset);
    const ports = sp.compute_ports(offset);
    const filter_list = sp.SERVICE_MODE_FILTERS[sp.DEFAULT_SERVICE_MODE];

    const expected_entries = filter_list
      ? Object.entries(ports).filter(([n]) => filter_list.includes(n))
      : Object.entries(ports);

    for (const [name, port] of expected_entries) {
      expect(result).toContain(String(port));
      expect(result).toContain(name);
    }
  });

  test('default mode uses DEFAULT_SERVICE_MODE', () => {
    const default_result = sp.format_port_table(offset);
    const explicit_result = sp.format_port_table(offset, { mode: sp.DEFAULT_SERVICE_MODE });
    expect(default_result).toBe(explicit_result);
  });

  describe('minimal mode', () => {
    test('only includes services in MINIMAL_SERVICES filter', () => {
      const result = sp.format_port_table(offset, { mode: 'minimal' });
      const lines = result.split('\n');

      expect(lines.length).toBe(sp.MINIMAL_SERVICES.length);

      for (const svc of sp.MINIMAL_SERVICES) {
        expect(result).toContain(svc);
      }
    });

    test('excludes services not in MINIMAL_SERVICES', () => {
      const result = sp.format_port_table(offset, { mode: 'minimal' });
      const all_services = Object.keys(sp.SERVICE_PORTS);
      const excluded = all_services.filter((s) => !sp.MINIMAL_SERVICES.includes(s));

      for (const svc of excluded) {
        expect(result).not.toContain(svc);
      }
    });

    test('does not include (minimal) tags', () => {
      const result = sp.format_port_table(offset, { mode: 'minimal' });
      expect(result).not.toContain('(minimal)');
    });
  });

  describe('full mode', () => {
    test('includes all services', () => {
      const result = sp.format_port_table(offset, { mode: 'full' });
      const lines = result.split('\n');

      expect(lines.length).toBe(Object.keys(sp.SERVICE_PORTS).length);

      for (const svc of Object.keys(sp.SERVICE_PORTS)) {
        expect(result).toContain(svc);
      }
    });

    test('tags minimal services with (minimal)', () => {
      const result = sp.format_port_table(offset, { mode: 'full' });

      for (const svc of sp.MINIMAL_SERVICES) {
        const line = result.split('\n').find((l) => l.includes(svc));
        expect(line).toContain('(minimal)');
      }
    });

    test('non-minimal services do not have (minimal) tag', () => {
      const result = sp.format_port_table(offset, { mode: 'full' });
      const non_minimal = Object.keys(sp.SERVICE_PORTS).filter(
        (s) => !sp.MINIMAL_SERVICES.includes(s)
      );

      for (const svc of non_minimal) {
        const line = result.split('\n').find((l) => l.includes(svc));
        expect(line).toBeDefined();
        expect(line).not.toContain('(minimal)');
      }
    });
  });

  test('each line is indented with two leading spaces', () => {
    const result = sp.format_port_table(offset, { mode: 'full' });
    const lines = result.split('\n');

    for (const line of lines) {
      expect(line).toMatch(/^  /);
    }
  });

  test('service names are padded to align port numbers', () => {
    const result = sp.format_port_table(offset, { mode: 'full' });
    const lines = result.split('\n');

    const port_column_positions = lines.map((line) => {
      const match = line.match(/\d+/);
      return match ? match.index : -1;
    });

    const unique_positions = new Set(port_column_positions.filter((p) => p >= 0));
    expect(unique_positions.size).toBe(1);
  });

  test('works with zero offset', () => {
    const result = sp.format_port_table(0, { mode: 'full' });
    for (const [name, base] of Object.entries(sp.SERVICE_PORTS)) {
      expect(result).toContain(String(base));
    }
  });
});

// ── find_free_offset ─────────────────────────────────────────────────────

describe('find_free_offset', () => {
  /**
   * service-ports.js destructures execSync at require-time:
   *   const { execSync } = require('child_process');
   *
   * To mock it, we must set up the mock BEFORE requiring the module.
   * Pattern: jest.resetModules() -> mock child_process -> require fresh module.
   */
  function mock_exec_sync(return_value) {
    jest.resetModules();
    const child_process = require('child_process');
    jest.spyOn(child_process, 'execSync').mockReturnValue(return_value);
    return require('../service-ports');
  }

  function mock_exec_sync_throw() {
    jest.resetModules();
    const child_process = require('child_process');
    jest.spyOn(child_process, 'execSync').mockImplementation(() => {
      throw new Error('Command failed');
    });
    return require('../service-ports');
  }

  afterEach(() => {
    jest.restoreAllMocks();
  });

  test('returns initial offset when no ports conflict', () => {
    const mod = mock_exec_sync('');
    const result = mod.find_free_offset(50);
    expect(result).toBe(50);
  });

  test('increments offset when initial offset has conflict', () => {
    const base_ports = Object.values(sp.SERVICE_PORTS);
    const first_base = base_ports[0];
    const conflicting_port = first_base + 50;

    const mod = mock_exec_sync(
      `node    12345 user   20u  IPv4 0x12345  0t0  TCP *:${conflicting_port} (LISTEN)\n`
    );

    const console_log_spy = jest.spyOn(console, 'log').mockImplementation(() => {});
    const result = mod.find_free_offset(50);

    expect(result).toBe(51);
    expect(console_log_spy).toHaveBeenCalledWith(
      expect.stringContaining('Port conflict at offset 50')
    );
  });

  test('skips multiple conflicting offsets', () => {
    const first_base = Object.values(sp.SERVICE_PORTS)[0];

    const mod = mock_exec_sync(
      `node 1 user 20u IPv4 0x1 0t0 TCP *:${first_base + 10} (LISTEN)\n` +
      `node 2 user 21u IPv4 0x2 0t0 TCP *:${first_base + 11} (LISTEN)\n`
    );

    const console_log_spy = jest.spyOn(console, 'log').mockImplementation(() => {});
    const result = mod.find_free_offset(10);

    expect(result).toBe(12);
    expect(console_log_spy).toHaveBeenCalledWith(
      expect.stringContaining('using 12 instead')
    );
  });

  test('falls back to initial offset after 100 attempts', () => {
    const all_ports = new Set();
    const base_ports = Object.values(sp.SERVICE_PORTS);
    for (let attempt = 0; attempt < 100; attempt++) {
      for (const base of base_ports) {
        all_ports.add(base + 200 + attempt);
      }
    }

    const lsof_lines = [...all_ports].map(
      (p) => `node 1 user 20u IPv4 0x1 0t0 TCP *:${p} (LISTEN)`
    );

    const mod = mock_exec_sync(lsof_lines.join('\n') + '\n');

    const console_warn_spy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    const console_log_spy = jest.spyOn(console, 'log').mockImplementation(() => {});
    const result = mod.find_free_offset(200);

    expect(result).toBe(200);
    expect(console_warn_spy).toHaveBeenCalledWith(
      expect.stringContaining('Could not find a conflict-free port offset')
    );
  });

  test('handles execSync failure gracefully (returns initial offset)', () => {
    const mod = mock_exec_sync_throw();
    const result = mod.find_free_offset(75);
    expect(result).toBe(75);
  });

  test('does not log when initial offset is free', () => {
    const mod = mock_exec_sync('');
    const console_log_spy = jest.spyOn(console, 'log').mockImplementation(() => {});
    mod.find_free_offset(50);

    expect(console_log_spy).not.toHaveBeenCalled();
  });

  test('parses port numbers from both lsof and ss output formats', () => {
    const base_ports = Object.values(sp.SERVICE_PORTS);
    const first_base = base_ports[0];

    const lsof_line = `node 1 user 20u IPv4 0x1 0t0 TCP *:${first_base + 30} (LISTEN)`;
    const ss_line = `LISTEN 0 128 0.0.0.0:${first_base + 31} 0.0.0.0:*`;

    const mod = mock_exec_sync(`${lsof_line}\n${ss_line}\n`);

    const console_log_spy = jest.spyOn(console, 'log').mockImplementation(() => {});
    const result = mod.find_free_offset(30);

    expect(result).toBeGreaterThanOrEqual(32);
  });
});

// ── Config-aware getters ─────────────────────────────────────────────────

describe('get_service_ports', () => {
  test('returns SERVICE_PORTS when cfg is null', () => {
    const result = sp.get_service_ports(null);
    expect(result).toEqual(sp.SERVICE_PORTS);
  });

  test('returns config ports when cfg is provided', () => {
    const cfg = {
      services: {
        ports: { web: 3000, api: 4000, worker: 5000 },
      },
    };
    const result = sp.get_service_ports(cfg);
    expect(result).toEqual({ web: 3000, api: 4000, worker: 5000 });
  });

  test('returns empty object from config with empty ports', () => {
    const cfg = { services: { ports: {} } };
    const result = sp.get_service_ports(cfg);
    expect(result).toEqual({});
  });
});

describe('get_minimal_services', () => {
  test('returns MINIMAL_SERVICES when cfg is null', () => {
    const result = sp.get_minimal_services(null);
    expect(result).toEqual(sp.MINIMAL_SERVICES);
  });

  test('returns config minimal list when cfg has modes.minimal', () => {
    const cfg = {
      services: {
        modes: { minimal: ['web', 'api'] },
      },
    };
    const result = sp.get_minimal_services(cfg);
    expect(result).toEqual(['web', 'api']);
  });

  test('returns default MINIMAL_SERVICES when cfg has no modes', () => {
    const cfg = { services: {} };
    const result = sp.get_minimal_services(cfg);
    expect(result).toEqual(sp.MINIMAL_SERVICES);
  });

  test('returns default MINIMAL_SERVICES when cfg modes has no minimal key', () => {
    const cfg = {
      services: {
        modes: { full: null, custom: ['svc1'] },
      },
    };
    const result = sp.get_minimal_services(cfg);
    expect(result).toEqual(sp.MINIMAL_SERVICES);
  });
});

describe('get_valid_service_modes', () => {
  test('returns VALID_SERVICE_MODES when cfg is null', () => {
    const result = sp.get_valid_service_modes(null);
    expect(result).toEqual(sp.VALID_SERVICE_MODES);
  });

  test('returns mode keys from config when cfg has modes', () => {
    const cfg = {
      services: {
        modes: { minimal: ['web'], full: null, debug: ['web', 'api'] },
      },
    };
    const result = sp.get_valid_service_modes(cfg);
    expect(result).toEqual(['minimal', 'full', 'debug']);
  });

  test('returns default modes when cfg has no modes property', () => {
    const cfg = { services: {} };
    const result = sp.get_valid_service_modes(cfg);
    expect(result).toEqual(sp.VALID_SERVICE_MODES);
  });

  test('returns empty array when cfg modes is empty', () => {
    const cfg = {
      services: { modes: {} },
    };
    const result = sp.get_valid_service_modes(cfg);
    expect(result).toEqual([]);
  });
});

describe('get_service_mode_filters', () => {
  test('returns SERVICE_MODE_FILTERS when cfg is null', () => {
    const result = sp.get_service_mode_filters(null);
    expect(result).toEqual(sp.SERVICE_MODE_FILTERS);
  });

  test('returns config modes when cfg has modes', () => {
    const cfg = {
      services: {
        modes: {
          minimal: ['web', 'api'],
          full: null,
        },
      },
    };
    const result = sp.get_service_mode_filters(cfg);
    expect(result).toEqual({
      minimal: ['web', 'api'],
      full: null,
    });
  });

  test('returns default filters when cfg has no modes property', () => {
    const cfg = { services: {} };
    const result = sp.get_service_mode_filters(cfg);
    expect(result).toEqual(sp.SERVICE_MODE_FILTERS);
  });

  test('returns config modes even if empty object', () => {
    const cfg = {
      services: { modes: {} },
    };
    const result = sp.get_service_mode_filters(cfg);
    expect(result).toEqual({});
  });
});

// ── Cross-function integration ───────────────────────────────────────────

describe('integration', () => {
  test('compute_ports + format_port_table consistency', () => {
    const offset = 64;
    const ports = sp.compute_ports(offset);
    const table = sp.format_port_table(offset, { mode: 'full' });

    for (const [name, port] of Object.entries(ports)) {
      expect(table).toContain(name);
      expect(table).toContain(String(port));
    }
  });

  test('MINIMAL_SERVICES aligns with SERVICE_MODE_FILTERS.minimal', () => {
    expect(sp.get_minimal_services(null)).toEqual(
      sp.get_service_mode_filters(null).minimal
    );
  });

  test('VALID_SERVICE_MODES matches get_service_mode_filters keys', () => {
    const filters = sp.get_service_mode_filters(null);
    expect([...sp.get_valid_service_modes(null)].sort()).toEqual(
      Object.keys(filters).sort()
    );
  });

  test('config-aware getters are consistent with each other', () => {
    const cfg = {
      services: {
        ports: { web: 8080, api: 9090 },
        modes: {
          lite: ['web'],
          full: null,
        },
      },
    };

    const ports = sp.get_service_ports(cfg);
    expect(ports).toEqual({ web: 8080, api: 9090 });

    const modes = sp.get_valid_service_modes(cfg);
    expect(modes).toEqual(['lite', 'full']);

    const filters = sp.get_service_mode_filters(cfg);
    expect(Object.keys(filters).sort()).toEqual([...modes].sort());
    expect(filters.lite).toEqual(['web']);
    expect(filters.full).toBeNull();
  });
});
