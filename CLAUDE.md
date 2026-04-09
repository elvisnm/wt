# wt — Worktree Toolkit

A config-driven worktree management system. Two components: **worktree-flow** (Node.js CLI scripts) and **worktree-dash-rs** (Rust TUI dashboard with embedded terminal). Attach to any project by dropping a `wt.config.js` at the repo root.

## Project Structure

```
wt/
├── wt.config.example.js      # Example config
├── wt.config.schema.md       # Full schema documentation
├── worktree-flow/                  # Node.js CLI scripts
│   ├── config.js                   # Config loader + helpers
│   ├── wt-menu.js                       # Interactive hub (@clack/prompts)
│   ├── wt-up.js           # Create/restart worktrees
│   ├── wt-down.js         # Stop/remove worktrees
│   ├── wt-status.js                # Status table
│   ├── wt-info.js                  # Detailed worktree info
│   ├── wt-logs.js                  # Docker compose logs
│   ├── wt-shell.js                  # Shell into container
│   ├── wt-restart.js               # Restart containers
│   ├── wt-create.js                # Interactive worktree creation wizard
│   ├── generate-docker-compose.js  # YAML generator (generate strategy)
│   ├── service-ports.js            # Port definitions
│   ├── wt-init.js            # Interactive config generator
│   ├── lib/
│   │   ├── utils.js                # Shared utilities (sanitize, env helpers, etc.)
│   │   └── debug.js                # Debug logging factory
│   ├── __tests__/                  # Jest test suites
│   └── [wt-autostop, wt-exec, wt-lan, wt-prune,
│        wt-rebuild-base, wt-service, wt-skip].js
├── worktree-dash-rs/               # Rust TUI dashboard (Ratatui + embedded PTY)
│   ├── Cargo.toml
│   ├── Makefile
│   └── src/
│       ├── main.rs                 # Entry point, event loop, --debug flag
│       ├── app/mod.rs              # App state, key handling, all actions
│       ├── config/mod.rs           # Config loader (shells to Node)
│       ├── worktree/mod.rs         # Discovery, env parsing, git metadata
│       ├── docker/mod.rs           # Container status, stats, PM2-in-docker
│       ├── pm2/mod.rs              # Host PM2 queries, isolated PM2_HOME
│       ├── pty/mod.rs              # Embedded PTY (portable-pty + alacritty_terminal)
│       ├── pty/split.rs            # Binary split tree, dot map
│       ├── ui/mod.rs               # Panel rendering (worktrees, services, details, tabs)
│       ├── ui/guide.rs             # Guide page (default right panel)
│       ├── ui/help.rs              # Keybindings help (?-key)
│       ├── ui/settings_tui.rs      # Settings editor (Shift+S)
│       ├── ui/overlay.rs           # Picker, confirm, input, notifications
│       ├── ui/splash.rs            # HeiHei splash screen
│       ├── ui/term_view.rs         # PTY → Ratatui cell renderer
│       ├── claude/mod.rs           # Usage API + OAuth token refresh
│       ├── daemon.rs              # Daemon process management (.wt/dev.pid, .wt/dev.log)
│       ├── init.rs                # Init wizard + stack auto-detection
│       ├── beads/mod.rs            # Task tracking via bd CLI
│       ├── settings/mod.rs         # Settings persistence (~/.wt/)
│       └── cmd.rs                  # Shared command execution
```

## Architecture

### Config-Driven Design

Every project-specific value comes from `wt.config.js`. Both Node.js and Go load the same file identically. The config defines: project name, Docker strategy, service ports, database, proxy, feature flags, env var names, and more.

**Two compose strategies:**

- **"generate"** — Creates a per-worktree `docker-compose.worktree.yml` from a template. One monolithic container running PM2 with multiple services inside.
- **"shared"** — Uses an existing `docker-compose.dev.yml` with env var substitution (`${BRANCH_SLUG}`, `${WEB_PORT}`, etc.). Multiple containers per worktree (one per service). Used by build-check.

### Config Loader (`worktree-flow/config.js`)

Walks upward from CWD to find `wt.config.js`. Deep-merges with defaults, resolves `{PREFIX}` templates in env vars, converts relative paths to absolute. Exports `load_config()` plus helpers: `container_name()`, `compose_project()`, `compute_offset()`, `compute_ports()`, `db_name()`, `domain_for()`, `get_compose_info()`, `worktree_var()`, etc. Shared utilities (path sanitization, env file manipulation, offset computation) live in `lib/utils.js`.

### Shared Compose Helper (`get_compose_info`)

For shared compose strategy, reads `.env.worktree` to get `BRANCH_SLUG` and port assignments, then returns `{ compose_file, project, slug, env }` — everything needed to run `docker compose -f <file> -p <project>` commands.

### Port Isolation

Each worktree gets a deterministic port offset computed from its path:
- **sha256**: hash → uint32 → mod range + min (default: 100–2100)
- **cksum**: char code sum → mod range + min (e.g., 1–99 for build-check)

All service base ports shift by this offset: `web: 3000 + 64 = 3064`.

### Worktree-Dash-RS (Rust TUI)

Single-process TUI with embedded PTY sessions — no tmux dependency. Built with Ratatui for rendering and alacritty_terminal + portable-pty for terminal emulation.

```
User's terminal
  └── wt (single Rust binary)
        ├── Left column (20%): Ratatui panels
        │     Notifications, Active Tabs, Worktrees
        │     Services, Details, Usage, Tasks
        └── Right area (80%): embedded PTY widgets
              Shell/Claude/Logs rendered via alacritty_terminal
              Split panes as recursive Ratatui layouts
```

Terminal sessions are PTY processes with output parsed through VTE into an alacritty_terminal grid, then rendered as Ratatui buffer cells. No tmux — all pane management is in-process.

**Split panes:** Binary split tree (SplitNode). `Shift+\` splits right, `Shift+-` splits below. Sessions within a group render as recursive `Layout::split()` with equal `Constraint::Ratio`. Each pane gets its own PTY with proper `SIGWINCH` on resize.

**Terminal mode:** `Ctrl+]` detaches from the focused session. `Ctrl+x` closes it. Number keys 1-9 jump directly to a session and enter terminal mode.

**Config-driven actions:** The picker adapts to the project type — Docker (start/stop/restart), local dev (start/restart), compiled tool (build/rebuild).

**Splash screen:** HeiHei ASCII art scaled to 77% of terminal, centered, with random dev quote. Shown during initial worktree discovery.

#### Dashboard Keybindings

**Global (any panel):**

| Key | Action | Feature gate |
|---|---|---|
| `Tab` / `Shift+Tab` | Cycle panels | — |
| `<` / `>` | Navigate panels | — |
| `w` | Jump to Worktrees panel | — |
| `s` | Jump to Services panel | — |
| `a` | Jump to Terminal (active) panel | — |
| `1`–`9` | Jump to tab N | — |
| `Shift+D` | Toggle Details panel | — |
| `Shift+K` | Toggle skip-worktree | `git.skipWorktree` |
| `Shift+U` | Toggle Claude usage | — |
| `Shift+L` | Toggle LAN mode | `lan` |
| `Shift+M` | Open maintenance picker | — |
| `Shift+T` | Open tasks overlay | — |

**Worktree panel:**

| Key | Action | Condition |
|---|---|---|
| `Enter` | Open action picker | — |
| `b` | Open bash shell | — |
| `c` | Open Claude Code | — |
| `d` | Toggle Details panel | — |
| `z` | Open local shell (zsh) | — |
| `l` | Open logs | — |
| `n` | Open create wizard | — |
| `i` | Show worktree info | — |
| `r` | Restart container/services | running |
| `t` | Stop container | running |
| `u` | Start container | stopped |

**Services panel:**

| Key | Action |
|---|---|
| `Enter` | Preview service logs (inline); press again to promote to full tab |
| `l` | Open service logs in tab |
| `r` | Restart service |

**Terminal panel:**

| Key | Action |
|---|---|
| `Enter` | Focus right pane (enter terminal) |
| `h` / `l` | Switch tabs left/right |
| `x` | Close current tab |

**Terminal mode** (while focused in a PTY session):

| Key | Action |
|---|---|
| `Ctrl+]` | Return focus to dashboard |
| `Ctrl+F` | Toggle fullscreen |
| `Ctrl+Q` | Quit dashboard (with confirmation) |

## Coding Conventions

### Node.js (worktree-flow)
- **CommonJS** — `require()` / `module.exports`
- **snake_case** for all functions: `find_docker_worktrees`, `read_env`, `compute_auto_offset`
- **Shared modules** — `lib/utils.js` (path sanitization, env file helpers, offset computation), `lib/debug.js` (debug logging factory)
- **Config pattern** — every script starts with:
  ```js
  const config_mod = require('./config');
  const config = config_mod.load_config({ required: false }) || null;
  ```
- **Legacy fallback** — `const value = config ? config.xxx : 'hardcoded_default';`
- **Shell execution** — `execSync` with `{ stdio: 'pipe', encoding: 'utf8' }`
- **No external deps** in runtime scripts (except `@clack/prompts` in wt-menu.js and wt-create.js). Jest is a devDependency for tests.

### Naming Conventions
- **Container**: `{name}-{alias}` (e.g., `bc-test-workflow-web`)
- **Compose project**: `{name}-{slug}` (e.g., `bc-test-workflow`)
- **Volume**: `{name}_{alias}_*` (underscores for Docker)
- **Database**: `{dbNamePrefix}{alias}` (e.g., `db_bulk_ship`)
- **Domain**: `{alias}.localhost` or configurable template
- **Env file**: `.env.worktree` (configurable via `env.filename`)

### Environment Variables
- **Project env vars** use a configurable prefix (e.g., `MYAPP_*`, `BC_*`)
- **Worktree vars** are always `WORKTREE_*` (name, alias, offset)
- **`{PREFIX}` template** in config gets replaced at load time

## Key Files to Understand

| If you want to understand... | Read these files |
|---|---|
| Config system | `worktree-flow/config.js`, `wt.config.schema.md` |
| Shared JS utilities | `worktree-flow/lib/utils.js`, `worktree-flow/lib/debug.js` |
| Worktree creation | `worktree-flow/wt-up.js` |
| Shared compose logic | `wt-up.js` (search "is_shared_compose"), `wt-status.js` (search "get_project_container_info") |
| Status monitoring | `worktree-flow/wt-status.js` |
| Interactive CLI | `worktree-flow/wt-menu.js`, `worktree-flow/wt-create.js` |
| Rust dashboard | `worktree-dash-rs/src/app/mod.rs`, `ui/mod.rs`, `config/mod.rs` |
| Worktree discovery | `worktree-dash-rs/src/worktree/mod.rs` |
| Daemon management | `worktree-dash-rs/src/daemon.rs` |
| Example configs | `wt.config.example.js`, check `~/dev/build-check/wt.config.js` |

## Git Conventions

- **NEVER include `Co-Authored-By` lines in commit messages.** Write commits as the developer, not as AI.
- Commit messages: imperative mood, concise subject line, optional body for context
- Do not commit `.env.worktree`, `docker-compose.worktree.yml`, `docker-compose.traefik.yml`, or `.docker-overrides/` — these are per-worktree runtime artifacts excluded via `.git/info/exclude`

## Building

The official `wt` binary is installed via Homebrew. When developing locally, build as `wt-dev`:

```bash
cd worktree-dash-rs && make dev
```

This produces `worktree-dash-rs/wt-dev`, symlinked from `/usr/local/bin/wt-dev`.

## Testing

- **Rust**: `cd worktree-dash-rs && cargo test`
- **Node.js**: `cd worktree-flow && npx jest`
- **Integration**: Open `wt-dev --debug` on a target project, test start/stop/logs lifecycle

## Adding Support for a New Project

1. Open `wt` in the project directory — the init wizard detects the stack and generates `wt.config.js`
2. Or create `wt.config.js` manually with `name`, `stack`, and optionally `dash.lifecycle` commands
