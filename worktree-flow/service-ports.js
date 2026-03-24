const { execSync } = require('child_process');
const { config, config_mod } = require('./lib/utils');

/**
 * Service port management — fully config-driven.
 *
 * All port definitions, service modes, and mode filters come from
 * workflow.config.js. When no config is loaded, exports are empty
 * and callers must guard against null/empty values.
 */

const SERVICE_PORTS = config ? config.services.ports : {};

const SERVICE_MODE_FILTERS = config && config.services.modes
  ? config.services.modes
  : {};

const VALID_SERVICE_MODES = config && config.services.modes
  ? Object.keys(config.services.modes)
  : [];

const DEFAULT_SERVICE_MODE = config && config.services.defaultMode
  ? config.services.defaultMode
  : VALID_SERVICE_MODES[0] || null;

const MINIMAL_SERVICES = SERVICE_MODE_FILTERS.minimal || [];

const ALL_SERVICE_NAMES = Object.keys(SERVICE_PORTS);

function compute_ports(offset) {
  if (!config) {
    throw new Error('No workflow.config.js found — cannot compute ports');
  }
  return config_mod.compute_ports(config, offset);
}

function format_port_table(offset, { mode = DEFAULT_SERVICE_MODE } = {}) {
  const ports = compute_ports(offset);
  const filter_list = mode && SERVICE_MODE_FILTERS[mode];
  const services = filter_list
    ? Object.entries(ports).filter(([name]) => filter_list.includes(name))
    : Object.entries(ports);

  if (services.length === 0) return '';

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

  if (base_ports.length === 0) return initial_offset;

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

module.exports = {
  SERVICE_PORTS,
  SERVICE_MODE_FILTERS,
  MINIMAL_SERVICES,
  VALID_SERVICE_MODES,
  DEFAULT_SERVICE_MODE,
  ALL_SERVICE_NAMES,
  compute_ports,
  format_port_table,
  find_free_offset,
};
