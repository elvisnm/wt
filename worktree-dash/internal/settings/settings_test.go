package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	s := Defaults()
	if s.LeftPanePct != DefaultLeftPanePct {
		t.Errorf("LeftPanePct = %d, want %d", s.LeftPanePct, DefaultLeftPanePct)
	}
	if s.DefaultPanels.Details || s.DefaultPanels.Usage || s.DefaultPanels.Tasks {
		t.Error("default panels should all be false")
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, DefaultLeftPanePct},
		{10, DefaultLeftPanePct},
		{14, DefaultLeftPanePct},
		{15, 15},
		{20, 20},
		{40, 40},
		{41, DefaultLeftPanePct},
		{100, DefaultLeftPanePct},
	}
	for _, tt := range tests {
		s := Settings{LeftPanePct: tt.input}
		s.clamp()
		if s.LeftPanePct != tt.want {
			t.Errorf("clamp(%d) = %d, want %d", tt.input, s.LeftPanePct, tt.want)
		}
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Use a temp dir to avoid touching real ~/.wt/
	tmp := t.TempDir()
	settings_dir_once.Do(func() {}) // prevent real init
	settings_dir = tmp

	s := Settings{
		DefaultPanels: PanelDefaults{Details: true, Usage: false, Tasks: true},
		LeftPanePct:   30,
	}

	if err := Save(s); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	path := filepath.Join(tmp, "settings.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}

	loaded := Load()
	if loaded.LeftPanePct != 30 {
		t.Errorf("LeftPanePct = %d, want 30", loaded.LeftPanePct)
	}
	if !loaded.DefaultPanels.Details {
		t.Error("Details should be true")
	}
	if loaded.DefaultPanels.Usage {
		t.Error("Usage should be false")
	}
	if !loaded.DefaultPanels.Tasks {
		t.Error("Tasks should be true")
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	tmp := t.TempDir()
	settings_dir = tmp

	s := Load()
	if s.LeftPanePct != DefaultLeftPanePct {
		t.Errorf("LeftPanePct = %d, want default %d", s.LeftPanePct, DefaultLeftPanePct)
	}
}

func TestLoadMalformedFileReturnsDefaults(t *testing.T) {
	tmp := t.TempDir()
	settings_dir = tmp

	os.WriteFile(filepath.Join(tmp, "settings.json"), []byte("not json{{{"), 0644)

	s := Load()
	if s.LeftPanePct != DefaultLeftPanePct {
		t.Errorf("LeftPanePct = %d, want default %d", s.LeftPanePct, DefaultLeftPanePct)
	}
}

func TestLoadOutOfRangeClampsToDefault(t *testing.T) {
	tmp := t.TempDir()
	settings_dir = tmp

	os.WriteFile(filepath.Join(tmp, "settings.json"), []byte(`{"left_pane_pct": 99}`), 0644)

	s := Load()
	if s.LeftPanePct != DefaultLeftPanePct {
		t.Errorf("LeftPanePct = %d, want default %d after clamp", s.LeftPanePct, DefaultLeftPanePct)
	}
}
