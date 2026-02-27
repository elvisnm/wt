const { execSync } = require('child_process');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

/**
 * Service-to-base-port mapping for Docker worktrees.
 *
 * These MUST match the base ports defined in globals.js (lines 61-79)
 * and the livereload port in scripts/deployment_scripts/build.js.
 *
 * At runtime, each port is offset by WORKTREE_PORT_OFFSET via
 * scripts/port-config.js → getPort(name, basePort).
 */

const SERVICE_PORTS = config ? config.services.ports : {
  socket_server: 3000,
  app: 3001,
  sync: 3002,
  ship_server: 3003,
  api: 3004,
  job_server: 3005,
  www: 3006,
  cache_server: 3008,
  insights_server: 3010,
  order_table_server: 3012,
  inventory_table_server: 3013,
  admin_server: 3050,
  livereload: 53099,
};

const MINIMAL_SERVICES = config && config.services.modes && config.services.modes.minimal
  ? config.services.modes.minimal
  : ['socket_server', 'app', 'api', 'admin_server', 'cache_server', 'order_table_server', 'inventory_table_server'];

function compute_ports(offset) {
  const result = {};
  for (const [name, base] of Object.entries(SERVICE_PORTS)) {
    result[name] = base + offset;
  }
  return result;
}

const SERVICE_MODE_FILTERS = config && config.services.modes
  ? Object.fromEntries(Object.entries(config.services.modes).map(([k, v]) => [k, v]))
  : {
    minimal: MINIMAL_SERVICES,
    full: null,
  };

const VALID_SERVICE_MODES = config && config.services.modes
  ? Object.keys(config.services.modes)
  : ['minimal', 'full'];

function format_port_table(offset, { mode = 'full' } = {}) {
  const ports = compute_ports(offset);
  const filter_list = SERVICE_MODE_FILTERS[mode];
  const services = filter_list
    ? Object.entries(ports).filter(([name]) => filter_list.includes(name))
    : Object.entries(ports);

  const max_name = Math.max(...services.map(([n]) => n.length));
  const lines = services.map(([name, port]) => {
    const padded = name.padEnd(max_name);
    const tag = !filter_list && MINIMAL_SERVICES.includes(name)
      ? '  (minimal)'
      : '';
    return `  ${padded}  ${port}${tag}`;
  });

  return lines.join('\n');
}

function get_listening_ports() {
  try {
    const output = execSync(
      'lsof -iTCP -sTCP:LISTEN -P -n 2>/dev/null || ss -tlnp 2>/dev/null',
      { stdio: ['pipe', 'pipe', 'pipe'], encoding: 'utf8' },
    );
    const ports = new Set();
    for (const line of output.split('\n')) {
      for (const m of line.matchAll(/:(\d+)\b/g)) {
        const port = Number.parseInt(m[1], 10);
        if (port > 0 && port < 65536) ports.add(port);
      }
    }
    return ports;
  } catch {
    return new Set();
  }
}

function find_free_offset(initial_offset) {
  const listening = get_listening_ports();
  const base_ports = Object.values(SERVICE_PORTS);

  for (let attempt = 0; attempt < 100; attempt++) {
    const offset = initial_offset + attempt;
    const conflict = base_ports.find((base) => listening.has(base + offset));
    if (!conflict) {
      if (attempt > 0) {
        console.log(`Port conflict at offset ${initial_offset}, using ${offset} instead.`);
      }
      return offset;
    }
  }

  console.warn(`Warning: Could not find a conflict-free port offset. Using ${initial_offset}.`);
  return initial_offset;
}

/**
 * Config-aware getter for service ports.
 * @param {object|null} cfg - Config object (if null, returns hardcoded SERVICE_PORTS)
 */
function get_service_ports(cfg) {
  return cfg ? cfg.services.ports : SERVICE_PORTS;
}

/**
 * Config-aware getter for minimal services list.
 * @param {object|null} cfg - Config object
 */
function get_minimal_services(cfg) {
  return cfg && cfg.services.modes && cfg.services.modes.minimal
    ? cfg.services.modes.minimal
    : MINIMAL_SERVICES;
}

/**
 * Config-aware getter for valid service modes.
 * @param {object|null} cfg - Config object
 */
function get_valid_service_modes(cfg) {
  return cfg && cfg.services.modes
    ? Object.keys(cfg.services.modes)
    : VALID_SERVICE_MODES;
}

/**
 * Config-aware getter for service mode filters.
 * @param {object|null} cfg - Config object
 */
function get_service_mode_filters(cfg) {
  return cfg && cfg.services.modes
    ? Object.fromEntries(Object.entries(cfg.services.modes).map(([k, v]) => [k, v]))
    : SERVICE_MODE_FILTERS;
}

module.exports = {
  SERVICE_PORTS,
  SERVICE_MODE_FILTERS,
  MINIMAL_SERVICES,
  VALID_SERVICE_MODES,
  compute_ports,
  format_port_table,
  find_free_offset,
  get_service_ports,
  get_minimal_services,
  get_valid_service_modes,
  get_service_mode_filters,
};
