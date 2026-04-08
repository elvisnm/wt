# CLI Commands

## Dashboard

### `wt` — TUI dashboard

```bash
wt [--debug]
```

Launches the interactive terminal dashboard with a tmux 2-pane layout: control panel (left) and terminal sessions (right).

| Option | Description |
|---|---|
| `--debug` | Write timestamped debug logs to `$TMPDIR/wt-debug.log` |

The `--debug` flag instruments the entire app flow: discovery, services, worktree creation, key events, and tick refreshes. Both the Go dashboard and Node.js scripts (wt-create.js) log to the same file when `WT_DEBUG=1` is set.

```bash
# In one terminal, run with debug:
wt --debug

# In another terminal, tail the log:
tail -f $TMPDIR/wt-debug.log
```

## Node.js Scripts

All scripts are in `worktree-flow/`. Run them directly with `node` or via package.json scripts.

## Worktree Lifecycle

### `wt-create.js` — Interactive creation wizard

```bash
node worktree-flow/wt-create.js
```

Menu-driven worktree creation. Prompts for branch, base ref, mode, alias, database strategy, and options. Uses `@clack/prompts` for the UI.

Also supports restarting stopped worktrees from the same wizard.

### `wt-up.js` — Create or restart a worktree

```bash
node worktree-flow/wt-up.js <name> [options]
```

| Option | Description |
|---|---|
| `--from=<ref>` | Base ref for new branch (e.g., `origin/main`) |
| `--branch=<name>` | Create new branch from existing source |
| `--alias=<name>` | Short identifier for container/DB/domain |
| `--open` | Open worktree in Cursor editor |
| `--rebuild` | Force rebuild Docker image |
| `--shared-db` | Use shared database instead of isolated |
| `--poll` | Enable PM2 polling mode |
| `--lan` | Enable LAN access via nip.io domain |
| `--no-docker` | Create worktree without Docker (local PM2 mode) |

**What it does:**
1. Creates git worktree (new branch or checkout of existing)
2. Computes deterministic port offset from branch name
3. Writes `.env.worktree` with all port assignments and metadata
4. For generate strategy: creates `docker-compose.worktree.yml`
5. Starts Docker containers
6. Waits for health check (generate strategy)
7. Optionally sets up LAN, opens editor

**For `--no-docker` (local mode):**
1. Creates git worktree
2. Computes port offset
3. Copies `setup.copyFiles` from repo root
4. Writes `.env.worktree` with local defaults
5. Generates PM2 ecosystem config (if `services.pm2` configured)
6. Installs dependencies
7. Starts PM2 services with isolated `PM2_HOME`

### `wt-down.js` — Stop or remove a worktree

```bash
node worktree-flow/wt-down.js <name> [options]
```

| Option | Description |
|---|---|
| `--remove` | Remove volumes and worktree directory |
| `--delete-branch` | Also delete the local git branch |
| `--force` | Force remove even with uncommitted changes |

Without `--remove`, just stops the container. With `--remove`, tears down everything: containers, volumes, Traefik config, git worktree, and optionally the branch. For local worktrees, stops the PM2 daemon (`pm2 kill` with `PM2_HOME`).

### `wt-restart.js` — Restart container

```bash
node worktree-flow/wt-restart.js <name>
```

Restarts the Docker container. For shared compose, restarts all services in the project.

## Information & Monitoring

### `wt-status.js` — Status table

```bash
node worktree-flow/wt-status.js
```

Displays all worktrees with:
- Container status (running/stopped/not found)
- Health state
- CPU and memory usage
- Service mode
- Uptime

For shared compose projects, aggregates stats across all service containers.

### `wt-info.js` — Detailed worktree info

```bash
node worktree-flow/wt-info.js <name>
```

Shows: alias, branch, container name, port assignments, URLs, database name, service mode, container status, and quick links.

### `wt-logs.js` — Container logs

```bash
node worktree-flow/wt-logs.js <name> [options]
```

| Option | Description |
|---|---|
| `-s <service>` | Specific service (PM2 name or compose service) |
| `-f` / `--follow` | Stream logs in real-time |

For generate strategy, can show PM2 per-service logs. For shared compose, uses `docker compose logs`.

### `wt-shell.js` — Shell into container

```bash
node worktree-flow/wt-shell.js <name>
```

Opens an interactive bash shell inside the running container. For shared compose, execs into the primary service container.

### `wt-exec.js` — Run command in container

```bash
node worktree-flow/wt-exec.js <name> <command...>
```

Runs an arbitrary command inside the container and returns output.

## Service Management

### `wt-service.js` — PM2 service control

```bash
node worktree-flow/wt-service.js <name> <action> <service>
```

Actions: `start`, `stop`, `restart`. Manages individual PM2 services inside a Docker container or local worktree (using `PM2_HOME` for isolated worktrees).

### `wt-lan.js` — Toggle LAN access

```bash
node worktree-flow/wt-lan.js <name> [--off]
```

Detects LAN IP and builds a nip.io domain (e.g., `my-feat.192.168.1.100.nip.io`). Updates `.env.worktree` and restarts the container. Requires `features.lan`.

### `wt-skip.js` — Toggle skip-worktree flags

```bash
node worktree-flow/wt-skip.js apply <name>    # Apply skip-worktree
node worktree-flow/wt-skip.js remove <name>   # Remove skip-worktree
node worktree-flow/wt-skip.js list <name>     # List skipped files
```

Applies or removes `git update-index --skip-worktree` on paths configured in `git.skipWorktree`. Hides noisy local-only changes (build artifacts, lock files, etc.) from `git status`. Supports both local and Docker worktrees. Also auto-applied on worktree creation when configured.

## Maintenance

### `wt-autostop.js` — Stop idle containers

```bash
node worktree-flow/wt-autostop.js [options]
```

| Option | Description |
|---|---|
| `--hours=<n>` | Idle threshold in hours (default: 2) |
| `--dry-run` | Preview which containers would stop |

Stops containers with CPU usage below 1% for the specified duration.

### `wt-prune.js` — Clean orphaned volumes

```bash
node worktree-flow/wt-prune.js [--dry-run]
```

Finds Docker volumes belonging to deleted worktrees and removes them.

### `wt-rebuild-base.js` — Rebuild base image

```bash
node worktree-flow/wt-rebuild-base.js
```

Rebuilds the prebaked Docker image defined in `docker.baseImage`.

## Interactive Hub

### `wt-menu.js` — Menu-driven CLI

```bash
node worktree-flow/wt-menu.js
```

An interactive menu (powered by `@clack/prompts`) that groups all commands into categories:

- **Create** — new worktree or restart stopped
- **Manage** — pick a worktree, then: info, logs, restart, stop, shell, services, remove
- **Config** — LAN access toggle
- **Maintenance** — prune volumes, autostop, rebuild base

## Config Generation

### `wt-init.js` — Generate wt.config.js

```bash
wt init [target-dir]                # auto-detect and generate config
wt init --custom=<name>             # copy wt.config.js.<name> and auto-personalize
wt init --custom=<name> --force     # overwrite existing config
wt init --personalize               # update machine-specific values in existing config
```

| Flag | Description |
|---|---|
| `--custom=<name>` | Copy `wt.config.js.<name>` template and auto-personalize (claude path) |
| `--personalize` | Update machine-specific values in an existing config |
| `--force` | Overwrite existing `wt.config.js` |
| `--dry-run` | Print what would be generated without writing |

**From a template:** Use `--custom=<name>` when the project ships a committed template (e.g., `wt.config.js.myteam`). This copies the template and auto-detects machine-specific values like the Claude binary path.

**From scratch:** Without flags, runs an interactive wizard that detects the project type (Node.js, Go, Rust, Python), finds docker-compose files, and generates a complete config.
