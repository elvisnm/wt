package docker

import (
	"strings"

	"github.com/elvisnm/wt/internal/cmdutil"
	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/worktree"
)

// FetchContainerStats updates CPU and memory stats for all docker worktrees
func FetchContainerStats(worktrees []worktree.Worktree, cfg *config.Config) []worktree.Worktree {
	raw, err := cmdutil.RunCmd("docker", "stats", "--no-stream", "--format", "json")
	if err != nil {
		return worktrees
	}

	prefix := ""
	if cfg != nil {
		prefix = cfg.ContainerPrefix()
	}

	stats := cmdutil.ParseJSONLines(raw)
	stats_map := make(map[string]map[string]interface{})

	for _, s := range stats {
		name := cmdutil.GetStringField(s, "Name", "name")
		if name != "" && strings.HasPrefix(name, prefix) {
			stats_map[name] = s
		}
	}

	for i := range worktrees {
		wt := &worktrees[i]
		s, ok := stats_map[wt.Container]
		if ok {
			wt.CPU = cmdutil.GetStringField(s, "CPUPerc")
			mem_usage := cmdutil.GetStringField(s, "MemUsage")
			parts := strings.SplitN(mem_usage, "/", 2)
			if len(parts) > 0 {
				wt.Mem = strings.TrimSpace(parts[0])
			}
			wt.MemPct = cmdutil.GetStringField(s, "MemPerc")
		} else if !wt.Running {
			wt.CPU = ""
			wt.Mem = ""
			wt.MemPct = ""
		}
	}

	return worktrees
}
