# CLI Commands

All commands are Node.js scripts in `worktree-flow/`. Run them directly with `node` or via package.json scripts.

## Worktree Lifecycle

### `dc-create.js` — Interactive creation wizard

```bash
node worktree-flow/dc-create.js
```

Menu-driven worktree creation. Prompts for branch, base ref, mode, alias, database strategy, and options. Uses `@clack/prompts` for the UI.

Also supports restarting stopped worktrees from the same wizard.

### `dc-worktree-up.js` — Create or restart a worktree

```bash
node worktree-flow/dc-worktree-up.js <name> [options]
```

| Option | Description |
|---|---|
| `--from=<ref>` | Base ref for new branch (e.g., `origin/main`) |
| `--branch=<name>` | Create new branch from existing source |
| `--alias=<name>` | Short identifier for container/DB/domain |
| `--mode=<mode>` | Service mode (from `services.modes` in config) |
| `--open` | Open worktree in Cursor editor |
| `--rebuild` | Force rebuild Docker image |
| `--shared-db` | Use shared database instead of isolated |
| `--seed` | Seed isolated DB from shared after creation |
| `--poll` | Enable PM2 polling mode |
| `--lan` | Enable LAN access via nip.io domain |
| `--host-build` | Run frontend build on host |
| `--no-host-build` | Disable host-build mode |

**What it does:**
1. Creates git worktree (new branch or checkout of existing)
2. Computes deterministic port offset from branch name
3. Writes `.env.worktree` with all port assignments and metadata
4. For generate strategy: creates `docker-compose.worktree.yml`
5. Starts Docker containers
6. Waits for health check (generate strategy)
7. Optionally seeds database, sets up LAN, opens editor

### `dc-worktree-down.js` — Stop or remove a worktree

```bash
node worktree-flow/dc-worktree-down.js <name> [options]
```

| Option | Description |
|---|---|
| `--remove` | Remove volumes and worktree directory |
| `--delete-branch` | Also delete the local git branch |
| `--force` | Force remove even with uncommitted changes |

Without `--remove`, just stops the container. With `--remove`, tears down everything: containers, volumes, Traefik config, git worktree, and optionally the branch.

### `dc-restart.js` — Restart container

```bash
node worktree-flow/dc-restart.js <name>
```

Restarts the Docker container. For shared compose, restarts all services in the project.

## Information & Monitoring

### `dc-status.js` — Status table

```bash
node worktree-flow/dc-status.js
```

Displays all worktrees with:
- Container status (running/stopped/not found)
- Health state
- CPU and memory usage
- Service mode
- Uptime

For shared compose projects, aggregates stats across all service containers.

### `dc-info.js` — Detailed worktree info

```bash
node worktree-flow/dc-info.js <name>
```

Shows: alias, branch, container name, port assignments, URLs, database name, service mode, container status, and quick links.

### `dc-logs.js` — Container logs

```bash
node worktree-flow/dc-logs.js <name> [options]
```

| Option | Description |
|---|---|
| `-s <service>` | Specific service (PM2 name or compose service) |
| `-f` / `--follow` | Stream logs in real-time |

For generate strategy, can show PM2 per-service logs. For shared compose, uses `docker compose logs`.

### `dc-bash.js` — Shell into container

```bash
node worktree-flow/dc-bash.js <name>
```

Opens an interactive bash shell inside the running container. For shared compose, execs into the primary service container.

### `dc-exec.js` — Run command in container

```bash
node worktree-flow/dc-exec.js <name> <command...>
```

Runs an arbitrary command inside the container and returns output.

## Database Operations

### `dc-seed.js` — Seed, drop, or reset database

```bash
node worktree-flow/dc-seed.js <name> [options]
```

| Option | Description |
|---|---|
| (default) | Seed from shared DB (`database.defaultDb`) |
| `--drop` | Drop the worktree's database |
| `--reset` | Drop + re-seed |

Uses the `database.seedCommand` and `database.dropCommand` templates from config. Supports MongoDB, PostgreSQL, MySQL, and Supabase.

### `dc-images-fix.js` — Fix image URLs

```bash
node worktree-flow/dc-images-fix.js [options]
```

| Option | Description |
|---|---|
| `--db=<name>` | Target database |
| `--dry-run` | Preview changes |

Converts absolute image URLs to relative paths in the database. Only available when `features.imagesFix` is enabled.

## Service Management

### `dc-service.js` — PM2 service control

```bash
node worktree-flow/dc-service.js <name> <action> <service>
```

Actions: `start`, `stop`, `restart`. Manages individual PM2 services inside a generate-strategy container.

### `dc-admin.js` — Toggle admin accounts

```bash
node worktree-flow/dc-admin.js set [--name=<name>]
node worktree-flow/dc-admin.js unset [--name=<name>]
```

Toggles admin account access in `.env.worktree`. Without `--name`, applies to all running worktrees. Requires `features.admin.enabled`.

### `dc-lan.js` — Toggle LAN access

```bash
node worktree-flow/dc-lan.js <name> [--off]
```

Detects LAN IP and builds a nip.io domain (e.g., `my-feat.192.168.1.100.nip.io`). Updates `.env.worktree` and restarts the container. Requires `features.lan`.

## Maintenance

### `dc-autostop.js` — Stop idle containers

```bash
node worktree-flow/dc-autostop.js [options]
```

| Option | Description |
|---|---|
| `--hours=<n>` | Idle threshold in hours (default: 2) |
| `--dry-run` | Preview which containers would stop |

Stops containers with CPU usage below 1% for the specified duration.

### `dc-prune.js` — Clean orphaned volumes

```bash
node worktree-flow/dc-prune.js [--dry-run]
```

Finds Docker volumes belonging to deleted worktrees and removes them.

### `dc-rebuild-base.js` — Rebuild base image

```bash
node worktree-flow/dc-rebuild-base.js
```

Rebuilds the prebaked Docker image defined in `docker.baseImage`.

## Interactive Hub

### `dc.js` — Menu-driven CLI

```bash
node worktree-flow/dc.js
```

An interactive menu (powered by `@clack/prompts`) that groups all commands into categories:

- **Create** — new worktree or restart stopped
- **Manage** — pick a worktree, then: info, logs, restart, stop, shell, services, remove
- **Database** — seed, reset, drop, fix-images
- **Admin** — toggle admin accounts, LAN access
- **Maintenance** — prune volumes, autostop, rebuild base

## Config Generation

### `workflow-init.js` — Generate workflow.config.js

```bash
node worktree-flow/workflow-init.js [target-dir]
```

Interactive wizard that detects your project type and generates a complete `workflow.config.js`. Detects: Node.js, Go, Rust, Python. Finds existing docker-compose files. Prompts for all config options.
