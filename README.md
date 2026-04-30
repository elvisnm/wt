# wt — Worktree Toolkit

A config-driven system for managing isolated [git worktrees](https://git-scm.com/docs/git-worktree). Run multiple feature branches simultaneously — each in its own Docker container or local PM2 environment with dedicated ports, database, and domain.

**Two components:**

- **worktree-flow** — Node.js CLI for creating, managing, and tearing down worktrees
- **worktree-dash-rs** — Rust TUI dashboard with embedded terminal sessions, split panes, and real-time container stats (replaces the Go dashboard)

## How It Works

Drop a `wt.config.js` at your project root. It tells wt everything about your project: services, ports, database, Docker strategy, features. Every script reads from this single config — no hardcoded values.

```
your-project/
  wt.config.js    <-- config goes here
  src/
  ...

your-project-worktrees/
  feat-login/           <-- each worktree is a full checkout
  fix-payment/          <-- with its own Docker environment
  feat-search/
```

Each worktree gets:
- A git worktree (isolated checkout of a branch)
- Isolated PM2 daemon (local mode) or Docker container(s)
- Deterministic port offset (no collisions between worktrees)
- Its own database (cloned from a source DB)
- A domain (e.g., `feat-login.localhost`)

## Installation

```bash
brew tap elvisnm/wt
brew install wt
```

Requires Node.js (installed automatically as a dependency).

## Updating

```bash
brew update && brew upgrade wt
```

## Quick Start

### 1. Generate a config

```bash
node worktree-flow/wt-init.js /path/to/your-project
```

This walks you through an interactive setup: project name, Docker strategy, services, ports, database, features. Outputs a `wt.config.js`.

### 2. Create a worktree

```bash
# Interactive wizard
node worktree-flow/wt-create.js

# Or direct
node worktree-flow/wt-up.js feat/my-feature --from=origin/main --alias=my-feat

# Or without Docker (local PM2)
node worktree-flow/wt-up.js feat/my-feature --from=origin/main --no-docker
```

### 3. Manage worktrees

```bash
# Status of all worktrees
node worktree-flow/wt-status.js

# Shell into a running container
node worktree-flow/wt-shell.js my-feat

# View logs
node worktree-flow/wt-logs.js my-feat -f

# Restart
node worktree-flow/wt-restart.js my-feat

# Stop
node worktree-flow/wt-down.js my-feat

# Remove completely (containers, volumes, git worktree)
node worktree-flow/wt-down.js my-feat --remove
```

### 4. Launch the dashboard

```bash
wt
```

## Documentation

| Topic | Link |
|---|---|
| Getting started | [docs/getting-started.md](docs/getting-started.md) |
| Dashboard guide | [docs/dashboard.md](docs/dashboard.md) |
| Configuration reference | [docs/configuration.md](docs/configuration.md) |
| Docker strategies | [docs/docker-strategies.md](docs/docker-strategies.md) |
| Full config schema | [wt.config.schema.md](wt.config.schema.md) |

## Beads Task Wiring

The dashboard ships a DAG tab that visualizes [beads](https://github.com/elvisnm/beads) tasks as a dependency graph. The tab is only as useful as the wiring of the underlying `bd` issues: blocking edges (`bd dep add`) should live only within a phase between real work items, never chain epics across phases, and never gate a sub-feature behind its own parent epic. The DAG groups cards by priority into columns, renders epics as label headers, and colors cards Ready (cyan) or Blocked (red) based on whether their open dependencies have closed.

For projects that use bd for orchestration (the blackbox-style "epic per phase, sub-features under each epic" pattern), the `/wire-beads` Claude Code skill is the canonical reference and audit tool. It explains the status-vs-readiness model, the `blocks` vs `parent-child` distinction, the within-phase linking rule, the anti-patterns to avoid, and ships an audit workflow that lists every current edge by type and flags violations. Invoke it before creating new bd issues, before adding deps, or when the DAG is showing tasks in an unexpected color (a column uniformly Ready means wiring is missing; uniformly Blocked behind one card means over-coupled). The skill lives in `~/dev/blackbox/.claude/skills/wire-beads/SKILL.md` and can be copied into any project that follows the same pattern.

## Project Structure

```
wt/
  wt.config.example.js      # Example config
  wt.config.schema.md       # Full schema reference
  worktree-flow/                  # Node.js CLI scripts
    config.js                     # Config loader + helpers
    wt-menu.js                         # Interactive hub menu
    wt-create.js                  # Interactive creation wizard
    wt-up.js             # Create/restart worktrees
    wt-down.js           # Stop/remove worktrees
    wt-status.js                  # Status table
    wt-info.js                    # Detailed worktree info
    wt-logs.js                    # Container logs
    wt-shell.js                    # Shell into container
    wt-restart.js                 # Restart container
    wt-service.js                 # PM2 service management
    wt-lan.js                     # LAN access toggle
    wt-skip.js           # Skip-worktree toggle
    wt-autostop.js                # Stop idle containers
    wt-prune.js                   # Clean orphaned volumes
    wt-rebuild-base.js            # Rebuild base Docker image
    generate-docker-compose.js    # Compose YAML generator
    create-worktree-env.js        # Env file generator
    wt-init.js              # Interactive config generator
    service-ports.js              # Port definitions & utilities
    config.js                     # Config loader & helpers
  worktree-dash-rs/               # Rust TUI dashboard (Ratatui)
    src/
      main.rs                     # Entry point, event loop
      app/                        # App state, key handling, actions
      config/                     # Config loader (evals JS via node)
      docker/                     # Container status, stats
      worktree/                   # Discovery, git metadata, types
      pty/                        # Embedded PTY + split pane tree
      pm2/                        # PM2 service queries
      ui/                         # Panels, guide, help, settings, overlays
      claude/                     # Usage API integration
      beads/                      # Task tracking integration
      settings/                   # Settings persistence (~/.wt/)
```

## Requirements

- **Node.js** >= 18
- **Rust** >= 1.80 (for dashboard, or use pre-built binary via Homebrew)
- **Docker** with Docker Compose v2 (for Docker worktrees; not needed for local mode)
- **Git** with worktree support

## License

MIT - see [LICENSE](LICENSE).
