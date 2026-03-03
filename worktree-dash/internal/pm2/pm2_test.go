package pm2

import (
	"encoding/json"
	"testing"

	"github.com/elvisnm/wt/internal/cmdutil"
	"github.com/elvisnm/wt/internal/worktree"
)

// ── Test helpers ─────────────────────────────────────────────────────────

// parse_services_from_json extracts the core parsing logic from FetchServices
// so it can be unit tested without shelling out to pm2.
func parse_services_from_json(raw string, wt_path string) []worktree.Service {
	var procs []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &procs); err != nil {
		return nil
	}

	services := []worktree.Service{
		{Name: "__all", DisplayName: "All services", Status: "online"},
	}

	for _, proc := range procs {
		name := cmdutil.GetStringField(proc, "name")
		if name == "" {
			continue
		}

		pm_cwd := ""
		env_map, has_env := get_env(proc)
		if has_env {
			pm_cwd = cmdutil.GetStringField(env_map, "pm_cwd")
		}

		if wt_path != "" {
			if len(pm_cwd) < len(wt_path) || pm_cwd[:len(wt_path)] != wt_path {
				continue
			}
		}

		svc := worktree.Service{
			Name:        name,
			DisplayName: name,
		}

		if has_env {
			svc.Status = cmdutil.GetStringField(env_map, "status")
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

// find_running_worktrees extracts the core logic from FetchRunningWorktrees
// so it can be unit tested without shelling out to pm2.
func find_running_worktrees(raw string, wt_paths map[string]string) map[string]bool {
	var procs []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &procs); err != nil {
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
			if len(pm_cwd) >= len(path) && pm_cwd[:len(path)] == path {
				result[name] = true
				break
			}
		}
	}

	return result
}

// ── get_string_field tests ──────────────────────────────────────────────

func TestGetStringField(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		keys     []string
		expected string
	}{
		{
			name:     "single key found",
			data:     map[string]interface{}{"name": "web"},
			keys:     []string{"name"},
			expected: "web",
		},
		{
			name:     "single key not found",
			data:     map[string]interface{}{"name": "web"},
			keys:     []string{"status"},
			expected: "",
		},
		{
			name:     "multiple keys first matches",
			data:     map[string]interface{}{"Name": "web", "name": "api"},
			keys:     []string{"Name", "name"},
			expected: "web",
		},
		{
			name:     "multiple keys second matches",
			data:     map[string]interface{}{"name": "api"},
			keys:     []string{"Name", "name"},
			expected: "api",
		},
		{
			name:     "value is not a string",
			data:     map[string]interface{}{"count": 42},
			keys:     []string{"count"},
			expected: "",
		},
		{
			name:     "value is nil",
			data:     map[string]interface{}{"key": nil},
			keys:     []string{"key"},
			expected: "",
		},
		{
			name:     "empty map",
			data:     map[string]interface{}{},
			keys:     []string{"anything"},
			expected: "",
		},
		{
			name:     "no keys provided",
			data:     map[string]interface{}{"name": "web"},
			keys:     []string{},
			expected: "",
		},
		{
			name:     "value is float64",
			data:     map[string]interface{}{"mem": 1024.5},
			keys:     []string{"mem"},
			expected: "",
		},
		{
			name:     "value is bool",
			data:     map[string]interface{}{"active": true},
			keys:     []string{"active"},
			expected: "",
		},
		{
			name:     "empty string value",
			data:     map[string]interface{}{"name": ""},
			keys:     []string{"name"},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cmdutil.GetStringField(tc.data, tc.keys...)
			if got != tc.expected {
				t.Errorf("cmdutil.GetStringField(%v, %v) = %q, want %q", tc.data, tc.keys, got, tc.expected)
			}
		})
	}
}

// ── get_env tests ───────────────────────────────────────────────────────

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name     string
		proc     map[string]interface{}
		has_env  bool
		pm_cwd   string
	}{
		{
			name: "pm2_env present with pm_cwd",
			proc: map[string]interface{}{
				"name": "web",
				"pm2_env": map[string]interface{}{
					"pm_cwd": "/home/user/worktrees/feat-login",
					"status": "online",
				},
			},
			has_env: true,
			pm_cwd:  "/home/user/worktrees/feat-login",
		},
		{
			name: "pm2_env present without pm_cwd",
			proc: map[string]interface{}{
				"name": "web",
				"pm2_env": map[string]interface{}{
					"status": "online",
				},
			},
			has_env: true,
			pm_cwd:  "",
		},
		{
			name:    "pm2_env missing",
			proc:    map[string]interface{}{"name": "web"},
			has_env: false,
			pm_cwd:  "",
		},
		{
			name:    "pm2_env is nil",
			proc:    map[string]interface{}{"name": "web", "pm2_env": nil},
			has_env: false,
			pm_cwd:  "",
		},
		{
			name:    "pm2_env is wrong type (string)",
			proc:    map[string]interface{}{"name": "web", "pm2_env": "invalid"},
			has_env: false,
			pm_cwd:  "",
		},
		{
			name:    "pm2_env is wrong type (float64)",
			proc:    map[string]interface{}{"name": "web", "pm2_env": 42.0},
			has_env: false,
			pm_cwd:  "",
		},
		{
			name: "empty pm2_env map",
			proc: map[string]interface{}{
				"name":    "web",
				"pm2_env": map[string]interface{}{},
			},
			has_env: true,
			pm_cwd:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, has := get_env(tc.proc)
			if has != tc.has_env {
				t.Errorf("get_env() has_env = %v, want %v", has, tc.has_env)
			}
			if has {
				got_cwd := cmdutil.GetStringField(env, "pm_cwd")
				if got_cwd != tc.pm_cwd {
					t.Errorf("get_env() pm_cwd = %q, want %q", got_cwd, tc.pm_cwd)
				}
			}
		})
	}
}

// ── FetchServices parsing logic tests ───────────────────────────────────

func TestParseServices_Basic(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-login",
				"status": "online",
				"restart_time": 0
			},
			"monit": {"memory": 104857600, "cpu": 2.5}
		},
		{
			"name": "api",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-login/services/api",
				"status": "online",
				"restart_time": 3
			},
			"monit": {"memory": 209715200, "cpu": 5.0}
		}
	]`

	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat-login")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

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
	if services[0].Status != "online" {
		t.Errorf("__all status: expected 'online', got %q", services[0].Status)
	}

	// web service
	web := services[1]
	if web.Name != "web" {
		t.Errorf("web Name: expected 'web', got %q", web.Name)
	}
	if web.DisplayName != "web" {
		t.Errorf("web DisplayName: expected 'web', got %q", web.DisplayName)
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
	if api.Name != "api" {
		t.Errorf("api Name: expected 'api', got %q", api.Name)
	}
	if api.Status != "online" {
		t.Errorf("api Status: expected 'online', got %q", api.Status)
	}
	if api.Memory != 209715200 {
		t.Errorf("api Memory: expected 209715200, got %d", api.Memory)
	}
	if api.CPU != 5.0 {
		t.Errorf("api CPU: expected 5.0, got %f", api.CPU)
	}
	if api.RestartCount != 3 {
		t.Errorf("api RestartCount: expected 3, got %d", api.RestartCount)
	}
}

func TestParseServices_FiltersByPath(t *testing.T) {
	pm2_json := `[
		{
			"name": "web-feat-a",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-a",
				"status": "online"
			},
			"monit": {"memory": 100000000, "cpu": 1.0}
		},
		{
			"name": "web-feat-b",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-b",
				"status": "online"
			},
			"monit": {"memory": 200000000, "cpu": 2.0}
		}
	]`

	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat-a")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	// Only __all + the feat-a service should be included
	if len(services) != 2 {
		t.Fatalf("expected 2 services (1 __all + 1 matching proc), got %d", len(services))
	}

	if services[1].Name != "web-feat-a" {
		t.Errorf("expected only feat-a service, got %q", services[1].Name)
	}
}

func TestParseServices_PathPrefixMatching(t *testing.T) {
	// Processes in subdirectories of the worktree path should also match
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-login",
				"status": "online"
			},
			"monit": {"memory": 100000000, "cpu": 1.0}
		},
		{
			"name": "api",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-login/packages/api",
				"status": "online"
			},
			"monit": {"memory": 200000000, "cpu": 2.0}
		},
		{
			"name": "worker",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-login-v2",
				"status": "online"
			},
			"monit": {"memory": 50000000, "cpu": 0.5}
		}
	]`

	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat-login")

	// feat-login and feat-login/packages/api match
	// feat-login-v2 also matches because it starts with the prefix
	// This is the same behavior as the production code (strings.HasPrefix)
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	// web, api, and worker all have pm_cwd starting with the prefix
	if len(services) != 4 {
		t.Fatalf("expected 4 services (1 __all + 3 matching), got %d", len(services))
	}
}

func TestParseServices_NoMatchingProcs(t *testing.T) {
	pm2_json := `[
		{
			"name": "web-other",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/other-project",
				"status": "online"
			},
			"monit": {"memory": 100000000, "cpu": 1.0}
		}
	]`

	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat-login")

	// Only __all would be in the list, and len == 1 triggers nil return
	if services != nil {
		t.Errorf("expected nil when no matching procs, got %v", services)
	}
}

func TestParseServices_EmptyList(t *testing.T) {
	services := parse_services_from_json("[]", "/home/user/worktrees/feat-login")
	if services != nil {
		t.Errorf("expected nil for empty proc list, got %v", services)
	}
}

func TestParseServices_InvalidJson(t *testing.T) {
	services := parse_services_from_json("not json at all", "/home/user/worktrees/feat")
	if services != nil {
		t.Errorf("expected nil for invalid JSON, got %v", services)
	}
}

func TestParseServices_EmptyJsonString(t *testing.T) {
	services := parse_services_from_json("", "/home/user/worktrees/feat")
	if services != nil {
		t.Errorf("expected nil for empty string, got %v", services)
	}
}

func TestParseServices_MissingName(t *testing.T) {
	pm2_json := `[
		{
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat",
				"status": "online"
			},
			"monit": {"memory": 50000000, "cpu": 1.0}
		}
	]`

	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat")
	// Nameless proc is skipped; only __all remains; len == 1 -> nil
	if services != nil {
		t.Errorf("expected nil when all procs are nameless, got %v", services)
	}
}

func TestParseServices_MissingPm2Env(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"monit": {"memory": 50000000, "cpu": 1.0}
		}
	]`

	// Without pm2_env, pm_cwd is empty and won't match any path
	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat")
	if services != nil {
		t.Errorf("expected nil when pm2_env missing (no path match), got %v", services)
	}
}

func TestParseServices_EmptyWtPath(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat",
				"status": "online"
			},
			"monit": {"memory": 100000000, "cpu": 1.5}
		}
	]`

	// Empty wt_path means no filtering (all procs match)
	services := parse_services_from_json(pm2_json, "")
	if services == nil {
		t.Fatal("expected non-nil services with empty wt_path")
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 services (1 __all + 1 proc), got %d", len(services))
	}
}

func TestParseServices_MissingMonit(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat",
				"status": "online",
				"restart_time": 2
			}
		}
	]`

	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	svc := services[1]
	if svc.Memory != 0 {
		t.Errorf("expected 0 memory when monit missing, got %d", svc.Memory)
	}
	if svc.CPU != 0 {
		t.Errorf("expected 0 CPU when monit missing, got %f", svc.CPU)
	}
	if svc.Status != "online" {
		t.Errorf("expected status 'online', got %q", svc.Status)
	}
	if svc.RestartCount != 2 {
		t.Errorf("expected restart count 2, got %d", svc.RestartCount)
	}
}

func TestParseServices_MonitWrongType(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat",
				"status": "online"
			},
			"monit": "invalid"
		}
	]`

	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	svc := services[1]
	if svc.Memory != 0 {
		t.Errorf("expected 0 memory when monit is wrong type, got %d", svc.Memory)
	}
	if svc.CPU != 0 {
		t.Errorf("expected 0 CPU when monit is wrong type, got %f", svc.CPU)
	}
}

func TestParseServices_StoppedService(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat",
				"status": "stopped",
				"restart_time": 5
			},
			"monit": {"memory": 0, "cpu": 0}
		}
	]`

	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	svc := services[1]
	if svc.Status != "stopped" {
		t.Errorf("expected status 'stopped', got %q", svc.Status)
	}
	if svc.RestartCount != 5 {
		t.Errorf("expected restart count 5, got %d", svc.RestartCount)
	}
}

func TestParseServices_ErroredService(t *testing.T) {
	pm2_json := `[
		{
			"name": "worker",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat",
				"status": "errored",
				"restart_time": 99
			},
			"monit": {"memory": 50000, "cpu": 0.1}
		}
	]`

	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	svc := services[1]
	if svc.Status != "errored" {
		t.Errorf("expected status 'errored', got %q", svc.Status)
	}
	if svc.RestartCount != 99 {
		t.Errorf("expected restart count 99, got %d", svc.RestartCount)
	}
}

func TestParseServices_LargeMemoryValues(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat",
				"status": "online"
			},
			"monit": {"memory": 2147483648, "cpu": 50.5}
		}
	]`

	services := parse_services_from_json(pm2_json, "/home/user/worktrees/feat")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	svc := services[1]
	// 2 GiB in bytes
	if svc.Memory != 2147483648 {
		t.Errorf("expected 2147483648 bytes, got %d", svc.Memory)
	}
	if svc.CPU != 50.5 {
		t.Errorf("expected 50.5%% CPU, got %f", svc.CPU)
	}
}

func TestParseServices_MultipleProcsVariousStatuses(t *testing.T) {
	pm2_json := `[
		{"name": "web", "pm2_env": {"pm_cwd": "/app", "status": "online", "restart_time": 0}, "monit": {"memory": 100000000, "cpu": 1.5}},
		{"name": "api", "pm2_env": {"pm_cwd": "/app", "status": "online", "restart_time": 1}, "monit": {"memory": 200000000, "cpu": 3.0}},
		{"name": "worker", "pm2_env": {"pm_cwd": "/app", "status": "stopped", "restart_time": 10}, "monit": {"memory": 0, "cpu": 0}},
		{"name": "cron", "pm2_env": {"pm_cwd": "/app", "status": "errored", "restart_time": 42}, "monit": {"memory": 50000, "cpu": 0.1}}
	]`

	services := parse_services_from_json(pm2_json, "/app")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	if len(services) != 5 {
		t.Fatalf("expected 5 services (1 __all + 4 procs), got %d", len(services))
	}

	expected := []struct {
		name     string
		status   string
		restarts int
		memory   int64
		cpu      float64
	}{
		{"web", "online", 0, 100000000, 1.5},
		{"api", "online", 1, 200000000, 3.0},
		{"worker", "stopped", 10, 0, 0},
		{"cron", "errored", 42, 50000, 0.1},
	}

	for i, exp := range expected {
		svc := services[i+1]
		if svc.Name != exp.name {
			t.Errorf("services[%d] Name: expected %q, got %q", i+1, exp.name, svc.Name)
		}
		if svc.Status != exp.status {
			t.Errorf("services[%d] Status: expected %q, got %q", i+1, exp.status, svc.Status)
		}
		if svc.RestartCount != exp.restarts {
			t.Errorf("services[%d] RestartCount: expected %d, got %d", i+1, exp.restarts, svc.RestartCount)
		}
		if svc.Memory != exp.memory {
			t.Errorf("services[%d] Memory: expected %d, got %d", i+1, exp.memory, svc.Memory)
		}
		if svc.CPU != exp.cpu {
			t.Errorf("services[%d] CPU: expected %f, got %f", i+1, exp.cpu, svc.CPU)
		}
	}
}

func TestParseServices_NoStatusInEnv(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/app"
			},
			"monit": {"memory": 100000000, "cpu": 1.0}
		}
	]`

	services := parse_services_from_json(pm2_json, "/app")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	svc := services[1]
	if svc.Status != "" {
		t.Errorf("expected empty status when not in env, got %q", svc.Status)
	}
}

func TestParseServices_NoRestartTimeInEnv(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/app",
				"status": "online"
			},
			"monit": {"memory": 100000000, "cpu": 1.0}
		}
	]`

	services := parse_services_from_json(pm2_json, "/app")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	svc := services[1]
	if svc.RestartCount != 0 {
		t.Errorf("expected 0 restart count when not in env, got %d", svc.RestartCount)
	}
}

func TestParseServices_RestartTimeWrongType(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/app",
				"status": "online",
				"restart_time": "not-a-number"
			},
			"monit": {"memory": 100000000, "cpu": 1.0}
		}
	]`

	services := parse_services_from_json(pm2_json, "/app")
	if services == nil {
		t.Fatal("expected non-nil services")
	}

	svc := services[1]
	if svc.RestartCount != 0 {
		t.Errorf("expected 0 restart count for wrong type, got %d", svc.RestartCount)
	}
}

// ── FetchRunningWorktrees parsing logic tests ───────────────────────────

func TestFindRunningWorktrees_Basic(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-login",
				"status": "online"
			}
		},
		{
			"name": "api",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-login/services/api",
				"status": "online"
			}
		}
	]`

	wt_paths := map[string]string{
		"/home/user/worktrees/feat-login":  "feat-login",
		"/home/user/worktrees/feat-signup": "feat-signup",
	}

	result := find_running_worktrees(pm2_json, wt_paths)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if !result["feat-login"] {
		t.Error("expected feat-login to be running")
	}
	if result["feat-signup"] {
		t.Error("expected feat-signup to NOT be running")
	}
}

func TestFindRunningWorktrees_StoppedNotCounted(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-login",
				"status": "stopped"
			}
		}
	]`

	wt_paths := map[string]string{
		"/home/user/worktrees/feat-login": "feat-login",
	}

	result := find_running_worktrees(pm2_json, wt_paths)
	if result["feat-login"] {
		t.Error("stopped process should not mark worktree as running")
	}
}

func TestFindRunningWorktrees_ErroredNotCounted(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/home/user/worktrees/feat-login",
				"status": "errored"
			}
		}
	]`

	wt_paths := map[string]string{
		"/home/user/worktrees/feat-login": "feat-login",
	}

	result := find_running_worktrees(pm2_json, wt_paths)
	if result["feat-login"] {
		t.Error("errored process should not mark worktree as running")
	}
}

func TestFindRunningWorktrees_MultiplePaths(t *testing.T) {
	pm2_json := `[
		{
			"name": "web-a",
			"pm2_env": {
				"pm_cwd": "/worktrees/feat-a",
				"status": "online"
			}
		},
		{
			"name": "web-b",
			"pm2_env": {
				"pm_cwd": "/worktrees/feat-b",
				"status": "online"
			}
		},
		{
			"name": "web-c",
			"pm2_env": {
				"pm_cwd": "/worktrees/feat-c",
				"status": "stopped"
			}
		}
	]`

	wt_paths := map[string]string{
		"/worktrees/feat-a": "feat-a",
		"/worktrees/feat-b": "feat-b",
		"/worktrees/feat-c": "feat-c",
		"/worktrees/feat-d": "feat-d",
	}

	result := find_running_worktrees(pm2_json, wt_paths)

	if !result["feat-a"] {
		t.Error("expected feat-a to be running")
	}
	if !result["feat-b"] {
		t.Error("expected feat-b to be running")
	}
	if result["feat-c"] {
		t.Error("expected feat-c to NOT be running (stopped)")
	}
	if result["feat-d"] {
		t.Error("expected feat-d to NOT be running (no proc)")
	}
}

func TestFindRunningWorktrees_NoPm2Env(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"monit": {"memory": 100000000, "cpu": 1.0}
		}
	]`

	wt_paths := map[string]string{
		"/worktrees/feat": "feat",
	}

	result := find_running_worktrees(pm2_json, wt_paths)
	if len(result) != 0 {
		t.Errorf("expected empty result when pm2_env missing, got %v", result)
	}
}

func TestFindRunningWorktrees_EmptyPmCwd(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"status": "online"
			}
		}
	]`

	wt_paths := map[string]string{
		"/worktrees/feat": "feat",
	}

	result := find_running_worktrees(pm2_json, wt_paths)
	if len(result) != 0 {
		t.Errorf("expected empty result when pm_cwd is empty, got %v", result)
	}
}

func TestFindRunningWorktrees_EmptyProcs(t *testing.T) {
	result := find_running_worktrees("[]", map[string]string{"/path": "name"})
	if len(result) != 0 {
		t.Errorf("expected empty result for empty proc list, got %v", result)
	}
}

func TestFindRunningWorktrees_InvalidJson(t *testing.T) {
	result := find_running_worktrees("not json", map[string]string{"/path": "name"})
	if result != nil {
		t.Errorf("expected nil for invalid JSON, got %v", result)
	}
}

func TestFindRunningWorktrees_SubdirectoryMatch(t *testing.T) {
	pm2_json := `[
		{
			"name": "api",
			"pm2_env": {
				"pm_cwd": "/worktrees/feat-login/packages/api",
				"status": "online"
			}
		}
	]`

	wt_paths := map[string]string{
		"/worktrees/feat-login": "feat-login",
	}

	result := find_running_worktrees(pm2_json, wt_paths)
	if !result["feat-login"] {
		t.Error("expected feat-login to be running (subdirectory pm_cwd match)")
	}
}

func TestFindRunningWorktrees_OnlyOnlineStatus(t *testing.T) {
	statuses := []string{"stopped", "errored", "launching", "waiting restart", ""}

	for _, status := range statuses {
		t.Run("status="+status, func(t *testing.T) {
			pm2_json := `[
				{
					"name": "web",
					"pm2_env": {
						"pm_cwd": "/worktrees/feat",
						"status": "` + status + `"
					}
				}
			]`

			wt_paths := map[string]string{
				"/worktrees/feat": "feat",
			}

			result := find_running_worktrees(pm2_json, wt_paths)
			if result["feat"] {
				t.Errorf("status=%q should not mark worktree as running", status)
			}
		})
	}
}

// ── FetchServices empty path test ───────────────────────────────────────

func TestFetchServices_EmptyPath(t *testing.T) {
	result := FetchServices("")
	if result != nil {
		t.Errorf("expected nil for empty wt_path, got %v", result)
	}
}

// ── FetchRunningWorktrees empty paths test ──────────────────────────────

func TestFetchRunningWorktrees_EmptyPaths(t *testing.T) {
	result := FetchRunningWorktrees(nil)
	if result != nil {
		t.Errorf("expected nil for nil wt_paths, got %v", result)
	}

	result = FetchRunningWorktrees(map[string]string{})
	if result != nil {
		t.Errorf("expected nil for empty wt_paths, got %v", result)
	}
}

// ── Realistic PM2 output test ───────────────────────────────────────────

func TestParseServices_RealisticPm2Output(t *testing.T) {
	// Simulates actual pm2 jlist output structure
	pm2_json := `[
		{
			"pid": 12345,
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/Users/dev/apps/skulabs-worktrees/feat-auth",
				"status": "online",
				"restart_time": 0,
				"pm_uptime": 1700000000000,
				"node_version": "20.10.0",
				"exec_mode": "fork_mode",
				"instances": 1,
				"pm_id": 0,
				"version": "1.0.0"
			},
			"monit": {
				"memory": 157286400,
				"cpu": 0.5
			},
			"pm_id": 0
		},
		{
			"pid": 12346,
			"name": "api",
			"pm2_env": {
				"pm_cwd": "/Users/dev/apps/skulabs-worktrees/feat-auth",
				"status": "online",
				"restart_time": 2,
				"pm_uptime": 1700000000000,
				"node_version": "20.10.0",
				"exec_mode": "fork_mode",
				"instances": 1,
				"pm_id": 1,
				"version": "1.0.0"
			},
			"monit": {
				"memory": 314572800,
				"cpu": 3.2
			},
			"pm_id": 1
		},
		{
			"pid": 0,
			"name": "cron",
			"pm2_env": {
				"pm_cwd": "/Users/dev/apps/skulabs-worktrees/feat-auth",
				"status": "stopped",
				"restart_time": 15,
				"pm_uptime": 0,
				"pm_id": 2
			},
			"monit": {
				"memory": 0,
				"cpu": 0
			},
			"pm_id": 2
		},
		{
			"pid": 99999,
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/Users/dev/apps/skulabs-worktrees/feat-dashboard",
				"status": "online",
				"restart_time": 0,
				"pm_id": 3
			},
			"monit": {
				"memory": 104857600,
				"cpu": 1.0
			},
			"pm_id": 3
		}
	]`

	services := parse_services_from_json(
		pm2_json,
		"/Users/dev/apps/skulabs-worktrees/feat-auth",
	)

	if services == nil {
		t.Fatal("expected non-nil services")
	}

	// __all + web + api + cron = 4 (feat-dashboard filtered out)
	if len(services) != 4 {
		t.Fatalf("expected 4 services, got %d", len(services))
	}

	// Verify web
	web := services[1]
	if web.Name != "web" {
		t.Errorf("expected web, got %q", web.Name)
	}
	if web.Status != "online" {
		t.Errorf("expected online, got %q", web.Status)
	}
	if web.Memory != 157286400 {
		t.Errorf("expected 157286400, got %d", web.Memory)
	}

	// Verify cron (stopped)
	cron := services[3]
	if cron.Name != "cron" {
		t.Errorf("expected cron, got %q", cron.Name)
	}
	if cron.Status != "stopped" {
		t.Errorf("expected stopped, got %q", cron.Status)
	}
	if cron.RestartCount != 15 {
		t.Errorf("expected 15 restarts, got %d", cron.RestartCount)
	}
}

func TestFindRunningWorktrees_RealisticOutput(t *testing.T) {
	pm2_json := `[
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/Users/dev/worktrees/feat-auth",
				"status": "online"
			}
		},
		{
			"name": "api",
			"pm2_env": {
				"pm_cwd": "/Users/dev/worktrees/feat-auth",
				"status": "online"
			}
		},
		{
			"name": "cron",
			"pm2_env": {
				"pm_cwd": "/Users/dev/worktrees/feat-auth",
				"status": "stopped"
			}
		},
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/Users/dev/worktrees/feat-dashboard",
				"status": "online"
			}
		},
		{
			"name": "web",
			"pm2_env": {
				"pm_cwd": "/Users/dev/worktrees/feat-broken",
				"status": "errored"
			}
		}
	]`

	wt_paths := map[string]string{
		"/Users/dev/worktrees/feat-auth":      "feat-auth",
		"/Users/dev/worktrees/feat-dashboard": "feat-dashboard",
		"/Users/dev/worktrees/feat-broken":    "feat-broken",
		"/Users/dev/worktrees/feat-unused":    "feat-unused",
	}

	result := find_running_worktrees(pm2_json, wt_paths)

	if !result["feat-auth"] {
		t.Error("expected feat-auth to be running")
	}
	if !result["feat-dashboard"] {
		t.Error("expected feat-dashboard to be running")
	}
	if result["feat-broken"] {
		t.Error("expected feat-broken to NOT be running (errored)")
	}
	if result["feat-unused"] {
		t.Error("expected feat-unused to NOT be running (no procs)")
	}
}
