# Getting Started

## Prerequisites

- [Homebrew](https://brew.sh/) (macOS/Linux)
- Node.js >= 18
- Git

## Install

```bash
brew tap elvisnm/wt
brew install wt
```

## First Run

Open any git project and launch the dashboard:

```bash
cd your-project
wt
```

If no `wt.config.js` exists, the **init wizard** appears:

1. **Stack detection** — auto-detects your project type (Node.js, Next.js, Python, Rust, Go, Rails, etc.)
2. **Project name** — defaults to the directory name
3. **Worktrees directory** — where worktrees are created (default: `../{name}-worktrees`)
4. **Preview and confirm** — writes `wt.config.js`

The dashboard loads and you're ready to create your first worktree.

## Create a Worktree

1. Select **Root** in the worktree list
2. Press **Enter** to open the action menu
3. Select **Create** (`n`)
4. Follow the prompts: branch name, base ref, alias

The worktree is created in the worktrees directory and appears in the list.

## Start a Worktree

1. Select a worktree in the list
2. Press **Enter** → **Start** (`u`)

What happens depends on your project:

| Project type | Start behavior |
|---|---|
| **Web app** (Node, Next, Python, etc.) | Runs `dash.localDevCommand` or lifecycle start as a daemon |
| **Build project** (Rust, Go) | Compiles and installs the binary |
| **Docker project** | Starts Docker containers |

Services appear in the **Services** panel with online/offline status.

## View Logs

- Press **`l`** on a worktree to view its logs
- Press **Enter** on a service in the Services panel for a log preview
- Press **`l`** on a service to open logs as a full tab

## Key Shortcuts

| Key | Action |
|---|---|
| `Enter` | Action menu / preview |
| `b` | Shell |
| `c` | Claude Code |
| `l` | Logs |
| `u` | Start / Build |
| `t` | Stop |
| `n` | Create worktree |
| `i` | Toggle details |
| `Ctrl+Q` | Quit |
| `?` | Full keybindings help |

## Configuration

The `wt.config.js` file controls everything. Minimal example:

```js
module.exports = {
  name: 'myapp',
  stack: 'node',

  repo: {
    worktreesDir: '../myapp-worktrees',
  },
};
```

For web apps with services:

```js
module.exports = {
  name: 'myapp',
  stack: 'node',

  repo: {
    worktreesDir: '../myapp-worktrees',
  },

  dash: {
    localDevCommand: 'pnpm dev',
    services: {
      manager: 'static',
      list: [
        { name: 'web', port: 3000 },
        { name: 'api', port: 4000 },
      ],
      runningCheck: 'devTab',
    },
  },
};
```

For build projects (Rust, Go):

```js
module.exports = {
  name: 'myapp',

  dash: {
    build: {
      cmd: 'cargo build && cp target/debug/myapp .builds/myapp-{alias}',
      install: '{path}/.builds/myapp-{alias}',
    },
    logFile: '/tmp/myapp-{alias}.log',
  },
};
```

## Next Steps

- [Configuration Reference](configuration.md) — every config field
- [Dashboard Guide](dashboard.md) — panels, keybindings, features
- [Docker Strategies](docker-strategies.md) — generate vs shared compose
