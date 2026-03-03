package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/ui"
	"github.com/elvisnm/wt/internal/worktree"
)

// ── actions_for_worktree ─────────────────────────────────────────────────

func TestActionsForWorktree_DockerRunning(t *testing.T) {
	wt := worktree.Worktree{Type: worktree.TypeDocker, Running: true, ContainerExists: true}
	got := actions_for_worktree(wt)
	if &got[0] != &ui.WorktreeActions[0] {
		t.Error("expected WorktreeActions for running docker worktree")
	}
}

func TestActionsForWorktree_DockerStopped(t *testing.T) {
	wt := worktree.Worktree{Type: worktree.TypeDocker, Running: false, ContainerExists: true}
	got := actions_for_worktree(wt)
	if &got[0] != &ui.StoppedActions[0] {
		t.Error("expected StoppedActions for stopped docker worktree")
	}
}

func TestActionsForWorktree_LocalRunning(t *testing.T) {
	wt := worktree.Worktree{Type: worktree.TypeLocal, Running: true}
	got := actions_for_worktree(wt)
	if &got[0] != &ui.LocalRunningActions[0] {
		t.Error("expected LocalRunningActions for running local worktree")
	}
}

func TestActionsForWorktree_LocalStopped(t *testing.T) {
	wt := worktree.Worktree{Type: worktree.TypeLocal, Running: false}
	got := actions_for_worktree(wt)
	if &got[0] != &ui.LocalActions[0] {
		t.Error("expected LocalActions for stopped local worktree")
	}
}

func TestActionsForWorktree_NoContainer(t *testing.T) {
	wt := worktree.Worktree{Type: worktree.TypeDocker, ContainerExists: false}
	got := actions_for_worktree(wt)
	if &got[0] != &ui.LocalActions[0] {
		t.Error("expected LocalActions when container does not exist")
	}
}

func TestActionsForWorktree_HostBuildRunning(t *testing.T) {
	wt := worktree.Worktree{Type: worktree.TypeDocker, Running: true, ContainerExists: true, HostBuild: true}
	got := actions_for_worktree(wt)
	if &got[0] != &ui.HostBuildRunningActions[0] {
		t.Error("expected HostBuildRunningActions for running host-build worktree")
	}
}

func TestActionsForWorktree_HostBuildStopped(t *testing.T) {
	wt := worktree.Worktree{Type: worktree.TypeDocker, Running: false, ContainerExists: true, HostBuild: true}
	got := actions_for_worktree(wt)
	if &got[0] != &ui.HostBuildStoppedActions[0] {
		t.Error("expected HostBuildStoppedActions for stopped host-build worktree")
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

// ── reload_aws_credentials ───────────────────────────────────────────────

func TestReloadAWSCredentials(t *testing.T) {
	tmp := t.TempDir()
	creds_path := filepath.Join(tmp, ".aws", "credentials")
	os.MkdirAll(filepath.Dir(creds_path), 0755)
	os.WriteFile(creds_path, []byte(`[default]
aws_access_key_id = AKIAIOSFODNN7EXAMPLE
aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
aws_session_token = FwoGZXIvYXdzEA==EXAMPLE
`), 0644)

	// Override HOME so reload_aws_credentials reads our test file
	t.Setenv("HOME", tmp)

	// Clear existing values
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")

	reload_aws_credentials()

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

func TestReloadAWSCredentials_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Should not panic, just return silently
	reload_aws_credentials()
}

// ── kill_local_dev_processes ─────────────────────────────────────────────

func TestKillLocalDevProcesses_NoMatch(t *testing.T) {
	// Use a path that no real process will match
	killed := kill_local_dev_processes("/nonexistent/path/for/testing")
	if killed {
		t.Error("expected no processes to be killed for nonexistent path")
	}
}
