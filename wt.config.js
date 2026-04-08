// wt.config.js — wt (Worktree Toolkit) self-hosting config
// Develop wt features using wt itself.
//
// wt is NOT a web app — it's a CLI tool that needs to be compiled.
// Instead of `start` (dev server), worktrees use `build` to produce
// a binary named wt-{alias} and symlink it to /usr/local/bin/.
//
// Workflow:
//   1. Create worktree: `n` → branch feat/fix-resize, alias fix-resize
//   2. Build: `u` (start) on the worktree → compiles wt-fix-resize
//   3. Test: open any terminal → wt-fix-resize --debug
//   4. Iterate: make changes → `r` (restart/rebuild) → test again

module.exports = {
  name: 'wt',

  repo: {
    worktreesDir: '../wt-worktrees',
    branchPrefixes: ['feat', 'fix', 'chore', 'refactor'],
    baseRefs: ['main'],
  },

  docker: {
    baseImage: null,
    composeStrategy: null,
    composeFile: null,
    generate: null,
    sharedInfra: {},
    proxy: {},
    envFiles: [],
  },

  services: {
    ports: {},
    primary: null,
    quickLinks: [],
  },

  portOffset: {
    algorithm: 'sha256',
    min: 100,
    range: 2000,
    autoResolve: false,
  },

  database: null,
  redis: null,

  env: {
    prefix: 'WT',
    filename: '.env.worktree',
    vars: {},
    worktreeVars: {
      name:  'WORKTREE_NAME',
      alias: 'WORKTREE_ALIAS',
    },
  },

  features: {
    lan: false,
    autostop: false,
    prune: false,
    rebuildBase: false,
  },

  dash: {
    // Build-oriented project: `start` compiles and installs the binary
    build: {
      // Command to build. Runs in the worktree root.
      // {alias} gets replaced with WORKTREE_ALIAS at runtime.
      cmd: 'cd worktree-dash-rs && cargo build && cp target/debug/wt ../.builds/wt-{alias}',
      // Where the built binary is placed. The directory is created automatically.
      install: '{path}/.builds/wt-{alias}',
    },
    logFile: '/tmp/wt-{alias}.log',
    commands: {
      shell:  { label: 'Shell',  cmd: 'zsh' },
      claude: { label: 'Claude', cmd: 'claude' },
    },
    localDevCommand: null,
    services: {
      manager: 'static',
      list: [],
      runningCheck: 'devTab',
    },
  },

  git: {
    skipWorktree: [],
  },

  paths: {
    flowScripts: null,
    dockerOverrides: null,
    buildScript: null,
  },
};
