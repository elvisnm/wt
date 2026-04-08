# Dashboard (worktree-dash-rs)

A terminal UI built with [Ratatui](https://ratatui.rs/) and embedded PTY sessions via [alacritty_terminal](https://github.com/alacritty/alacritty). No tmux dependency — terminal sessions run as embedded widgets with split pane support.

## Running

From your project directory (where `wt.config.js` lives):

```bash
wt
```

Debug mode (logs to `/tmp/wt-dash.log`):

```bash
wt --debug
```

Dev build (for wt development):

```bash
wt-dev --debug
```

## Layout

```
+------ Left (20%) ------+-------------- Right (80%) ---------------+
| Notifications           |                                           |
+-------------------------+  Guide / Terminal / Settings / Help       |
| Active Tabs             |                                           |
|  1 ● Shell (feat)       |  $ echo "embedded PTY"                   |
|  2 ● Claude (feat)      |  embedded PTY                            |
|    3 ● Shell (feat)     |                                           |
|    4 ● Logs (feat)      |  ┌─────────────┬─────────────┐           |
|              ● ●        |  │ Split Left   │ Split Right  │           |
+-------------------------+  └─────────────┴─────────────┘           |
| Worktrees               |                                           |
|  ● feat-login    3% 84M |                                           |
|  ◯ fix-payment  stopped  |                                           |
|  ◇ feat-search   local   |                                           |
+-------------------------+                                           |
| Services                |                                           |
|  ● api         2% 120MB |                                           |
|  ● web         1%  85MB |                                           |
+-------------------------+-------------------------------------------+
```

**Panels (left column):**
- **Notifications** — confirmation dialogs, pickers, messages (auto-dismiss after 5s)
- **Active Tabs** — terminal sessions with group support and dot map
- **Worktrees** — list with status, CPU/mem stats
- **Services** — PM2 services for selected worktree
- **Details** — metadata (toggle with Shift+D)
- **Usage** — Claude API utilization (toggle with Shift+U)
- **Tasks** — Beads task tracking (toggle with Shift+T)

**Right panel:**
- Guide page (default), Help page (?), Settings (Shift+S)
- Terminal sessions with embedded PTY
- Split pane layouts (horizontal/vertical)

## Keybindings

### Navigation

| Key | Action |
|---|---|
| `j` / `k` / `Up` / `Down` | Move up/down in lists |
| `Tab` / `Shift+Tab` | Cycle panels |
| `Left` / `Right` | Cycle panels |
| `a` / `w` / `s` | Jump to Active Tabs / Worktrees / Services |
| `1`-`9` | Jump to session N (enters terminal mode) |
| `?` | Toggle keybindings help |
| `Ctrl+Q` / `Ctrl+C` | Quit (with confirmation) |
| `Ctrl+B` | Toggle sidebar visibility |
| `Esc` | Dismiss notification |

### Worktree Actions

| Key | Action |
|---|---|
| `Enter` | Open action picker (context-aware) |
| `n` | Create new worktree |
| `b` | Open shell (container or local) |
| `c` | Open Claude Code |
| `l` | Preview logs |
| `z` | Open host shell (zsh) |
| `g` | Pull latest (via wt-pull.js) |
| `i` | Show worktree info |
| `u` | Start / Build (config-driven) |
| `t` | Stop container/dev server |
| `r` | Restart / Rebuild (config-driven) |
| `o` | Start individual service |
| `p` | Stop individual service |

### Global Operations

| Key | Action |
|---|---|
| `Shift+D` | Toggle Details panel |
| `Shift+U` | Toggle Claude Usage panel |
| `Shift+T` | Toggle Beads Tasks panel |
| `Shift+S` | Open Settings |
| `Shift+K` | Skip-worktree toggle |
| `Shift+L` | LAN mode toggle |
| `Shift+M` | Maintenance (prune/autostop) |

### Terminal Panel

| Key | Action |
|---|---|
| `Enter` | Focus session (enter terminal mode) |
| `h` / `l` | Switch tab groups |
| `Up` / `Down` | Navigate within group |
| `x` | Close session under cursor |
| `Shift+\` | Split right (horizontal) |
| `Shift+-` | Split below (vertical) |

### Terminal Mode (focused)

| Key | Action |
|---|---|
| `Ctrl+]` | Detach (return to dashboard) |
| `Ctrl+x` | Close focused session |
| All other keys | Sent to terminal |

### Split Panes

When in a split group, sessions are arranged in a tree layout:

```
Active Tabs:
  ●  Shell (3 panes)
    1 ● Shell (feat)       ● ●
    2 ● Claude (feat)      ●
    3 ● Logs (feat)
```

- `Shift+\` — split the selected session horizontally (new pane to the right)
- `Shift+-` — split the selected session vertically (new pane below)
- Dot map shows the layout with orange highlight for the focused pane
- Max panes per group: configurable in Settings (default 4)

## Project Types

The dashboard adapts to your project type via `wt.config.js`:

| Config | Project Type | Actions |
|---|---|---|
| `docker.composeFile` set | Docker web app | Start/Stop/Restart containers |
| `dash.localDevCommand` set | Local dev server | Start/Stop/Restart dev |
| `dash.build` set | Compiled CLI/tool | Build/Rebuild binary |
| Nothing | Bare repo | Shell/Claude/Pull only |

### Build-Oriented Projects

For compiled tools (not web apps), add `dash.build` to your config:

```js
dash: {
  build: {
    cmd: 'cargo build && cp target/debug/myapp ../myapp-{alias}',
    install: '/usr/local/bin/myapp-{alias}',
  },
}
```

This replaces Start/Stop with Build/Rebuild. `{alias}` is replaced with the worktree alias. The `install` path creates a symlink so the binary is available system-wide.

## Settings

`Shift+S` opens the settings editor. Persisted to `~/.wt/settings.json`:

- **Default Panels** — which panels open on startup (Details, Usage, Tasks)
- **Left Pane Width** — 15-40%
- **Max Panes Per Group** — 2-6 sessions
- **Claude Auto Mode** — launch Claude with `--enable-auto-mode`

## Real-Time Updates

Background polling (no manual refresh needed):
- **Container status** — every 5s (`docker ps` + `docker inspect`)
- **Resource stats** — every 3s (`docker stats`)
- **Worktree discovery** — every 5s (scans worktrees directory)
- **Services** — on worktree selection change

## Debug Mode

```bash
wt --debug
```

Logs terminal key events and internal state to `/tmp/wt-dash.log`. Useful for diagnosing keybinding issues across different terminal emulators.
