package worktree

import (
	"os/exec"
	"strings"
)

// FindRepoRoot returns the git toplevel directory
func FindRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
