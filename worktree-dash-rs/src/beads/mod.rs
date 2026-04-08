use serde::Deserialize;

/// A beads task from `bd list --json`.
#[derive(Debug, Clone, Deserialize)]
pub struct Task {
    pub id: String,
    #[serde(default)]
    pub title: String,
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub priority: serde_json::Value,
    #[serde(rename = "issue_type", default)]
    pub task_type: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub labels: Vec<String>,
    #[serde(default)]
    pub created_at: String,
    #[serde(default)]
    pub updated_at: String,
}

/// Fetch open beads tasks via `bd list --json --status=open`.
pub fn fetch_tasks() -> Result<Vec<Task>, String> {
    let output = std::process::Command::new("bd")
        .args(["list", "--json", "--status=open", "--limit", "50"])
        .output()
        .map_err(|e| format!("bd list failed: {}", e))?;

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        return Err(format!("bd list failed: {}", stderr.trim()));
    }

    serde_json::from_slice(&output.stdout)
        .map_err(|e| format!("failed to parse tasks: {}", e))
}

/// Fetch task detail.
pub fn fetch_detail(id: &str) -> Result<Task, String> {
    let output = std::process::Command::new("bd")
        .args(["show", id, "--json"])
        .output()
        .map_err(|e| format!("bd show failed: {}", e))?;

    if !output.status.success() {
        return Err("bd show failed".to_string());
    }

    // bd show returns an array — take the first element
    let tasks: Vec<Task> = serde_json::from_slice(&output.stdout)
        .map_err(|e| format!("failed to parse task detail: {}", e))?;
    tasks.into_iter().next().ok_or_else(|| "task not found".to_string())
}

/// Delete a beads task.
pub fn delete_task(id: &str) -> Result<(), String> {
    let output = std::process::Command::new("bd")
        .args(["delete", id, "--force"])
        .output()
        .map_err(|e| format!("bd delete failed: {}", e))?;

    if output.status.success() {
        Ok(())
    } else {
        Err("bd delete failed".to_string())
    }
}

/// Close a beads task.
pub fn close_task(id: &str) -> Result<(), String> {
    let output = std::process::Command::new("bd")
        .args(["close", id])
        .output()
        .map_err(|e| format!("bd close failed: {}", e))?;

    if output.status.success() {
        Ok(())
    } else {
        Err("bd close failed".to_string())
    }
}
