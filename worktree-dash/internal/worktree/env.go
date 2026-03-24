package worktree

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// WriteEnvVar updates or appends a KEY=VALUE pair in the worktree env file.
// If the key already exists, its value is replaced in-place.
// If the key does not exist, the line is appended.
func WriteEnvVar(worktree_path, filename, key, value string) error {
	env_path := filepath.Join(worktree_path, filename)
	data, err := os.ReadFile(env_path)
	if err != nil {
		// File doesn't exist — create with the single entry
		return os.WriteFile(env_path, []byte(key+"="+value+"\n"), 0644)
	}

	content := string(data)
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `=.*$`)
	if re.MatchString(content) {
		content = re.ReplaceAllString(content, key+"="+value)
	} else {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += key + "=" + value + "\n"
	}

	return os.WriteFile(env_path, []byte(content), 0644)
}
