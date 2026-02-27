# workflow.config.js — Configuration Schema

This document defines the configuration schema for the workflow system (worktree-flow + worktree-dash). A `workflow.config.js` file at the project root replaces all project-specific hardcoding, making the system attachable to any project.

## File Location & Resolution

The config loader searches upward from CWD for `workflow.config.js`. This file lives at the **project root** (next to `package.json`, `go.mod`, etc.).

## Format

```js
// workflow.config.js
module.exports = { ... }
```

JavaScript (not JSON/YAML) — allows comments, env var interpolation, and conditional logic.

---

## Schema

```js
module.exports = {
  // ─── Project Identity ──────────────────────────────────────────────
  // Used as prefix for containers, volumes, networks, compose projects
  name: "myapp",                  // REQUIRED. e.g. "myapp", "build-check", "acme"

  // ─── Repo Layout ───────────────────────────────────────────────────
  repo: {
    // Where worktree directories are created. Relative to repo root parent.
    // Default: "{name}-worktrees" (sibling to repo root)
    worktreesDir: "../myapp-worktrees",

    // Branch name prefixes allowed during dc:create validation.
    // Set to null to skip validation.
    branchPrefixes: ["feat", "fix", "ops", "hotfix", "release", "chore"],

    // Remote refs used as base branch candidates in dc:create.
    // null = auto-detected from git (origin/main, origin/master, origin/develop).
    baseRefs: null,  // e.g. ["origin/main", "origin/develop"]
  },

  // ─── Docker ────────────────────────────────────────────────────────
  docker: {
    // Base image used for worktree containers.
    // If using a prebaked image, set this to the full name:tag.
    baseImage: "myapp-dev:latest",

    // The docker-compose file to use for worktrees.
    // "generate" = auto-generate per worktree (generate strategy)
    // "<path>"   = use a shared compose file with env var substitution (build-check pattern)
    composeStrategy: "generate",

    // Only used when composeStrategy is "generate":
    generate: {
      // Path mounted inside the container as the working directory
      containerWorkdir: "/app",

      // Entrypoint command run when container starts
      entrypoint: "pnpm dev",

      // Additional volume mounts (host:container format, relative to worktree root)
      extraMounts: [
        // e.g. "../fakes3:/app/fakes3:cached"
      ],

      // Additional environment variables injected into the container
      extraEnv: {
        // e.g. NODE_ENV: "development"
      },

      // Files to copy from the main repo into each worktree on create/restart.
      // Each entry: { src: "relative/path/in/repo", dst: "relative/path/in/worktree" }
      overrideFiles: [],
    },

    // Env files to copy from the main repo into each worktree on creation.
    // Useful for shared strategy where services need .env files present.
    envFiles: [],  // e.g. [".env", "apps/api/.env"]

    // Only used when composeStrategy is a path (like build-check):
    // The shared compose file path (relative to repo root)
    composeFile: null,  // e.g. "docker/docker-compose.dev.yml"

    // Shared infrastructure — external services the worktree containers connect to
    sharedInfra: {
      // External Docker network to join (must already exist)
      // null = create an isolated network per worktree
      network: "myapp-infra_default",

      // Path to shared infra docker-compose project (Traefik, DB, Redis, etc.)
      // null = no shared infra (each worktree is self-contained)
      composePath: "~/apps/myapp-infra",
    },

    // Reverse proxy config
    proxy: {
      // "traefik" = Traefik routing (works with both strategies):
      //   - "generate": writes dynamic config files to dynamicDir
      //   - shared: auto-generates a docker-compose.traefik.yml override with labels
      // "ports"   = just map ports to host (no reverse proxy)
      // null      = disabled
      type: "traefik",

      // Only for type "traefik":
      // Directory for dynamic config files (relative to sharedInfra.composePath)
      dynamicDir: "traefik/dynamic",

      // Domain template. {alias} is replaced with worktree alias.
      domainTemplate: "{alias}.localhost",
    },
  },

  // ─── Services ──────────────────────────────────────────────────────
  // Services that run inside each worktree container (PM2 processes, etc.)
  // Each service has a name and a base port.
  services: {
    // Map of service_name -> base_port
    // Port offset is added to each base port for worktree isolation.
    ports: {
      socket_server: 3000,
      app: 3001,
      sync: 3002,
      ship_server: 3003,
      api: 3004,
      job_server: 3005,
      www: 3006,
      cache_server: 3008,
      insights_server: 3010,
      order_table_server: 3012,
      inventory_table_server: 3013,
      admin_server: 3050,
      livereload: 53099,
    },

    // Named groups of services. Keys are mode names, values are arrays of service names.
    // null value = all services.
    modes: {
      minimal: ["socket_server", "app", "api", "admin_server", "cache_server", "order_table_server", "inventory_table_server"],
      full: null,
    },

    // Default mode when creating a worktree
    defaultMode: "minimal",

    // The "primary" service used for URL display, health checks, quick links
    primary: "app",

    // Quick links shown in the dashboard details panel.
    // Each entry: { label, service, pathPrefix }
    quickLinks: [
      { label: "Web", service: "app", pathPrefix: "" },
      { label: "API", service: "api", pathPrefix: "" },
      { label: "Admin", service: "admin_server", pathPrefix: "" },
    ],
  },

  // ─── Port Offset ──────────────────────────────────────────────────
  portOffset: {
    // Algorithm: "sha256" (SHA-256 hash mod range) or "cksum" (char code sum mod range)
    algorithm: "sha256",

    // Range for offset computation: offset = (hash % range) + min
    min: 100,
    range: 2000,

    // Auto-resolve port collisions by incrementing offset
    autoResolve: true,
  },

  // ─── Database ──────────────────────────────────────────────────────
  database: {
    // "mongodb" | "postgres" | "supabase" | "mysql" | null
    type: "mongodb",

    // Connection details (used for seed/drop/reset operations and env generation)
    // For mongodb:
    host: "localhost",
    containerHost: "mongo",              // hostname inside Docker network
    port: 27017,
    defaultDb: "db",                     // the "source" database to clone from
    replicaSet: "rl0",                   // null if not using replica set

    // Database naming for worktrees: "{prefix}{alias}"
    dbNamePrefix: "db_",

    // Seed/dump commands (for mongodb)
    seedCommand: "mongodump --archive --db={sourceDb} | mongorestore --archive --nsFrom='{sourceDb}.*' --nsTo='{targetDb}.*' --drop",
    dropCommand: "mongosh --eval 'db.getSiblingDB(\"{targetDb}\").dropDatabase()'",

    // For postgres/supabase, these would be different:
    // seedCommand: "pg_dump {sourceDb} | psql {targetDb}",
    // dropCommand: "dropdb {targetDb}",
  },

  // ─── Redis ─────────────────────────────────────────────────────────
  redis: {
    // null = no Redis
    containerHost: "redis",
    port: 6379,
  },

  // ─── Environment Variables ─────────────────────────────────────────
  // Controls which env vars are written to .env.worktree and their naming.
  env: {
    // Prefix for project-specific env vars (e.g. "MYAPP", "BC")
    prefix: "MYAPP",

    // The env file name generated inside each worktree
    filename: ".env.worktree",

    // Map of logical keys -> env var names.
    // These are written to .env.worktree for each worktree.
    // Values support templates: {alias}, {name}, {offset}, {domain}, {dbName}
    vars: {
      projectPath:    "{PREFIX}_PATH",
      dbConnection:   "{PREFIX}_LOCAL_MONGO",
      dbReplicaSet:   "{PREFIX}_LOCAL_MONGO_REPLICA_SET",
      redisHost:      "{PREFIX}_LOCAL_REDIS_HOST",
      redisPort:      "{PREFIX}_LOCAL_REDIS_PORT",
      localIp:        "{PREFIX}_LOCAL_IP",
      appUrl:         "{PREFIX}_LOCAL_APP_URL",
      lanDomain:      "{PREFIX}_LAN_DOMAIN",
      adminAccounts:  "{PREFIX}_LOCAL_ADMIN_ACCOUNTS",
      environment:    "{PREFIX}_ENV",
    },

    // Worktree-specific vars (these use the "WORKTREE_" prefix always)
    worktreeVars: {
      name:           "WORKTREE_NAME",
      alias:          "WORKTREE_ALIAS",
      hostBuild:      "WORKTREE_HOST_BUILD",
      services:       "WORKTREE_SERVICES",
      hostPortOffset: "WORKTREE_HOST_PORT_OFFSET",
      portOffset:     "WORKTREE_PORT_OFFSET",
      portBase:       "WORKTREE_PORT_BASE",
      poll:           "WORKTREE_POLL",
      devHeap:        "WORKTREE_DEV_HEAP",
    },
  },

  // ─── Git ──────────────────────────────────────────────────────────
  git: {
    // Paths to mark with git update-index --skip-worktree on creation.
    // Hides noisy local-only changes from git status (build artifacts, lock files, etc.)
    // Empty array or omitted = disabled.
    skipWorktree: ["build/", ".beads/", "CLAUDE.md", "pnpm-lock.yaml"],
  },

  // ─── Features (optional capabilities) ──────────────────────────────
  features: {
    // Host-build mode (esbuild on host, skips livereload in container)
    hostBuild: true,

    // LAN access via nip.io domains
    lan: true,

    // Admin account toggling
    admin: {
      enabled: true,
      defaultUserId: "your-user-id-here",
    },

    // AWS credential mounting into containers
    awsCredentials: true,

    // Container autostop for idle worktrees
    autostop: true,

    // Orphaned volume pruning
    prune: true,

    // Image URL fixing (project-specific, may not be needed)
    imagesFix: false,

    // Prebaked image rebuilding
    rebuildBase: true,

    // Default Node.js heap size for dev (MB)
    devHeap: 2048,
  },

  // ─── Dashboard (worktree-dash specific) ────────────────────────────
  dash: {
    // Commands to open in terminal tabs
    commands: {
      shell:  { label: "Shell", cmd: "bash" },
      claude: { label: "Claude", cmd: "claude" },
      zsh:    { label: "Zsh", cmd: "zsh" },
      logs:   { label: "Logs", cmd: null },  // null = built-in log viewer
      dev:    { label: "Dev", cmd: "pnpm dev" },
      build:  { label: "Build", cmd: null },  // null = uses dc:build
    },

    // Dev start command for local (non-Docker) worktrees
    localDevCommand: "pnpm dev",
  },

  // ─── Paths (resolved relative to repo root) ───────────────────────
  paths: {
    // Where worktree-flow scripts live (for dash to call them)
    flowScripts: "scripts/worktree",

    // Docker overrides directory copied into each worktree
    dockerOverrides: ".docker-overrides",

    // Build script path (for host-build mode)
    buildScript: "scripts/deployment_scripts/build.js",
  },
};
```

---

## Example: build-check config

```js
// ~/dev/build-check/workflow.config.js
module.exports = {
  name: "bc",

  repo: {
    worktreesDir: "../build-check-worktrees",
    branchPrefixes: ["feature", "fix", "chore", "hotfix"],
  },

  docker: {
    baseImage: null,  // uses per-service Dockerfiles
    composeStrategy: "docker/docker-compose.dev.yml",
    composeFile: "docker/docker-compose.dev.yml",

    generate: null,  // not using generated compose

    sharedInfra: {
      network: null,  // no shared network — each worktree is isolated
      composePath: null,
    },

    proxy: {
      type: "ports",  // just port mapping, no Traefik
      domainTemplate: null,
    },
  },

  services: {
    ports: {
      web: 3000,
      api: 4000,
    },

    modes: {
      default: null,  // all services always
    },

    defaultMode: "default",
    primary: "web",

    quickLinks: [
      { label: "Web", service: "web", pathPrefix: "" },
      { label: "API", service: "api", pathPrefix: "/api" },
    ],
  },

  portOffset: {
    algorithm: "cksum",
    min: 1,
    range: 99,
    autoResolve: false,
  },

  database: {
    type: "supabase",
    host: null,           // external (Supabase cloud)
    containerHost: null,
    port: null,
    defaultDb: null,
    seedCommand: null,    // no local DB seeding
    dropCommand: null,
  },

  redis: null,

  env: {
    prefix: "BC",
    filename: ".env.worktree",
    vars: {
      projectPath: null,       // not needed — uses PROJECT_ROOT env var
    },
    worktreeVars: {
      name:           "WORKTREE_NAME",
      alias:          "WORKTREE_ALIAS",
      hostPortOffset: "WORKTREE_HOST_PORT_OFFSET",
    },
  },

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
  },

  dash: {
    commands: {
      shell:  { label: "Shell", cmd: "bash" },
      claude: { label: "Claude", cmd: "claude" },
      dev:    { label: "Dev", cmd: "pnpm dev" },
    },
    localDevCommand: "turbo dev",
  },

  paths: {
    flowScripts: null,  // uses global workflow-flow install
    dockerOverrides: null,
    buildScript: null,
  },
};
```

---

## Example: generate strategy config

```js
// Example: generate strategy project
module.exports = {
  name: "myapp",

  repo: {
    worktreesDir: "../myapp-worktrees",
    branchPrefixes: ["feat", "fix", "ops", "hotfix", "release", "chore"],
  },

  docker: {
    baseImage: "myapp-dev:latest",
    composeStrategy: "generate",

    generate: {
      containerWorkdir: "/app",
      entrypoint: "pnpm dev",
      extraMounts: ["../fakes3:/app/fakes3:cached"],
      extraEnv: {},
    },

    sharedInfra: {
      network: "myapp-infra_default",
      composePath: "~/apps/myapp-infra",
    },

    proxy: {
      type: "traefik",
      dynamicDir: "traefik/dynamic",
      domainTemplate: "{alias}.localhost",
    },
  },

  services: {
    ports: {
      socket_server: 3000,
      app: 3001,
      sync: 3002,
      ship_server: 3003,
      api: 3004,
      job_server: 3005,
      www: 3006,
      cache_server: 3008,
      insights_server: 3010,
      order_table_server: 3012,
      inventory_table_server: 3013,
      admin_server: 3050,
      livereload: 53099,
    },

    modes: {
      minimal: ["socket_server", "app", "api", "admin_server", "cache_server", "order_table_server", "inventory_table_server"],
      full: null,
    },

    defaultMode: "minimal",
    primary: "app",

    quickLinks: [
      { label: "Web", service: "app", pathPrefix: "" },
      { label: "API", service: "api", pathPrefix: "" },
      { label: "Admin", service: "admin_server", pathPrefix: "" },
    ],
  },

  portOffset: {
    algorithm: "sha256",
    min: 100,
    range: 2000,
    autoResolve: true,
  },

  database: {
    type: "mongodb",
    host: "localhost",
    containerHost: "mongo",
    port: 27017,
    defaultDb: "db",
    replicaSet: "rl0",
    dbNamePrefix: "db_",
    seedCommand: "mongodump --archive --db={sourceDb} | mongorestore --archive --nsFrom='{sourceDb}.*' --nsTo='{targetDb}.*' --drop",
    dropCommand: "mongosh --eval 'db.getSiblingDB(\"{targetDb}\").dropDatabase()'",
  },

  redis: {
    containerHost: "redis",
    port: 6379,
  },

  env: {
    prefix: "MYAPP",
    filename: ".env.worktree",
    vars: {
      projectPath:    "{PREFIX}_PATH",
      dbConnection:   "{PREFIX}_LOCAL_MONGO",
      dbReplicaSet:   "{PREFIX}_LOCAL_MONGO_REPLICA_SET",
      redisHost:      "{PREFIX}_LOCAL_REDIS_HOST",
      redisPort:      "{PREFIX}_LOCAL_REDIS_PORT",
      localIp:        "{PREFIX}_LOCAL_IP",
      appUrl:         "{PREFIX}_LOCAL_APP_URL",
      lanDomain:      "{PREFIX}_LAN_DOMAIN",
      adminAccounts:  "{PREFIX}_LOCAL_ADMIN_ACCOUNTS",
      environment:    "{PREFIX}_ENV",
    },
    worktreeVars: {
      name:           "WORKTREE_NAME",
      alias:          "WORKTREE_ALIAS",
      hostBuild:      "WORKTREE_HOST_BUILD",
      services:       "WORKTREE_SERVICES",
      hostPortOffset: "WORKTREE_HOST_PORT_OFFSET",
      portOffset:     "WORKTREE_PORT_OFFSET",
      portBase:       "WORKTREE_PORT_BASE",
      poll:           "WORKTREE_POLL",
      devHeap:        "WORKTREE_DEV_HEAP",
    },
  },

  features: {
    hostBuild: true,
    lan: true,
    admin: {
      enabled: true,
      defaultUserId: "your-user-id-here",
    },
    awsCredentials: true,
    autostop: true,
    prune: true,
    imagesFix: true,
    rebuildBase: true,
    devHeap: 2048,
  },

  dash: {
    commands: {
      shell:  { label: "Shell", cmd: "bash" },
      claude: { label: "Claude", cmd: "claude" },
      zsh:    { label: "Zsh", cmd: "zsh" },
      logs:   { label: "Logs", cmd: null },
      dev:    { label: "Dev", cmd: "pnpm dev" },
      build:  { label: "Build", cmd: null },
    },
    localDevCommand: "pnpm dev",
  },

  paths: {
    flowScripts: "scripts/worktree",
    dockerOverrides: ".docker-overrides",
    buildScript: "scripts/deployment_scripts/build.js",
  },
};
```

---

## Key Design Decisions

### 1. Two compose strategies
- **`"generate"`**: worktree-flow generates a `docker-compose.worktree.yml` per worktree with hardcoded service definitions, ports, mounts. Traefik support writes dynamic config files to `proxy.dynamicDir`.
- **`"<path>"`** (build-check pattern): uses a shared compose file with env var substitution (`${WEB_PORT}`, `${BRANCH_SLUG}`, `${PROJECT_ROOT}`). Traefik support auto-generates a `docker-compose.traefik.yml` override with per-service labels and network attachment.

### 2. Environment variable templating
`{PREFIX}` is replaced with `env.prefix` at runtime. This lets the same logical var names produce project-specific env var names (`MYAPP_MONGO_URL` vs `BC_LOCAL_MONGO`).

### 3. Feature flags
Optional capabilities (host-build, LAN, admin, imagesFix) are toggled per project. The CLI and dashboard hide/show actions based on these flags.

### 4. Null = disabled
Any section set to `null` disables that feature. e.g. `redis: null` means no Redis, `database.seedCommand: null` means no seed operation available.

### 5. JavaScript format
Allows `require()` for shared config, `process.env` access, and comments. Both the Node.js loader (worktree-flow) and Go loader (worktree-dash) can read this — Go parses it via `node -e "console.log(JSON.stringify(require('./workflow.config.js')))"`.

### 6. `wt init` auto-detection
`workflow-init.js` auto-detects the local environment to pre-populate config values:
- **Traefik**: runs `docker ps -a` to find running Traefik containers, inspects their networks, and sets `docker.proxy.type = "traefik"` with the detected network name.
- **Claude**: searches common paths (`which claude`, `~/.claude/local/claude`, nvm bins, Cursor extensions) and sets `dash.commands.claude.cmd` to the full resolved path.

### 7. Per-worktree runtime artifacts
These files are generated per worktree and should never be committed:
- `.env.worktree` — environment variables
- `docker-compose.worktree.yml` — generated compose file (generate strategy)
- `docker-compose.traefik.yml` — Traefik override labels (shared strategy + Traefik proxy)
- `.docker-overrides/` — copied override files

---

## Hardcoded Reference Mapping

Every hardcoded value found in the audit maps to a config field:

| Hardcoded Value | Config Path |
|---|---|
| `"myapp-"` container prefix | `name` + `"-"` |
| `"myapp_"` volume prefix | `name` + `"_"` |
| `"myapp-dev:latest"` | `docker.baseImage` |
| `"myapp-infra_default"` | `docker.sharedInfra.network` |
| derived from composePath + dynamicDir | `docker.sharedInfra.composePath` + `docker.proxy.dynamicDir` |
| `"~/apps/myapp-infra"` | `docker.sharedInfra.composePath` |
| `"/app"` mount path | `docker.generate.containerWorkdir` |
| `"pnpm dev"` entrypoint | `docker.generate.entrypoint` |
| `"fakes3"` mount | `docker.generate.extraMounts` |
| `3000-53099` port numbers | `services.ports.*` |
| `MINIMAL_SERVICES` array | `services.modes.minimal` |
| `"minimal"/"full"` mode names | keys of `services.modes` |
| `MYAPP_*` env vars | `env.vars.*` with `env.prefix` |
| `WORKTREE_*` env vars | `env.worktreeVars.*` |
| `"mongo"` | `database.containerHost` |
| `27017` | `database.port` |
| `"db"` default database | `database.defaultDb` |
| `"db_"` prefix | `database.dbNamePrefix` |
| `"rl0"` replica set | `database.replicaSet` |
| `"redis"` | `redis.containerHost` |
| `6379` | `redis.port` |
| `feat/fix/ops/...` branches | `repo.branchPrefixes` |
| `origin/main`, `origin/master` base refs | `repo.baseRefs` |
| `"{alias}.localhost"` domain | `docker.proxy.domainTemplate` |
| `"your-user-id-here"` admin | `features.admin.defaultUserId` |
| `2048` heap size | `features.devHeap` |
| `"--filter name=myapp-"` | derived from `name` |
| SHA-256 offset algorithm | `portOffset.algorithm` |
| `% 2000 + 100` offset range | `portOffset.range` + `portOffset.min` |
| `"scripts/worktree"` path | `paths.flowScripts` |
| `".docker-overrides"` | `paths.dockerOverrides` |
