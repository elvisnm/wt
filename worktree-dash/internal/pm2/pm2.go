package pm2

import (
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/elvisnm/wt/internal/worktree"
)

// FetchServices runs `pm2 jlist` on the host and returns parsed services
// for a specific worktree. Matches processes whose pm_cwd starts with wt_path.
func FetchServices(wt_path string) []worktree.Service {
	if wt_path == "" {
		return nil
	}

	procs := fetch_procs()
	if procs == nil {
		return nil
	}

	services := []worktree.Service{
		{Name: "__all", DisplayName: "All services", Status: "online"},
	}

	for _, proc := range procs {
		name := get_string_field(proc, "name")
		if name == "" {
			continue
		}

		pm_cwd := ""
		env_map, has_env := get_env(proc)
		if has_env {
			pm_cwd = get_string_field(env_map, "pm_cwd")
		}

		if !strings.HasPrefix(pm_cwd, wt_path) {
			continue
		}

		svc := worktree.Service{
			Name:        name,
			DisplayName: name,
		}

		if has_env {
			svc.Status = get_string_field(env_map, "status")
			if restart, ok := env_map["restart_time"].(float64); ok {
				svc.RestartCount = int(restart)
			}
		}

		if monit_raw, ok := proc["monit"]; ok {
			if monit, ok := monit_raw.(map[string]interface{}); ok {
				if mem, ok := monit["memory"].(float64); ok {
					svc.Memory = int64(mem)
				}
				if cpu, ok := monit["cpu"].(float64); ok {
					svc.CPU = cpu
				}
			}
		}

		services = append(services, svc)
	}

	if len(services) == 1 {
		return nil
	}

	return services
}

// FetchRunningWorktrees runs `pm2 jlist` once and checks which of the given
// worktree paths have at least one "online" process (matched by pm_cwd).
func FetchRunningWorktrees(wt_paths map[string]string) map[string]bool {
	if len(wt_paths) == 0 {
		return nil
	}

	procs := fetch_procs()
	if procs == nil {
		return nil
	}

	result := make(map[string]bool)

	for _, proc := range procs {
		env_map, has_env := get_env(proc)
		if !has_env {
			continue
		}

		status := get_string_field(env_map, "status")
		if status != "online" {
			continue
		}

		pm_cwd := get_string_field(env_map, "pm_cwd")
		if pm_cwd == "" {
			continue
		}

		for path, name := range wt_paths {
			if strings.HasPrefix(pm_cwd, path) {
				result[name] = true
				break
			}
		}
	}

	return result
}

func fetch_procs() []map[string]interface{} {
	raw, err := run_cmd("pm2", "jlist")
	if err != nil {
		return nil
	}

	var procs []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &procs); err != nil {
		return nil
	}

	return procs
}

func get_env(proc map[string]interface{}) (map[string]interface{}, bool) {
	if env_raw, ok := proc["pm2_env"]; ok {
		if env, ok := env_raw.(map[string]interface{}); ok {
			return env, true
		}
	}
	return nil, false
}

func run_cmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

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
