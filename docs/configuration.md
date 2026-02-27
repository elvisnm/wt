# Configuration Reference

All configuration lives in `workflow.config.js` at your project root. This is a CommonJS module — you can use `require()`, environment variables, and conditional logic.

For the annotated full schema with inline comments, see [workflow.config.schema.md](../workflow.config.schema.md).

## Minimal Config

```js
module.exports = {
  name: 'myapp',
  services: {
    ports: { web: 3000 },
    primary: 'web',
  },
};
```

Everything else has sensible defaults. This gives you: generate strategy, `.env.worktree` files, SHA-256 port offsets, and basic autostop/prune features.

## Config Fields

### name (required)

```js
name: 'myapp'
```

Project identifier. Used as prefix for containers (`myapp-alias`), volumes (`myapp_alias_*`), compose projects (`myapp-slug`), and env var prefix fallback.

### repo

```js
repo: {
  worktreesDir: '../myapp-worktrees',
  branchPrefixes: ['feat', 'fix', 'chore', 'hotfix', 'release'],
}
```

| Field | Default | Description |
|---|---|---|
| `worktreesDir` | `../{name}-worktrees` | Where worktree directories are created. Relative to repo root parent |
| `branchPrefixes` | `['feat', 'fix', ...]` | Allowed branch prefixes for validation. `null` to skip validation |

### docker

```js
docker: {
  baseImage: 'myapp-dev:latest',
  composeStrategy: 'generate',
  generate: { ... },
  composeFile: null,
  sharedInfra: { ... },
  proxy: { ... },
}
```

| Field | Default | Description |
|---|---|---|
| `baseImage` | `null` | Docker image name:tag. `null` means compose must define images |
| `composeStrategy` | `'generate'` | `'generate'` or a path to a shared compose file |
| `composeFile` | `null` | Explicit path to shared compose file (for non-generate strategy) |

#### docker.generate

Only used when `composeStrategy` is `'generate'`.

| Field | Default | Description |
|---|---|---|
| `containerWorkdir` | `'/app'` | Mount point inside container |
| `entrypoint` | `'pnpm dev'` | Startup command |
| `extraMounts` | `[]` | Additional volume mounts (`host:container` format) |
| `extraEnv` | `{}` | Additional environment variables |

#### docker.sharedInfra

| Field | Default | Description |
|---|---|---|
| `network` | `null` | External Docker network to join. `null` = isolated per worktree |
| `composePath` | `null` | Path to shared infra project (Traefik, DB, Redis). Supports `~` |

#### docker.proxy

| Field | Default | Description |
|---|---|---|
| `type` | `'ports'` | `'traefik'`, `'ports'`, or `null` |
| `dynamicDir` | `'traefik/dynamic'` | Traefik dynamic config directory (relative to `sharedInfra.composePath`) |
| `domainTemplate` | `'{alias}.localhost'` | Domain template. `{alias}` replaced at runtime |

### services

```js
services: {
  ports: {
    web: 3000,
    api: 4000,
    worker: 5000,
  },
  modes: {
    minimal: ['web', 'api'],
    full: null,
  },
  defaultMode: 'minimal',
  primary: 'web',
  quickLinks: [
    { label: 'Web', service: 'web', pathPrefix: '' },
    { label: 'API', service: 'api', pathPrefix: '/api' },
  ],
}
```

| Field | Default | Description |
|---|---|---|
| `ports` | `{}` | Map of service name to base port |
| `modes` | `{ default: null }` | Named service subsets. `null` = all services |
| `defaultMode` | first mode key | Mode used when `--mode` is omitted |
| `primary` | first service | Primary service for health checks, URLs, exec |
| `quickLinks` | `[]` | Links shown in dashboard details |

### portOffset

```js
portOffset: {
  algorithm: 'sha256',
  min: 100,
  range: 2000,
  autoResolve: true,
}
```

| Field | Default | Description |
|---|---|---|
| `algorithm` | `'sha256'` | `'sha256'` or `'cksum'` |
| `min` | `100` | Minimum offset value |
| `range` | `2000` | Offset range: `(hash % range) + min` |
| `autoResolve` | `true` | Auto-increment on port collision |

### database

```js
database: {
  type: 'mongodb',
  host: 'localhost',
  containerHost: 'myapp_mongo',
  port: 27017,
  defaultDb: 'mydb',
  replicaSet: null,
  dbNamePrefix: 'db_',
  seedCommand: 'mongodump ... | mongorestore ...',
  dropCommand: 'mongosh ...',
}
```

| Field | Default | Description |
|---|---|---|
| `type` | `null` | `'mongodb'`, `'postgres'`, `'supabase'`, `'mysql'`, or `null` |
| `host` | `null` | DB host (for host-side operations) |
| `containerHost` | `null` | DB hostname inside Docker network |
| `port` | `null` | DB port |
| `defaultDb` | `null` | Source database for seeding |
| `replicaSet` | `null` | MongoDB replica set name |
| `dbNamePrefix` | `'db_'` | Worktree DB naming: `{prefix}{alias}` |
| `seedCommand` | `null` | Seed command template. `{sourceDb}` and `{targetDb}` replaced |
| `dropCommand` | `null` | Drop command template. `{targetDb}` replaced |

Set `database: { type: null }` to disable database features.

### redis

```js
redis: {
  containerHost: 'myapp_redis',
  port: 6379,
}
```

Set to `null` to disable Redis.

### env

```js
env: {
  prefix: 'MYAPP',
  filename: '.env.worktree',
  vars: {
    projectPath: '{PREFIX}_PATH',
    dbConnection: '{PREFIX}_DATABASE_URL',
    environment: '{PREFIX}_ENV',
  },
  worktreeVars: {
    name: 'WORKTREE_NAME',
    alias: 'WORKTREE_ALIAS',
    hostPortOffset: 'WORKTREE_HOST_PORT_OFFSET',
  },
}
```

| Field | Default | Description |
|---|---|---|
| `prefix` | uppercase(`name`) | Prefix for project env vars |
| `filename` | `'.env.worktree'` | Env file name in each worktree |
| `vars` | `{}` | Map of logical key to env var name. `{PREFIX}` replaced with `prefix` |
| `worktreeVars` | hardcoded `WORKTREE_*` | Map of logical key to `WORKTREE_*` var name |

The `{PREFIX}` template in `vars` values gets replaced with `env.prefix` at load time. So `'{PREFIX}_PATH'` with prefix `'MYAPP'` becomes `'MYAPP_PATH'`.

### features

```js
features: {
  hostBuild: false,
  lan: false,
  admin: { enabled: false },
  awsCredentials: false,
  autostop: true,
  prune: true,
  imagesFix: false,
  rebuildBase: false,
  devHeap: null,
}
```

| Field | Default | Description |
|---|---|---|
| `hostBuild` | `false` | Host-side frontend builds |
| `lan` | `false` | LAN access via nip.io |
| `admin` | `{ enabled: false }` | Admin account toggling. Set `defaultUserId` when enabled |
| `awsCredentials` | `false` | Mount `~/.aws` into containers |
| `autostop` | `true` | Auto-stop idle containers |
| `prune` | `true` | Orphaned volume cleanup |
| `imagesFix` | `false` | Image URL fixing in DB |
| `rebuildBase` | `false` | Base image rebuild command |
| `devHeap` | `null` | Node.js heap size in MB |

### dash

```js
dash: {
  commands: {
    shell:  { label: 'Shell',  cmd: 'bash' },
    claude: { label: 'Claude', cmd: 'claude' },
    logs:   { label: 'Logs',   cmd: null },
  },
  localDevCommand: 'pnpm dev',
}
```

| Field | Default | Description |
|---|---|---|
| `commands` | shell + claude | Terminal tab commands. `cmd: null` = built-in handler |
| `localDevCommand` | `'pnpm dev'` | Dev command for non-Docker worktrees |

See [Dashboard — Custom Commands](dashboard.md#custom-commands) for details on adding commands.

### paths

```js
paths: {
  flowScripts: null,
  dockerOverrides: null,
  buildScript: null,
}
```

| Field | Default | Description |
|---|---|---|
| `flowScripts` | `null` | Directory with worktree-flow scripts. `null` = `scripts/worktree` |
| `dockerOverrides` | `null` | Directory copied into each worktree. `null` = disabled |
| `buildScript` | `null` | Build script for host-build mode. `null` = disabled |

## Config Loading

The config loader (`worktree-flow/config.js`) walks upward from CWD to find `workflow.config.js`. It:

1. Requires the file
2. Deep-merges with defaults
3. Resolves `{PREFIX}` templates in env var names
4. Converts relative paths to absolute
5. Exports the resolved config plus helper functions

Helper functions exported from `config.js`:

| Function | Description |
|---|---|
| `load_config({ cwd, required })` | Load and resolve config |
| `container_name(config, alias)` | `{name}-{alias}` |
| `compose_project(config, slug)` | `{name}-{slug}` |
| `compute_offset(config, input)` | Deterministic port offset |
| `compute_ports(config, offset)` | All service ports shifted |
| `db_name(config, alias)` | `{prefix}{alias}` |
| `domain_for(config, alias)` | Domain from template |
| `get_compose_info(config, path)` | Shared compose helper |
| `services_for_mode(config, mode)` | Service list for a mode |
| `feature_enabled(config, name)` | Check feature flag |

## Examples

Two complete example configs are included in [workflow.config.schema.md](../workflow.config.schema.md):
- **Generate strategy example** — Monolithic container, multiple services, MongoDB, Traefik, full features
- **build-check** — Shared compose, 2 services, Supabase, minimal features
