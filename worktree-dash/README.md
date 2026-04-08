# worktree-dash

Terminal dashboard for managing worktrees, containers, and services.

## Install

**1. Install Go** (1.25 or later)

```bash
brew install go
```

**2. Clone this repo**

```bash
git clone git@github.com:elvisnm/worktree-dash.git
```

**3. Build and install**

```bash
cd worktree-dash
make install
```

This compiles the binary and copies it to `/usr/local/bin/worktree-dash`.

**4. Run it**

From your project repo:

```bash
pnpm dash
```

## Features

- Real-time container stats (CPU, memory, uptime) for Docker worktrees
- PM2 service management for both Docker and local worktrees
- Local worktree support: start/stop/restart with isolated PM2_HOME
- URL, CPU, and memory display for local worktrees in the details panel
- Integrated terminal sessions (shell, logs, Claude)
