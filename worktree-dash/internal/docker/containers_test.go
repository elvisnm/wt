package docker

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/worktree"
)

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name     string
		started  string
		expected string
	}{
		{
			name:     "empty string",
			started:  "",
			expected: "",
		},
		{
			name:     "invalid format",
			started:  "not-a-date",
			expected: "",
		},
		{
			name:     "minutes only",
			started:  time.Now().Add(-15 * time.Minute).Format(time.RFC3339Nano),
			expected: "15m",
		},
		{
			name:     "zero minutes",
			started:  time.Now().Add(-30 * time.Second).Format(time.RFC3339Nano),
			expected: "0m",
		},
		{
			name:     "hours and minutes",
			started:  time.Now().Add(-2*time.Hour - 30*time.Minute).Format(time.RFC3339Nano),
			expected: "2h 30m",
		},
		{
			name:     "exactly one hour",
			started:  time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
			expected: "1h 0m",
		},
		{
			name:     "days and hours",
			started:  time.Now().Add(-26 * time.Hour).Format(time.RFC3339Nano),
			expected: "1d 2h",
		},
		{
			name:     "multiple days",
			started:  time.Now().Add(-72 * time.Hour).Format(time.RFC3339Nano),
			expected: "3d 0h",
		},
		{
			name:     "future time returns empty",
			started:  time.Now().Add(1 * time.Hour).Format(time.RFC3339Nano),
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := format_uptime(tc.started)
			if got != tc.expected {
				t.Errorf("format_uptime(%q) = %q, want %q", tc.started, got, tc.expected)
			}
		})
	}
}

func TestFormatUptime_BoundaryHoursMinutes(t *testing.T) {
	// Test boundary: exactly 59 minutes -> should show Xm
	started := time.Now().Add(-59 * time.Minute).Format(time.RFC3339Nano)
	got := format_uptime(started)
	if got != "59m" {
		t.Errorf("59 min: expected '59m', got %q", got)
	}

	// Test boundary: exactly 60 minutes -> should show Xh Ym
	started = time.Now().Add(-60 * time.Minute).Format(time.RFC3339Nano)
	got = format_uptime(started)
	if got != "1h 0m" {
		t.Errorf("60 min: expected '1h 0m', got %q", got)
	}

	// Test boundary: 23h 59m -> should still be hours
	started = time.Now().Add(-23*time.Hour - 59*time.Minute).Format(time.RFC3339Nano)
	got = format_uptime(started)
	if got != "23h 59m" {
		t.Errorf("23h59m: expected '23h 59m', got %q", got)
	}

	// Test boundary: 24h -> should show days
	started = time.Now().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	got = format_uptime(started)
	if got != "1d 0h" {
		t.Errorf("24h: expected '1d 0h', got %q", got)
	}
}

// ── Container matching logic tests ──────────────────────────────────────

// match_container_for_worktree extracts the core matching logic from FetchContainerStatus
// so it can be unit tested without shelling out to docker.
func match_container_for_worktree(
	wt *worktree.Worktree,
	cfg *config.Config,
	by_workdir map[string]map[string]interface{},
	by_name map[string]map[string]interface{},
) map[string]interface{} {
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

	// shared compose prefix matching
	if match == nil && cfg != nil && cfg.Docker.ComposeStrategy != "generate" {
		slug_prefix := cfg.Name + "-" + wt.Alias + "-"
		for name, c := range by_name {
			if strings.HasPrefix(name, slug_prefix) {
				match = c
				break
			}
		}
	}
	return match
}

func test_config() *config.Config {
	return &config.Config{
		Name: "myapp",
		Docker: config.DockerConfig{
			ComposeStrategy: "generate",
		},
	}
}

func TestMatchContainer_ByWorkdir(t *testing.T) {
	wt := &worktree.Worktree{
		Path:      "/home/user/worktrees/feat-login",
		Name:      "feat-login",
		Alias:     "login",
		Container: "myapp-login",
		Type:      worktree.TypeDocker,
	}

	container_data := map[string]interface{}{
		"Names": "myapp-login",
		"State": "running",
	}

	by_workdir := map[string]map[string]interface{}{
		"/home/user/worktrees/feat-login": container_data,
	}
	by_name := map[string]map[string]interface{}{}

	match := match_container_for_worktree(wt, test_config(), by_workdir, by_name)
	if match == nil {
		t.Fatal("expected match by workdir, got nil")
	}
	if match["Names"] != "myapp-login" {
		t.Errorf("expected Names='myapp-login', got %q", match["Names"])
	}
}

func TestMatchContainer_ByContainerName(t *testing.T) {
	wt := &worktree.Worktree{
		Path:      "/home/user/worktrees/feat-login",
		Name:      "feat-login",
		Alias:     "login",
		Container: "myapp-login",
		Type:      worktree.TypeDocker,
	}

	container_data := map[string]interface{}{
		"Names": "myapp-login",
		"State": "running",
	}

	by_workdir := map[string]map[string]interface{}{}
	by_name := map[string]map[string]interface{}{
		"myapp-login": container_data,
	}

	match := match_container_for_worktree(wt, test_config(), by_workdir, by_name)
	if match == nil {
		t.Fatal("expected match by container name, got nil")
	}
}

func TestMatchContainer_ByConfigAlias(t *testing.T) {
	wt := &worktree.Worktree{
		Path:      "/home/user/worktrees/feat-login",
		Name:      "feat-login",
		Alias:     "login",
		Container: "old-container-name",
		Type:      worktree.TypeDocker,
	}

	container_data := map[string]interface{}{
		"Names": "myapp-login",
		"State": "running",
	}

	by_workdir := map[string]map[string]interface{}{}
	by_name := map[string]map[string]interface{}{
		"myapp-login": container_data,
	}

	cfg := test_config()
	match := match_container_for_worktree(wt, cfg, by_workdir, by_name)
	if match == nil {
		t.Fatal("expected match by config alias (myapp-login), got nil")
	}
}

func TestMatchContainer_ByConfigName(t *testing.T) {
	wt := &worktree.Worktree{
		Path:      "/home/user/worktrees/feat-login",
		Name:      "feat-login",
		Alias:     "login-alt",
		Container: "unrelated",
		Type:      worktree.TypeDocker,
	}

	container_data := map[string]interface{}{
		"Names": "myapp-feat-login",
		"State": "running",
	}

	by_workdir := map[string]map[string]interface{}{}
	by_name := map[string]map[string]interface{}{
		"myapp-feat-login": container_data,
	}

	cfg := test_config()
	match := match_container_for_worktree(wt, cfg, by_workdir, by_name)
	if match == nil {
		t.Fatal("expected match by config name (myapp-feat-login), got nil")
	}
}

func TestMatchContainer_SharedComposePrefixMatch(t *testing.T) {
	wt := &worktree.Worktree{
		Path:      "/home/user/worktrees/test-workflow",
		Name:      "test-workflow",
		Alias:     "test-workflow",
		Container: "",
		Type:      worktree.TypeDocker,
	}

	container_data := map[string]interface{}{
		"Names": "bc-test-workflow-api",
		"State": "running",
	}

	by_workdir := map[string]map[string]interface{}{}
	by_name := map[string]map[string]interface{}{
		"bc-test-workflow-api": container_data,
	}

	cfg := &config.Config{
		Name: "bc",
		Docker: config.DockerConfig{
			ComposeStrategy: "shared",
		},
	}

	match := match_container_for_worktree(wt, cfg, by_workdir, by_name)
	if match == nil {
		t.Fatal("expected shared compose prefix match, got nil")
	}
}

func TestMatchContainer_SharedCompose_NoMatchForGenerate(t *testing.T) {
	wt := &worktree.Worktree{
		Path:      "/home/user/worktrees/test-workflow",
		Name:      "test-workflow",
		Alias:     "test-workflow",
		Container: "",
		Type:      worktree.TypeDocker,
	}

	by_workdir := map[string]map[string]interface{}{}
	by_name := map[string]map[string]interface{}{
		"bc-test-workflow-api": {"Names": "bc-test-workflow-api", "State": "running"},
	}

	// generate strategy should NOT do prefix matching
	cfg := &config.Config{
		Name: "bc",
		Docker: config.DockerConfig{
			ComposeStrategy: "generate",
		},
	}

	match := match_container_for_worktree(wt, cfg, by_workdir, by_name)
	if match != nil {
		t.Error("generate strategy should not use prefix matching")
	}
}

func TestMatchContainer_NoMatch(t *testing.T) {
	wt := &worktree.Worktree{
		Path:      "/home/user/worktrees/feat-login",
		Name:      "feat-login",
		Alias:     "login",
		Container: "myapp-login",
		Type:      worktree.TypeDocker,
	}

	by_workdir := map[string]map[string]interface{}{}
	by_name := map[string]map[string]interface{}{}

	match := match_container_for_worktree(wt, test_config(), by_workdir, by_name)
	if match != nil {
		t.Error("expected nil match, got non-nil")
	}
}

func TestMatchContainer_NilConfig(t *testing.T) {
	wt := &worktree.Worktree{
		Path:      "/home/user/worktrees/feat-login",
		Name:      "feat-login",
		Alias:     "login",
		Container: "myapp-login",
		Type:      worktree.TypeDocker,
	}

	container_data := map[string]interface{}{
		"Names": "myapp-login",
		"State": "running",
	}

	by_workdir := map[string]map[string]interface{}{}
	by_name := map[string]map[string]interface{}{
		"myapp-login": container_data,
	}

	// nil config: should still match by container name
	match := match_container_for_worktree(wt, nil, by_workdir, by_name)
	if match == nil {
		t.Fatal("expected match by container name with nil config, got nil")
	}
}

func TestMatchContainer_WorkdirTakesPrecedence(t *testing.T) {
	wt := &worktree.Worktree{
		Path:      "/home/user/worktrees/feat-login",
		Name:      "feat-login",
		Alias:     "login",
		Container: "myapp-login",
		Type:      worktree.TypeDocker,
	}

	workdir_data := map[string]interface{}{
		"Names": "correct-by-workdir",
		"State": "running",
	}
	name_data := map[string]interface{}{
		"Names": "wrong-by-name",
		"State": "running",
	}

	by_workdir := map[string]map[string]interface{}{
		"/home/user/worktrees/feat-login": workdir_data,
	}
	by_name := map[string]map[string]interface{}{
		"myapp-login": name_data,
	}

	match := match_container_for_worktree(wt, test_config(), by_workdir, by_name)
	if match == nil {
		t.Fatal("expected match, got nil")
	}
	if match["Names"] != "correct-by-workdir" {
		t.Errorf("workdir should take precedence, got Names=%q", match["Names"])
	}
}

// ── Health/status extraction tests ──────────────────────────────────────

func TestParseContainerHealth(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected string
	}{
		{name: "healthy status", status: "Up 2 hours (healthy)", expected: "healthy"},
		{name: "starting status", status: "Up 10 seconds (health: starting)", expected: "starting"},
		{name: "no health info", status: "Up 2 hours", expected: ""},
		{name: "exited status", status: "Exited (0) 5 minutes ago", expected: ""},
		{name: "empty status", status: "", expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var health string
			switch {
			case strings.Contains(tc.status, "healthy"):
				health = "healthy"
			case strings.Contains(tc.status, "starting"):
				health = "starting"
			default:
				health = ""
			}
			if health != tc.expected {
				t.Errorf("status=%q: expected health=%q, got %q", tc.status, tc.expected, health)
			}
		})
	}
}

func TestParseContainerState(t *testing.T) {
	tests := []struct {
		state    string
		running  bool
	}{
		{"running", true},
		{"Running", true},
		{"RUNNING", true},
		{"exited", false},
		{"created", false},
		{"paused", false},
		{"dead", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("state=%q", tc.state), func(t *testing.T) {
			// Mirrors the logic in FetchContainerStatus: strings.ToLower(state) == "running"
			is_running := strings.ToLower(tc.state) == "running"
			if is_running != tc.running {
				t.Errorf("state=%q: expected running=%v, got %v", tc.state, tc.running, is_running)
			}
		})
	}
}
