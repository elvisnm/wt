package docker

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// run_cmd executes a command and returns trimmed stdout
func run_cmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// parse_json_lines parses newline-delimited JSON into a slice of maps
func parse_json_lines(raw string) []map[string]interface{} {
	var results []map[string]interface{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(line), &data); err == nil {
			results = append(results, data)
		}
	}
	return results
}

// get_string_field safely extracts a string from a map, trying both cases
func get_string_field(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := data[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
