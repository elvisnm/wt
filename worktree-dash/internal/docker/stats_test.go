package docker

import (
	"strings"
	"testing"

	"github.com/elvisnm/wt/internal/cmdutil"
	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/worktree"
)

// ── Stats parsing helper tests ──────────────────────────────────────────

// apply_stats_to_worktrees extracts the core logic from FetchContainerStats
// so it can be unit tested without shelling out to docker.
func apply_stats_to_worktrees(
	worktrees []worktree.Worktree,
	stats_json string,
	cfg *config.Config,
) []worktree.Worktree {
	prefix := ""
	if cfg != nil {
		prefix = cfg.ContainerPrefix()
	}

	stats := cmdutil.ParseJSONLines(stats_json)
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

func TestApplyStats_MatchesContainer(t *testing.T) {
	worktrees := []worktree.Worktree{
		{
			Name:      "feat-login",
			Container: "myapp-login",
			Type:      worktree.TypeDocker,
			Running:   true,
		},
	}

	stats_json := `{"Name":"myapp-login","CPUPerc":"2.50%","MemUsage":"256MiB / 8GiB","MemPerc":"3.12%"}`

	cfg := &config.Config{Name: "myapp"}

	result := apply_stats_to_worktrees(worktrees, stats_json, cfg)

	if result[0].CPU != "2.50%" {
		t.Errorf("CPU: expected '2.50%%', got %q", result[0].CPU)
	}
	if result[0].Mem != "256MiB" {
		t.Errorf("Mem: expected '256MiB', got %q", result[0].Mem)
	}
	if result[0].MemPct != "3.12%" {
		t.Errorf("MemPct: expected '3.12%%', got %q", result[0].MemPct)
	}
}

func TestApplyStats_MultipleContainers(t *testing.T) {
	worktrees := []worktree.Worktree{
		{Name: "feat-a", Container: "myapp-feat-a", Type: worktree.TypeDocker, Running: true},
		{Name: "feat-b", Container: "myapp-feat-b", Type: worktree.TypeDocker, Running: true},
	}

	stats_json := `{"Name":"myapp-feat-a","CPUPerc":"1.00%","MemUsage":"128MiB / 8GiB","MemPerc":"1.56%"}
{"Name":"myapp-feat-b","CPUPerc":"5.00%","MemUsage":"512MiB / 8GiB","MemPerc":"6.25%"}`

	cfg := &config.Config{Name: "myapp"}
	result := apply_stats_to_worktrees(worktrees, stats_json, cfg)

	if result[0].CPU != "1.00%" {
		t.Errorf("feat-a CPU: expected '1.00%%', got %q", result[0].CPU)
	}
	if result[1].CPU != "5.00%" {
		t.Errorf("feat-b CPU: expected '5.00%%', got %q", result[1].CPU)
	}
	if result[1].Mem != "512MiB" {
		t.Errorf("feat-b Mem: expected '512MiB', got %q", result[1].Mem)
	}
}

func TestApplyStats_StoppedContainerCleared(t *testing.T) {
	worktrees := []worktree.Worktree{
		{
			Name:      "feat-stopped",
			Container: "myapp-stopped",
			Type:      worktree.TypeDocker,
			Running:   false,
			CPU:       "old-cpu",
			Mem:       "old-mem",
			MemPct:    "old-pct",
		},
	}

	// No matching stats for the stopped container
	stats_json := `{"Name":"myapp-other","CPUPerc":"1.00%","MemUsage":"128MiB / 8GiB","MemPerc":"1.56%"}`

	cfg := &config.Config{Name: "myapp"}
	result := apply_stats_to_worktrees(worktrees, stats_json, cfg)

	if result[0].CPU != "" {
		t.Errorf("stopped container CPU should be cleared, got %q", result[0].CPU)
	}
	if result[0].Mem != "" {
		t.Errorf("stopped container Mem should be cleared, got %q", result[0].Mem)
	}
	if result[0].MemPct != "" {
		t.Errorf("stopped container MemPct should be cleared, got %q", result[0].MemPct)
	}
}

func TestApplyStats_RunningButNoStats_PreservesValues(t *testing.T) {
	worktrees := []worktree.Worktree{
		{
			Name:      "feat-running",
			Container: "myapp-running",
			Type:      worktree.TypeDocker,
			Running:   true,
			CPU:       "previous-cpu",
			Mem:       "previous-mem",
			MemPct:    "previous-pct",
		},
	}

	// No matching stats for this running container
	stats_json := `{"Name":"myapp-other","CPUPerc":"1.00%","MemUsage":"128MiB / 8GiB","MemPerc":"1.56%"}`

	cfg := &config.Config{Name: "myapp"}
	result := apply_stats_to_worktrees(worktrees, stats_json, cfg)

	// Running containers without stats should keep their existing values
	if result[0].CPU != "previous-cpu" {
		t.Errorf("running container CPU should be preserved, got %q", result[0].CPU)
	}
	if result[0].Mem != "previous-mem" {
		t.Errorf("running container Mem should be preserved, got %q", result[0].Mem)
	}
}

func TestApplyStats_PrefixFiltering(t *testing.T) {
	worktrees := []worktree.Worktree{
		{Name: "feat-a", Container: "myapp-feat-a", Type: worktree.TypeDocker, Running: true},
	}

	// Stats include a container from a different project
	stats_json := `{"Name":"myapp-feat-a","CPUPerc":"2.00%","MemUsage":"256MiB / 8GiB","MemPerc":"3.12%"}
{"Name":"other-project-feat-a","CPUPerc":"99.00%","MemUsage":"7GiB / 8GiB","MemPerc":"87.50%"}`

	cfg := &config.Config{Name: "myapp"}
	result := apply_stats_to_worktrees(worktrees, stats_json, cfg)

	if result[0].CPU != "2.00%" {
		t.Errorf("should match myapp prefix, got CPU=%q", result[0].CPU)
	}
}

func TestApplyStats_NilConfig(t *testing.T) {
	worktrees := []worktree.Worktree{
		{Name: "feat-a", Container: "myapp-feat-a", Type: worktree.TypeDocker, Running: true},
	}

	stats_json := `{"Name":"myapp-feat-a","CPUPerc":"2.00%","MemUsage":"256MiB / 8GiB","MemPerc":"3.12%"}`

	// nil config means no prefix filtering, all stats are considered
	result := apply_stats_to_worktrees(worktrees, stats_json, nil)

	if result[0].CPU != "2.00%" {
		t.Errorf("nil config should still match by container name, got CPU=%q", result[0].CPU)
	}
}

func TestApplyStats_EmptyStatsJson(t *testing.T) {
	worktrees := []worktree.Worktree{
		{Name: "feat-a", Container: "myapp-feat-a", Type: worktree.TypeDocker, Running: false, CPU: "old"},
	}

	result := apply_stats_to_worktrees(worktrees, "", &config.Config{Name: "myapp"})

	if result[0].CPU != "" {
		t.Errorf("stopped container with empty stats should be cleared, got %q", result[0].CPU)
	}
}

// ── Memory usage parsing tests ──────────────────────────────────────────

func TestMemUsageParsing(t *testing.T) {
	tests := []struct {
		name     string
		usage    string
		expected string
	}{
		{name: "MiB / GiB", usage: "256MiB / 8GiB", expected: "256MiB"},
		{name: "GiB / GiB", usage: "1.5GiB / 8GiB", expected: "1.5GiB"},
		{name: "KiB / MiB", usage: "512KiB / 1024MiB", expected: "512KiB"},
		{name: "with spaces", usage: "  256MiB  / 8GiB", expected: "256MiB"},
		{name: "no slash", usage: "256MiB", expected: "256MiB"},
		{name: "empty string", usage: "", expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parts := strings.SplitN(tc.usage, "/", 2)
			got := ""
			if len(parts) > 0 {
				got = strings.TrimSpace(parts[0])
			}
			if got != tc.expected {
				t.Errorf("usage=%q: expected %q, got %q", tc.usage, tc.expected, got)
			}
		})
	}
}
