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
	var actions []ui.PickerAction
	if wt.Type == worktree.TypeLocal {
		if wt.Running {
			actions = m.filter_local_running_actions()
		} else {
			actions = ui.LocalActions
		}
	} else if !wt.ContainerExists {
		actions = ui.LocalActions
	} else if wt.Running {
		actions = ui.WorktreeActions
	} else {
		actions = ui.StoppedActions
	}

	// When claude auto-mode is OFF, add "Claude (Auto)" option after "Claude"
	if !m.claude_auto_mode {
		actions = insert_claude_auto(actions)
	}
	return actions
}

// insert_claude_auto inserts ActionClaudeAuto right after the "c" (Claude) entry.
func insert_claude_auto(actions []ui.PickerAction) []ui.PickerAction {
	for i, a := range actions {
		if a.Key == "c" {
			result := make([]ui.PickerAction, 0, len(actions)+1)
			result = append(result, actions[:i+1]...)
			result = append(result, ui.ActionClaudeAuto)
			result = append(result, actions[i+1:]...)
			return result
		}
	}
	return actions
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
		if !strings.Contains(line, "node") {
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

