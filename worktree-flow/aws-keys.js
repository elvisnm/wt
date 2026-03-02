const fs = require('fs');
const path = require('path');
const os = require('os');
const readline = require('readline');

const SENTINEL_PATH = path.join(os.tmpdir(), 'wt-aws-keys-done');
const DEBUG_LOG = path.join(os.tmpdir(), 'wt-debug.log');

function debug(msg) {
  if (process.env.WT_DEBUG !== '1') return;
  const ts = new Date().toTimeString().slice(0, 12);
  try { fs.appendFileSync(DEBUG_LOG, `[${ts}] [aws-keys.js] ${msg}\n`); } catch {}
}

function detect_shell() {
  const shell = process.env.SHELL || '';
  const base = path.basename(shell);

  if (base === 'zsh') return 'zsh';
  if (base === 'bash') return 'bash';
  if (base === 'fish') return 'fish';

  return 'unknown';
}

function resolve_profile_path(shell_name) {
  const home = os.homedir();

  if (shell_name === 'zsh') {
    const candidates = ['.zprofile', '.zshrc'];
    for (const f of candidates) {
      const p = path.join(home, f);
      if (fs.existsSync(p)) return p;
    }
    return path.join(home, '.zprofile');
  }

  if (shell_name === 'bash') {
    const candidates = ['.bash_profile', '.bashrc', '.profile'];
    for (const f of candidates) {
      const p = path.join(home, f);
      if (fs.existsSync(p)) return p;
    }
    return path.join(home, '.bash_profile');
  }

  if (shell_name === 'fish') {
    return path.join(home, '.config', 'fish', 'config.fish');
  }

  return null;
}

function parse_aws_keys(lines) {
  const combined = lines.join('\n');

  const key_match = combined.match(/AWS_ACCESS_KEY_ID[=\s]*"?([A-Za-z0-9/+=]+)"?/);
  const secret_match = combined.match(/AWS_SECRET_ACCESS_KEY[=\s]*"?([A-Za-z0-9/+=]+)"?/);
  const token_match = combined.match(/AWS_SESSION_TOKEN[=\s]*"?([A-Za-z0-9/+=]+)"?/);

  if (!key_match || !secret_match || !token_match) {
    return null;
  }

  return {
    access_key_id: key_match[1],
    secret_access_key: secret_match[1],
    session_token: token_match[1],
  };
}

function update_shell_profile(keys, shell_name, profile_path) {
  if (!profile_path) {
    console.warn(`Could not detect shell profile for "${shell_name}". Skipping profile update.`);
    console.warn('~/.aws/credentials was still updated — the AWS SDK will use it directly.');
    return;
  }

  if (shell_name === 'fish') {
    update_fish_profile(keys, profile_path);
    return;
  }

  let content = '';
  if (fs.existsSync(profile_path)) {
    content = fs.readFileSync(profile_path, 'utf8');
  }

  content = content
    .replace(/^export AWS_ACCESS_KEY_ID=.*\n?/m, '')
    .replace(/^export AWS_SECRET_ACCESS_KEY=.*\n?/m, '')
    .replace(/^export AWS_SESSION_TOKEN=.*\n?/m, '');

  content = content.trimEnd() + '\n';
  content += `export AWS_ACCESS_KEY_ID="${keys.access_key_id}"\n`;
  content += `export AWS_SECRET_ACCESS_KEY="${keys.secret_access_key}"\n`;
  content += `export AWS_SESSION_TOKEN="${keys.session_token}"\n`;

  fs.writeFileSync(profile_path, content, 'utf8');
  console.log(`Updated ${profile_path}`);
}

function update_fish_profile(keys, profile_path) {
  const dir = path.dirname(profile_path);
  fs.mkdirSync(dir, { recursive: true });

  let content = '';
  if (fs.existsSync(profile_path)) {
    content = fs.readFileSync(profile_path, 'utf8');
  }

  content = content
    .replace(/^set -gx AWS_ACCESS_KEY_ID .*\n?/m, '')
    .replace(/^set -gx AWS_SECRET_ACCESS_KEY .*\n?/m, '')
    .replace(/^set -gx AWS_SESSION_TOKEN .*\n?/m, '');

  content = content.trimEnd() + '\n';
  content += `set -gx AWS_ACCESS_KEY_ID "${keys.access_key_id}"\n`;
  content += `set -gx AWS_SECRET_ACCESS_KEY "${keys.secret_access_key}"\n`;
  content += `set -gx AWS_SESSION_TOKEN "${keys.session_token}"\n`;

  fs.writeFileSync(profile_path, content, 'utf8');
  console.log(`Updated ${profile_path}`);
}

function update_aws_credentials(keys) {
  const aws_dir = path.join(os.homedir(), '.aws');
  fs.mkdirSync(aws_dir, { recursive: true });

  const creds_path = path.join(aws_dir, 'credentials');
  const content = `[default]
aws_access_key_id = ${keys.access_key_id}
aws_secret_access_key = ${keys.secret_access_key}
aws_session_token = ${keys.session_token}
`;

  fs.writeFileSync(creds_path, content, 'utf8');
  console.log(`Updated ${creds_path}`);
}

async function read_input_lines() {
  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
  });

  const lines = [];

  return new Promise((resolve) => {
    console.log('Paste your AWS export block (3 lines, press Enter after each):');
    console.log('');

    rl.on('line', (line) => {
      lines.push(line);
      if (lines.length >= 3) {
        rl.close();
        resolve(lines);
      }
    });

    rl.on('close', () => {
      resolve(lines);
    });
  });
}

async function main() {
  debug('main: started, SENTINEL_PATH=' + SENTINEL_PATH);
  const lines = await read_input_lines();
  debug('main: got ' + lines.length + ' lines');

  // Clean up any stale sentinel from a previous run
  try { fs.unlinkSync(SENTINEL_PATH); } catch {}
  debug('main: cleaned stale sentinel');

  if (lines.length < 3) {
    debug('main: FAIL - only ' + lines.length + ' lines');
    console.error('Expected 3 lines of AWS export commands.');
    fs.writeFileSync(SENTINEL_PATH, '1');
    debug('main: wrote sentinel=1, exiting');
    process.exit(1);
  }

  const keys = parse_aws_keys(lines);
  if (!keys) {
    debug('main: FAIL - could not parse keys from lines');
    console.error('Failed to parse AWS keys. Make sure you pasted the full export block.');
    console.error('Expected format:');
    console.error('  export AWS_ACCESS_KEY_ID="..."');
    console.error('  export AWS_SECRET_ACCESS_KEY="..."');
    console.error('  export AWS_SESSION_TOKEN="..."');
    fs.writeFileSync(SENTINEL_PATH, '1');
    debug('main: wrote sentinel=1, exiting');
    process.exit(1);
  }

  debug('main: parsed keys OK, access_key_id=' + keys.access_key_id.slice(0, 8) + '...');
  console.log('');
  console.log(`AWS_ACCESS_KEY_ID=${keys.access_key_id}`);
  console.log('AWS_SECRET_ACCESS_KEY=[hidden]');
  console.log('AWS_SESSION_TOKEN=[hidden]');
  console.log('');

  const shell_name = detect_shell();
  const profile_path = resolve_profile_path(shell_name);
  debug('main: shell=' + shell_name + ' profile=' + profile_path);

  console.log(`Detected shell: ${shell_name} (${process.env.SHELL || 'unknown'})`);

  try {
    update_shell_profile(keys, shell_name, profile_path);
    debug('main: update_shell_profile done');
  } catch (e) {
    debug('main: update_shell_profile ERROR: ' + e.message);
    console.error('Warning: failed to update shell profile:', e.message);
  }

  try {
    update_aws_credentials(keys);
    debug('main: update_aws_credentials done');
  } catch (e) {
    debug('main: update_aws_credentials ERROR: ' + e.message);
    console.error('Error: failed to write ~/.aws/credentials:', e.message);
    fs.writeFileSync(SENTINEL_PATH, '1');
    debug('main: wrote sentinel=1 (credentials write failed), exiting');
    process.exit(1);
  }

  console.log('');
  console.log('Done. Dashboard will restart services with fresh keys.');

  debug('main: writing sentinel=0');
  fs.writeFileSync(SENTINEL_PATH, '0');
  debug('main: sentinel written, verifying...');
  const verify = fs.readFileSync(SENTINEL_PATH, 'utf8');
  debug('main: sentinel verified: ' + JSON.stringify(verify));
  debug('main: exiting 0');
  process.exit(0);
}

main();
