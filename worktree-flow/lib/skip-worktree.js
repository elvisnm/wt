const fs = require('fs');
const path = require('path');
const { config, run } = require('./utils');

// ── Config accessors ─────────────────────────────────────────────────────

function get_copy_files() {
  return config && config.setup && Array.isArray(config.setup.copyFiles)
    ? config.setup.copyFiles
    : [];
}

function get_skip_paths() {
  return config && config.git && Array.isArray(config.git.skipWorktree)
    ? config.git.skipWorktree
    : [];
}

// ── Git helpers ──────────────────────────────────────────────────────────

function resolve_tracked_files(worktree_path, patterns) {
  if (patterns.length === 0) return [];
  const pathspecs = patterns.map((p) => `"${p}"`).join(' ');
  try {
    return run(`git -C "${worktree_path}" ls-files -z ${pathspecs}`)
      .split('\0')
      .filter(Boolean);
  } catch {
    return [];
  }
}

function skip_files(worktree_path, files) {
  if (files.length === 0) return;
  const file_args = files.map((f) => `"${f}"`).join(' ');
  run(`git -C "${worktree_path}" update-index --skip-worktree ${file_args}`);
}

function unskip_files(worktree_path, files) {
  if (files.length === 0) return;
  const file_args = files.map((f) => `"${f}"`).join(' ');
  run(`git -C "${worktree_path}" update-index --no-skip-worktree ${file_args}`);
}

function checkout_files(worktree_path, files) {
  if (files.length === 0) return;
  const file_args = files.map((f) => `"${f}"`).join(' ');
  run(`git -C "${worktree_path}" checkout -- ${file_args}`);
}

// ── Composite operations ─────────────────────────────────────────────────

function get_tracked_copy_files(worktree_path) {
  return resolve_tracked_files(worktree_path, get_copy_files());
}

function apply_skip_worktree(worktree_path) {
  const paths = get_skip_paths();
  if (paths.length === 0) return;

  const tracked = resolve_tracked_files(worktree_path, paths);
  skip_files(worktree_path, tracked);
}

function copy_setup_files(repo_root, worktree_path) {
  const files = get_copy_files();
  if (files.length === 0) return 0;

  let copied = 0;
  for (const rel of files) {
    const src = path.join(repo_root, rel);
    const dst = path.join(worktree_path, rel);
    if (!fs.existsSync(src)) continue;

    try {
      const stat = fs.statSync(src);
      if (stat.isDirectory()) {
        fs.cpSync(src, dst, { recursive: true });
      } else {
        fs.mkdirSync(path.dirname(dst), { recursive: true });
        fs.copyFileSync(src, dst);
      }
      copied++;
    } catch (e) {
      console.warn(`Warning: Could not copy ${rel}: ${e.message}`);
    }
  }

  if (copied > 0) {
    console.log(`Copied ${copied} setup file(s) from repo root.`);
  }

  // Apply skip-worktree on tracked copyFiles so they don't pollute git status
  const tracked = resolve_tracked_files(worktree_path, files);
  skip_files(worktree_path, tracked);
  if (tracked.length > 0) {
    console.log(`Skip-worktree: ${tracked.length} setup file(s)`);
  }

  return copied;
}

module.exports = {
  get_copy_files,
  get_skip_paths,
  get_tracked_copy_files,
  resolve_tracked_files,
  skip_files,
  unskip_files,
  checkout_files,
  apply_skip_worktree,
  copy_setup_files,
};
