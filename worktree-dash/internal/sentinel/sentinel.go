package sentinel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	Create       = "wt-create-done"
	SkipWorktree = "wt-skip-worktree-done"
	AWSKeys      = "wt-aws-keys-done"
	HeiHei       = "wt-heihei-done"
	Picker       = "wt-picker-done"
	Confirm      = "wt-confirm-done"
	Input        = "wt-input-done"
	AgentNotify  = "wt-agent-notify"
)

// Result holds the parsed output of a sentinel file.
type Result struct {
	Raw       string // full file content, trimmed
	ExitCode  int    // first integer on line 1 (-1 if unparseable)
}

// Read reads and removes a sentinel file from os.TempDir().
// Returns nil if the file doesn't exist yet (script still running).
func Read(name string) *Result {
	path := filepath.Join(os.TempDir(), name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_ = os.Remove(path)

	raw := strings.TrimSpace(string(data))
	exit_code := -1
	fmt.Sscanf(raw, "%d", &exit_code)

	return &Result{Raw: raw, ExitCode: exit_code}
}

// Path returns the full path to a sentinel file.
func Path(name string) string {
	return filepath.Join(os.TempDir(), name)
}

// Clear removes a sentinel file if it exists (e.g. before starting a new script).
func Clear(name string) {
	_ = os.Remove(filepath.Join(os.TempDir(), name))
}

// Exists returns true if a sentinel file is present (without reading or removing it).
func Exists(name string) bool {
	_, err := os.Stat(filepath.Join(os.TempDir(), name))
	return err == nil
}

// Write creates a sentinel file with the given content (typically an exit code).
func Write(name, content string) error {
	return os.WriteFile(filepath.Join(os.TempDir(), name), []byte(content), 0644)
}
