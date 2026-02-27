// Test fixture config for config_test.go
module.exports = {
  name: 'myapp',

  repo: {
    worktreesDir: '../myapp-worktrees',
    branchPrefixes: ['feat', 'fix', 'hotfix'],
    baseRefs: ['origin/main'],
  },

  docker: {
    baseImage: 'myapp-dev:latest',
    composeStrategy: 'generate',

    sharedInfra: {
      network: 'myapp-infra_default',
      composePath: '/tmp/myapp-infra',
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
  },

  database: {
    type: 'mongodb',
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
      projectPath:  '{PREFIX}_PATH',
      dbConnection: '{PREFIX}_MONGO_URL',
      redisHost:    '{PREFIX}_REDIS_HOST',
    },
    worktreeVars: {
      name:           'WORKTREE_NAME',
      alias:          'WORKTREE_ALIAS',
      hostBuild:      'WORKTREE_HOST_BUILD',
      services:       'WORKTREE_SERVICES',
      hostPortOffset: 'WORKTREE_HOST_PORT_OFFSET',
      portOffset:     'WORKTREE_PORT_OFFSET',
      portBase:       'WORKTREE_PORT_BASE',
    },
  },

  features: {
    hostBuild: true,
    admin: { enabled: true, defaultUserId: null },
  },

  dash: {
    commands: {
      shell: { label: 'Shell', cmd: 'bash' },
    },
    localDevCommand: 'pnpm dev',
  },
};
