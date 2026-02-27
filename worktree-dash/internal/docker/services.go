package docker

import (
	"encoding/json"
	"strings"

	"github.com/elvisnm/wt/internal/worktree"
)

// FetchServices runs `pm2 jlist` inside a container and returns parsed services.
// wt_name is the worktree directory name, used to strip the suffix from PM2 service names.
func FetchServices(container string, wt_name string) []worktree.Service {
	if container == "" {
		return nil
	}

	raw, err := run_cmd("docker", "exec", container, "pm2", "jlist")
	if err != nil {
		return nil
	}

	var pm2_procs []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &pm2_procs); err != nil {
		return nil
	}

	suffix := ""
	if wt_name != "" {
		suffix = "-" + wt_name
	}

	services := []worktree.Service{
		{Name: "__all", DisplayName: "All services", Status: "online"},
	}

	for _, proc := range pm2_procs {
		name := get_string_field(proc, "name")
		if name == "" {
			continue
		}

		display := name
		if suffix != "" && strings.HasSuffix(display, suffix) {
			display = display[:len(display)-len(suffix)]
		}

		svc := worktree.Service{
			Name:        name,
			DisplayName: display,
		}

		if env_raw, ok := proc["pm2_env"]; ok {
			if env, ok := env_raw.(map[string]interface{}); ok {
				svc.Status = get_string_field(env, "status")
				if restart, ok := env["restart_time"].(float64); ok {
					svc.RestartCount = int(restart)
				}
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

	return services
}
