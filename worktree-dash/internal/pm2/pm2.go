package pm2

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elvisnm/wt/internal/cmdutil"
	"github.com/elvisnm/wt/internal/worktree"
)

// FetchServices runs `pm2 jlist` on the host and returns parsed services
// for a specific worktree. Matches processes whose pm_cwd starts with wt_path.
func FetchServices(wt_path string) []worktree.Service {
	if wt_path == "" {
		return nil
	}

	procs := fetch_procs("")
	if procs == nil {
		return nil
	}

	return parse_services(procs, wt_path)
}

// FetchServicesWithHome runs `pm2 jlist` with an isolated PM2_HOME and returns
// all processes (no pm_cwd filtering needed since the daemon is worktree-specific).
func FetchServicesWithHome(pm2_home string) []worktree.Service {
	if pm2_home == "" {
		return nil
	}

	procs := fetch_procs(pm2_home)
	if procs == nil {
		return nil
	}

	return parse_services(procs, "")
}

// FetchRunningWorktrees runs `pm2 jlist` once and checks which of the given
// worktree paths have at least one "online" process (matched by pm_cwd).
func FetchRunningWorktrees(wt_paths map[string]string) map[string]bool {
	if len(wt_paths) == 0 {
		return nil
	}

	procs := fetch_procs("")
	if procs == nil {
		return nil
	}

	result := make(map[string]bool)

	for _, proc := range procs {
		env_map, has_env := get_env(proc)
		if !has_env {
			continue
		}

		status := cmdutil.GetStringField(env_map, "status")
		if status != "online" {
			continue
		}

		pm_cwd := cmdutil.GetStringField(env_map, "pm_cwd")
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

// StatusWithHome queries an isolated PM2 daemon and returns whether any process
// is online, plus aggregate CPU% and memory strings. Single pm2 jlist call.
func StatusWithHome(pm2_home string) (running bool, cpu string, mem string) {
	procs := fetch_procs(pm2_home)
	if procs == nil {
		return false, "", ""
	}

	var total_cpu float64
	var total_mem int64
	for _, proc := range procs {
		env_map, has_env := get_env(proc)
		if has_env && cmdutil.GetStringField(env_map, "status") == "online" {
			running = true
		}
		if monit_raw, ok := proc["monit"]; ok {
			if monit, ok := monit_raw.(map[string]interface{}); ok {
				if c, ok := monit["cpu"].(float64); ok {
					total_cpu += c
				}
				if m, ok := monit["memory"].(float64); ok {
					total_mem += int64(m)
				}
			}
		}
	}

	if total_cpu > 0 || total_mem > 0 {
		cpu = fmt.Sprintf("%.1f%%", total_cpu)
		mem_mb := total_mem / 1024 / 1024
		if mem_mb >= 1024 {
			mem = fmt.Sprintf("%.1fGB", float64(mem_mb)/1024)
		} else {
			mem = fmt.Sprintf("%dMB", mem_mb)
		}
	}
	return running, cpu, mem
}

// HomeEnv returns the PM2_HOME environment variable slice for an isolated daemon.
func HomeEnv(pm2_home string) []string {
	return []string{fmt.Sprintf("PM2_HOME=%s", pm2_home)}
}

// Start launches PM2 with the given ecosystem config file.
// The ecosystem config is the sole source of truth for env vars — no --update-env
// flag is used. Call Kill() first to ensure a clean daemon (matches dc-create.js).
func Start(pm2_home string, ecosystem_config string, cwd string, extra_env []string) (string, error) {
	env := append([]string{}, extra_env...)
	if pm2_home != "" {
		env = append(env, fmt.Sprintf("PM2_HOME=%s", pm2_home))
	}
	return cmdutil.RunCmdDirEnv(env, cwd, "pm2", "start", ecosystem_config)
}

// Kill stops the PM2 daemon for an isolated worktree.
func Kill(pm2_home string) (string, error) {
	if pm2_home == "" {
		return "", fmt.Errorf("pm2_home required for Kill")
	}
	return cmdutil.RunCmdEnv(HomeEnv(pm2_home), "pm2", "kill")
}

func parse_services(procs []map[string]interface{}, wt_path string) []worktree.Service {
	services := []worktree.Service{
		{Name: "__all", DisplayName: "All services", Status: "online"},
	}

	for _, proc := range procs {
		name := cmdutil.GetStringField(proc, "name")
		if name == "" {
			continue
		}

		env_map, has_env := get_env(proc)

		// If wt_path is set, filter by pm_cwd
		if wt_path != "" {
			pm_cwd := ""
			if has_env {
				pm_cwd = cmdutil.GetStringField(env_map, "pm_cwd")
			}
			if !strings.HasPrefix(pm_cwd, wt_path) {
				continue
			}
		}

		svc := worktree.Service{
			Name:        name,
			DisplayName: name,
		}

		if has_env {
			svc.Status = cmdutil.GetStringField(env_map, "status")
			// Hide stopped processes from the services panel. Errored processes
			// remain visible so users can see and investigate failures.
			if svc.Status == "stopped" {
				continue
			}
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

func fetch_procs(pm2_home string) []map[string]interface{} {
	var raw string
	var err error

	if pm2_home != "" {
		raw, err = cmdutil.RunCmdEnv(HomeEnv(pm2_home), "pm2", "jlist")
	} else {
		raw, err = cmdutil.RunCmd("pm2", "jlist")
	}
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

