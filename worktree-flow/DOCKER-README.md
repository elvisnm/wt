# Docker Worktrees - Full Reference

Comprehensive documentation for the Docker worktree system. For a quick start guide, see [README.md](./README.md).

## Architecture

### Overview

Each Docker worktree is an isolated dev environment: its own container, `.localhost` domain, MongoDB database, and Redis key prefix. All worktrees share the host's MongoDB, Redis, and Traefik instances.

```
Host machine
├── myapp-infra/                       # shared infrastructure
│   ├── mongo (port 27017)
│   ├── redis (port 6379)
│   └── traefik (Traefik, port 80)
│
├── myapp/                             # main repo (where you run dc:* commands)
│   └── node_modules/                  # used by host-build symlink
│
└── myapp-worktrees/                 # sibling directory
    ├── feat-my-feature/               # one worktree
    │   ├── .env.worktree
    │   ├── .docker-overrides/
    │   ├── docker-compose.worktree.yml
    │   └── node_modules/              # Docker volume (normal) or symlink (host-build)
    └── feat-another/                  # another worktree
```

### Shared Services

Run once on the host via `myapp-infra`, shared by all containers:

| Service | Container | Address inside Docker |
|---------|-----------|----------------------|
| MongoDB | `mongo` | `mongo:27017` |
| Redis | `redis` | `redis:6379` |
| Traefik | `traefik` | Port 80 on host |

All worktree containers connect via the `myapp-infra_default` Docker network.

### Per-Container Services

Each container runs its own PM2 services via `pnpm dev`. The number of services depends on the **service mode**.

**Minimal mode** (`--mode=minimal`, 7 services) — core services for the app to function:

| Service | Base Port | Example (offset=200) |
|---------|-----------|---------------------|
| socket_server | 3000 | 3200 |
| app | 3001 | 3201 |
| api | 3004 | 3204 |
| cache_server | 3008 | 3208 |
| order_table_server | 3012 | 3212 |
| inventory_table_server | 3013 | 3213 |
| admin_server | 3050 | 3250 |

**Full mode** (default, all services) — adds sync, shipping, jobs, insights:

| Service | Base Port | Example (offset=200) |
|---------|-----------|---------------------|
| socket_server | 3000 | 3200 |
| app | 3001 | 3201 |
| sync | 3002 | 3202 |
| ship_server | 3003 | 3203 |
| api | 3004 | 3204 |
| job_server | 3005 | 3205 |
| www | 3006 | 3206 |
| cache_server | 3008 | 3208 |
| insights_server | 3010 | 3210 |
| order_table_server | 3012 | 3212 |
| inventory_table_server | 3013 | 3213 |
| admin_server | 3050 | 3250 |
| livereload | 53099 | 53299 |

Minimal mode is useful for lighter workloads — the app requires `cache_server` for replication data and the table servers for order/inventory pages. Use `dc:service` to start extras on-demand without changing the mode.

The port map is defined in `scripts/worktree/service-ports.js` and matches `globals.js`. Local dev (`pnpm dev`) also supports modes: `pnpm dev --minimal`, `pnpm dev --full`.

## Traefik Routing

Each worktree gets a `.localhost` subdomain routed by Traefik:

- `http://<alias>.localhost/` -> container app server (port 3001)
- `http://<alias>.localhost/socket.io/` -> container socket server (port 3000)

`.localhost` subdomains resolve to `127.0.0.1` in all modern browsers — no `/etc/hosts` needed. This enables **multiple worktrees running simultaneously** since each has a unique domain.

Traefik dynamic config files are generated per worktree in `myapp-infra/traefik/dynamic/<alias>.yml` and picked up automatically by the Traefik file provider.

**Example Traefik config:**

```yaml
http:
  routers:
    bulk-ship-app:
      rule: "Host(`bulk-ship.localhost`)"
      entryPoints: [web]
      service: bulk-ship-app
    bulk-ship-socket:
      rule: "Host(`bulk-ship.localhost`) && PathPrefix(`/socket.io`)"
      entryPoints: [web]
      service: bulk-ship-socket
      priority: 100
  services:
    bulk-ship-app:
      loadBalancer:
        servers:
          - url: "http://host.docker.internal:4875"
    bulk-ship-socket:
      loadBalancer:
        servers:
          - url: "http://host.docker.internal:4874"
```

## Port Configuration

### Docker Worktrees

Services inside the container bind to **base ports** (3000, 3001, etc.). Docker maps them to **offset ports** on the host:

```yaml
# docker-compose.worktree.yml
ports:
  - "4875:3001"   # host:container — offset is unique per worktree
```

**Port offset computation:** Offsets are computed deterministically from the worktree path using a SHA-256 hash. If the computed offset collides with ports already in use, `find_free_offset()` in `service-ports.js` scans listening TCP ports via `lsof`/`ss` and increments until all ports are free.

**Why `WORKTREE_HOST_PORT_OFFSET` (not `WORKTREE_PORT_OFFSET`):** The offset is stored in `.env.worktree` as `WORKTREE_HOST_PORT_OFFSET`. This name is intentional — the container's `port-config.js` only reads `WORKTREE_PORT_OFFSET`. If the offset was named `WORKTREE_PORT_OFFSET`, services inside the container would read it and bind to `base + offset` ports (e.g. 3001+1874=4875) instead of base ports (3001). Since Docker maps `host:4875 -> container:3001`, nothing would be listening on container port 3001 and the app would fail.

Use `pnpm dc:info <name>` to see exact ports for any worktree.

### Native Worktrees

Port offsetting is handled by the application via environment variables:

```
.env.worktree (WORKTREE_PORT_OFFSET=N)
  -> scripts/port-config.js (resolveOffset())
    -> globals.js (getPort(name, basePort) = basePort + offset)
      -> each PM2 process binds to its computed port
```

- `WORKTREE_PORT_OFFSET`: shift each server port by this offset
- `WORKTREE_PORT_BASE`: alternative — offset is computed as `WORKTREE_PORT_BASE - 3000`

Ports are only offset when `{PREFIX}_ENV=development`. The livereload server (port `53099`) is also shifted. PM2 process names include `WORKTREE_NAME` when set.

## Data Isolation

Each worktree gets an isolated MongoDB database and Redis key prefix by default:

| Resource | Shared (`--shared-db`) | Isolated (default) |
|----------|----------------------|-------------------|
| MongoDB | `db` | `db_<alias>` (e.g. `db_bypass`) |
| Redis | no prefix | `<alias>:` prefix (e.g. `bypass:`) |

The alias is stored in `.env.worktree` as `WORKTREE_ALIAS` and persists across restarts. Auto-derived from branch name (e.g. `feat/bulk-ship-api` -> `bulk-ship`) or set explicitly with `--alias`.

Use `db:seed` to populate an isolated database from the shared `db`. Protected databases (`db`, `admin`, `local`, `config`) can never be dropped.

## What Gets Generated

`dc:up` creates:

1. **Git worktree** in a sibling `<repo>-worktrees/` folder
2. **`.env.worktree`** with Docker-specific env vars (see Env Var Reference below)
3. **`.docker-overrides/`** with current `ecosystem.dev.config.js` and `scripts/dev.js` from main repo
4. **`docker-compose.worktree.yml`** with port mappings (filtered by mode), volumes, overrides, and network config
5. **Pre-baked image** (`myapp-dev:latest`) — auto-builds via `dc:rebuild-base` if missing
6. **Container** — on first boot, the entrypoint seeds `node_modules` from the pre-baked image via rsync (~10s). Subsequent starts are instant

### Per-Container Volumes

| Volume | Purpose |
|--------|---------|
| `{name}_{alias}_node_modules` | Linux-compiled node_modules (persisted across restarts) |

### Dev Script Overrides

Worktree branches may be forked from older code that lacks service mode filtering. `dc:up` copies `ecosystem.dev.config.js` and `scripts/dev.js` from the main repo into `.docker-overrides/`. They are volume-mounted read-only into the container, overriding the branch's versions:

```yaml
volumes:
  - ./.docker-overrides/ecosystem.dev.config.js:/app/ecosystem.dev.config.js:ro
  - ./.docker-overrides/scripts/dev.js:/app/scripts/dev.js:ro
```

Updated on every `dc:up`, so overrides stay current with the main repo.

### Upstream Tracking

Worktrees configure `branch.<name>.remote` and `branch.<name>.merge` so `git push` and lazygit target `origin/<branch-name>` instead of the base ref. Prevents accidental pushes to the wrong remote branch.

## How Host-Build Mode Works

Host-build runs esbuild on the host instead of inside Docker for ~7x faster frontend rebuilds (native file I/O vs Docker's VirtioFS).

### The Problem

Inside Docker, esbuild watches the entire `frontend/` directory. File I/O goes through VirtioFS (Docker Desktop on macOS), which is significantly slower than native I/O. A full frontend rebuild that takes ~2s natively can take 15s+ through Docker.

### The Solution

Split the work: the container runs PM2 services (API, socket, etc.) while esbuild runs natively on the host.

```
Host                              Container
┌─────────────────────┐           ┌────────────────────────┐
│ pnpm dc:build       │           │ pnpm dev --no-build    │
│  -> esbuild watch   │           │  -> PM2 services only  │
│  -> writes build/   │──bind──>  │  -> serves build/      │
│                     │  mount    │  -> API, socket, etc.  │
│ node_modules/ ──────│──symlink  │  node_modules/ (volume)│
└─────────────────────┘           └────────────────────────┘
```

### Step by Step

1. **`dc:up --host-build`** enables host-build mode:
   - Stops the running container (Docker holds a lock on `node_modules/`)
   - Removes the `node_modules/` directory in the worktree
   - Creates a **symlink** to the main repo's `node_modules/`
   - Sets `WORKTREE_HOST_BUILD=true` in `.env.worktree`
   - Generates compose with `command: ["pnpm", "dev", "--no-build"]`
   - Starts the container with `--force-recreate`

2. **`dc:build`** runs the frontend build on the host:
   - Reads `.env.worktree` for env vars (`WORKTREE_HOST_PORT_OFFSET` for livereload port)
   - Applies frontend overrides from `.docker-overrides/frontend/` (e.g. `cache_socket.js`)
   - Runs `build.js develop --watch` from the **main repo** (not the worktree branch's version)
   - Restores overridden files on exit (SIGINT/SIGTERM)

3. **`dc:up --no-host-build`** reverts to normal:
   - Removes the `node_modules` symlink
   - Removes `WORKTREE_HOST_BUILD` from `.env.worktree`
   - Generates compose with `command: ["pnpm", "dev"]` (includes esbuild)
   - Force-recreates the container (Docker volume provides `node_modules`)

### Why the Main Repo's build.js

`dc:build` uses the main repo's `scripts/deployment_scripts/build.js`, not the worktree branch's. This is because older branches use `http://${global.local_ip}:${global.servers.app.port}/assets/` for asset URLs (includes the port number). This breaks with Traefik routing where assets are served via the `.localhost` domain on port 80. The main repo's version uses `${global.servers.app.url}assets/` which works correctly.

The build script runs with `cwd` set to the worktree, so it reads the worktree's `globals.js`, `.env.worktree`, and `frontend/` directory. Only the build script itself comes from the main repo.

### node_modules Coexistence

The `node_modules` symlink and Docker named volume serve different consumers:

- **Host (esbuild):** Sees the symlink -> resolves to main repo's `node_modules/` -> esbuild can require modules
- **Container (PM2):** The Docker named volume (`{name}_{alias}_node_modules`) overlays the symlink at `/app/node_modules` -> PM2 services use Linux-compiled modules from the volume

Both work simultaneously without conflict.

### Frontend Overrides

Some frontend files in the worktree branch may have incorrect behavior for Docker (e.g. `cache_socket.js` using `.includes('localhost')` instead of exact match `=== 'localhost'`). The `.docker-overrides/frontend/` directory contains corrected versions. `dc:build` applies these before building and restores originals on exit.

## How LAN Mode Works

LAN mode makes a worktree accessible to any device on the same network using [nip.io](https://nip.io) wildcard DNS.

### How It Works

1. **`dc:lan <name>`** detects your LAN IP (e.g. `10.1.10.118`)
2. Generates a nip.io domain: `<alias>.<ip>.nip.io` (e.g. `bulk-ship.10.1.10.118.nip.io`)
3. Updates Traefik config to accept both `.localhost` and `.nip.io` domains
4. Updates `{PREFIX}_LOCAL_IP` and `{PREFIX}_APP_URL` in `.env.worktree` to use the nip.io domain (so asset URLs are LAN-accessible)
5. Sets `{PREFIX}_LAN_DOMAIN` in `.env.worktree`
6. Recreates the container to pick up the new env vars

nip.io is a free wildcard DNS service: `anything.10.1.10.118.nip.io` resolves to `10.1.10.118`. No DNS config or `/etc/hosts` changes needed on any device.

### Limitations

- Cookies are domain-specific — logging in on `.localhost` doesn't carry over to `.nip.io`. Log in again on the LAN URL
- Stripe requires HTTPS for non-localhost domains (expected error, not a bug)
- If your LAN IP changes (e.g. switching Wi-Fi networks), run `dc:lan` again

### Disabling

`dc:lan <name> --off` reverts everything:
- Traefik config back to `.localhost` only
- Env vars back to `<alias>.localhost`
- Removes `{PREFIX}_LAN_DOMAIN`
- Recreates the container

## Command Reference

Full parameter details for each command. For quick usage, see [README.md](./README.md).

### `pnpm dash`

Full terminal UI (TUI) dashboard for managing worktrees, containers, and services. Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) in Go. Requires the `worktree-dash` repo cloned as a sibling directory — see [Installing the Dashboard](./README.md#installing-the-dashboard).

```
pnpm dash
```

### `pnpm dc`

Interactive CLI hub for all Docker worktree operations. Shows existing worktrees with status, then presents a menu:

```
pnpm dc
```

**Menu options:**
- **Create** — launches the guided creation flow (same as `dc:create`)
- **Manage** — pick a worktree, then: view info, logs (with service picker), restart, stop, open bash, manage PM2 services, or remove
- **Database** — seed, drop, reset, or fix image URLs with worktree picker and confirmations
- **Admin & config** — toggle admin access (single or all worktrees), toggle LAN access
- **Maintenance** — prune orphaned volumes, stop idle containers, rebuild base image

No flags needed. Ctrl+C exits cleanly at any step. The hub delegates to existing `dc:*` scripts — no logic duplication.

### `pnpm dc:create`

Interactive guided worktree creation. Walks you through branch selection, alias, service mode, database, and extras step-by-step. Shows a summary with the equivalent `dc:up` command before executing. Also accessible from `pnpm dc` → "Create a new worktree".

```
pnpm dc:create
```

Three paths:
1. **Create a new branch** — prompts for branch name (must use `feat/`, `fix/`, etc. prefix) and base ref
2. **Check out a remote branch** — fetches origin, lets you search and select from remote branches
3. **Restart a stopped worktree** — lists stopped worktrees and restarts the selected one

No flags needed — all options are collected interactively. Ctrl+C exits cleanly at any step.

### `pnpm dc:up`

Creates a worktree and starts a Docker-isolated environment. If the worktree already exists, restarts it and regenerates the compose file with any new flags.

```
pnpm dc:up <name> [flags]
```

**Parameters:**

| Flag | Default | Description |
|------|---------|-------------|
| `--from=<ref>` | — | Base ref to create the branch from |
| `--branch=<new>` | — | Create a new branch name using `<name>` as source |
| `--alias=<short>` | auto from branch | Short name for container, DB, and domain |
| `--mode=<mode>` | `full` | Service mode: `full` (all) or `minimal` (7) |
| `--shared-db` | off (isolated db) | Use shared `db` instead of isolated `db_<alias>` |
| `--seed` | off | Seed isolated database from shared `db` after creation |
| `--host-build` | off | Run esbuild on host. Use `dc:build` in separate terminal |
| `--no-host-build` | — | Disable host-build, revert to container builds |
| `--lan` | off | Enable LAN access via nip.io domain |
| `--poll` | off | Enable PM2 polling mode (2s interval) |
| `--rebuild` | off | Force-recreate the container |
| `--open` | off | Open worktree in Cursor |

**Branch scenarios:**

| Scenario | Command |
|----------|---------|
| New branch from canary | `pnpm dc:up feat/new --from=origin/canary` |
| Existing remote branch | `pnpm dc:up feat/abc --from=origin/feat/abc` |
| Existing local branch | `pnpm dc:up feat/abc` |
| New branch from local source | `pnpm dc:up my-branch --branch=feat/new` |
| Restart stopped worktree | `pnpm dc:up feat/abc` |

**Sticky behavior:** `--host-build` sets `WORKTREE_HOST_BUILD=true` in `.env.worktree`. On subsequent `dc:up` calls without `--host-build`, the mode is preserved. Use `--no-host-build` to explicitly disable it.

### `pnpm dc:down`

Stops the Docker container. Optionally removes worktree and/or branch.

```
pnpm dc:down <name> [--remove] [--delete-branch] [--force]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--remove` | off | Stop container, remove volumes and worktree directory (keeps branch) |
| `--delete-branch` | off | Also delete the local branch (requires `--remove`) |
| `--force` | off | Force removal even with uncommitted changes |

Without `--remove`, the container is stopped but volumes and worktree directory are preserved for fast restart.

### `pnpm dc:info`

Shows container status, quick links, and service port mappings.

```
pnpm dc:info <name>     # detailed view with port table and URLs
pnpm dc:info --all      # compact list of all Docker worktrees
```

### `pnpm dc:logs`

View container logs or filter by PM2 service.

```
pnpm dc:logs <name> [-f] [--tail=<n>] [-s <service>]
```

| Flag | Default | Description |
|------|---------|-------------|
| `-f`, `--follow` | off | Follow logs in real time |
| `--tail=<n>`, `--t:<n>` | `100` | Number of lines to show |
| `-s <service>` | all | Filter to a specific PM2 service |

**Available services:** `app`, `api`, `socket_server`, `admin_server`, `ship_server`, `job_server`, `combined_sync`, `listings_sync`, `cache_server`, `insights_server`, `order_table_server`, `inventory_table_server`

### `pnpm dc:build`

Runs the frontend build (esbuild with `--watch`) on the host for a `--host-build` worktree.

```
pnpm dc:build <name>
```

**Prerequisites:**
- `WORKTREE_HOST_BUILD=true` in `.env.worktree` (set by `dc:up --host-build`)
- `node_modules` symlink in the worktree (created by `dc:up --host-build`)
- Container running (started by `dc:up`)

See [How Host-Build Mode Works](#how-host-build-mode-works) for architecture details.

### `pnpm dc:lan`

Toggle LAN access for a running worktree.

```
pnpm dc:lan <name>          # enable LAN access
pnpm dc:lan <name> --off    # disable, revert to .localhost
```

See [How LAN Mode Works](#how-lan-mode-works) for architecture details.

### `pnpm dc:restart`

Restart the container without rebuilding. Preserves volumes and picks up fresh env/credentials.

```
pnpm dc:restart <name>
```

### `pnpm dc:bash` / `pnpm dc:exec`

```bash
pnpm dc:bash <name>                   # interactive shell
pnpm dc:exec <name> <cmd...>          # run a command
pnpm dc:exec <name> pm2 list          # check PM2 processes
pnpm dc:exec <name> pm2 restart app   # restart a service
```

### `pnpm dc:service`

Start, stop, or restart individual PM2 services inside a running container.

```
pnpm dc:service <name> list               # show running services
pnpm dc:service <name> start <service>    # start a service
pnpm dc:service <name> stop <service>     # stop a service
pnpm dc:service <name> restart <service>  # restart a service
```

**Available services:** `app`, `api`, `socket_server`, `serviceHostServer`, `combined_sync`, `listings_sync`, `admin_server`, `ship_server`, `job_server`, `insights_server`, `cache_server`, `order_table_server`, `inventory_table_server`

### `pnpm dc:status`

Shows all Docker worktrees in a table with status, service mode, uptime, memory usage, CPU, and domain.

### `pnpm dc:autostop`

Stops idle worktree containers. Never touches `myapp-infra` infrastructure containers.

| Flag | Default | Description |
|------|---------|-------------|
| `--hours=<N>` | `2` | Idle threshold in hours |
| `--dry-run` | off | Preview without stopping |

Idle = CPU <1% for longer than the threshold.

### `pnpm dc:rebuild-base`

Rebuilds the pre-baked Docker image (`myapp-dev:latest`) from the current `package.json`, `pnpm-lock.yaml`, and `patches/`. The image is shared by all worktrees. `dc:up` auto-builds it if missing.

### `pnpm dc:prune`

Removes orphaned Docker volumes left behind by deleted worktrees. Volumes for active worktrees and running containers are never removed.

| Flag | Default | Description |
|------|---------|-------------|
| `--dry-run` | off | Preview without removing |

### `pnpm db:seed` / `pnpm db:drop` / `pnpm db:reset`

Manage the worktree's isolated MongoDB database. Target database name is derived from `WORKTREE_ALIAS`.

```
pnpm db:seed <name>              # copy shared db into worktree db
pnpm db:drop <name>              # drop the worktree db
pnpm db:drop --db=<db_name>      # drop a database directly (worktree not needed)
pnpm db:reset <name>             # drop + re-seed (fresh copy)
```

**Notes:**
- Uses `mongodump`/`mongorestore` to clone all collections
- Protected databases (`db`, `admin`, `local`, `config`) can never be dropped
- Stop the container before dropping to prevent services from recreating the database

### `pnpm db:images:fix`

Converts absolute fakes3 image URLs to relative paths (`/fakes3/...`) so images load on any domain.

```
pnpm db:images:fix [--db=<db_name>] [--dry-run]
```

Scans `items`, `listings`, `kits`, `liquid_templates`, and `accounts` collections. Fixes `image`, `images`, `thumbnail_url`, `avatar`, and `logo` fields. Safe to run multiple times.

### `pnpm admin:set` / `pnpm admin:unset`

Toggle admin account access on Docker worktree containers.

| Flag | Default | Description |
|------|---------|-------------|
| `--name=<name>` | all running | Target a specific worktree |
| `--user-id=<id>` | — | Set a specific user as admin |

Updates `.env.worktree` and force-recreates the container.

### `pnpm aws:keys`

Prompts for AWS credentials, updates the host shell profile + `~/.aws/credentials`, and restarts all running containers.

### Native Worktree Commands

```
pnpm worktree <name> [--from=<ref>] [--branch=<new>] [--open] [--no-docker]
pnpm worktree:remove <name> [--branch=<branch>] [--force] [--no-prune]
node scripts/worktree/create-worktree-env.js [--auto] [--offset <n>] [--base <n>] [--dir <path>] [--force] [--docker]
```

## Env Var Reference

### Docker Mode (`.env.worktree`)

| Variable | Value | Description |
|----------|-------|-------------|
| `{PREFIX}_PATH` | `/app` | App root inside container |
| `WORKTREE_NAME` | `<name>` | Worktree directory name |
| `WORKTREE_ALIAS` | `<alias>` | Short name for container, DB, Redis prefix |
| `{PREFIX}_MONGO_URL` | `mongodb://mongo:27017/db_<alias>` | MongoDB connection (isolated) |
| `{PREFIX}_MONGO_REPLICA_SET` | `rl0` | Replica set name |
| `{PREFIX}_REDIS_HOST` | `redis` | Redis hostname |
| `{PREFIX}_REDIS_PORT` | `6379` | Redis port |
| `{PREFIX}_LOCAL_IP` | `<alias>.localhost` | Domain used by build.js for asset URLs |
| `{PREFIX}_APP_URL` | `http://<alias>.localhost/` | App base URL via Traefik |
| `WORKTREE_DEV_HEAP` | `2048` | Max old space size (MB) for all PM2 services |
| `WORKTREE_HOST_PORT_OFFSET` | `<N>` | Host port offset (invisible to container — see [Port Configuration](#port-configuration)) |

**Optional variables (set by flags):**

| Variable | Set by | Description |
|----------|--------|-------------|
| `WORKTREE_POLL` | `--poll` | Enables PM2 polling mode (2s interval) |
| `WORKTREE_HOST_BUILD` | `--host-build` | Skips esbuild in container, run `dc:build` on host |
| `{PREFIX}_LAN_DOMAIN` | `--lan` / `dc:lan` | nip.io domain for LAN access |
| `{PREFIX}_ADMIN_ACCOUNTS` | `admin:set` | Admin account ID(s) |

### Native Mode (`.env.worktree`)

| Variable | Description |
|----------|-------------|
| `{PREFIX}_PATH` | Resolved worktree directory path |
| `WORKTREE_NAME` | Worktree directory name |
| `WORKTREE_PORT_OFFSET` | Port offset read by `port-config.js` |
| `WORKTREE_PORT_BASE` | Alternative: offset = `WORKTREE_PORT_BASE - 3000` |

## Requirements

- Docker Desktop running (RAM: 12GB+ recommended)
- `myapp-infra` stack: `mongo`, `redis`, `traefik` (Traefik)
- `~/.aws` directory on host (mounted read-only for AWS Secrets Manager access)
- Pre-baked image (`pnpm dc:rebuild-base`) — auto-built on first `dc:up` if missing

### Starting Traefik Without Restarting Mongo/Redis

```bash
cd ~/apps/myapp-infra && docker compose up -d traefik
```

Verify all three are running:

```bash
docker ps --filter "name=mongo" --filter "name=redis" --filter "name=traefik" --format "table {{.Names}}\t{{.Status}}"
```

## Safety

`.env.worktree`, `docker-compose.worktree.yml`, and `.docker-overrides/` are git-ignored and should not be committed.

## Known Limitations

- **File watching on macOS Docker:** VirtioFS supports inotify well. If slow, use `--poll` for 2s polling
- **Debug ports (9225-9241):** Not exposed by default. Add to `docker-compose.worktree.yml` manually
- **Memory:** ~11 GB for full mode (13 services at 2048 MB cap). Minimal uses ~7 GB
- **Log rotation:** PM2 logs auto-rotated at 50 MB per service, 3 files retained (Docker only)
- **LAN cookies:** Logging in on `.localhost` doesn't carry over to `.nip.io` — log in again
- **Stripe on LAN:** Requires HTTPS for non-localhost domains (expected Stripe limitation)

## Key Source Files

| File | Purpose |
|------|---------|
| [worktree-dash](https://github.com/elvisnm/worktree-dash) | TUI dashboard (`dash`) — Go/Bubble Tea binary |
| `scripts/worktree/dc.js` | Interactive CLI hub for all operations (`dc`) |
| `scripts/worktree/dc-create.js` | Interactive guided creation (`dc:create`) |
| `scripts/worktree/dc-worktree-up.js` | Main `dc:up` logic (create, restart, mode switching) |
| `scripts/worktree/generate-docker-compose.js` | Generates `docker-compose.worktree.yml` and Traefik config |
| `scripts/worktree/dc-build.js` | Host-build command (`dc:build`) |
| `scripts/worktree/dc-lan.js` | LAN access toggle (`dc:lan`) |
| `scripts/worktree/service-ports.js` | Port map, offset computation, conflict detection |
| `scripts/worktree/create-worktree-env.js` | Generates `.env.worktree` |
| `scripts/port-config.js` | Runtime port resolution (reads `WORKTREE_PORT_OFFSET`) |
| `docker/docker-entrypoint.sh` | Container entrypoint (rsync node_modules, start dev) |
