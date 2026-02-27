package docker

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/worktree"
)

// FetchContainerStatus updates running state, health, and uptime for all docker worktrees
func FetchContainerStatus(worktrees []worktree.Worktree, cfg *config.Config) []worktree.Worktree {
	filter := ""
	if cfg != nil {
		filter = cfg.ContainerFilter()
	}
	args := []string{"ps", "-a", "--format", "json"}
	if filter != "" {
		args = []string{"ps", "-a", "--filter", filter, "--format", "json"}
	}
	raw, err := run_cmd("docker", args...)
	if err != nil {
		return worktrees
	}

	containers := parse_json_lines(raw)

	by_name := make(map[string]map[string]interface{})
	by_workdir := make(map[string]map[string]interface{})

	wd_re := regexp.MustCompile(`com\.docker\.compose\.project\.working_dir=([^,]+)`)

	for _, c := range containers {
		name := get_string_field(c, "Names", "names")
		if name != "" {
			by_name[name] = c
		}
		labels := get_string_field(c, "Labels", "labels")
		if m := wd_re.FindStringSubmatch(labels); m != nil {
			by_workdir[m[1]] = c
		}
	}

	for i := range worktrees {
		wt := &worktrees[i]
		if wt.Type != worktree.TypeDocker {
			continue
		}

		match := by_workdir[wt.Path]
		if match == nil {
			match = by_name[wt.Container]
		}
		if match == nil && cfg != nil {
			match = by_name[cfg.ContainerName(wt.Alias)]
		}
		if match == nil && cfg != nil {
			match = by_name[cfg.ContainerName(wt.Name)]
		}

		// For shared compose: try prefix matching (e.g. bc-test-workflow-api matches bc-test-workflow-)
		if match == nil && cfg != nil && cfg.Docker.ComposeStrategy != "generate" {
			slug_prefix := cfg.Name + "-" + wt.Alias + "-"
			for name, c := range by_name {
				if strings.HasPrefix(name, slug_prefix) {
					match = c
					break
				}
			}
		}

		if match == nil {
			wt.Running = false
			wt.ContainerExists = false
			wt.Health = ""
			wt.Started = ""
			wt.Uptime = ""
			continue
		}

		matched_name := get_string_field(match, "Names", "names")
		if matched_name != "" && matched_name != wt.Container {
			wt.Container = matched_name
		}

		wt.ContainerExists = true
		state := strings.ToLower(get_string_field(match, "State", "state"))
		wt.Running = state == "running"

		status := get_string_field(match, "Status", "status")
		switch {
		case strings.Contains(status, "healthy"):
			wt.Health = "healthy"
		case strings.Contains(status, "starting"):
			wt.Health = "starting"
		default:
			wt.Health = ""
		}
	}

	// Inspect running containers for detailed health/uptime
	for i := range worktrees {
		wt := &worktrees[i]
		if wt.Type != worktree.TypeDocker || !wt.Running {
			continue
		}

		inspect_raw, err := run_cmd("docker", "inspect", "--format", "json", wt.Container)
		if err != nil {
			continue
		}

		var parsed []map[string]interface{}
		if err := json.Unmarshal([]byte(inspect_raw), &parsed); err != nil || len(parsed) == 0 {
			continue
		}

		info := parsed[0]
		if state_raw, ok := info["State"]; ok {
			if state, ok := state_raw.(map[string]interface{}); ok {
				if health_raw, ok := state["Health"]; ok {
					if health, ok := health_raw.(map[string]interface{}); ok {
						if s, ok := health["Status"].(string); ok {
							wt.Health = s
						}
					}
				}
				if started, ok := state["StartedAt"].(string); ok {
					wt.Started = started
					wt.Uptime = format_uptime(started)
				}
			}
		}
	}

	return worktrees
}

func format_uptime(started_at string) string {
	if started_at == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, started_at)
	if err != nil {
		return ""
	}
	diff := time.Since(t)
	if diff < 0 {
		return ""
	}

	mins := int(diff.Minutes())
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	hours := mins / 60
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, mins%60)
	}
	days := hours / 24
	return fmt.Sprintf("%dd %dh", days, hours%24)
}
