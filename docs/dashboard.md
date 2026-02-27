# Dashboard (worktree-dash)

A terminal UI built with [Bubbletea](https://github.com/charmbracelet/bubbletea) that provides real-time monitoring and management of all worktrees.

## Running

```bash
cd worktree-dash
go build -o worktree-dash .
./worktree-dash
```

Or with the Makefile:

```bash
cd worktree-dash && make run
```

The dashboard discovers worktrees by reading `workflow.config.js` from the repo root (found by walking up from CWD).

## Layout

```
+------ Left (28%) ------+------------ Right (72%) -----------+
|                         |                                     |
|  Worktrees (list)       |  Terminal Tabs                      |
|  - feat-login  [R]      |  [Shell] [Claude] [Logs]            |
|  - fix-payment [R]      |                                     |
|  > feat-search [S]      |  $ echo "interactive PTY"           |
|                         |  interactive PTY                     |
+-------------------------+                                     |
|  Details                |                                     |
|  Alias: feat-login      |                                     |
|  Branch: feat/login     |                                     |
|  Port: 3101             |                                     |
|  Domain: login.local..  |                                     |
+-------------------------+                                     |
|  Services               |                                     |
|  app       [R] 120MB    +-------------------------------------+
|  api       [R]  85MB    |  Activity                           |
|  socket    [R]  42MB    |  - Restarting feat-login...         |
+-------------------------+-------------------------------------+
```

**Four panels:**
- **Worktrees** — list with status icons, navigate with j/k
- **Details** — metadata for the selected worktree (alias, branch, ports, URLs, mode, DB)
- **Services** — PM2 services with status and memory (for generate strategy)
- **Terminal** — tabbed PTY sessions (shell, claude, logs, custom commands)

## Keybindings

### Navigation

| Key | Action |
|---|---|
| `j` / `k` / `Up` / `Down` | Move up/down in lists |
| `Tab` / `Shift+Tab` | Cycle panels forward/back |
| `Left` / `Right` | Switch panel |
| `<` / `>` | Switch panel |
| `a` / `w` / `s` / `d` | Jump to panel (Active Tabs / Worktrees / Services / Details) |
| `PgUp` / `PgDn` | Scroll page up/down |
| `?` | Show keybindings help |
| `q` / `Ctrl+C` | Quit |
| `Esc` | Close overlay / go back |

### Worktree Actions

| Key | Action |
|---|---|
| `Enter` | Open action picker for selected worktree |
| `n` | Create new worktree (launches dc-create wizard) |
| `u` | Start (up) container |
| `t` | Stop (terminate) container |
| `r` | Restart container |
| `i` | Show worktree info |
| `b` | Open shell (bash) in container |
| `c` | Open Claude in container |
| `l` | Preview logs |

### Global Operations

| Key | Action |
|---|---|
| `A` | AWS credentials (mount into containers) |
| `D` | Database operations (seed/drop/reset/fix-images) |
| `K` | Skip-worktree toggle (apply/remove) |
| `L` | LAN access toggle (on/off) |
| `X` | Admin account toggle (set/unset) |
| `M` | Maintenance (prune/autostop/rebuild) |

### Services Panel

| Key | Action |
|---|---|
| `Enter` | Preview logs for selected service |
| `l` | Pin service logs to a terminal tab |
| `r` | Restart selected service |

### Terminal Panel

| Key | Action |
|---|---|
| `h` / `<` | Previous tab |
| `l` / `>` | Next tab |
| `x` | Close current tab |
| `Enter` | Attach to tab (keystrokes go to PTY) |
| `Esc` | Detach from tab (keystrokes go to UI) |

## Custom Commands

The terminal tabs are configured via `dash.commands` in your config:

```js
dash: {
  commands: {
    shell:  { label: 'Shell',  cmd: 'bash' },
    claude: { label: 'Claude', cmd: 'claude' },
    logs:   { label: 'Logs',   cmd: null },     // null = built-in log viewer
    dev:    { label: 'Dev',    cmd: 'pnpm dev' },
    build:  { label: 'Build',  cmd: null },     // null = built-in build runner
  },
}
```

- `cmd: 'bash'` — runs `docker exec -it <container> bash` (Docker) or `bash` (local)
- `cmd: 'claude'` — runs `claude` in the worktree directory
- `cmd: null` — triggers a built-in handler (logs viewer or build runner)
- `cmd: 'pnpm dev'` — runs the command inside the container or locally

You can add any command. It will appear as a terminal tab option.

## Real-Time Updates

The dashboard polls Docker in the background:
- **Container status** — every 5 seconds (`docker ps`)
- **Resource stats** — every 3 seconds (`docker stats`)
- **Services** — on demand when a worktree is selected (`docker exec pm2 jlist`)

## Config Loading

The Go dashboard loads `workflow.config.js` by executing Node.js:

```
node -e "console.log(JSON.stringify(require('./workflow.config.js')))"
```

This means the config file can use `require()`, environment variables, and conditional logic — the dashboard sees the final resolved values.
