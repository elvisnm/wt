const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const { format_port_table } = require('./service-ports');
const { config, config_mod, resolve_worktree_path, read_offset } = require('./lib/utils');

function main() {
  const name = process.argv[2];

  if (!name || name.startsWith('-')) {
    console.log('Usage:');
    console.log('  pnpm dc:restart <name>    Restart the Docker container');
    process.exit(1);
  }

  const worktree_path = resolve_worktree_path(name);

  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    process.exit(1);
  }

  const shared = config ? config_mod.get_compose_info(config, worktree_path) : null;
  const compose_file = path.join(worktree_path, 'docker-compose.worktree.yml');

  if (!shared && !fs.existsSync(compose_file)) {
    console.error(`No docker-compose file found for: ${worktree_path}`);
    process.exit(1);
  }

  console.log(`Restarting ${name}...`);

  if (shared) {
    execSync(
      `docker compose -f "${shared.compose_file}" -p "${shared.project}" restart`,
      { stdio: 'inherit', env: { ...process.env, ...shared.env } },
    );
    const offset = read_offset(worktree_path);
    const ports = config_mod.compute_ports(config, offset);
    console.log('\nService Ports:');
    for (const [svc, port] of Object.entries(ports)) {
      console.log(`  ${svc.padEnd(20)} ${port}`);
    }
  } else {
    execSync('docker compose -f docker-compose.worktree.yml restart', {
      stdio: 'inherit',
      cwd: worktree_path,
    });
    const offset = read_offset(worktree_path);
    console.log('\nService Ports:');
    console.log(format_port_table(offset));
  }
}

main();
