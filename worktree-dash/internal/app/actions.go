package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/ui"
	"github.com/elvisnm/wt/internal/worktree"

	tea "github.com/charmbracelet/bubbletea"
)

// flow_scripts_dir returns the directory containing worktree flow scripts.
// Resolution order:
//  1. cfg.Paths.FlowScripts (explicit per-project config)
//  2. WT_SCRIPTS_DIR env var
//  3. Relative to binary: ../share/wt/worktree-flow/ (Homebrew layout)
//  4. Relative to binary: ../worktree-flow/ (dev layout)
//  5. Legacy fallback: <repo_root>/scripts/worktree
func flow_scripts_dir(repo_root string, cfg *config.Config) string {
	if cfg != nil && cfg.Paths.FlowScripts != "" {
		p := cfg.Paths.FlowScripts
		if !filepath.IsAbs(p) {
			return filepath.Join(repo_root, p)
		}
		return p
	}

	if dir := os.Getenv("WT_SCRIPTS_DIR"); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			binDir := filepath.Dir(exe)
			// Homebrew: <prefix>/share/wt/worktree-flow/
			brewPath := filepath.Join(binDir, "..", "share", "wt", "worktree-flow")
			if info, err := os.Stat(brewPath); err == nil && info.IsDir() {
				return brewPath
			}
			// Dev: <repo>/worktree-dash/../worktree-flow/
			devPath := filepath.Join(binDir, "..", "worktree-flow")
			if info, err := os.Stat(devPath); err == nil && info.IsDir() {
				return devPath
			}
		}
	}

	return filepath.Join(repo_root, "scripts", "worktree")
}

// MsgActionStarted sets immediate visual feedback on a worktree
type MsgActionStarted struct {
	WtName string
	Status string // "starting...", "stopping...", "restarting..."
}

// MsgActionOutput carries the result text from a completed action
type MsgActionOutput struct {
	Output string
	Err    error
}

func (m *Model) actions_for_worktree(wt worktree.Worktree) []ui.PickerAction {
	if wt.Type == worktree.TypeLocal {
		if wt.Running {
			return m.filter_local_running_actions()
		}
		return ui.LocalActions
	}
	if !wt.ContainerExists {
		return ui.LocalActions
	}
	if wt.HostBuild {
		if wt.Running {
			return ui.HostBuildRunningActions
		}
		return ui.HostBuildStoppedActions
	}
	if wt.Running {
		return ui.WorktreeActions
	}
	return ui.StoppedActions
}

// filter_local_running_actions returns LocalRunningActions, excluding
// "Start service" when all configured services are running, and
// "Stop service" when no configured services are running.
func (m *Model) filter_local_running_actions() []ui.PickerAction {
	has_stopped, has_running := m.service_availability()
	if has_stopped && has_running {
		return ui.LocalRunningActions
	}
	actions := make([]ui.PickerAction, 0, len(ui.LocalRunningActions))
	for _, a := range ui.LocalRunningActions {
		if !has_stopped && a.Label == "Start service" {
			continue
		}
		if !has_running && a.Label == "Stop service" {
			continue
		}
		actions = append(actions, a)
	}
	return actions
}

// service_availability checks configured services against running PM2 processes
// and returns whether any are stopped and whether any are running.
func (m *Model) service_availability() (has_stopped, has_running bool) {
	if m.cfg == nil || len(m.cfg.Dash.Services.List) == 0 {
		return false, false
	}
	running := m.running_base_names(m.selected_alias())
	for _, entry := range m.cfg.Dash.Services.List {
		for _, b := range entry.BaseProcesses() {
			if running[b] {
				has_running = true
			} else {
				has_stopped = true
			}
			if has_running && has_stopped {
				return
			}
		}
	}
	return
}

func (m *Model) selected_alias() string {
	if wt := m.selected_worktree(); wt != nil {
		return wt.Alias
	}
	return ""
}

func cmd_docker_action(action string, wt worktree.Worktree, repo_root string, cfg *config.Config) tea.Cmd {
	// For lifecycle actions, send a started message first, then run the command
	switch action {
	case "start", "stop", "restart":
		status_map := map[string]string{
			"start":   "starting...",
			"stop":    "stopping...",
			"restart": "restarting...",
		}
		return tea.Sequence(
			func() tea.Msg {
				return MsgActionStarted{WtName: wt.Name, Status: status_map[action]}
			},
			func() tea.Msg {
				var out string
				var err error
				switch action {
				case "restart":
					out, err = run_docker("restart", wt.Container)
				case "stop":
					out, err = run_docker("stop", wt.Container)
				case "start":
					out, err = run_worktree_up(wt, repo_root, cfg)
				}
				return MsgActionOutput{Output: out, Err: err}
			},
		)
	default:
		return func() tea.Msg {
			return MsgActionOutput{Output: fmt.Sprintf("Unknown action: %s", action)}
		}
	}
}

// kill_local_dev_processes finds and kills all node processes running from
// the given worktree path. Returns true if any processes were killed.
func kill_local_dev_processes(wt_path string) bool {
	out, err := exec.Command("ps", "-eo", "pid,args").CombinedOutput()
	if err != nil {
		return false
	}

	killed := false
	my_pid := os.Getpid()
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, wt_path) {
			continue
		}
		// Match node processes running from this worktree
		if !strings.Contains(line, "node") && !strings.Contains(line, "esbuild") {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		pid := 0
		fmt.Sscanf(fields[0], "%d", &pid)
		if pid <= 0 || pid == my_pid {
			continue
		}
		if p, err := os.FindProcess(pid); err == nil {
			p.Signal(os.Kill)
			killed = true
		}
	}
	return killed
}

// reload_aws_credentials reads ~/.aws/credentials and sets env vars
// so child processes (like pnpm dev) inherit the fresh keys.
func reload_aws_credentials() {
	debug_log("[aws] reload_aws_credentials: start")
	home, err := os.UserHomeDir()
	if err != nil {
		debug_log("[aws] reload_aws_credentials: UserHomeDir error: %v", err)
		return
	}
	creds_path := filepath.Join(home, ".aws", "credentials")
	data, err := os.ReadFile(creds_path)
	if err != nil {
		debug_log("[aws] reload_aws_credentials: ReadFile error: %v", err)
		return
	}
	debug_log("[aws] reload_aws_credentials: read %s (%d bytes)", creds_path, len(data))
	keys_set := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") || line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "aws_access_key_id":
			os.Setenv("AWS_ACCESS_KEY_ID", val)
			safe := val
			if len(safe) > 8 {
				safe = safe[:8]
			}
			debug_log("[aws] reload_aws_credentials: set AWS_ACCESS_KEY_ID=%s...", safe)
			keys_set++
		case "aws_secret_access_key":
			os.Setenv("AWS_SECRET_ACCESS_KEY", val)
			debug_log("[aws] reload_aws_credentials: set AWS_SECRET_ACCESS_KEY=[hidden]")
			keys_set++
		case "aws_session_token":
			os.Setenv("AWS_SESSION_TOKEN", val)
			debug_log("[aws] reload_aws_credentials: set AWS_SESSION_TOKEN=[hidden]")
			keys_set++
		}
	}
	debug_log("[aws] reload_aws_credentials: done, set %d keys", keys_set)
}

// export_sso_credentials runs `aws configure export-credentials` to extract
// temporary credentials from the SSO session, writes them to ~/.aws/credentials,
// and sets them as env vars so child processes (PM2, pnpm dev) inherit them.
func export_sso_credentials(profile string) error {
	debug_log("[aws] export_sso_credentials: profile=%s", profile)
	cmd := exec.Command("aws", "configure", "export-credentials", "--profile", profile, "--format", "env-no-export")
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		debug_log("[aws] export_sso_credentials: command failed: %v", err)
		return fmt.Errorf("aws configure export-credentials failed: %w", err)
	}

	// Parse KEY=VALUE lines
	creds := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		creds[parts[0]] = parts[1]
	}

	access_key := creds["AWS_ACCESS_KEY_ID"]
	secret_key := creds["AWS_SECRET_ACCESS_KEY"]
	session_token := creds["AWS_SESSION_TOKEN"]

	if access_key == "" || secret_key == "" {
		debug_log("[aws] export_sso_credentials: missing credentials in output")
		return fmt.Errorf("export-credentials returned incomplete credentials")
	}

	// Set env vars for current process + children
	os.Setenv("AWS_ACCESS_KEY_ID", access_key)
	os.Setenv("AWS_SECRET_ACCESS_KEY", secret_key)
	os.Setenv("AWS_SESSION_TOKEN", session_token)
	safe := access_key
	if len(safe) > 8 {
		safe = safe[:8]
	}
	debug_log("[aws] export_sso_credentials: set env vars (key=%s...)", safe)

	// Write to ~/.aws/credentials so the SDK credential chain picks them up
	home, err := os.UserHomeDir()
	if err != nil {
		debug_log("[aws] export_sso_credentials: UserHomeDir error: %v", err)
		return nil // env vars are set, credentials file is best-effort
	}
	creds_content := fmt.Sprintf("[default]\naws_access_key_id = %s\naws_secret_access_key = %s\naws_session_token = %s\n",
		access_key, secret_key, session_token)
	creds_path := filepath.Join(home, ".aws", "credentials")
	if err := os.WriteFile(creds_path, []byte(creds_content), 0600); err != nil {
		debug_log("[aws] export_sso_credentials: WriteFile error: %v", err)
		return nil // env vars are set, credentials file is best-effort
	}
	debug_log("[aws] export_sso_credentials: wrote %s", creds_path)

	return nil
}

func run_docker(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func run_worktree_up(wt worktree.Worktree, repo_root string, cfg *config.Config) (string, error) {
	script := filepath.Join(flow_scripts_dir(repo_root, cfg), "dc-worktree-up.js")
	cmd := exec.Command("node", script, wt.Name)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

