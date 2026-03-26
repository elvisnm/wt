package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elvisnm/wt/internal/aws"
	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/ui"
	"github.com/elvisnm/wt/internal/worktree"
)

// ── actions_for_worktree ─────────────────────────────────────────────────

func TestActionsForWorktree_DockerRunning(t *testing.T) {
	m := &Model{claude_auto_mode: true} // auto-mode ON skips insert_claude_auto
	wt := worktree.Worktree{Type: worktree.TypeDocker, Running: true, ContainerExists: true}
	got := m.actions_for_worktree(wt)
	if &got[0] != &ui.WorktreeActions[0] {
		t.Error("expected WorktreeActions for running docker worktree")
	}
}

func TestActionsForWorktree_DockerStopped(t *testing.T) {
	m := &Model{claude_auto_mode: true}
	wt := worktree.Worktree{Type: worktree.TypeDocker, Running: false, ContainerExists: true}
	got := m.actions_for_worktree(wt)
	if &got[0] != &ui.StoppedActions[0] {
		t.Error("expected StoppedActions for stopped docker worktree")
	}
}

func TestActionsForWorktree_LocalRunning_NoConfig(t *testing.T) {
	m := &Model{}
	wt := worktree.Worktree{Type: worktree.TypeLocal, Running: true}
	got := m.actions_for_worktree(wt)
	// No config means no services → both "Start service" and "Stop service" excluded
	for _, a := range got {
		if a.Label == "Start service" {
			t.Error("expected no 'Start service' action without config")
		}
		if a.Label == "Stop service" {
			t.Error("expected no 'Stop service' action without config")
		}
	}
}

func TestActionsForWorktree_LocalRunning_WithStoppedServices(t *testing.T) {
	cfg := &config.Config{}
	cfg.Dash.Services.List = []config.DashServiceEntry{
		{Name: "app", Port: 3001},
		{Name: "api", Port: 3004},
	}
	m := &Model{
		cfg: cfg,
		services: []worktree.Service{
			{Name: "app", Status: "online"},
		},
	}
	wt := worktree.Worktree{Type: worktree.TypeLocal, Running: true}
	got := m.actions_for_worktree(wt)
	found_start := false
	found_stop := false
	for _, a := range got {
		if a.Label == "Start service" {
			found_start = true
		}
		if a.Label == "Stop service" {
			found_stop = true
		}
	}
	if !found_start {
		t.Error("expected 'Start service' action when stopped services exist")
	}
	if !found_stop {
		t.Error("expected 'Stop service' action when running services exist")
	}
}

func TestActionsForWorktree_LocalRunning_AllRunning(t *testing.T) {
	cfg := &config.Config{}
	cfg.Dash.Services.List = []config.DashServiceEntry{
		{Name: "app", Port: 3001},
		{Name: "api", Port: 3004},
	}
	m := &Model{
		cfg: cfg,
		services: []worktree.Service{
			{Name: "app", Status: "online"},
			{Name: "api", Status: "online"},
		},
	}
	wt := worktree.Worktree{Type: worktree.TypeLocal, Running: true}
	got := m.actions_for_worktree(wt)
	for _, a := range got {
		if a.Label == "Start service" {
			t.Error("expected no 'Start service' action when all services running")
		}
	}
	found_stop := false
	for _, a := range got {
		if a.Label == "Stop service" {
			found_stop = true
		}
	}
	if !found_stop {
		t.Error("expected 'Stop service' action when services are running")
	}
}

func TestActionsForWorktree_LocalStopped(t *testing.T) {
	m := &Model{}
	wt := worktree.Worktree{Type: worktree.TypeLocal, Running: false}
	got := m.actions_for_worktree(wt)
	// No config means no modes → "Switch mode" filtered out
	for _, a := range got {
		if a.Key == "m" {
			t.Error("expected no 'Switch mode' action when no modes configured")
		}
	}
	if len(got) == 0 {
		t.Error("expected non-empty actions for stopped local worktree")
	}
}

func TestActionsForWorktree_NoContainer(t *testing.T) {
	m := &Model{}
	wt := worktree.Worktree{Type: worktree.TypeDocker, ContainerExists: false}
	got := m.actions_for_worktree(wt)
	// No config means no modes → "Switch mode" filtered out
	for _, a := range got {
		if a.Key == "m" {
			t.Error("expected no 'Switch mode' action when no modes configured")
		}
	}
	if len(got) == 0 {
		t.Error("expected non-empty actions when container does not exist")
	}
}

func TestActionsForWorktree_HostBuildRunning(t *testing.T) {
	m := &Model{claude_auto_mode: true}
	wt := worktree.Worktree{Type: worktree.TypeDocker, Running: true, ContainerExists: true, HostBuild: true}
	got := m.actions_for_worktree(wt)
	if &got[0] != &ui.HostBuildRunningActions[0] {
		t.Error("expected HostBuildRunningActions for running host-build worktree")
	}
}

func TestActionsForWorktree_HostBuildStopped(t *testing.T) {
	m := &Model{claude_auto_mode: true}
	wt := worktree.Worktree{Type: worktree.TypeDocker, Running: false, ContainerExists: true, HostBuild: true}
	got := m.actions_for_worktree(wt)
	if &got[0] != &ui.HostBuildStoppedActions[0] {
		t.Error("expected HostBuildStoppedActions for stopped host-build worktree")
	}
}

// ── insert_claude_auto ──────────────────────────────────────────────────

func TestInsertClaudeAuto_InsertsAfterClaude(t *testing.T) {
	actions := []ui.PickerAction{
		{Key: "b", Label: "Shell"},
		{Key: "c", Label: "Claude"},
		{Key: "z", Label: "Zsh"},
	}
	result := insert_claude_auto(actions)
	if len(result) != 4 {
		t.Fatalf("want 4 actions, got %d", len(result))
	}
	if result[1].Key != "c" {
		t.Errorf("result[1] should be 'c', got %q", result[1].Key)
	}
	if result[2].Key != "C" {
		t.Errorf("result[2] should be 'C' (auto), got %q", result[2].Key)
	}
	if result[3].Key != "z" {
		t.Errorf("result[3] should be 'z', got %q", result[3].Key)
	}
}

func TestInsertClaudeAuto_NoClaude(t *testing.T) {
	actions := []ui.PickerAction{
		{Key: "b", Label: "Shell"},
		{Key: "z", Label: "Zsh"},
	}
	result := insert_claude_auto(actions)
	if len(result) != 2 {
		t.Errorf("should return unchanged when no 'c' key, got %d", len(result))
	}
}

func TestActionsForWorktree_ClaudeAutoInserted(t *testing.T) {
	m := &Model{claude_auto_mode: false} // auto OFF → should insert [C]
	wt := worktree.Worktree{Type: worktree.TypeDocker, Running: true, ContainerExists: true}
	got := m.actions_for_worktree(wt)
	found := false
	for _, a := range got {
		if a.Key == "C" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected [C] Claude (Auto) when claude_auto_mode is OFF")
	}
}

func TestActionsForWorktree_ClaudeAutoNotInserted(t *testing.T) {
	m := &Model{claude_auto_mode: true} // auto ON → no [C] option
	wt := worktree.Worktree{Type: worktree.TypeDocker, Running: true, ContainerExists: true}
	got := m.actions_for_worktree(wt)
	for _, a := range got {
		if a.Key == "C" {
			t.Error("should NOT have [C] Claude (Auto) when claude_auto_mode is ON")
		}
	}
}

// ── flow_scripts_dir ─────────────────────────────────────────────────────

func TestFlowScriptsDir_ConfigAbsPath(t *testing.T) {
	cfg := &config.Config{}
	cfg.Paths.FlowScripts = "/custom/scripts"
	got := flow_scripts_dir("/repo", cfg)
	if got != "/custom/scripts" {
		t.Errorf("expected /custom/scripts, got %s", got)
	}
}

func TestFlowScriptsDir_ConfigRelPath(t *testing.T) {
	cfg := &config.Config{}
	cfg.Paths.FlowScripts = "scripts/wt"
	got := flow_scripts_dir("/repo", cfg)
	if got != "/repo/scripts/wt" {
		t.Errorf("expected /repo/scripts/wt, got %s", got)
	}
}

func TestFlowScriptsDir_EnvVar(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("WT_SCRIPTS_DIR", tmp)
	got := flow_scripts_dir("/repo", nil)
	if got != tmp {
		t.Errorf("expected %s, got %s", tmp, got)
	}
}

func TestFlowScriptsDir_EnvVarInvalid(t *testing.T) {
	t.Setenv("WT_SCRIPTS_DIR", "/nonexistent/path")
	got := flow_scripts_dir("/repo", nil)
	// Should fall through to binary-relative or legacy
	if got == "/nonexistent/path" {
		t.Error("should not use nonexistent WT_SCRIPTS_DIR path")
	}
}

func TestFlowScriptsDir_LegacyFallback(t *testing.T) {
	t.Setenv("WT_SCRIPTS_DIR", "")
	got := flow_scripts_dir("/repo", nil)
	expected := "/repo/scripts/worktree"
	if got != expected {
		// May resolve to binary-relative path if binary exists — that's also OK
		if !filepath.IsAbs(got) {
			t.Errorf("expected absolute path, got %s", got)
		}
	}
}

// ── aws.Refresh ─────────────────────────────────────────────────────────

func TestAWSRefreshFromFile(t *testing.T) {
	tmp := t.TempDir()
	creds_path := filepath.Join(tmp, ".aws", "credentials")
	os.MkdirAll(filepath.Dir(creds_path), 0755)
	os.WriteFile(creds_path, []byte(`[default]
aws_access_key_id = AKIAIOSFODNN7EXAMPLE
aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
aws_session_token = FwoGZXIvYXdzEA==EXAMPLE
`), 0644)

	// Override HOME so aws.Refresh reads our test file
	t.Setenv("HOME", tmp)

	// Clear existing values
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")

	if err := aws.Refresh(""); err != nil {
		t.Fatalf("aws.Refresh failed: %v", err)
	}

	if got := os.Getenv("AWS_ACCESS_KEY_ID"); got != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("AWS_ACCESS_KEY_ID = %q, want AKIAIOSFODNN7EXAMPLE", got)
	}
	if got := os.Getenv("AWS_SECRET_ACCESS_KEY"); got != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("AWS_SECRET_ACCESS_KEY not set correctly, got %q", got)
	}
	if got := os.Getenv("AWS_SESSION_TOKEN"); got != "FwoGZXIvYXdzEA==EXAMPLE" {
		t.Errorf("AWS_SESSION_TOKEN not set correctly, got %q", got)
	}
}

func TestAWSRefresh_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Should return an error, not panic
	if err := aws.Refresh(""); err == nil {
		t.Error("expected error when credentials file is missing")
	}
}

// ── kill_local_dev_processes ─────────────────────────────────────────────

func TestKillLocalDevProcesses_NoMatch(t *testing.T) {
	// Use a path that no real process will match
	killed := kill_local_dev_processes("/nonexistent/path/for/testing")
	if killed {
		t.Error("expected no processes to be killed for nonexistent path")
	}
}
