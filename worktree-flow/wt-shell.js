// wt shell <alias> — open a host shell with cwd set to the worktree directory.
// For a bash shell *inside* the running container, use `wt bash <alias>`.

const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');
const { resolve_worktree_path } = require('./lib/utils');

function main() {
  const name = process.argv[2];

  if (!name || name.startsWith('-')) {
    console.log('Usage:');
    console.log('  wt shell <name>   Open a host shell with cwd set to the worktree folder');
    console.log('');
    console.log('For a bash shell inside the running container, use `wt bash <name>`.');
    process.exit(1);
  }

  const worktree_path = resolve_worktree_path(name);

  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    process.exit(1);
  }

  const shell = process.env.SHELL || '/bin/zsh';
  const child = spawn(shell, [], {
    stdio: 'inherit',
    cwd: worktree_path,
    env: { ...process.env, WT_WORKTREE: path.basename(worktree_path) },
  });

  child.on('exit', (code, signal) => {
    if (signal) process.kill(process.pid, signal);
    else process.exit(code ?? 0);
  });

  child.on('error', (err) => {
    console.error(`Failed to launch shell (${shell}): ${err.message}`);
    process.exit(1);
  });
}

main();
