package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	SentinelCreate      = "wt-create-done"
	SentinelSkipWorktree = "wt-skip-worktree-done"
	SentinelAWSKeys     = "wt-aws-keys-done"
	SentinelHeiHei      = "wt-heihei-done"
)

// sentinel_result holds the parsed output of a sentinel file.
type sentinel_result struct {
	raw       string // full file content, trimmed
	exit_code int    // first integer on line 1 (-1 if unparseable)
}

// read_sentinel reads and removes a sentinel file from os.TempDir().
// Returns nil if the file doesn't exist yet (script still running).
// The sentinel filename should be like "wt-create-done".
func read_sentinel(name string) *sentinel_result {
	path := filepath.Join(os.TempDir(), name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_ = os.Remove(path)

	raw := strings.TrimSpace(string(data))
	exit_code := -1
	fmt.Sscanf(raw, "%d", &exit_code)

	return &sentinel_result{raw: raw, exit_code: exit_code}
}

// clear_sentinel removes a sentinel file if it exists (e.g. before starting a new script).
func clear_sentinel(name string) {
	_ = os.Remove(filepath.Join(os.TempDir(), name))
}

// sentinel_exists returns true if a sentinel file is present (without reading or removing it).
func sentinel_exists(name string) bool {
	_, err := os.Stat(filepath.Join(os.TempDir(), name))
	return err == nil
}
