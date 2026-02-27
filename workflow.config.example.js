// workflow.config.example.js — Example workflow configuration
//
// Copy this to your project root as `workflow.config.js` and customize.
// This example shows a Node.js monolith using the "generate" compose strategy
// with MongoDB, Redis, and Traefik proxy.

module.exports = {
  name: 'myapp',

  repo: {
    worktreesDir: '../myapp-worktrees',
    branchPrefixes: ['feat', 'fix', 'ops', 'hotfix', 'release', 'chore'],
    baseRefs: ['origin/main', 'origin/develop'],
  },

  docker: {
    baseImage: 'myapp-dev:latest',
    composeStrategy: 'generate',

    generate: {
      containerWorkdir: '/app',
      entrypoint: 'pnpm dev',
      extraMounts: [],
      extraEnv: {},
      overrideFiles: [
        // { src: 'ecosystem.config.js', dst: '.docker-overrides/ecosystem.config.js' },
      ],
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
      admin: 3002,
    },

    modes: {
      minimal: ['web', 'api'],
      full: null,
    },

    defaultMode: 'minimal',
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
    type: 'mongodb',
    host: 'localhost',
    containerHost: 'mongo',
    port: 27017,
    defaultDb: 'myapp',
    replicaSet: 'rs0',
    dbNamePrefix: 'db_',
    seedCommand: "mongodump --archive --db={sourceDb} | mongorestore --archive --nsFrom='{sourceDb}.*' --nsTo='{targetDb}.*' --drop",
    dropCommand: 'mongosh --eval \'db.getSiblingDB("{targetDb}").dropDatabase()\'',
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
      adminAccounts: '{PREFIX}_ADMIN_ACCOUNTS',
      environment:   '{PREFIX}_ENV',
    },
  },

  git: {
    skipWorktree: ['build/', '.beads/', 'CLAUDE.md', 'pnpm-lock.yaml'],
  },

  features: {
    hostBuild: true,
    lan: true,
    admin: {
      enabled: true,
      defaultUserId: null, // set to a user ID from your database
    },
    awsCredentials: false,
    autostop: true,
    prune: true,
    imagesFix: false,
    rebuildBase: true,
    devHeap: 2048,
  },

  dash: {
    commands: {
      shell:  { label: 'Shell',  cmd: 'bash' },
      claude: { label: 'Claude', cmd: 'claude' },
      zsh:    { label: 'Zsh',    cmd: 'zsh' },
      logs:   { label: 'Logs',   cmd: null },
      dev:    { label: 'Dev',    cmd: 'pnpm dev' },
      build:  { label: 'Build',  cmd: null },
    },
    localDevCommand: 'pnpm dev',
  },

  paths: {
    flowScripts: null,         // null = use wt's built-in scripts
    dockerOverrides: '.docker-overrides',
    buildScript: null,
  },
};
