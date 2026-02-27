const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

function run(command, opts = {}) {
  return execSync(command, { stdio: 'pipe', encoding: 'utf8', ...opts }).trim();
}

function main() {
  const repo_root = run('git rev-parse --show-toplevel');
  const docker_dir = path.join(repo_root, 'docker');

  const pkg_src = path.join(repo_root, 'package.json');
  const lock_src = path.join(repo_root, 'pnpm-lock.yaml');
  const patches_src = path.join(repo_root, 'patches');
  const pkg_dst = path.join(docker_dir, 'package.json');
  const lock_dst = path.join(docker_dir, 'pnpm-lock.yaml');
  const patches_dst = path.join(docker_dir, 'patches');

  const dockerfile = path.join(docker_dir, 'Dockerfile.dev-prebaked');
  if (!fs.existsSync(dockerfile)) {
    console.error(`Dockerfile not found: ${dockerfile}`);
    process.exit(1);
  }

  console.log('Copying package.json, pnpm-lock.yaml, and patches/ into docker/ ...');
  fs.copyFileSync(pkg_src, pkg_dst);
  fs.copyFileSync(lock_src, lock_dst);
  if (fs.existsSync(patches_src)) {
    fs.mkdirSync(patches_dst, { recursive: true });
    for (const file of fs.readdirSync(patches_src)) {
      fs.copyFileSync(path.join(patches_src, file), path.join(patches_dst, file));
    }
  }

  const image_name = config ? config.docker.baseImage : 'dev:latest';
  try {
    console.log(`Building ${image_name} image...`);
    console.log('(this may take a few minutes on first build)\n');
    execSync(
      `docker build -t ${image_name} -f "${dockerfile}" "${docker_dir}"`,
      { stdio: 'inherit' },
    );
    console.log(`\nDone. Image ${image_name} is ready.`);
    console.log('New worktrees will use pre-baked node_modules for faster boot.');
  } finally {
    if (fs.existsSync(pkg_dst)) fs.unlinkSync(pkg_dst);
    if (fs.existsSync(lock_dst)) fs.unlinkSync(lock_dst);
    if (fs.existsSync(patches_dst)) fs.rmSync(patches_dst, { recursive: true, force: true });
  }
}

main();
