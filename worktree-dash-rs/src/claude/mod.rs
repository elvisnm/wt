use serde::Deserialize;

/// Claude API usage data from platform.claude.com
#[derive(Debug, Clone, Deserialize)]
pub struct Usage {
    pub five_hour: UsagePeriod,
    pub seven_day: UsagePeriod,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UsagePeriod {
    pub utilization: f64,
    pub resets_at: Option<String>,
}

#[derive(Debug, Deserialize)]
struct Credentials {
    #[serde(rename = "claudeAiOauth")]
    claude_ai_oauth: Option<OAuthTokens>,
}

#[derive(Debug, Deserialize)]
struct OAuthTokens {
    #[serde(rename = "accessToken")]
    access_token: String,
    #[serde(rename = "refreshToken")]
    refresh_token: Option<String>,
}

#[derive(Debug, Deserialize)]
struct RefreshResponse {
    access_token: String,
    refresh_token: Option<String>,
}

const KEYCHAIN_SERVICE: &str = "Claude Code-credentials";
const TOKEN_URL: &str = "https://platform.claude.com/v1/oauth/token";
const USAGE_URL: &str = "https://platform.claude.com/api/oauth/usage";
const CLIENT_ID: &str = "9d1c250a-e61b-44d9-88ed-5944d1962f5e";

/// Fetch Claude usage — handles token refresh on 401.
pub fn fetch_usage() -> Result<Usage, String> {
    let creds = read_credentials()?;
    let oauth = creds.claude_ai_oauth
        .ok_or("no claudeAiOauth in credentials")?;

    // Try with current token
    match call_usage_api(&oauth.access_token) {
        Ok(usage) => return Ok(usage),
        Err(e) if e.contains("401") => {
            // Token expired — try refresh
        }
        Err(e) => return Err(e),
    }

    // Refresh token
    let refresh_token = oauth.refresh_token
        .ok_or("token expired, no refresh token available")?;

    let new_tokens = refresh_access_token(&refresh_token)?;

    // Write refreshed tokens back to keychain
    let _ = write_tokens(&new_tokens.access_token, new_tokens.refresh_token.as_deref().unwrap_or(&refresh_token));

    // Retry with new token
    call_usage_api(&new_tokens.access_token)
}

fn call_usage_api(token: &str) -> Result<Usage, String> {
    let output = std::process::Command::new("curl")
        .args([
            "-s",
            "-w", "\n%{http_code}",
            "-H", &format!("Authorization: Bearer {}", token),
            "-H", "anthropic-beta: oauth-2025-04-20",
            USAGE_URL,
        ])
        .output()
        .map_err(|e| format!("curl failed: {}", e))?;

    let body = String::from_utf8_lossy(&output.stdout).to_string();
    let lines: Vec<&str> = body.trim().rsplitn(2, '\n').collect();
    let status = lines.first().unwrap_or(&"0");
    let response = if lines.len() > 1 { lines[1] } else { "" };

    if *status == "401" {
        return Err("401 unauthorized".to_string());
    }
    if !status.starts_with('2') {
        return Err(format!("usage API {}: {}", status, &response.chars().take(100).collect::<String>()));
    }

    serde_json::from_str(response)
        .map_err(|e| format!("failed to parse usage: {}", e))
}

fn refresh_access_token(refresh_token: &str) -> Result<RefreshResponse, String> {
    let body = serde_json::json!({
        "grant_type": "refresh_token",
        "refresh_token": refresh_token,
        "client_id": CLIENT_ID,
        "scope": "user:profile user:inference user:sessions:claude_code user:mcp_servers",
    });

    let output = std::process::Command::new("curl")
        .args([
            "-s",
            "-X", "POST",
            "-H", "Content-Type: application/json",
            "-d", &body.to_string(),
            TOKEN_URL,
        ])
        .output()
        .map_err(|e| format!("refresh request failed: {}", e))?;

    if !output.status.success() {
        return Err("token refresh request failed".to_string());
    }

    serde_json::from_slice(&output.stdout)
        .map_err(|e| format!("failed to parse refresh response: {}", e))
}

fn read_credentials() -> Result<Credentials, String> {
    let output = std::process::Command::new("security")
        .args(["find-generic-password", "-s", KEYCHAIN_SERVICE, "-w"])
        .output()
        .map_err(|e| format!("keychain read failed: {}", e))?;

    if !output.status.success() {
        return Err("no Claude Code credentials in Keychain".to_string());
    }

    let raw = String::from_utf8_lossy(&output.stdout).trim().to_string();
    serde_json::from_str(&raw)
        .map_err(|e| format!("invalid credentials JSON: {}", e))
}

fn write_tokens(access_token: &str, refresh_token: &str) -> Result<(), String> {
    // Read current credentials, update tokens, write back
    let mut creds: serde_json::Value = {
        let output = std::process::Command::new("security")
            .args(["find-generic-password", "-s", KEYCHAIN_SERVICE, "-w"])
            .output()
            .map_err(|e| format!("keychain read failed: {}", e))?;
        let raw = String::from_utf8_lossy(&output.stdout).trim().to_string();
        serde_json::from_str(&raw).map_err(|e| format!("parse: {}", e))?
    };

    if let Some(oauth) = creds.get_mut("claudeAiOauth") {
        oauth["accessToken"] = serde_json::json!(access_token);
        oauth["refreshToken"] = serde_json::json!(refresh_token);
    }

    let updated = creds.to_string();

    // Delete old and add new
    let _ = std::process::Command::new("security")
        .args(["delete-generic-password", "-s", KEYCHAIN_SERVICE])
        .output();

    let output = std::process::Command::new("security")
        .args(["add-generic-password", "-s", KEYCHAIN_SERVICE, "-w", &updated, "-U"])
        .output()
        .map_err(|e| format!("keychain write failed: {}", e))?;

    if !output.status.success() {
        return Err("keychain write failed".to_string());
    }
    Ok(())
}
