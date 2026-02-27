# Getting Started

This guide walks through adding wt to an existing project.

## Prerequisites

- Node.js >= 18
- Docker with Compose v2
- Git
- Your project has a git repository

## Step 1: Generate a Config

From the wt directory, run the init wizard targeting your project:

```bash
node worktree-flow/workflow-init.js /path/to/your-project
```

The wizard will:
1. Detect your project type (Node.js, Go, Rust, Python)
2. Find existing docker-compose files
3. Ask about services, ports, database, features
4. Write `workflow.config.js` to your project root

You can also create the config manually. Here's a minimal example:

```js
// workflow.config.js
module.exports = {
  name: 'myapp',

  docker: {
    composeStrategy: 'generate',
    generate: {
      entrypoint: 'npm run dev',
    },
  },

  services: {
    ports: { web: 3000 },
    primary: 'web',
  },

  database: { type: null },
  redis: null,

  env: {
    prefix: 'MYAPP',
    filename: '.env.worktree',
  },

  features: {
    autostop: true,
    prune: true,
  },
};
```

## Step 2: Choose a Docker Strategy

You have two options. Pick the one that fits your project.

### Option A: Generate (recommended for simple setups)

wt generates a `docker-compose.worktree.yml` for each worktree. One container per worktree runs all your services inside (via PM2 or your entrypoint).

```js
docker: {
  composeStrategy: 'generate',
  baseImage: 'myapp-dev:latest',  // or null to use Dockerfile
  generate: {
    containerWorkdir: '/app',
    entrypoint: 'npm run dev',
  },
},
```

Best for: monolithic apps, single-container setups, PM2-managed services.

### Option B: Shared Compose

Use an existing `docker-compose.yml` with environment variable substitution. wt writes port assignments and metadata to `.env.worktree`, then runs `docker compose -f <file> -p <project> up`.

```js
docker: {
  composeStrategy: 'docker/docker-compose.dev.yml',
  composeFile: 'docker/docker-compose.dev.yml',
  generate: null,
},
```

Your compose file uses variables like `${WEB_PORT}`, `${BRANCH_SLUG}`, `${PROJECT_ROOT}`:

```yaml
services:
  web:
    build: ./docker/web
    container_name: myapp-${BRANCH_SLUG:-dev}-web
    ports:
      - "${WEB_PORT:-3000}:3000"
    volumes:
      - ${PROJECT_ROOT}:/app
```

Best for: multi-container setups, existing compose workflows, monorepos.

See [Docker Strategies](docker-strategies.md) for full details on both approaches.

## Step 3: Add Package Scripts (Optional)

If your project uses npm/pnpm, add convenience scripts to `package.json`:

```json
{
  "scripts": {
    "dc": "node /path/to/wt/worktree-flow/dc.js",
    "dc:create": "node /path/to/wt/worktree-flow/dc-create.js",
    "dc:up": "node /path/to/wt/worktree-flow/dc-worktree-up.js",
    "dc:down": "node /path/to/wt/worktree-flow/dc-worktree-down.js",
    "dc:status": "node /path/to/wt/worktree-flow/dc-status.js",
    "dc:info": "node /path/to/wt/worktree-flow/dc-info.js",
    "dc:logs": "node /path/to/wt/worktree-flow/dc-logs.js",
    "dc:bash": "node /path/to/wt/worktree-flow/dc-bash.js",
    "dc:restart": "node /path/to/wt/worktree-flow/dc-restart.js",
    "dc:seed": "node /path/to/wt/worktree-flow/dc-seed.js"
  }
}
```

Or set `paths.flowScripts` in your config to the wt scripts directory and the dashboard will find them automatically.

## Step 4: Create Your First Worktree

```bash
# Interactive wizard (recommended for first time)
pnpm dc:create
# or
node /path/to/wt/worktree-flow/dc-create.js

# Direct creation
pnpm dc:up feat/my-feature --from=origin/main
```

The wizard will prompt for:
- Branch name (validated against `repo.branchPrefixes`)
- Base ref (what to fork from)
- Service mode (which services to run)
- Alias (short name — auto-derived from branch)
- Database strategy (isolated or shared)
- Extra options (host-build, LAN, polling)

## Step 5: Verify

```bash
# Check status
pnpm dc:status

# Get detailed info
pnpm dc:info my-feat

# Open a shell
pnpm dc:bash my-feat
```

## Next Steps

- [CLI Commands Reference](commands.md) — all available commands and options
- [Configuration Reference](configuration.md) — every config field explained
- [Dashboard Guide](dashboard.md) — using the Go TUI
