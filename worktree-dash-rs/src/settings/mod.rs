#[cfg(test)]
#[path = "settings_test.rs"]
mod tests;

use serde::{Deserialize, Serialize};
use std::fs;
use std::path::PathBuf;

pub const DEFAULT_LEFT_PANE_PCT: u16 = 20;
pub const MIN_LEFT_PANE_PCT: u16 = 15;
pub const MAX_LEFT_PANE_PCT: u16 = 40;

pub const DEFAULT_MAX_PANES_PER_GROUP: u16 = 4;
pub const MIN_MAX_PANES_PER_GROUP: u16 = 2;
pub const MAX_MAX_PANES_PER_GROUP: u16 = 6;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Settings {
    #[serde(default)]
    pub default_panels: PanelDefaults,
    #[serde(default = "default_left_pane_pct")]
    pub left_pane_pct: u16,
    #[serde(default = "default_max_panes")]
    pub max_panes_per_group: u16,
    #[serde(default)]
    pub claude_auto_mode: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PanelDefaults {
    #[serde(default)]
    pub details: bool,
    #[serde(default)]
    pub usage: bool,
    #[serde(default)]
    pub tasks: bool,
    #[serde(default = "default_true")]
    pub services: bool,
}

fn default_true() -> bool { true }

impl Default for PanelDefaults {
    fn default() -> Self {
        Self {
            details: false,
            usage: false,
            tasks: false,
            services: true,
        }
    }
}

fn default_left_pane_pct() -> u16 { DEFAULT_LEFT_PANE_PCT }
fn default_max_panes() -> u16 { DEFAULT_MAX_PANES_PER_GROUP }

impl Default for Settings {
    fn default() -> Self {
        Self {
            default_panels: PanelDefaults::default(),
            left_pane_pct: DEFAULT_LEFT_PANE_PCT,
            max_panes_per_group: DEFAULT_MAX_PANES_PER_GROUP,
            claude_auto_mode: false,
        }
    }
}

impl Settings {
    pub fn clamp(&mut self) {
        if self.left_pane_pct < MIN_LEFT_PANE_PCT || self.left_pane_pct > MAX_LEFT_PANE_PCT {
            self.left_pane_pct = DEFAULT_LEFT_PANE_PCT;
        }
        if self.max_panes_per_group < MIN_MAX_PANES_PER_GROUP
            || self.max_panes_per_group > MAX_MAX_PANES_PER_GROUP
        {
            self.max_panes_per_group = DEFAULT_MAX_PANES_PER_GROUP;
        }
    }

    pub fn load() -> Self {
        let path = settings_path();
        let data = match fs::read_to_string(&path) {
            Ok(d) => d,
            Err(_) => return Self::default(),
        };
        let mut s: Self = match serde_json::from_str(&data) {
            Ok(s) => s,
            Err(_) => return Self::default(),
        };
        s.clamp();
        s
    }

    pub fn save(&self) -> Result<(), std::io::Error> {
        let mut s = self.clone();
        s.clamp();

        let data = serde_json::to_string_pretty(&s)?;
        let path = settings_path();

        // Ensure directory exists
        if let Some(dir) = path.parent() {
            fs::create_dir_all(dir)?;
        }

        // Atomic write
        let tmp = path.with_extension("json.tmp");
        fs::write(&tmp, format!("{}\n", data))?;
        fs::rename(&tmp, &path)?;
        Ok(())
    }
}

fn settings_dir() -> PathBuf {
    let home = std::env::var("HOME").unwrap_or_else(|_| "/tmp".to_string());
    PathBuf::from(home).join(".wt")
}

pub fn settings_path() -> PathBuf {
    settings_dir().join("settings.json")
}
