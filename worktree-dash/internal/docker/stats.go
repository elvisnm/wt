package docker

import (
	"strings"

	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/worktree"
)

// FetchContainerStats updates CPU and memory stats for all docker worktrees
func FetchContainerStats(worktrees []worktree.Worktree, cfg *config.Config) []worktree.Worktree {
	raw, err := run_cmd("docker", "stats", "--no-stream", "--format", "json")
	if err != nil {
		return worktrees
	}

	prefix := ""
	if cfg != nil {
		prefix = cfg.ContainerPrefix()
	}

	stats := parse_json_lines(raw)
	stats_map := make(map[string]map[string]interface{})

	for _, s := range stats {
		name := get_string_field(s, "Name", "name")
		if name != "" && strings.HasPrefix(name, prefix) {
			stats_map[name] = s
		}
	}

	for i := range worktrees {
		wt := &worktrees[i]
		s, ok := stats_map[wt.Container]
		if ok {
			wt.CPU = get_string_field(s, "CPUPerc")
			mem_usage := get_string_field(s, "MemUsage")
			parts := strings.SplitN(mem_usage, "/", 2)
			if len(parts) > 0 {
				wt.Mem = strings.TrimSpace(parts[0])
			}
			wt.MemPct = get_string_field(s, "MemPerc")
		} else if !wt.Running {
			wt.CPU = ""
			wt.Mem = ""
			wt.MemPct = ""
		}
	}

	return worktrees
}
