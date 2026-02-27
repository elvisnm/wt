# Docker Compose Strategies

wt supports two strategies for running Docker containers. The choice depends on your project structure.

## Generate Strategy

**Config:** `docker.composeStrategy: 'generate'`

wt auto-generates a `docker-compose.worktree.yml` for each worktree. A single container runs all your services (typically via PM2).

### How it works

1. `dc-worktree-up.js` calls `generate-docker-compose.js`
2. The generator creates a YAML file with:
   - Container name: `{name}-{alias}` (e.g., `myapp-my-feat`)
   - Base image from `docker.baseImage`
   - Volume mounts: code, node_modules, extra mounts
   - Port mappings: each service port offset by the worktree's offset
   - Environment: loaded from `.env.worktree`
   - Entrypoint: from `docker.generate.entrypoint`
3. Services run inside the container managed by PM2
4. Health check polls container until healthy

### Example generated compose

```yaml
services:
  app:
    container_name: myapp-my-feat
    image: myapp-dev:latest
    working_dir: /app
    command: ["pnpm", "dev"]
    volumes:
      - .:/app:cached
      - app_node_modules:/app/node_modules
    ports:
      - "3500:3000"   # socket_server (3000 + 500 offset)
      - "3501:3001"   # app
      - "3504:3004"   # api
      ...
    env_file:
      - .env.worktree
    networks:
      - myapp-infra_default
```

### When to use

- Single-container applications
- PM2-managed multi-service setups
- Projects using a prebaked Docker image
- When you want wt to handle all Docker configuration

### Service modes

The generate strategy supports service modes — named subsets of services:

```js
services: {
  ports: { app: 3001, api: 3004, worker: 3005, admin: 3050 },
  modes: {
    minimal: ['app', 'api'],
    full: null,  // null = all services
  },
  defaultMode: 'minimal',
},
```

When creating a worktree with `--mode=minimal`, only the listed services start. PM2 manages this filtering at runtime.

## Shared Compose Strategy

**Config:** `docker.composeStrategy: 'path/to/compose.yml'`

Use an existing docker-compose file with environment variable substitution. wt writes variables to `.env.worktree` and runs `docker compose -f <file> -p <project> up`.

### How it works

1. `dc-worktree-up.js` computes a port offset and branch slug
2. Writes `.env.worktree` with: `BRANCH_SLUG`, `WEB_PORT`, `API_PORT`, `PROJECT_ROOT`, `REPO_ROOT`, etc.
3. Runs: `docker compose -f <compose_file> -p <project_name> up --build -d`
4. The compose project name is `{name}-{slug}` (e.g., `bc-test-workflow`)
5. Each service gets its own container: `{project}-{service}` (e.g., `bc-test-workflow-web`)

### Example shared compose file

```yaml
# docker/docker-compose.dev.yml
services:
  web:
    build:
      context: ..
      dockerfile: docker/web/Dockerfile
    container_name: myapp-${BRANCH_SLUG:-dev}-web
    ports:
      - "${WEB_PORT:-3000}:3000"
    volumes:
      - ${PROJECT_ROOT}:/app
    environment:
      - DATABASE_URL=${DATABASE_URL}

  api:
    build:
      context: ..
      dockerfile: docker/api/Dockerfile
    container_name: myapp-${BRANCH_SLUG:-dev}-api
    ports:
      - "${API_PORT:-4000}:4000"
    volumes:
      - ${PROJECT_ROOT}:/app
```

### Environment variables available

When using shared compose, wt sets these in `.env.worktree`:

| Variable | Example | Description |
|---|---|---|
| `BRANCH_SLUG` | `test-workflow` | Sanitized branch name |
| `PROJECT_ROOT` | `/Users/x/worktrees/feat-login` | Worktree directory |
| `REPO_ROOT` | `/Users/x/myapp` | Main repo root |
| `WEB_PORT` | `3042` | Per-service port (base + offset) |
| `API_PORT` | `4042` | Per-service port (base + offset) |
| `WORKTREE_ALIAS` | `test-workflow` | Alias |
| `WORKTREE_NAME` | `feat-test-workflow` | Directory name |

Service port variable names are derived from the service name: `{SERVICE_NAME}_PORT` (uppercased).

### When to use

- Multi-container setups (separate web, api, worker containers)
- Projects that already have a docker-compose for development
- Monorepos with per-service Dockerfiles
- When you want full control over the compose configuration

## Comparison

| Aspect | Generate | Shared |
|---|---|---|
| Containers per worktree | 1 (PM2 inside) | 1 per service |
| Compose file | Auto-generated | You maintain it |
| Service isolation | Process-level (PM2) | Container-level |
| Port mapping | Host ports offset, container ports fixed | Both sides configurable |
| Health checks | Built-in polling | Define in your compose |
| Service modes | Supported (PM2 filtering) | Not applicable (compose profiles instead) |
| Config complexity | Lower | Higher (you write the compose) |

## Port Isolation

Both strategies use the same port offset mechanism. Each worktree gets a deterministic offset computed from its path or name:

### SHA-256 algorithm (default)

```
offset = (SHA256(input)[0:4] as uint32) % range + min
```

Default range: 100-2100. Good for large teams with many worktrees.

### cksum algorithm

```
offset = (char_code_sum_with_rotation) % range + min
```

Simpler, smaller range (e.g., 1-99). Good for small projects.

### Collision resolution

When `portOffset.autoResolve` is true, wt checks if the computed ports are already in use. If they conflict, it increments the offset until a free range is found (up to 100 attempts).

### Port assignment

Each service's port is: `base_port + offset`.

```
Service ports (offset=42):
  web:  3000 + 42 = 3042
  api:  4000 + 42 = 4042
```
