use std::process::Command;

/// Execute a command and return trimmed stdout.
pub fn run_cmd(name: &str, args: &[&str]) -> Result<String, std::io::Error> {
    let output = Command::new(name).args(args).output()?;
    if !output.status.success() {
        return Err(std::io::Error::new(
            std::io::ErrorKind::Other,
            format!(
                "{} failed: {}",
                name,
                String::from_utf8_lossy(&output.stderr)
            ),
        ));
    }
    Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
}

/// Execute a command with extra environment variables.
pub fn run_cmd_env(extra_env: &[(&str, &str)], name: &str, args: &[&str]) -> Result<String, std::io::Error> {
    let mut cmd = Command::new(name);
    cmd.args(args);
    for (k, v) in extra_env {
        cmd.env(k, v);
    }
    let output = cmd.output()?;
    if !output.status.success() {
        return Err(std::io::Error::new(
            std::io::ErrorKind::Other,
            format!("{} failed", name),
        ));
    }
    Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
}

/// Parse newline-delimited JSON into a vec of serde_json Values.
pub fn parse_json_lines(raw: &str) -> Vec<serde_json::Value> {
    raw.lines()
        .filter(|l| !l.trim().is_empty())
        .filter_map(|l| serde_json::from_str(l).ok())
        .collect()
}

/// Safely extract a string from a JSON value, trying multiple keys.
pub fn get_string_field(data: &serde_json::Value, keys: &[&str]) -> String {
    for key in keys {
        if let Some(s) = data.get(key).and_then(|v| v.as_str()) {
            return s.to_string();
        }
    }
    String::new()
}
