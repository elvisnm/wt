const os = require('os');

function is_lan_ip(address) {
  return address.startsWith('192.168.') || address.startsWith('10.') || address.startsWith('172.');
}

function get_lan_ip() {
  const interfaces = os.networkInterfaces();
  const candidates = [];
  for (const name of Object.keys(interfaces)) {
    for (const iface of interfaces[name]) {
      if (iface.family === 'IPv4' && !iface.internal) {
        candidates.push({ name, address: iface.address });
      }
    }
  }
  const lan = candidates.find((c) => is_lan_ip(c.address));
  return lan ? lan.address : (candidates[0] ? candidates[0].address : null);
}

function build_lan_domain(alias, ip) {
  return `${alias}.${ip}.nip.io`;
}

module.exports = { get_lan_ip, build_lan_domain };

if (require.main === module) {
  const ip = get_lan_ip();
  if (ip) {
    console.log(ip);
  } else {
    console.error('Could not detect LAN IP address.');
    process.exit(1);
  }
}
