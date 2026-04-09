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

/// Update a beads task. Only non-empty fields are sent.
pub fn update_task(
    id: &str,
    title: Option<&str>,
    priority: Option<u8>,
    task_type: Option<&str>,
    description: Option<&str>,
    labels: Option<&[String]>,
) -> Result<(), String> {
    let mut args: Vec<String> = vec!["update".to_string(), id.to_string()];

    if let Some(t) = title {
        args.push("--title".to_string());
        args.push(t.to_string());
    }
    if let Some(p) = priority {
        args.push("--priority".to_string());
        args.push(p.to_string());
    }
    if let Some(tp) = task_type {
        args.push("--type".to_string());
        args.push(tp.to_string());
    }
    if let Some(d) = description {
        args.push("--description".to_string());
        args.push(d.to_string());
    }
    if let Some(l) = labels {
        args.push("--set-labels".to_string());
        args.push(l.join(","));
    }

    let output = std::process::Command::new("bd")
        .args(&args)
        .output()
        .map_err(|e| format!("bd update failed: {}", e))?;

    if output.status.success() {
        Ok(())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr);
        Err(format!("bd update failed: {}", stderr.trim()))
    }
}

// ── Task Editor ─────────────────────────────────────────────────

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EditorField {
    Title,
    Priority,
    Type,
    Description,
    Labels,
}

pub const EDITOR_FIELDS: &[EditorField] = &[
    EditorField::Title,
    EditorField::Priority,
    EditorField::Type,
    EditorField::Description,
    EditorField::Labels,
];

#[derive(Debug, Clone)]
pub struct TaskEditor {
    pub task_id: String,
    pub title: String,
    pub priority: String,
    pub task_type: String,
    pub description: String,
    pub labels: String,
    pub cursor: usize,
    pub cursor_pos: usize, // character position within current field
    pub is_new: bool,
}

impl TaskEditor {
    pub fn from_task(task: &Task) -> Self {
        let priority = match &task.priority {
            serde_json::Value::Number(n) => n.to_string(),
            serde_json::Value::String(s) => s.trim_start_matches('P').to_string(),
            _ => "2".to_string(),
        };
        Self {
            task_id: task.id.clone(),
            title: task.title.clone(),
            priority,
            task_type: task.task_type.clone(),
            description: task.description.clone(),
            labels: task.labels.join(", "),
            cursor: 0,
            cursor_pos: 0,
            is_new: false,
        }
    }

    pub fn new_task() -> Self {
        Self {
            task_id: String::new(),
            title: String::new(),
            priority: "2".to_string(),
            task_type: "task".to_string(),
            description: String::new(),
            labels: String::new(),
            cursor: 0,
            cursor_pos: 0,
            is_new: true,
        }
    }

    pub fn current_field(&self) -> EditorField {
        EDITOR_FIELDS[self.cursor]
    }

    pub fn navigate(&mut self, delta: i8) {
        let new = self.cursor as i32 + delta as i32;
        self.cursor = new.clamp(0, EDITOR_FIELDS.len() as i32 - 1) as usize;
        // Clamp cursor_pos to new field length
        let len = self.field_value().len();
        if self.cursor_pos > len {
            self.cursor_pos = len;
        }
    }

    pub fn field_value(&self) -> &str {
        match self.current_field() {
            EditorField::Title => &self.title,
            EditorField::Priority => &self.priority,
            EditorField::Type => &self.task_type,
            EditorField::Description => &self.description,
            EditorField::Labels => &self.labels,
        }
    }

    fn field_value_mut(&mut self) -> &mut String {
        match EDITOR_FIELDS[self.cursor] {
            EditorField::Title => &mut self.title,
            EditorField::Priority => &mut self.priority,
            EditorField::Type => &mut self.task_type,
            EditorField::Description => &mut self.description,
            EditorField::Labels => &mut self.labels,
        }
    }

    pub fn insert_char(&mut self, c: char) {
        let pos = self.cursor_pos;
        let field = self.field_value_mut();
        if pos >= field.len() {
            field.push(c);
        } else {
            field.insert(pos, c);
        }
        self.cursor_pos = pos + c.len_utf8();
    }

    pub fn delete_char(&mut self) {
        if self.cursor_pos == 0 {
            return;
        }
        let pos = self.cursor_pos;
        let field = self.field_value_mut();
        let prev = field[..pos]
            .char_indices()
            .next_back()
            .map(|(i, _)| i)
            .unwrap_or(0);
        field.remove(prev);
        self.cursor_pos = prev;
    }

    pub fn move_cursor_left(&mut self) {
        if self.cursor_pos == 0 {
            return;
        }
        let field = self.field_value();
        self.cursor_pos = field[..self.cursor_pos]
            .char_indices()
            .next_back()
            .map(|(i, _)| i)
            .unwrap_or(0);
    }

    pub fn move_cursor_right(&mut self) {
        let field = self.field_value();
        if self.cursor_pos >= field.len() {
            return;
        }
        self.cursor_pos = field[self.cursor_pos..]
            .char_indices()
            .nth(1)
            .map(|(i, _)| self.cursor_pos + i)
            .unwrap_or(field.len());
    }

    pub fn move_cursor_home(&mut self) {
        self.cursor_pos = 0;
    }

    pub fn move_cursor_end(&mut self) {
        self.cursor_pos = self.field_value().len();
    }

    /// Move cursor up one line within a multiline field.
    /// Returns true if moved, false if already on the first line (caller should navigate fields).
    pub fn move_cursor_line_up(&mut self) -> bool {
        let field = self.field_value();
        if !field.contains('\n') {
            return false;
        }
        let before = &field[..self.cursor_pos];
        // Find the start of the current line
        let cur_line_start = before.rfind('\n').map(|i| i + 1).unwrap_or(0);
        if cur_line_start == 0 {
            return false; // already on first line
        }
        let col = self.cursor_pos - cur_line_start;
        // Find the start of the previous line
        let prev_content = &field[..cur_line_start - 1]; // exclude the \n
        let prev_line_start = prev_content.rfind('\n').map(|i| i + 1).unwrap_or(0);
        let prev_line_len = cur_line_start - 1 - prev_line_start;
        self.cursor_pos = prev_line_start + col.min(prev_line_len);
        true
    }

    /// Move cursor down one line within a multiline field.
    /// Returns true if moved, false if already on the last line (caller should navigate fields).
    pub fn move_cursor_line_down(&mut self) -> bool {
        let field = self.field_value();
        if !field.contains('\n') {
            return false;
        }
        let before = &field[..self.cursor_pos];
        let cur_line_start = before.rfind('\n').map(|i| i + 1).unwrap_or(0);
        let col = self.cursor_pos - cur_line_start;
        // Find the end of the current line (next \n)
        let after = &field[self.cursor_pos..];
        let next_newline = after.find('\n');
        match next_newline {
            None => false, // already on last line
            Some(offset) => {
                let next_line_start = self.cursor_pos + offset + 1;
                let rest = &field[next_line_start..];
                let next_line_len = rest.find('\n').unwrap_or(rest.len());
                self.cursor_pos = next_line_start + col.min(next_line_len);
                true
            }
        }
    }

    pub fn move_cursor_word_left(&mut self) {
        let field = self.field_value();
        let before = &field[..self.cursor_pos];
        // Skip trailing whitespace, then skip word chars
        let trimmed = before.trim_end();
        if trimmed.is_empty() {
            self.cursor_pos = 0;
            return;
        }
        let word_end = trimmed.rfind(|c: char| c.is_whitespace())
            .map(|i| i + 1)
            .unwrap_or(0);
        self.cursor_pos = word_end;
    }

    pub fn move_cursor_word_right(&mut self) {
        let field = self.field_value();
        if self.cursor_pos >= field.len() {
            return;
        }
        let after = &field[self.cursor_pos..];
        // Skip current word chars, then skip whitespace
        let skip_word = after.find(|c: char| c.is_whitespace()).unwrap_or(after.len());
        let rest = &after[skip_word..];
        let skip_space = rest.find(|c: char| !c.is_whitespace()).unwrap_or(rest.len());
        self.cursor_pos += skip_word + skip_space;
    }

    pub fn kill_to_end(&mut self) {
        let pos = self.cursor_pos;
        let field = self.field_value_mut();
        field.truncate(pos);
    }

    pub fn kill_to_start(&mut self) {
        let pos = self.cursor_pos;
        let field = self.field_value_mut();
        field.drain(..pos);
        self.cursor_pos = 0;
    }

    pub fn delete_word_backward(&mut self) {
        if self.cursor_pos == 0 {
            return;
        }
        let pos = self.cursor_pos;
        let field = self.field_value_mut();
        let before = &field[..pos];
        let trimmed = before.trim_end();
        let word_start = if trimmed.is_empty() {
            0
        } else {
            trimmed.rfind(|c: char| c.is_whitespace())
                .map(|i| i + 1)
                .unwrap_or(0)
        };
        field.drain(word_start..pos);
        self.cursor_pos = word_start;
    }

    pub fn delete_char_forward(&mut self) {
        let pos = self.cursor_pos;
        let field = self.field_value_mut();
        if pos < field.len() {
            field.remove(pos);
        }
    }

    /// Save changes via `bd create` (new) or `bd update` (edit).
    pub fn save(&self) -> Result<(), String> {
        if self.title.trim().is_empty() {
            return Err("Title is required".to_string());
        }
        let priority: Option<u8> = self.priority.parse().ok();
        let labels: Vec<String> = self
            .labels
            .split(',')
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect();

        if self.is_new {
            create_task(
                &self.title,
                priority.unwrap_or(2),
                &self.task_type,
                if self.description.is_empty() { None } else { Some(&self.description) },
                &labels,
            )
        } else {
            update_task(
                &self.task_id,
                Some(&self.title),
                priority,
                Some(&self.task_type),
                Some(&self.description),
                Some(&labels),
            )
        }
    }
}

/// Create a new beads task.
pub fn create_task(
    title: &str,
    priority: u8,
    task_type: &str,
    description: Option<&str>,
    labels: &[String],
) -> Result<(), String> {
    let mut args: Vec<String> = vec![
        "create".to_string(),
        "--title".to_string(),
        title.to_string(),
        "--priority".to_string(),
        priority.to_string(),
        "--type".to_string(),
        task_type.to_string(),
    ];

    if let Some(d) = description {
        args.push("--description".to_string());
        args.push(d.to_string());
    }

    if !labels.is_empty() {
        args.push("--labels".to_string());
        args.push(labels.join(","));
    }

    let output = std::process::Command::new("bd")
        .args(&args)
        .output()
        .map_err(|e| format!("bd create failed: {}", e))?;

    if output.status.success() {
        Ok(())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr);
        Err(format!("bd create failed: {}", stderr.trim()))
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
