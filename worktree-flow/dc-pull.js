#!/usr/bin/env node

/**
 * dc-pull.js — Pull remote changes into a worktree safely.
 *
 * When setup.copyFiles are present, tracked files get overwritten during
 * worktree creation and marked with skip-worktree so they don't pollute
 * git status. This script handles the unskip → checkout → pull → re-copy
 * → re-skip dance so `git pull` just works.
 *
 * Usage:
 *   node dc-pull.js [worktree-name]
 *
 * If no name is given, operates on the current working directory
 * (must be inside a worktree).
 */

const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const { run } = require('./lib/utils');
const {
  get_copy_files,
  get_tracked_copy_files,
  unskip_files,
  checkout_files,
  copy_setup_files,
  apply_skip_worktree,
} = require('./lib/skip-worktree');

// ── Main ─────────────────────────────────────────────────────────────────

function parse_args(argv) {
  const opts = { repo: null, worktree: null };
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === '--repo' && argv[i + 1]) { opts.repo = argv[++i]; continue; }
    if (argv[i] === '--worktree' && argv[i + 1]) { opts.worktree = argv[++i]; continue; }
    if (!opts.worktree && !argv[i].startsWith('-')) { opts.worktree = argv[i]; }
  }
  return opts;
}

function main() {
  const opts = parse_args(process.argv.slice(2));
  let worktree_path = opts.worktree || process.cwd();
  let repo_root = opts.repo;

  if (!repo_root) {
    try {
      // In a worktree, --git-common-dir points to the main repo's .git/
      const common = run(`git -C "${worktree_path}" rev-parse --git-common-dir`);
      repo_root = path.resolve(worktree_path, common, '..');
    } catch {
      console.error('Could not determine repo root.');
      process.exit(1);
    }
  }

  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree path does not exist: ${worktree_path}`);
    process.exit(1);
  }

  const copy_files = get_copy_files();
  const tracked_copy_files = get_tracked_copy_files(worktree_path);
  const has_copy_files = tracked_copy_files.length > 0;

  // Pre-check: abort if there are dirty tracked files outside of managed copyFiles
  const managed = new Set(tracked_copy_files);
  try {
    const output = execSync('git status --porcelain', { cwd: worktree_path, encoding: 'utf8' });
    const lines = output.split('\n').filter(Boolean);
    const dirty = [];
    for (const line of lines) {
      const status = line.slice(0, 2);
      const file = line.slice(3);
      // Skip untracked files (??), and skip managed copyFiles
      if (status === '??' || managed.has(file)) continue;
      dirty.push(file);
    }
    if (dirty.length > 0) {
      console.error('Aborted: you have uncommitted changes that would conflict with pull:\n');
      for (const f of dirty) {
        console.error(`  ${f}`);
      }
      console.error('\nCommit or stash your changes first.');
      process.exit(1);
    }
  } catch {
    // git status failed — let pull handle it
  }

  // Step 1: Unskip copy files so git can see them again
  if (has_copy_files) {
    console.log(`Unskipping ${tracked_copy_files.length} setup file(s)...`);
    unskip_files(worktree_path, tracked_copy_files);
  }

  // Step 2: Checkout (restore) those files to their committed state
  if (has_copy_files) {
    console.log('Restoring files to branch state...');
    checkout_files(worktree_path, tracked_copy_files);
  }

  // Step 3: Pull
  console.log('Pulling...');
  try {
    execSync('git pull', { cwd: worktree_path, stdio: 'inherit' });
  } catch {
    // Pull failed — re-copy and re-skip before exiting so the worktree
    // doesn't end up in a broken state
    console.error('\nPull failed. Restoring setup files...');
    if (has_copy_files) {
      copy_setup_files(repo_root, worktree_path);
      apply_skip_worktree(worktree_path);
    }
    process.exit(1);
  }

  // Step 4: Re-copy setup files from repo root (also re-applies skip-worktree)
  if (copy_files.length > 0) {
    copy_setup_files(repo_root, worktree_path);
  }

  // Step 5: Re-skip config skipWorktree paths
  apply_skip_worktree(worktree_path);

  console.log('Done.');
}

main();
