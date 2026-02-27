# Worktrees

Run isolated dev environments using Docker containers. Each worktree gets its own container, `.localhost` domain, MongoDB database, and Redis key prefix.

All `dc:*` commands run from the **main repo** — they find the worktree automatically by name.

## Quick Start

```bash
pnpm dash                                          # TUI dashboard (recommended)
pnpm dc                                            # interactive CLI hub
pnpm dc:create                                     # interactive creation shortcut
pnpm dc:up feat/my-feature --from=origin/canary    # create worktree + start container
pnpm dc:info feat/my-feature                       # shows URLs and ports
```

Open `http://my-feature.localhost/` in your browser.

## Commands

### Dashboard (TUI)

```bash
pnpm dash
```

Full terminal UI for managing worktrees, containers, and services. Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) (Go). Requires the `worktree-dash` binary — see [Installing the Dashboard](#installing-the-dashboard) below.

### Interactive CLI Hub

```bash
pnpm dc
```

Text-based interactive hub. Shows existing worktrees, then lets you pick an action: create, manage (logs/restart/stop/bash/services), database ops, admin/LAN config, or maintenance. No flags to memorize. No extra dependencies — runs on Node.js.

### Interactive Creation

```bash
pnpm dc:create
```

Step-by-step guided creation — prompts for branch, base ref, alias, service mode, database, and extras. Shows a summary with the equivalent `dc:up` command before executing. Also accessible from `pnpm dc` → "Create a new worktree".

### Create & Start

```
pnpm dc:up <name> [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--from=<ref>` | — | Base ref to create the branch from |
| `--branch=<new>` | — | Create a new branch name using `<name>` as source |
| `--alias=<short>` | auto from branch | Short name for container, DB, and domain (e.g. `feat/bulk-ship-api` -> `bulk-ship`) |
| `--mode=<mode>` | `full` | Service mode: `full` (all services) or `minimal` (7 core services) |
| `--shared-db` | off (isolated db) | Use shared `db` instead of creating isolated `db_<alias>` |
| `--seed` | off | Seed isolated database from shared `db` after creation |
| `--host-build` | off | Run esbuild on host (~7x faster frontend). Use `dc:build` in a separate terminal |
| `--no-host-build` | — | Disable host-build mode, revert to container builds |
| `--lan` | off | Enable LAN access via nip.io domain |
| `--poll` | off | Enable PM2 polling mode (2s interval, for slow file watchers) |
| `--rebuild` | off | Force-recreate the container |
| `--open` | off | Open worktree in Cursor |

Running `dc:up` on an existing worktree restarts it and regenerates the compose file with any new flags.

```bash
# Examples
pnpm dc:up feat/new --from=origin/canary                    # new branch from canary
pnpm dc:up feat/abc --from=origin/feat/abc                  # existing remote branch
pnpm dc:up feat/abc                                         # restart stopped worktree
pnpm dc:up feat/abc --host-build --shared-db                # combine flags
pnpm dc:up feat/long-branch-name --alias=short              # custom alias
```

### Stop & Cleanup

```bash
pnpm dc:down <name>                              # stop container (keeps volumes + worktree)
pnpm dc:down <name> --remove                     # remove container, volumes, worktree (keeps branch)
pnpm dc:down <name> --remove --delete-branch     # full teardown including local branch
pnpm dc:down <name> --remove --force             # force remove even with uncommitted changes
pnpm dc:restart <name>                           # restart without rebuilding
pnpm dc:prune [--dry-run]                        # remove orphaned Docker volumes
pnpm dc:autostop [--hours=N] [--dry-run]         # stop idle containers (default: >2h, CPU <1%)
```

### Monitor & Debug

```bash
pnpm dc:info <name>                  # status, ports, quick links
pnpm dc:info --all                   # list all worktrees
pnpm dc:status                       # table: all worktrees with status/memory/CPU
pnpm dc:logs <name> [-f] [-s <svc>]  # view logs (default: last 100 lines, -f follow, -s filter)
pnpm dc:bash <name>                  # interactive shell inside container
pnpm dc:exec <name> <cmd...>         # run a command inside container
pnpm dc:service <name> list          # show PM2 services
pnpm dc:service <name> start <svc>   # start a service on-demand
pnpm dc:service <name> stop <svc>    # stop a service
```

### Database

Each worktree gets an isolated database (`db_<alias>`) by default. Use `--shared-db` to share the main `db`.

```bash
pnpm db:seed <name>                  # copy shared db into worktree db
pnpm db:drop <name>                  # drop the worktree db
pnpm db:drop --db=<db_name>          # drop by name (after worktree removed)
pnpm db:reset <name>                 # drop + re-seed (fresh copy)
pnpm db:images:fix [--db=<db_name>]  # fix absolute fakes3 image URLs to relative paths
```

### LAN Sharing

Share your worktree with anyone on the same network. Uses [nip.io](https://nip.io) wildcard DNS — no `/etc/hosts` changes, works on phones.

```bash
pnpm dc:lan <name>           # enable: http://<alias>.<ip>.nip.io/
pnpm dc:lan <name> --off     # disable, revert to .localhost
```

### Host-Build Mode

Run esbuild on the host instead of inside Docker — ~7x faster frontend rebuilds.

```bash
# Terminal 1: start container (PM2 services only, no esbuild)
pnpm dc:up feat/abc --host-build

# Terminal 2: start frontend build on host
pnpm dc:build feat/abc
```

Disable with `pnpm dc:up feat/abc --no-host-build`.

### Admin & Credentials

```bash
pnpm admin:set [--name=<name>] [--user-id=<id>]   # enable admin (default: all running containers)
pnpm admin:unset [--name=<name>]                   # disable admin (default: all running containers)
pnpm aws:keys                                      # update AWS creds, restart all containers
```

### Switching Between Modes

All mode switches are seamless — no manual cleanup required.

| Switch to | Command |
|-----------|---------|
| Normal Docker | `pnpm dc:up <name>` |
| Host-build | `pnpm dc:up <name> --host-build` + `pnpm dc:build <name>` |
| Back to normal | `pnpm dc:up <name> --no-host-build` |
| LAN on | `pnpm dc:lan <name>` |
| LAN off | `pnpm dc:lan <name> --off` |
| Minimal mode | `pnpm dc:up <name> --mode=minimal` |
| Full mode | `pnpm dc:up <name> --mode=full` |

Flags can be combined: `pnpm dc:up feat/abc --host-build --shared-db --lan`

### Native Worktrees

```bash
pnpm worktree <name> --from=<ref> --no-docker    # create without Docker
pnpm worktree:remove <name>                       # remove a worktree
```

### Git Helpers

```bash
pnpm git:build:skip      # hide build/, .beads/, CLAUDE.md, pnpm-lock.yaml from git status
pnpm git:build:unskip    # undo the above
```

## Common Workflows

### New feature with seeded data
```bash
pnpm dc:up feat/my-feature --from=origin/canary
pnpm db:seed feat/my-feature
```

### Test someone else's branch
```bash
git fetch origin
pnpm dc:up feat/their-branch --from=origin/feat/their-branch
# ... test ...
pnpm dc:down feat/their-branch --remove
```

### Stop and resume later
```bash
pnpm dc:down feat/my-feature          # stop, keep everything
pnpm dc:up feat/my-feature            # instant restart
```

### Fast frontend development
```bash
pnpm dc:up feat/my-feature --host-build --shared-db --from=origin/canary
pnpm dc:build feat/my-feature          # separate terminal
```

### Share on LAN for mobile testing
```bash
pnpm dc:lan feat/my-feature
# Share: http://my-feature.192.168.1.42.nip.io/
```

### Debug a service
```bash
pnpm dc:logs feat/my-feature -s app -f       # follow app logs
pnpm dc:exec feat/my-feature pm2 list        # check PM2 processes
pnpm dc:bash feat/my-feature                 # interactive shell
```

## Installing the Dashboard

`pnpm dash` requires the `worktree-dash` Go binary in a sibling directory. One-time setup:

```bash
# 1. Install Go (if you don't have it)
brew install go

# 2. Clone and build
git clone git@github.com:elvisnm/worktree-dash.git ~/apps/worktree-dash
cd ~/apps/worktree-dash && make build
```

To update later: `cd ~/apps/worktree-dash && git pull && make build`.

## Notes

- All commands run from the **main repo**, not the worktree directory
- Each worktree gets a `.localhost` domain (e.g. `http://<alias>.localhost/`). Multiple can run simultaneously
- Alias is auto-derived from branch (e.g. `feat/bypass-delivery` -> `bypass-delivery`) or set with `--alias`
- First boot seeds node_modules from the pre-baked image (~10s rsync), subsequent starts are instant
- Run `pnpm dc:rebuild-base` when dependencies change significantly
- Service modes: `full` (all, ~11 GB RAM) or `minimal` (7 core services). Use `dc:service` to start extras on-demand
- PM2 services capped at 2048 MB each. Increase Docker Desktop RAM to 12GB+ if needed
- If file watching is slow, use `--poll` to switch PM2 to polling mode
- Requires Docker Desktop with the shared infrastructure stack running (Mongo, Redis, Traefik)

For architecture details, port configuration, env vars, and internals, see [DOCKER-README.md](./DOCKER-README.md).
