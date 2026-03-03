package docker

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elvisnm/wt/internal/worktree"
)

// parse_pm2_services extracts the core PM2 parsing logic from FetchServices
// so it can be unit tested without shelling out to docker.
func parse_pm2_services(pm2_json string, wt_name string) []worktree.Service {
	var pm2_procs []map[string]interface{}
	if err := json.Unmarshal([]byte(pm2_json), &pm2_procs); err != nil {
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

func TestParsePm2Services_Basic(t *testing.T) {
	pm2_json := `[
		{
			"name": "web-feat-login",
			"pm2_env": {"status": "online", "restart_time": 0},
			"monit": {"memory": 104857600, "cpu": 2.5}
		},
		{
			"name": "api-feat-login",
			"pm2_env": {"status": "online", "restart_time": 3},
			"monit": {"memory": 209715200, "cpu": 5.0}
		}
	]`

	services := parse_pm2_services(pm2_json, "feat-login")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	// First entry is always __all
	if len(services) != 3 {
		t.Fatalf("expected 3 services (1 __all + 2 procs), got %d", len(services))
	}

	// __all sentinel
	if services[0].Name != "__all" {
		t.Errorf("first service should be __all, got %q", services[0].Name)
	}
	if services[0].DisplayName != "All services" {
		t.Errorf("__all display name: expected 'All services', got %q", services[0].DisplayName)
	}

	// web service
	web := services[1]
	if web.Name != "web-feat-login" {
		t.Errorf("web Name: expected 'web-feat-login', got %q", web.Name)
	}
	if web.DisplayName != "web" {
		t.Errorf("web DisplayName: expected 'web' (suffix stripped), got %q", web.DisplayName)
	}
	if web.Status != "online" {
		t.Errorf("web Status: expected 'online', got %q", web.Status)
	}
	if web.Memory != 104857600 {
		t.Errorf("web Memory: expected 104857600, got %d", web.Memory)
	}
	if web.CPU != 2.5 {
		t.Errorf("web CPU: expected 2.5, got %f", web.CPU)
	}
	if web.RestartCount != 0 {
		t.Errorf("web RestartCount: expected 0, got %d", web.RestartCount)
	}

	// api service
	api := services[2]
	if api.Name != "api-feat-login" {
		t.Errorf("api Name: expected 'api-feat-login', got %q", api.Name)
	}
	if api.DisplayName != "api" {
		t.Errorf("api DisplayName: expected 'api' (suffix stripped), got %q", api.DisplayName)
	}
	if api.RestartCount != 3 {
		t.Errorf("api RestartCount: expected 3, got %d", api.RestartCount)
	}
	if api.Memory != 209715200 {
		t.Errorf("api Memory: expected 209715200, got %d", api.Memory)
	}
}

func TestParsePm2Services_NoSuffix(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {"status": "online"},
			"monit": {"memory": 50000000, "cpu": 1.0}
		}
	]`

	services := parse_pm2_services(pm2_json, "")
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}

	// Without suffix, display name should equal name
	if services[1].DisplayName != "web" {
		t.Errorf("without suffix, DisplayName should be 'web', got %q", services[1].DisplayName)
	}
}

func TestParsePm2Services_StoppedService(t *testing.T) {
	pm2_json := `[
		{
			"name": "web-myapp",
			"pm2_env": {"status": "stopped", "restart_time": 5},
			"monit": {"memory": 0, "cpu": 0}
		}
	]`

	services := parse_pm2_services(pm2_json, "myapp")
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}

	svc := services[1]
	if svc.Status != "stopped" {
		t.Errorf("expected status 'stopped', got %q", svc.Status)
	}
	if svc.RestartCount != 5 {
		t.Errorf("expected restart count 5, got %d", svc.RestartCount)
	}
	if svc.Memory != 0 {
		t.Errorf("expected memory 0, got %d", svc.Memory)
	}
}

func TestParsePm2Services_EmptyList(t *testing.T) {
	services := parse_pm2_services("[]", "myapp")
	if len(services) != 1 {
		t.Fatalf("expected 1 service (__all only), got %d", len(services))
	}
	if services[0].Name != "__all" {
		t.Errorf("expected __all, got %q", services[0].Name)
	}
}

func TestParsePm2Services_InvalidJson(t *testing.T) {
	services := parse_pm2_services("not json at all", "myapp")
	if services != nil {
		t.Errorf("expected nil for invalid JSON, got %v", services)
	}
}

func TestParsePm2Services_MissingName(t *testing.T) {
	pm2_json := `[
		{
			"pm2_env": {"status": "online"},
			"monit": {"memory": 50000000, "cpu": 1.0}
		}
	]`

	services := parse_pm2_services(pm2_json, "myapp")
	if len(services) != 1 {
		t.Fatalf("expected 1 service (__all only, skipping nameless), got %d", len(services))
	}
}

func TestParsePm2Services_MissingPm2Env(t *testing.T) {
	pm2_json := `[
		{
			"name": "web-myapp",
			"monit": {"memory": 50000000, "cpu": 1.0}
		}
	]`

	services := parse_pm2_services(pm2_json, "myapp")
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}

	svc := services[1]
	if svc.Status != "" {
		t.Errorf("expected empty status when pm2_env missing, got %q", svc.Status)
	}
	if svc.RestartCount != 0 {
		t.Errorf("expected 0 restarts when pm2_env missing, got %d", svc.RestartCount)
	}
}

func TestParsePm2Services_MissingMonit(t *testing.T) {
	pm2_json := `[
		{
			"name": "web-myapp",
			"pm2_env": {"status": "online", "restart_time": 0}
		}
	]`

	services := parse_pm2_services(pm2_json, "myapp")
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}

	svc := services[1]
	if svc.Memory != 0 {
		t.Errorf("expected 0 memory when monit missing, got %d", svc.Memory)
	}
	if svc.CPU != 0 {
		t.Errorf("expected 0 CPU when monit missing, got %f", svc.CPU)
	}
}

func TestParsePm2Services_SuffixNotStrippedWhenNoMatch(t *testing.T) {
	pm2_json := `[
		{
			"name": "web-other-suffix",
			"pm2_env": {"status": "online"},
			"monit": {"memory": 0, "cpu": 0}
		}
	]`

	services := parse_pm2_services(pm2_json, "myapp")
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}

	// Suffix "-myapp" doesn't match "-other-suffix", so display name should be unchanged
	if services[1].DisplayName != "web-other-suffix" {
		t.Errorf("expected unchanged display name 'web-other-suffix', got %q", services[1].DisplayName)
	}
}

func TestParsePm2Services_MultipleProcs(t *testing.T) {
	pm2_json := `[
		{"name": "web-wt", "pm2_env": {"status": "online", "restart_time": 0}, "monit": {"memory": 100000000, "cpu": 1.5}},
		{"name": "api-wt", "pm2_env": {"status": "online", "restart_time": 1}, "monit": {"memory": 200000000, "cpu": 3.0}},
		{"name": "worker-wt", "pm2_env": {"status": "stopped", "restart_time": 10}, "monit": {"memory": 0, "cpu": 0}},
		{"name": "cron-wt", "pm2_env": {"status": "errored", "restart_time": 99}, "monit": {"memory": 50000, "cpu": 0.1}}
	]`

	services := parse_pm2_services(pm2_json, "wt")
	if len(services) != 5 {
		t.Fatalf("expected 5 services (1 __all + 4 procs), got %d", len(services))
	}

	expected := []struct {
		name         string
		display      string
		status       string
		restarts     int
	}{
		{"web-wt", "web", "online", 0},
		{"api-wt", "api", "online", 1},
		{"worker-wt", "worker", "stopped", 10},
		{"cron-wt", "cron", "errored", 99},
	}

	for i, exp := range expected {
		svc := services[i+1] // skip __all at index 0
		if svc.Name != exp.name {
			t.Errorf("services[%d] Name: expected %q, got %q", i+1, exp.name, svc.Name)
		}
		if svc.DisplayName != exp.display {
			t.Errorf("services[%d] DisplayName: expected %q, got %q", i+1, exp.display, svc.DisplayName)
		}
		if svc.Status != exp.status {
			t.Errorf("services[%d] Status: expected %q, got %q", i+1, exp.status, svc.Status)
		}
		if svc.RestartCount != exp.restarts {
			t.Errorf("services[%d] RestartCount: expected %d, got %d", i+1, exp.restarts, svc.RestartCount)
		}
	}
}

func TestParsePm2Services_LargeMemoryValues(t *testing.T) {
	pm2_json := `[
		{
			"name": "web-myapp",
			"pm2_env": {"status": "online"},
			"monit": {"memory": 2147483648, "cpu": 50.5}
		}
	]`

	services := parse_pm2_services(pm2_json, "myapp")
	svc := services[1]

	// 2 GiB in bytes
	if svc.Memory != 2147483648 {
		t.Errorf("expected 2147483648 bytes, got %d", svc.Memory)
	}
	if svc.CPU != 50.5 {
		t.Errorf("expected 50.5%% CPU, got %f", svc.CPU)
	}
}
