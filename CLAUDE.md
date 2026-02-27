# wt — Worktree Toolkit

A config-driven Docker worktree management system. Two components: **worktree-flow** (Node.js CLI) and **worktree-dash** (Go TUI dashboard). Attach to any project by dropping a `workflow.config.js` at the repo root.

## Project Structure

```
wt/
├── workflow.config.example.js      # Example config
├── workflow.config.schema.md       # Full schema documentation
├── worktree-flow/                  # Node.js CLI scripts
│   ├── config.js                   # Config loader + 15 helper functions
│   ├── dc.js                       # Interactive hub (@clack/prompts)
│   ├── dc-worktree-up.js           # Create/restart worktrees
│   ├── dc-worktree-down.js         # Stop/remove worktrees
│   ├── dc-status.js                # Status table
│   ├── dc-info.js                  # Detailed worktree info
│   ├── dc-logs.js                  # Docker compose logs
│   ├── dc-bash.js                  # Shell into container
│   ├── dc-restart.js               # Restart containers
│   ├── dc-create.js                # Interactive worktree creation wizard
│   ├── generate-docker-compose.js  # YAML generator (generate strategy)
│   ├── service-ports.js            # Port definitions + mode filtering
│   ├── workflow-init.js            # Interactive config generator
│   └── [dc-admin, dc-autostop, dc-build, dc-exec, dc-images-fix,
│        dc-lan, dc-prune, dc-rebuild-base, dc-seed, dc-service].js
└── worktree-dash/                  # Go TUI (Bubbletea)
    ├── main.go
    ├── go.mod / go.sum
    ├── Makefile
    └── internal/
        ├── app/                    # Model, Update, View, Actions, Keys
        ├── config/                 # Config loader (runs node to eval JS)
        ├── docker/                 # Container status, stats, services
        ├── worktree/               # Discovery, git metadata, types
        ├── terminal/               # Tmux session manager + pane layout
        ├── pm2/                    # PM2 service queries
        └── ui/                     # Panels, picker, overlays, layout
```

## Architecture

### Config-Driven Design

Every project-specific value comes from `workflow.config.js`. Both Node.js and Go load the same file identically. The config defines: project name, Docker strategy, service ports, database, proxy, feature flags, env var names, and more.

**Two compose strategies:**

- **"generate"** — Creates a per-worktree `docker-compose.worktree.yml` from a template. One monolithic container running PM2 with multiple services inside.
- **"shared"** — Uses an existing `docker-compose.dev.yml` with env var substitution (`${BRANCH_SLUG}`, `${WEB_PORT}`, etc.). Multiple containers per worktree (one per service). Used by build-check.

### Config Loader (`worktree-flow/config.js`)

Walks upward from CWD to find `workflow.config.js`. Deep-merges with defaults, resolves `{PREFIX}` templates in env vars, converts relative paths to absolute. Exports `load_config()` plus helpers: `container_name()`, `compose_project()`, `compute_offset()`, `compute_ports()`, `db_name()`, `domain_for()`, `get_compose_info()`, etc.

### Shared Compose Helper (`get_compose_info`)

For shared compose strategy, reads `.env.worktree` to get `BRANCH_SLUG` and port assignments, then returns `{ compose_file, project, slug, env }` — everything needed to run `docker compose -f <file> -p <project>` commands.

### Port Isolation

Each worktree gets a deterministic port offset computed from its path:
- **sha256**: hash → uint32 → mod range + min (default: 100–2100)
- **cksum**: char code sum → mod range + min (e.g., 1–99 for build-check)

All service base ports shift by this offset: `web: 3000 + 64 = 3064`.

### Worktree-Dash (Go TUI)

Tmux-native 2-pane layout: bubbletea control app (left pane, 20%) and native terminal session (right pane, 80%). The Go binary runs in two modes:

- **Outer mode** (`wt`): Shows a HeiHei splash screen, creates a tmux session with a 2-pane layout, runs itself inside pane 0, blocks until discovery completes, then attaches the user.
- **Inner mode** (`WT_INNER=1 wt`): Runs the bubbletea app inside pane 0, controls pane 1 via tmux commands.

```
User's terminal
  └── tmux attach -L wt-{pid} -t wt
        ├── pane 0 (left, 20%): bubbletea control app
        │     Worktree list, Details, Services
        │     Tab bar, Activity line
        └── pane 1 (right, 80%): native terminal session
              Real shell/claude/logs — native text selection
```

Terminal sessions use `swap-pane` to display in the right pane. Each session is a tmux window — `swap-pane` brings it into the viewport, switching tabs swaps one out and another in. This gives native text selection in the terminal pane.

Real-time container stats via `docker stats`. Integrated terminal sessions for shell, claude, logs, build.

**Fullscreen mode:** `tmux resize-pane -Z` zooms the right pane to fill the entire window. `prefix+f` toggles zoom. `prefix+q` returns to split view (auto-unzooms if zoomed).

**Tmux prefix key:** `Ctrl+]` — rarely conflicts with terminal apps. `prefix+q` returns focus to the dashboard. `prefix+f` toggles fullscreen. `prefix+1-9` jumps to tab N.

**AWS keys lifecycle:** When `features.awsCredentials` is enabled, `Shift+A` opens the aws-keys script in a terminal tab. The user pastes their 3-line AWS export block. When the script finishes (detected via sentinel file `$TMPDIR/wt-aws-keys-done`), the dashboard auto-closes the tab, reloads `~/.aws/credentials` into the process env, then restarts all affected services — local worktrees via `restart_local_services` (kill processes, close dev tab, open fresh dev tab with new env) and Docker worktrees via stop + start.

**Host-build lifecycle:** When a worktree is created with host-build mode, the dashboard automatically opens an esbuild watch tab after creation completes. Stopping a host-build worktree closes the build tab and stops the container.

**Creation wizard:** The `dc-create.js` wizard presents a three-way environment selector (Docker / Docker + Host Build / Local) and a mode selector when multiple service modes are defined. On restart, the previously selected mode is preserved by reading it from the existing compose file.

**Splash screen:** On launch, a HeiHei (Moana chicken) ASCII art splash screen is displayed, scaled to 77% of terminal size. The splash stays visible until worktree discovery completes (synchronized via `tmux wait-for`).

#### Dashboard Keybindings

**Global (any panel):**

| Key | Action | Feature gate |
|---|---|---|
| `Tab` / `Shift+Tab` | Cycle panels | — |
| `<` / `>` | Navigate panels | — |
| `w` | Jump to Worktrees panel | — |
| `s` | Jump to Services panel | — |
| `a` | Jump to Terminal (active) panel | — |
| `d` | Jump to Details panel | — |
| `1`–`9` | Jump to tab N | — |
| `Shift+A` | Open AWS keys | `awsCredentials` |
| `Shift+D` | Open database picker | — |
| `Shift+X` | Toggle admin accounts | `admin` |
| `Shift+L` | Toggle LAN mode | `lan` |
| `Shift+M` | Open maintenance picker | — |

**Worktree panel:**

| Key | Action | Condition |
|---|---|---|
| `Enter` | Open action picker | — |
| `b` | Open bash shell | — |
| `c` | Open Claude Code | — |
| `z` | Open local shell (zsh) | — |
| `l` | Open logs | — |
| `n` | Open create wizard | — |
| `i` | Show worktree info | — |
| `e` | Open esbuild watch | host-build + running |
| `r` | Restart container/services | running |
| `t` | Stop container/host build | running |
| `u` | Start container/host build | stopped |

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

**Tmux prefix bindings** (prefix = `Ctrl+]`):

| Key | Action |
|---|---|
| `prefix+q` | Return focus to dashboard (auto-unzooms) |
| `prefix+f` | Toggle fullscreen (zoom right pane) |
| `prefix+1`–`9` | Jump to tab N |

## Coding Conventions

### Node.js (worktree-flow)
- **CommonJS** — `require()` / `module.exports`
- **snake_case** for all functions: `find_docker_worktrees`, `read_env`, `compute_auto_offset`
- **Config pattern** — every script starts with:
  ```js
  const config_mod = require('./config');
  const config = config_mod.load_config({ required: false }) || null;
  ```
- **Legacy fallback** — `const value = config ? config.xxx : 'hardcoded_default';`
- **Shell execution** — `execSync` with `{ stdio: 'pipe', encoding: 'utf8' }`
- **No external deps** in scripts (except `@clack/prompts` in dc.js and dc-create.js)

### Go (worktree-dash)
- **snake_case** for unexported, **CamelCase** for exported
- **Config via Node.js** — `exec.Command("node", "-e", script)` to evaluate JS config
- **Package layout** — `internal/{app, config, docker, worktree, terminal, pm2, ui}`
- **Bubbletea pattern** — Model/Update/View + Cmd messages
- **Background refresh** — goroutines for stats (3s) and status (5s)

### Naming Conventions
- **Container**: `{name}-{alias}` (e.g., `bc-test-workflow-web`)
- **Compose project**: `{name}-{slug}` (e.g., `bc-test-workflow`)
- **Volume**: `{name}_{alias}_*` (underscores for Docker)
- **Database**: `{dbNamePrefix}{alias}` (e.g., `db_bulk_ship`)
- **Domain**: `{alias}.localhost` or configurable template
- **Env file**: `.env.worktree` (configurable via `env.filename`)

### Environment Variables
- **Project env vars** use a configurable prefix (e.g., `MYAPP_*`, `BC_*`)
- **Worktree vars** are always `WORKTREE_*` (name, alias, offset, services, host-build)
- **`{PREFIX}` template** in config gets replaced at load time

## Key Files to Understand

| If you want to understand... | Read these files |
|---|---|
| Config system | `worktree-flow/config.js`, `workflow.config.schema.md` |
| Worktree creation | `worktree-flow/dc-worktree-up.js` |
| Shared compose logic | `dc-worktree-up.js` (search "is_shared_compose"), `dc-status.js` (search "get_project_container_info") |
| Status monitoring | `worktree-flow/dc-status.js` |
| Interactive CLI | `worktree-flow/dc.js`, `worktree-flow/dc-create.js` |
| Go dashboard | `worktree-dash/internal/app/model.go`, `update.go`, `view.go` |
| Go config loader | `worktree-dash/internal/config/config.go` |
| Go discovery | `worktree-dash/internal/worktree/discover.go` |
| Example configs | `workflow.config.example.js`, check `~/dev/build-check/workflow.config.js` |

## Git Conventions

- **NEVER include `Co-Authored-By` lines in commit messages.** Write commits as the developer, not as AI.
- Commit messages: imperative mood, concise subject line, optional body for context
- Do not commit `.env.worktree`, `docker-compose.worktree.yml`, `docker-compose.traefik.yml`, or `.docker-overrides/` — these are per-worktree runtime artifacts excluded via `.git/info/exclude`

## Building

The official `wt` binary is installed via Homebrew. When developing locally, always build as `wt-dev` to avoid overwriting the installed version:

```bash
cd worktree-dash && go build -o wt-dev .
```

This produces `worktree-dash/wt-dev`. Never build as just `wt` — that conflicts with the Homebrew-managed binary at `/usr/local/bin/wt`.

## Testing

- **Go**: `cd worktree-dash && go test ./...`
- **Node.js**: No test framework — test by running commands against a real project with `workflow.config.js`
- **Integration**: Create a worktree on a target project, verify dc:status/dc:info/dc:logs/dc:restart/dc:down lifecycle

## Adding Support for a New Project

1. Create `workflow.config.js` at the project root (or run `node worktree-flow/workflow-init.js`)
2. Set `name`, `services.ports`, `docker.composeStrategy`, `docker.composeFile`
3. Add `dc:*` scripts to `package.json` pointing to `worktree-flow/dc-*.js`
4. Run `pnpm dc:create` or `node worktree-flow/dc-create.js` to create a worktree
