const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const config_mod = require('./config');
const config = config_mod.load_config({ required: false }) || null;

function resolve_worktree_path(name) {
  const repo_root = execSync('git rev-parse --show-toplevel', {
    stdio: 'pipe',
    encoding: 'utf8',
  }).trim();
  const worktrees_dir = config && config.repo._worktreesDirResolved
    ? config.repo._worktreesDirResolved
    : path.join(path.dirname(repo_root), `${path.basename(repo_root)}-worktrees`);
  return path.join(worktrees_dir, name.replace(/\//g, '-'));
}

function read_env_value(env_file, key) {
  if (!fs.existsSync(env_file)) return null;
  const content = fs.readFileSync(env_file, 'utf8');
  const match = content.match(new RegExp(`^${key}=(.+)$`, 'm'));
  return match ? match[1].trim() : null;
}

function parse_db_name_from_mongo_url(url) {
  if (!url) return null;
  const match = url.match(/\/([^/?]+)(\?|$)/);
  return match ? match[1] : null;
}

function find_mongo_container() {
  try {
    const output = execSync('docker ps --format "{{.Names}}"', {
      stdio: 'pipe',
      encoding: 'utf8',
    }).trim();
    const names = output.split('\n').filter(Boolean);
    const project_name = config ? config.name : 'project';
    return names.find((n) => n.includes(project_name) && n.includes('mongo')) || null;
  } catch {
    return null;
  }
}

const MONGO_HOST = config
  ? `${config.database.host}:${config.database.port}`
  : 'localhost:27017';
const SOURCE_DB = config ? config.database.defaultDb : 'db';

function parse_args(argv) {
  const options = { name: null, db: null, drop: false, reset: false };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--drop') {
      options.drop = true;
    } else if (arg === '--reset') {
      options.reset = true;
    } else if (arg === '--db') {
      options.db = argv[++i];
    } else if (arg.startsWith('--db=')) {
      options.db = arg.split('=')[1];
    } else if (!arg.startsWith('--') && !options.name) {
      options.name = arg;
    }
  }
  return options;
}

function resolve_target_db(name) {
  const worktree_path = resolve_worktree_path(name);
  if (!fs.existsSync(worktree_path)) {
    console.error(`Worktree not found: ${worktree_path}`);
    process.exit(1);
  }

  const env_file = path.join(worktree_path, '.env.worktree');
  const mongo_env_key = config ? config_mod.env_var(config, 'dbConnection') : 'MONGO_URL';
  const mongo_url = read_env_value(env_file, mongo_env_key);
  const target_db = parse_db_name_from_mongo_url(mongo_url);

  if (!target_db) {
    console.error(`Could not determine target database from .env.worktree ${mongo_env_key}`);
    process.exit(1);
  }

  if (target_db === SOURCE_DB) {
    console.error(`Target database is "${SOURCE_DB}" (the shared database). Nothing to do.`);
    console.error('This worktree does not have a per-worktree database. Re-create it with --alias to enable isolation.');
    process.exit(1);
  }

  return target_db;
}

function require_mongo_container() {
  const container = find_mongo_container();
  if (!container) {
    console.error('Could not find a running MongoDB container. Is the infra stack running?');
    process.exit(1);
  }
  return container;
}

function seed_database(mongo_container, target_db) {
  console.log(`Seeding: ${SOURCE_DB} -> ${target_db}`);
  console.log(`Mongo container: ${mongo_container}`);
  console.log('');

  const dump_cmd = `docker exec ${mongo_container} mongodump --host ${MONGO_HOST} --db ${SOURCE_DB} --archive`;
  const restore_cmd = `docker exec -i ${mongo_container} mongorestore --host ${MONGO_HOST} --archive --nsFrom="${SOURCE_DB}.*" --nsTo="${target_db}.*" --drop`;

  try {
    execSync(`${dump_cmd} | ${restore_cmd}`, {
      stdio: 'inherit',
      shell: true,
    });
  } catch (error) {
    console.error('');
    console.error('Seed failed:', error.message);
    process.exit(1);
  }

  console.log('');
  console.log(`Seed complete. Database "${target_db}" is ready.`);
}

const PROTECTED_DBS = ['db', 'admin', 'local', 'config'];

function drop_database(mongo_container, target_db) {
  if (PROTECTED_DBS.includes(target_db)) {
    console.error(`REFUSED: Cannot drop "${target_db}" — this is a protected database.`);
    process.exit(1);
  }

  console.log(`Dropping database: ${target_db}`);
  console.log(`Mongo container: ${mongo_container}`);
  console.log('');

  try {
    execSync(
      `docker exec ${mongo_container} mongosh --host ${MONGO_HOST} --quiet --eval "db.getSiblingDB('${target_db}').dropDatabase()"`,
      { stdio: 'inherit' },
    );
  } catch {
    try {
      execSync(
        `docker exec ${mongo_container} mongo --host ${MONGO_HOST} --quiet --eval "db.getSiblingDB('${target_db}').dropDatabase()"`,
        { stdio: 'inherit' },
      );
    } catch (error) {
      console.error('');
      console.error('Drop failed:', error.message);
      process.exit(1);
    }
  }

  console.log('');
  console.log(`Database "${target_db}" dropped.`);
}

function main() {
  const options = parse_args(process.argv.slice(2));

  if (!options.name && !options.db) {
    console.log('Usage:');
    console.log('  pnpm dc:seed <worktree-name>            Copy shared db into worktree db');
    console.log('  pnpm dc:seed <worktree-name> --drop      Drop the worktree db');
    console.log('  pnpm dc:seed <worktree-name> --reset     Drop and re-seed from shared db');
    console.log('  pnpm dc:seed --db=<db_name> --drop       Drop a database directly (worktree not needed)');
    process.exit(1);
  }

  const target_db = options.db || resolve_target_db(options.name);

  if (PROTECTED_DBS.includes(target_db)) {
    console.error(`REFUSED: Cannot operate on "${target_db}" — this is a protected database.`);
    process.exit(1);
  }

  const mongo_container = require_mongo_container();

  if (options.drop) {
    drop_database(mongo_container, target_db);
  } else if (options.reset) {
    drop_database(mongo_container, target_db);
    console.log('');
    seed_database(mongo_container, target_db);
  } else {
    seed_database(mongo_container, target_db);
  }
}

main();
