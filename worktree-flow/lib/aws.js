/**
 * aws.js — Unified AWS credential management for worktree-flow.
 *
 * Provides a single entry point for loading AWS credentials, supporting
 * both SSO profile export and plaintext credentials file reading.
 * Standalone CLI usage (without the Go dashboard) now gets SSO support.
 */

const { execSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

/**
 * Read ~/.aws/credentials and return an env object with AWS_* vars.
 * Returns {} if the file doesn't exist or can't be parsed.
 */
function load_aws_env() {
  const creds_path = path.join(os.homedir(), '.aws', 'credentials');
  let data;
  try {
    data = fs.readFileSync(creds_path, 'utf8');
  } catch {
    return {};
  }

  const env = {};
  const key_map = {
    aws_access_key_id: 'AWS_ACCESS_KEY_ID',
    aws_secret_access_key: 'AWS_SECRET_ACCESS_KEY',
    aws_session_token: 'AWS_SESSION_TOKEN',
  };

  for (const line of data.split('\n')) {
    const trimmed = line.trim();
    if (trimmed.startsWith('[') || trimmed === '') continue;
    const idx = trimmed.indexOf('=');
    if (idx === -1) continue;
    const key = trimmed.slice(0, idx).trim();
    const val = trimmed.slice(idx + 1).trim();
    if (key_map[key]) {
      env[key_map[key]] = val;
    }
  }

  return env;
}

/**
 * Export SSO credentials by running `aws configure export-credentials`.
 * Parses KEY=VALUE output, writes to ~/.aws/credentials, and returns
 * the env object.
 *
 * @param {string} profile - SSO profile name
 * @returns {object} { AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN }
 * @throws {Error} if export fails or returns incomplete credentials
 */
function export_sso_credentials(profile) {
  const output = execSync(
    `aws configure export-credentials --profile "${profile}" --format env-no-export`,
    { stdio: ['pipe', 'pipe', 'pipe'], encoding: 'utf8' },
  );

  const creds = {};
  for (const line of output.split('\n')) {
    const trimmed = line.trim();
    const parts = trimmed.split('=');
    if (parts.length < 2) continue;
    const key = parts[0];
    const val = parts.slice(1).join('=');
    creds[key] = val;
  }

  const access_key = creds.AWS_ACCESS_KEY_ID;
  const secret_key = creds.AWS_SECRET_ACCESS_KEY;
  const session_token = creds.AWS_SESSION_TOKEN;

  if (!access_key || !secret_key) {
    throw new Error('export-credentials returned incomplete credentials');
  }

  // Write to ~/.aws/credentials so the SDK credential chain picks them up
  const creds_content = `[default]\naws_access_key_id = ${access_key}\naws_secret_access_key = ${secret_key}\naws_session_token = ${session_token || ''}\n`;
  const creds_path = path.join(os.homedir(), '.aws', 'credentials');
  try {
    fs.writeFileSync(creds_path, creds_content, { mode: 0o600 });
  } catch {
    // best-effort — return the env vars regardless
  }

  return {
    AWS_ACCESS_KEY_ID: access_key,
    AWS_SECRET_ACCESS_KEY: secret_key,
    AWS_SESSION_TOKEN: session_token || '',
  };
}

/**
 * Refresh AWS credentials — unified entry point.
 *
 * If config has awsCredentials.ssoProfile, exports from SSO session.
 * Otherwise reads from ~/.aws/credentials file.
 *
 * @param {object|null} config - Loaded workflow config (or null)
 * @returns {object} AWS env vars { AWS_ACCESS_KEY_ID, ... }
 */
function refresh_credentials(config) {
  if (config && config.features && config.features.awsCredentials) {
    const aws_config = config.features.awsCredentials;
    const profile = typeof aws_config === 'object' ? aws_config.ssoProfile : null;
    if (profile) {
      try {
        return export_sso_credentials(profile);
      } catch {
        // SSO export failed — fall back to credentials file
      }
    }
  }
  return load_aws_env();
}

module.exports = {
  load_aws_env,
  export_sso_credentials,
  refresh_credentials,
};
