package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/elvisnm/wt/internal/aws"
	"github.com/elvisnm/wt/internal/esbuild"
	"github.com/elvisnm/wt/internal/labels"
	"github.com/elvisnm/wt/internal/pm2"
	"github.com/elvisnm/wt/internal/ui"
	"github.com/elvisnm/wt/internal/worktree"

	tea "github.com/charmbracelet/bubbletea"
)

// close_dev_tabs closes the dev server and create wizard tabs for a worktree.
func (m *Model) close_dev_tabs(alias string) {
	m.term_mgr.CloseByLabel(labels.Tab(labels.Dev, alias))
	m.term_mgr.CloseByLabel(labels.Tab(labels.Create, alias))
	m.term_mgr.CloseByLabel(labels.Create)
}

// start_dev_server starts PM2 and esbuild as independent daemons for a local worktree.
// Both survive dashboard close. On reopen, discovery detects them via PM2 status and PID files.
func (m Model) start_dev_server(wt worktree.Worktree) (Model, tea.Cmd) {
	debug_log("[services] start_dev_server: alias=%s path=%s isolated=%v", wt.Alias, wt.Path, wt.IsolatedPM2)

	// Refresh AWS credentials so PM2 processes get the latest keys
	if err := aws.Refresh(m.sso_profile()); err != nil {
		debug_log("[services] start_dev_server: aws.Refresh failed: %v", err)
	}

	// Full mode: use the project's ecosystem.dev.config.js (has proper heap sizes).
	// Other modes: regenerate ecosystem.worktree.config.js with filtered services.
	var ecosystem_config string
	if wt.Mode == "full" {
		dev_config := filepath.Join(wt.Path, "ecosystem.dev.config.js")
		if _, err := os.Stat(dev_config); err == nil {
			ecosystem_config = dev_config
			debug_log("[services] start_dev_server: full mode, using ecosystem.dev.config.js")
		}
	}

	if ecosystem_config == "" {
		ecosystem_config = filepath.Join(wt.Path, "ecosystem.worktree.config.js")

		// Regenerate ecosystem config so it picks up current env (no stale AWS creds)
		gen_script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "generate-ecosystem-config.js")
		if _, err := os.Stat(gen_script); err == nil {
			gen_args := []string{gen_script, "--dir", wt.Path}
			if wt.Mode != "" {
				gen_args = append(gen_args, "--mode", wt.Mode)
			}
			gen_cmd := exec.Command("node", gen_args...)
			gen_cmd.Dir = wt.Path
			gen_cmd.Env = os.Environ()
			if gen_out, gen_err := gen_cmd.CombinedOutput(); gen_err != nil {
				debug_log("[services] start_dev_server: regenerate ecosystem failed: %v (%s)", gen_err, string(gen_out))
			} else {
				debug_log("[services] start_dev_server: regenerated ecosystem config")
			}
		}
	}

	if _, err := os.Stat(ecosystem_config); os.IsNotExist(err) {
		debug_log("[services] start_dev_server: ecosystem config not found at %s", ecosystem_config)
		m.activity = fmt.Sprintf("error: ecosystem config not found for %s", wt.Alias)
		return m, nil
	}

	// Build env vars for PM2
	var pm2_home string
	if wt.IsolatedPM2 {
		pm2_home = wt.PM2Home()
	}

	// Load .env.worktree so ecosystem.dev.config.js can read WORKTREE_PORT_OFFSET, etc.
	var extra_env []string
	env_filename := ".env.worktree"
	if m.cfg != nil && m.cfg.Env.Filename != "" {
		env_filename = m.cfg.Env.Filename
	}
	env_path := filepath.Join(wt.Path, env_filename)
	if data, err := os.ReadFile(env_path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			extra_env = append(extra_env, line)
		}
	}

	// Start PM2 daemon
	out, err := pm2.Start(pm2_home, ecosystem_config, wt.Path, extra_env)
	if err != nil {
		debug_log("[services] start_dev_server: PM2 start failed: %v (output: %s)", err, out)
		m.activity = fmt.Sprintf("PM2 start failed: %v", err)
		return m, nil
	}
	debug_log("[services] start_dev_server: PM2 started")

	// Start esbuild watcher as daemon
	build_script := ""
	if m.cfg != nil && m.cfg.Paths.BuildScript != "" {
		build_script = filepath.Join(wt.Path, m.cfg.Paths.BuildScript)
	}
	if build_script != "" {
		state_dir := wt.PM2Home()
		extra_env := build_esbuild_env(wt, m.cfg)
		if err := esbuild.Start(build_script, wt.Path, state_dir, extra_env); err != nil {
			debug_log("[services] start_dev_server: esbuild start failed: %v", err)
			// Non-fatal — PM2 is running, esbuild can be started manually
			m.activity = fmt.Sprintf("started %s (esbuild failed: %v)", wt.Alias, err)
		} else {
			debug_log("[services] start_dev_server: esbuild started")
		}
	}

	m.terminal_output = ""
	m.activity = fmt.Sprintf("started %s", wt.Alias)

	return m, tea.Batch(
		tick_after(100*time.Millisecond, "render"),
		tick_after(3*time.Second, "status"),
	)
}

// stop_dev_server stops PM2 services for a local worktree (with confirmation)
func (m Model) stop_dev_server(wt worktree.Worktree) (Model, tea.Cmd) {
	return m.open_panel_confirm("Stop", fmt.Sprintf("Stop dev server on %s?", wt.Alias),
		func(mdl *Model) (Model, tea.Cmd) { return mdl.run_stop_dev_server(wt) })
}

func (m Model) run_stop_dev_server(wt worktree.Worktree) (Model, tea.Cmd) {
	manager := "pm2"
	if m.cfg != nil {
		manager = m.cfg.ServiceManager()
	}
	debug_log("[services] run_stop_dev_server: alias=%s manager=%s", wt.Alias, manager)

	// Close the dev server terminal session if it exists
	m.close_dev_tabs(wt.Alias)
	// Close any open service log tabs for this worktree
	m.close_worktree_logs(wt)
	m.close_preview()
	if m.term_mgr.Count() == 0 && m.focus == PanelTerminal {
		m.focus = PanelWorktrees
	}

	if manager != "pm2" {
		// For non-pm2 managers, closing the dev tab + killing processes is sufficient
		return m, tea.Sequence(
			func() tea.Msg {
				return MsgActionStarted{WtName: wt.Name, Status: "stopping..."}
			},
			func() tea.Msg {
				kill_local_dev_processes(wt.Path)
				return MsgActionOutput{}
			},
		)
	}

	// Stop esbuild watcher daemon if running
	esbuild.Stop(wt.PM2Home())

	// Check for isolated PM2_HOME
	if wt.IsolatedPM2 {
		return m, tea.Sequence(
			func() tea.Msg {
				return MsgActionStarted{WtName: wt.Name, Status: "stopping..."}
			},
			func() tea.Msg {
				out, err := run_host_cmd_env(pm2.HomeEnv(wt.PM2Home()), "pm2", "kill")
				return MsgActionOutput{Output: out, Err: err}
			},
		)
	}

	svc_names := make([]string, 0, len(m.services))
	for _, svc := range m.services {
		if svc.Name != "__all" {
			svc_names = append(svc_names, svc.Name)
		}
	}

	return m, tea.Sequence(
		func() tea.Msg {
			return MsgActionStarted{WtName: wt.Name, Status: "stopping..."}
		},
		func() tea.Msg {
			var last_err error
			var last_out string
			for _, name := range svc_names {
				out, err := run_host_cmd("pm2", "delete", name)
				if err != nil {
					last_err = err
					last_out = out
				}
			}
			return MsgActionOutput{Output: last_out, Err: last_err}
		},
	)
}

// restart_local_services kills and restarts a local worktree's dev server
// so it picks up fresh environment (e.g. updated AWS keys).
func (m Model) restart_local_services(wt worktree.Worktree) (Model, tea.Cmd) {
	debug_log("[aws] restart_local_services: %s (path=%s)", wt.Alias, wt.Path)
	m.activity = fmt.Sprintf("Restarting %s...", wt.Alias)

	// Kill OS-level node processes for this worktree
	debug_log("[aws] restart_local_services: killing dev processes")
	kill_local_dev_processes(wt.Path)

	// Kill the PM2 daemon so it restarts with fresh env vars (especially AWS keys).
	// Without this, pm2 start --update-env updates the config but the daemon itself
	// keeps its old environment and passes stale credentials to spawned processes.
	if wt.IsolatedPM2 {
		debug_log("[aws] restart_local_services: killing PM2 daemon (pm2_home=%s)", wt.PM2Home())
		pm2.Kill(wt.PM2Home())
	}

	// Close any existing terminal tabs for this worktree
	m.close_dev_tabs(wt.Alias)

	// Start a fresh dev server (aws.Refresh is called inside)
	return m.start_dev_server(wt)
}

// is_static_local returns true if the worktree uses the static service manager
// and is a local worktree (not Docker). Used to gate per-service actions.
func (m Model) is_static_local(wt worktree.Worktree) bool {
	if m.cfg == nil || wt.Type != worktree.TypeLocal {
		return false
	}
	return m.cfg.ServiceManager() == "static"
}

// start_worktree dispatches to the appropriate start method based on worktree type.
func (m Model) start_worktree(wt worktree.Worktree) (Model, tea.Cmd) {
	if wt.Type == worktree.TypeLocal {
		return m.start_dev_server(wt)
	}
	if wt.HostBuild {
		return m.start_host_build(wt)
	}
	if wt.Type == worktree.TypeDocker {
		return m, cmd_docker_action("start", wt, m.repo_root, m.cfg)
	}
	return m, nil
}

// open_esbuild_watch opens a terminal tab running dc-build.js for a host-build worktree
func (m Model) open_esbuild_watch(wt worktree.Worktree) (Model, tea.Cmd) {
	w, h := m.right_pane_dimensions()

	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-build.js")
	label := labels.Tab(labels.Build, wt.Alias)
	shell_cmd := fmt.Sprintf("node %q %s", script, wt.Name)

	_, err := m.term_mgr.Open(label, "bash", []string{"-c", shell_cmd}, w, h, m.repo_root)
	if err != nil {
		m.terminal_output = fmt.Sprintf("Failed to start esbuild watch: %v", err)
		m.prev_focus = m.focus; m.focus = PanelTerminal
		return m, nil
	}

	m.terminal_output = ""
	m.prev_focus = m.focus; m.focus = PanelTerminal
	// Focus the right pane for native terminal interaction
	if m.pane_layout != nil {
		m.pane_layout.FocusRight()
	}
	return m, tick_after(100*time.Millisecond, "render")
}

// start_host_build starts the Docker container and then opens esbuild watch on the host
func (m Model) start_host_build(wt worktree.Worktree) (Model, tea.Cmd) {
	m.activity = fmt.Sprintf("starting... %s", wt.Alias)

	// Refresh AWS credentials so the container gets fresh tokens
	if err := aws.Refresh(m.sso_profile()); err != nil {
		debug_log("[host-build] start: aws.Refresh failed: %v", err)
	}

	return m, tea.Sequence(
		func() tea.Msg {
			return MsgActionStarted{WtName: wt.Name, Status: "starting..."}
		},
		func() tea.Msg {
			out, err := run_worktree_up(wt, m.repo_root, m.cfg)
			if err != nil {
				return MsgActionOutput{Output: out, Err: err}
			}
			return MsgOpenBuildAfterStart{WtName: wt.Name}
		},
	)
}

// restart_host_build restarts the Docker container and reopens the esbuild watch tab
func (m Model) restart_host_build(wt worktree.Worktree) (Model, tea.Cmd) {
	build_label := labels.Tab(labels.Build, wt.Alias)
	m.term_mgr.CloseByLabel(build_label)
	debug_log("[restart] host-build %s: closed esbuild tab, restarting docker", wt.Alias)

	// Refresh AWS credentials so the container gets fresh tokens
	if err := aws.Refresh(m.sso_profile()); err != nil {
		debug_log("[host-build] restart: aws.Refresh failed: %v", err)
	}

	return m, tea.Sequence(
		func() tea.Msg {
			return MsgActionStarted{WtName: wt.Name, Status: "restarting..."}
		},
		func() tea.Msg {
			out, err := run_docker("restart", wt.Container)
			if err != nil {
				return MsgActionOutput{Output: out, Err: err}
			}
			return MsgOpenBuildAfterStart{WtName: wt.Name}
		},
	)
}

// stop_host_build closes the esbuild watch session and stops the Docker container
func (m Model) stop_host_build(wt worktree.Worktree) (Model, tea.Cmd) {
	build_label := labels.Tab(labels.Build, wt.Alias)
	m.term_mgr.CloseByLabel(build_label)
	if m.term_mgr.Count() == 0 && m.focus == PanelTerminal {
		m.focus = PanelWorktrees
	}

	return m, cmd_docker_action("stop", wt, m.repo_root, m.cfg)
}

// remove_worktree opens a picker to choose removal mode
func (m Model) remove_worktree(wt worktree.Worktree) (Model, tea.Cmd) {
	m.picker_open = true
	m.picker_cursor = 0
	m.picker_actions = ui.RemoveActions
	m.picker_context = pickerRemove
	m.recalc_layout()
	return m, nil
}

func (m Model) execute_remove_action(action ui.PickerAction) (Model, tea.Cmd) {
	wt := m.selected_worktree()
	if wt == nil {
		return m, nil
	}

	switch action.Key {
	case "n":
		return m.run_remove_worktree(*wt, false)
	case "f":
		return m.run_remove_worktree(*wt, true)
	}

	return m, nil
}

func (m Model) run_remove_worktree(wt worktree.Worktree, force bool) (Model, tea.Cmd) {
	// Close any terminal sessions for this worktree
	for _, prefix := range []string{labels.Shell, labels.Claude, labels.Logs, labels.Dev, labels.Build} {
		m.term_mgr.CloseByLabel(labels.Tab(prefix, wt.Alias))
	}
	if m.term_mgr.Count() == 0 && m.focus == PanelTerminal {
		m.focus = PanelWorktrees
	}

	m.services = nil
	m.service_cursor = 0
	m.close_preview()

	script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "dc-worktree-down.js")

	args := []string{script, wt.Name, "--remove"}
	if force {
		args = append(args, "--force")
	}

	return m, tea.Sequence(
		func() tea.Msg {
			return MsgActionStarted{WtName: wt.Name, Status: "removing..."}
		},
		func() tea.Msg {
			out, err := run_host_cmd("node", args...)
			return MsgActionOutput{Output: out, Err: err}
		},
	)
}
