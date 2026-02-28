# wt — Worktree Toolkit

A config-driven system for managing Docker-powered [git worktrees](https://git-scm.com/docs/git-worktree). Run multiple feature branches simultaneously, each in its own isolated container environment with dedicated ports, database, and domain.

**Two components:**

- **worktree-flow** — Node.js CLI for creating, managing, and tearing down worktrees
- **worktree-dash** — Go TUI dashboard with real-time container stats and integrated terminal sessions

## How It Works

Drop a `workflow.config.js` at your project root. It tells wt everything about your project: services, ports, database, Docker strategy, features. Every script reads from this single config — no hardcoded values.

```
your-project/
  workflow.config.js    <-- config goes here
  src/
  ...

your-project-worktrees/
  feat-login/           <-- each worktree is a full checkout
  fix-payment/          <-- with its own Docker environment
  feat-search/
```

Each worktree gets:
- A git worktree (isolated checkout of a branch)
- Docker container(s) running your services
- Deterministic port offset (no collisions between worktrees)
- Its own database (cloned from a source DB)
- A domain (e.g., `feat-login.localhost`)

## Installation

```bash
brew tap elvisnm/wt
brew install wt
```

Requires Node.js (installed automatically as a dependency).

## Quick Start

### 1. Generate a config

```bash
node worktree-flow/workflow-init.js /path/to/your-project
```

This walks you through an interactive setup: project name, Docker strategy, services, ports, database, features. Outputs a `workflow.config.js`.

### 2. Create a worktree

```bash
# Interactive wizard
node worktree-flow/dc-create.js

# Or direct
node worktree-flow/dc-worktree-up.js feat/my-feature --from=origin/main --alias=my-feat
```

### 3. Manage worktrees

```bash
# Status of all worktrees
node worktree-flow/dc-status.js

# Shell into a running container
node worktree-flow/dc-bash.js my-feat

# View logs
node worktree-flow/dc-logs.js my-feat -f

# Restart
node worktree-flow/dc-restart.js my-feat

# Stop
node worktree-flow/dc-worktree-down.js my-feat

# Remove completely (containers, volumes, git worktree)
node worktree-flow/dc-worktree-down.js my-feat --remove
```

### 4. Launch the dashboard

```bash
wt
```

## Documentation

| Topic | Link |
|---|---|
| Configuration reference | [docs/configuration.md](docs/configuration.md) |
| CLI commands | [docs/commands.md](docs/commands.md) |
| Dashboard keybindings | [docs/dashboard.md](docs/dashboard.md) |
| Docker strategies | [docs/docker-strategies.md](docs/docker-strategies.md) |
| Adding wt to your project | [docs/getting-started.md](docs/getting-started.md) |
| Full config schema | [workflow.config.schema.md](workflow.config.schema.md) |

## Project Structure

```
wt/
  workflow.config.example.js      # Example config
  workflow.config.schema.md       # Full schema reference
  worktree-flow/                  # Node.js CLI scripts
    config.js                     # Config loader + helpers
    dc.js                         # Interactive hub menu
    dc-create.js                  # Interactive creation wizard
    dc-worktree-up.js             # Create/restart worktrees
    dc-worktree-down.js           # Stop/remove worktrees
    dc-status.js                  # Status table
    dc-info.js                    # Detailed worktree info
    dc-logs.js                    # Container logs
    dc-bash.js                    # Shell into container
    dc-restart.js                 # Restart container
    dc-seed.js                    # Database seed/drop/reset
    dc-service.js                 # PM2 service management
    dc-lan.js                     # LAN access toggle
    dc-skip-worktree.js           # Skip-worktree toggle
    dc-admin.js                   # Admin account toggle
    dc-autostop.js                # Stop idle containers
    dc-prune.js                   # Clean orphaned volumes
    dc-rebuild-base.js            # Rebuild base Docker image
    generate-docker-compose.js    # Compose YAML generator
    create-worktree-env.js        # Env file generator
    workflow-init.js              # Interactive config generator
    service-ports.js              # Port definitions & utilities
    config.js                     # Config loader & helpers
  worktree-dash/                  # Go TUI dashboard
    main.go
    internal/
      app/                        # Model, Update, View, Keys, Actions
      config/                     # Config loader (evals JS via node)
      docker/                     # Container status, stats
      worktree/                   # Discovery, types
      terminal/                   # PTY session manager
      pm2/                        # PM2 service queries
      ui/                         # Panels, picker, overlays
```

## Requirements

- **Node.js** >= 18
- **Go** >= 1.21 (for dashboard)
- **Docker** with Docker Compose v2
- **Git** with worktree support

## License

MIT - see [LICENSE](LICENSE).
