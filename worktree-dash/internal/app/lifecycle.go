package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

// start_dev_server starts PM2 as a daemon for a local worktree.
// It survives dashboard close. On reopen, discovery detects it via PM2 status.
func (m Model) start_dev_server(wt worktree.Worktree) (Model, tea.Cmd) {
	debug_log("[services] start_dev_server: alias=%s path=%s isolated=%v", wt.Alias, wt.Path, wt.IsolatedPM2)

	// Use the project's ecosystem.dev.config.js when available (has proper heap sizes).
	// Fall back to ecosystem.worktree.config.js, regenerating it so env stays current.
	var ecosystem_config string
	dev_config := filepath.Join(wt.Path, "ecosystem.dev.config.js")
	if _, err := os.Stat(dev_config); err == nil {
		ecosystem_config = dev_config
		debug_log("[services] start_dev_server: using ecosystem.dev.config.js")
	}

	if ecosystem_config == "" {
		ecosystem_config = filepath.Join(wt.Path, "ecosystem.worktree.config.js")

		// Regenerate ecosystem config so it picks up current env (no stale AWS creds)
		gen_script := filepath.Join(flow_scripts_dir(m.repo_root, m.cfg), "generate-ecosystem-config.js")
		if _, err := os.Stat(gen_script); err == nil {
			gen_args := []string{gen_script, "--dir", wt.Path}
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
// so it picks up fresh environment.
func (m Model) restart_local_services(wt worktree.Worktree) (Model, tea.Cmd) {
	debug_log("[services] restart_local_services: %s (path=%s)", wt.Alias, wt.Path)
	m.activity = fmt.Sprintf("Restarting %s...", wt.Alias)

	// Kill OS-level node processes for this worktree
	debug_log("[services] restart_local_services: killing dev processes")
	kill_local_dev_processes(wt.Path)

	// Kill the PM2 daemon so it restarts with fresh env vars.
	if wt.IsolatedPM2 {
		debug_log("[services] restart_local_services: killing PM2 daemon (pm2_home=%s)", wt.PM2Home())
		pm2.Kill(wt.PM2Home())
	}

	// Close any existing terminal tabs for this worktree
	m.close_dev_tabs(wt.Alias)

	// Start a fresh dev server
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
	if wt.Type == worktree.TypeDocker {
		return m, cmd_docker_action("start", wt, m.repo_root, m.cfg)
	}
	return m, nil
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
	for _, prefix := range []string{labels.Shell, labels.Claude, labels.Logs, labels.Dev} {
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
