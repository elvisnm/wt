// wt.config.example.js — Example wt configuration
//
// Copy this to your project root as `wt.config.js` and customize.
// This example shows a Node.js project using the "generate" compose strategy
// with MongoDB, Redis, and Traefik proxy.
//
// For a minimal config, you only need: name, docker.baseImage, and services.ports.

module.exports = {
  name: 'myapp',

  repo: {
    worktreesDir: '../myapp-worktrees',
    branchPrefixes: ['feat', 'fix', 'ops', 'hotfix', 'release', 'chore'],
    baseRefs: ['origin/main', 'origin/develop'],
  },

  docker: {
    baseImage: 'myapp-dev:latest',
    composeStrategy: 'generate', // 'generate' or 'shared'

    generate: {
      containerWorkdir: '/app',
      entrypoint: 'npm start',
      entrypointScript: null, // e.g. 'docker-entrypoint.sh' — sets ENTRYPOINT in compose
      extraMounts: [],
      extraEnv: {},
    },

    sharedInfra: {
      network: 'myapp-infra_default',
      composePath: '~/apps/myapp-infra',
    },

    proxy: {
      type: 'traefik',
      dynamicDir: 'traefik/dynamic',
      domainTemplate: '{alias}.localhost',
    },
  },

  services: {
    ports: {
      web: 3000,
      api: 3001,
    },

    primary: 'web',

    quickLinks: [
      { label: 'Web', service: 'web', pathPrefix: '' },
      { label: 'API', service: 'api', pathPrefix: '' },
    ],
  },

  portOffset: {
    algorithm: 'sha256',
    min: 100,
    range: 2000,
    autoResolve: true,
  },

  database: {
    type: 'mongodb', // 'mongodb', 'postgresql', 'mysql', or null
    host: 'localhost',
    containerHost: 'mongo',
    port: 27017,
    defaultDb: 'myapp',
    replicaSet: 'rs0',
    dbNamePrefix: 'db_',
  },

  redis: {
    containerHost: 'redis',
    port: 6379,
  },

  env: {
    prefix: 'MYAPP',
    filename: '.env.worktree',
    vars: {
      projectPath:   '{PREFIX}_PATH',
      dbConnection:  '{PREFIX}_MONGO_URL',
      dbReplicaSet:  '{PREFIX}_MONGO_REPLICA_SET',
      redisHost:     '{PREFIX}_REDIS_HOST',
      redisPort:     '{PREFIX}_REDIS_PORT',
      localIp:       '{PREFIX}_LOCAL_IP',
      appUrl:        '{PREFIX}_APP_URL',
      lanDomain:     '{PREFIX}_LAN_DOMAIN',
      environment:   '{PREFIX}_ENV',
    },
  },

  setup: {
    symlinks: [
      // { src: '../.claude/skills', dst: '.claude/skills' },
    ],
  },

  git: {
    skipWorktree: [],
  },

  features: {
    lan: false,
    autostop: true,
    prune: true,
    rebuildBase: false,
  },

  dash: {
    commands: {
      shell:  { label: 'Shell',  cmd: 'bash' },
      claude: { label: 'Claude', cmd: 'claude' },
    },
    localDevCommand: 'npm start',
  },

  paths: {
    flowScripts: null, // null = use wt's built-in scripts
  },
};
