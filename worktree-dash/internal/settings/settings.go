package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const (
	DefaultLeftPanePct = 20
	MinLeftPanePct     = 15
	MaxLeftPanePct     = 40

	DefaultMaxPanesPerGroup = 4
	MinMaxPanesPerGroup     = 2
	MaxMaxPanesPerGroup     = 6
)

// Settings holds user preferences that persist across dashboard sessions.
type Settings struct {
	// DefaultPanels controls which optional panels are visible on startup.
	DefaultPanels PanelDefaults `json:"default_panels"`

	// LeftPanePct is the width of the left bubbletea pane as a percentage (15-40).
	LeftPanePct int `json:"left_pane_pct"`

	// Split pane limit: max sessions per group (2-6)
	MaxPanesPerGroup int `json:"max_panes_per_group"`

	// ClaudeAutoMode: when true, claude always opens with --enable-auto-mode
	ClaudeAutoMode bool `json:"claude_auto_mode"`
}

// PanelDefaults controls which optional panels open by default.
type PanelDefaults struct {
	Details bool `json:"details"`
	Usage   bool `json:"usage"`
	Tasks   bool `json:"tasks"`
}

// Defaults returns a Settings with default values.
func Defaults() Settings {
	return Settings{
		LeftPanePct:      DefaultLeftPanePct,
		MaxPanesPerGroup: DefaultMaxPanesPerGroup,
	}
}

// clamp ensures all fields are within valid ranges.
func (s *Settings) clamp() {
	if s.LeftPanePct < MinLeftPanePct || s.LeftPanePct > MaxLeftPanePct {
		s.LeftPanePct = DefaultLeftPanePct
	}
	if s.MaxPanesPerGroup < MinMaxPanesPerGroup || s.MaxPanesPerGroup > MaxMaxPanesPerGroup {
		s.MaxPanesPerGroup = DefaultMaxPanesPerGroup
	}
}

// ── Persistence ─────────────────────────────────────────────────────────

var settings_dir_once sync.Once
var settings_dir string

// dir returns ~/.wt/, creating it if needed.
func dir() string {
	settings_dir_once.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		settings_dir = filepath.Join(home, ".wt")
		os.MkdirAll(settings_dir, 0755)
	})
	return settings_dir
}

// Path returns the full path to the settings file.
func Path() string {
	return filepath.Join(dir(), "settings.json")
}

// Load reads settings from ~/.wt/settings.json.
// Returns defaults if the file doesn't exist or is malformed.
func Load() Settings {
	s := Defaults()

	data, err := os.ReadFile(Path())
	if err != nil {
		return s
	}

	if err := json.Unmarshal(data, &s); err != nil {
		return Defaults()
	}

	s.clamp()
	return s
}

// DraftPath returns the path to the temporary draft file used for unsaved changes.
func DraftPath() string {
	return filepath.Join(os.TempDir(), "wt-settings-draft.json")
}

// SaveDraft writes settings to a temporary draft file (for unsaved exit detection).
func SaveDraft(s Settings) error {
	s.clamp()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(DraftPath(), data, 0644)
}

// ClearDraft removes the temporary draft file.
func ClearDraft() {
	os.Remove(DraftPath())
}

// SaveRaw writes raw JSON bytes to the settings file.
func SaveRaw(data []byte) error {
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}

// Save writes settings to ~/.wt/settings.json atomically.
func Save(s Settings) error {
	s.clamp()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// Atomic write: tmp file + rename
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}
