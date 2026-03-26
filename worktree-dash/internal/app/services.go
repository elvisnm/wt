package app

import (
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/docker"
	"github.com/elvisnm/wt/internal/esbuild"
	"github.com/elvisnm/wt/internal/labels"
	"github.com/elvisnm/wt/internal/pm2"
	"github.com/elvisnm/wt/internal/terminal"
	"github.com/elvisnm/wt/internal/ui"
	"github.com/elvisnm/wt/internal/worktree"

	tea "github.com/charmbracelet/bubbletea"
)

func cmd_fetch_services(wt worktree.Worktree, cfg *config.Config) tea.Cmd {
	manager := "pm2"
	if cfg != nil {
		manager = cfg.DockerServiceManager()
	}
	return func() tea.Msg {
		debug_log("[services] fetch_services: %s manager=%s container=%s", wt.Alias, manager, wt.Container)
		var svcs []worktree.Service
		switch manager {
		case "static":
			svcs = build_static_services(cfg, &wt)
		default:
			svcs = docker.FetchServices(wt.Container, wt.Name)
		}
		debug_log("[services] fetch_services: %s returned %d services", wt.Alias, len(svcs))
		return MsgServicesUpdated{Services: svcs}
	}
}

func cmd_fetch_local_services(wt worktree.Worktree, cfg *config.Config) tea.Cmd {
	has_static := cfg != nil && len(cfg.Dash.Services.List) > 0
	return func() tea.Msg {
		debug_log("[services] fetch_local_services: %s has_static=%v path=%s", wt.Alias, has_static, wt.Path)

		var svcs []worktree.Service
		var pm2_svcs []worktree.Service
		if wt.IsolatedPM2 {
			pm2_svcs = pm2.FetchServicesWithHome(wt.PM2Home())
		} else {
			pm2_svcs = pm2.FetchServices(wt.Path)
		}

		if has_static {
			svcs = build_static_services(cfg, &wt)
			svcs = merge_pm2_into_static(svcs, pm2_svcs, cfg)
		} else {
			svcs = pm2_svcs
		}

		// Append esbuild watcher status (only if project has a build script)
		if cfg != nil && cfg.Paths.BuildScript != "" {
			esbuild_status := "stopped"
			if esbuild.IsRunning(wt.PM2Home()) {
				esbuild_status = "online"
			}
			svcs = append(svcs, worktree.Service{
				Name:        "esbuild",
				DisplayName: "esbuild (watch)",
				Status:      esbuild_status,
			})
		}

		debug_log("[services] fetch_local_services: %s returned %d services", wt.Alias, len(svcs))
		return MsgServicesUpdated{Services: svcs}
	}
}

// build_static_services returns services from the config's static list.
// For local worktrees, checks port liveness on the base port.
// For Docker worktrees, queries the actual host port mapping from Docker.
func build_static_services(cfg *config.Config, wt *worktree.Worktree) []worktree.Service {
	if cfg == nil || len(cfg.Dash.Services.List) == 0 {
		return nil
	}

	services := []worktree.Service{
		{Name: "__all", DisplayName: "All services", Status: "online"},
	}

	// Filter by worktree mode if set
	var mode_filter map[string]bool
	if wt != nil && wt.Mode != "" {
		if mode_svcs := cfg.ServicesForMode(wt.Mode); mode_svcs != nil {
			mode_filter = make(map[string]bool, len(mode_svcs))
			for _, s := range mode_svcs {
				mode_filter[s] = true
			}
		}
	}

	all_online := true
	for _, entry := range cfg.Dash.Services.List {
		if mode_filter != nil && !mode_filter[entry.Name] {
			continue
		}
		svc := worktree.Service{
			Name:        entry.Name,
			DisplayName: entry.Name,
			Status:      "unknown",
		}

		if wt != nil && entry.Port > 0 {
			port := 0
			if wt.Type == worktree.TypeDocker {
				// Query actual host port from Docker
				port = docker_host_port(container_for_service(*wt, entry.Name, cfg), entry.Port)
			} else {
				port = entry.Port + wt.Offset
			}
			if port > 0 {
				addr := fmt.Sprintf("127.0.0.1:%d", port)
				conn, err := net.DialTimeout("tcp4", addr, 500*time.Millisecond)
				if err == nil {
					conn.Close()
					svc.Status = "online"
					debug_log("[services] port_check: %s port=%d -> online", entry.Name, port)
				} else {
					svc.Status = "stopped"
					all_online = false
					debug_log("[services] port_check: %s port=%d -> stopped (%v)", entry.Name, port, err)
				}
			} else {
				svc.Status = "stopped"
				all_online = false
				debug_log("[services] port_check: %s port=%d -> stopped (no port)", entry.Name, entry.Port)
			}
		}

		services = append(services, svc)
	}

	if !all_online {
		services[0].Status = "degraded"
	}

	if len(services) == 1 {
		return nil
	}

	return services
}

// merge_pm2_into_static merges PM2 runtime status into the static service list.
// Matches PM2 processes to config entries by name or by the processes field.
// A config entry is marked online if ANY of its mapped PM2 processes is online.
func merge_pm2_into_static(static_svcs []worktree.Service, pm2_svcs []worktree.Service, cfg *config.Config) []worktree.Service {
	if len(pm2_svcs) == 0 {
		return static_svcs
	}

	// Build lookup of PM2 services by name (skip __all)
	pm2_map := make(map[string]*worktree.Service)
	for i := range pm2_svcs {
		if pm2_svcs[i].Name == "__all" {
			continue
		}
		pm2_map[pm2_svcs[i].Name] = &pm2_svcs[i]
	}

	// Build mapping: PM2 process name -> config service index
	// Uses config's "processes" field, falling back to matching by name
	pm2_to_static := make(map[string]int) // pm2 name -> static_svcs index
	if cfg != nil {
		for i, entry := range cfg.Dash.Services.List {
			static_idx := i + 1 // +1 for __all at index 0
			for _, proc := range entry.BaseProcesses() {
				pm2_to_static[proc] = static_idx
			}
		}
	}

	// Update matching static entries with PM2 status.
	// PM2 names are namespaced (e.g. "app-feat-my-branch"), so try the full
	// name first, then strip known worktree suffixes to find the base name.
	for pm2_name, pm2_svc := range pm2_map {
		idx, ok := pm2_to_static[pm2_name]
		if !ok {
			// Strip worktree suffix: "app-feat-my-branch" -> "app"
			base := pm2_name
			for _, entry := range cfg.Dash.Services.List {
				for _, proc := range entry.BaseProcesses() {
					if strings.HasPrefix(pm2_name, proc+"-") {
						base = proc
						break
					}
				}
				if base != pm2_name {
					break
				}
			}
			idx, ok = pm2_to_static[base]
		}
		if !ok {
			debug_log("[services] merge_pm2: no config match for PM2 process %q", pm2_name)
			continue
		}
		if idx < 0 || idx >= len(static_svcs) {
			continue
		}
		if pm2_svc.Status == "online" && static_svcs[idx].Status != "online" {
			static_svcs[idx].Status = "online"
			debug_log("[services] merge_pm2: %s -> %s (via PM2 %s)", static_svcs[idx].Name, "online", pm2_name)
		}
		// Accumulate CPU/memory from all mapped PM2 processes
		static_svcs[idx].CPU += pm2_svc.CPU
		static_svcs[idx].Memory += pm2_svc.Memory
		if pm2_svc.RestartCount > static_svcs[idx].RestartCount {
			static_svcs[idx].RestartCount = pm2_svc.RestartCount
		}
	}

	// Recalculate __all status
	all_online := true
	for _, svc := range static_svcs {
		if svc.Name == "__all" {
			continue
		}
		if svc.Status != "online" {
			all_online = false
			break
		}
	}
	if len(static_svcs) > 0 && static_svcs[0].Name == "__all" {
		if all_online {
			static_svcs[0].Status = "online"
		} else {
			static_svcs[0].Status = "degraded"
		}
	}

	return static_svcs
}

// docker_host_port returns the host port mapped to a container's internal port.
// Uses `docker port <container> <port>` to get the actual mapping.
func docker_host_port(container string, internal_port int) int {
	out, err := exec.Command("docker", "port", container, fmt.Sprintf("%d", internal_port)).Output()
	if err != nil {
		return 0
	}
	// Output format: "0.0.0.0:3048\n[::]:3048\n"
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	parts := strings.SplitN(line, ":", 2)
	if len(parts) == 2 {
		if p, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
			return p
		}
	}
	return 0
}

// mark_local_running checks whether local worktrees are running.
// Uses the configured runningCheck method: "pm2" (default) or "devTab".
// Worktrees with an isolated .pm2 directory use PM2_HOME-aware checks.
func mark_local_running(wts []worktree.Worktree, cfg *config.Config, term_mgr *terminal.Manager) []worktree.Worktree {
	check := "pm2"
	if cfg != nil {
		check = cfg.Dash.Services.RunningCheck
	}
	debug_log("[discovery] mark_local_running: method=%s", check)

	// Collect all local worktree paths for status checking
	local_paths := make(map[string]string)
	for i := range wts {
		if wts[i].Type == worktree.TypeLocal {
			local_paths[wts[i].Path] = wts[i].Name
		}
	}
	if len(local_paths) == 0 {
		return wts
	}

	switch check {
	case "devTab":
		// Check for running dev/create terminal tabs first
		var fallback_paths map[string]string
		for i := range wts {
			if wts[i].Type != worktree.TypeLocal || term_mgr == nil {
				continue
			}
			dev_label := labels.Tab(labels.Dev, wts[i].Alias)
			create_label := labels.Tab(labels.Create, wts[i].Alias)
			wts[i].Running = term_mgr.IsLabelAlive(dev_label) ||
				term_mgr.IsLabelAlive(create_label)
			if wts[i].Running {
				debug_log("[discovery]   %s running=true (devTab)", wts[i].Alias)
			} else {
				if fallback_paths == nil {
					fallback_paths = make(map[string]string)
				}
				fallback_paths[wts[i].Path] = wts[i].Name
			}
		}
		// Fallback: check PM2 for worktrees without a dev tab
		if len(fallback_paths) > 0 {
			// For isolated PM2, check each worktree's own PM2_HOME
			for i := range wts {
				if wts[i].Type != worktree.TypeLocal || wts[i].Running {
					continue
				}
				if wts[i].IsolatedPM2 {
					running, cpu, mem := pm2.StatusWithHome(wts[i].PM2Home())
					wts[i].Running = running
					wts[i].CPU = cpu
					wts[i].Mem = mem
					if running {
						debug_log("[discovery]   %s running=true (pm2_home fallback)", wts[i].Alias)
					}
					delete(fallback_paths, wts[i].Path)
				}
			}
			// Non-isolated PM2 fallback
			if len(fallback_paths) > 0 {
				running := pm2.FetchRunningWorktrees(fallback_paths)
				for i := range wts {
					if wts[i].Type == worktree.TypeLocal && !wts[i].Running && running[wts[i].Name] {
						wts[i].Running = true
						debug_log("[discovery]   %s running=true (pm2 fallback)", wts[i].Alias)
					}
				}
			}
		}
	default: // "pm2"
		// For isolated PM2, check each worktree's own PM2_HOME
		for i := range wts {
			if wts[i].Type != worktree.TypeLocal {
				continue
			}
			if wts[i].IsolatedPM2 {
				running, cpu, mem := pm2.StatusWithHome(wts[i].PM2Home())
				wts[i].Running = running
				wts[i].CPU = cpu
				wts[i].Mem = mem
				if running {
					debug_log("[discovery]   %s running=true (pm2_home)", wts[i].Alias)
				}
				delete(local_paths, wts[i].Path)
			}
		}
		// Non-isolated: global PM2 check
		if len(local_paths) > 0 {
			running := pm2.FetchRunningWorktrees(local_paths)
			for i := range wts {
				if wts[i].Type == worktree.TypeLocal && !wts[i].Running {
					if _, ok := local_paths[wts[i].Path]; ok {
						wts[i].Running = running[wts[i].Name]
					}
				}
			}
		}
	}
	return wts
}

func (m Model) refresh_services() tea.Cmd {
	wt := m.selected_worktree()
	if wt == nil {
		debug_log("[services] refresh_services: no selected worktree (cursor=%d, len=%d)", m.cursor, len(m.worktrees))
		return nil
	}
	if !wt.Running {
		debug_log("[services] refresh_services: %s not running (type=%v)", wt.Alias, wt.Type)
		return nil
	}
	debug_log("[services] refresh_services: %s type=%v running=%v", wt.Alias, wt.Type, wt.Running)
	if wt.Type == worktree.TypeDocker {
		return cmd_fetch_services(*wt, m.cfg)
	}
	return cmd_fetch_local_services(*wt, m.cfg)
}

// running_base_names returns a set of base service names (without worktree suffix)
// from the currently fetched m.services. PM2 names like "app-feat-test" become "app".
func (m *Model) running_base_names(alias string) map[string]bool {
	running := make(map[string]bool)
	suffix := ""
	if alias != "" {
		suffix = "-" + alias
	}
	for _, svc := range m.services {
		if svc.Status != "online" {
			continue
		}
		name := svc.Name
		if suffix != "" && strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
		}
		running[name] = true
	}
	return running
}

// pm2_log_target returns the PM2 process name(s) to pass to `pm2 logs` for a service.
// For isolated PM2, process names are suffixed with the worktree alias.
// For multi-process services (e.g. sync -> combined_sync, listings_sync), returns a
// regex pattern so pm2 logs shows all matching processes.
func (m Model) pm2_log_target(svc worktree.Service, wt worktree.Worktree) string {
	names := []string{svc.Name}
	if m.cfg != nil {
		for _, entry := range m.cfg.Dash.Services.List {
			if entry.Name == svc.Name {
				names = pm2_process_names(entry, wt.Name)
				break
			}
		}
	} else if wt.IsolatedPM2 && wt.Name != "" {
		names = []string{svc.Name + "-" + wt.Name}
	}

	if len(names) == 1 {
		return names[0]
	}
	// Multiple processes: use regex pattern for pm2 logs
	return "/" + strings.Join(names, "|") + "/"
}

// pm2_process_names returns the PM2 process names for a config service entry,
// namespaced with the worktree alias (e.g. "app" -> "app-feat-test").
func pm2_process_names(entry config.DashServiceEntry, alias string) []string {
	bases := entry.BaseProcesses()
	if alias == "" {
		return bases
	}
	names := make([]string, len(bases))
	for i, b := range bases {
		names[i] = b + "-" + alias
	}
	return names
}

func (m Model) open_start_service_picker(wt worktree.Worktree) (Model, tea.Cmd) {
	return m.open_service_picker(wt, pickerStartService)
}

func (m Model) open_stop_service_picker(wt worktree.Worktree) (Model, tea.Cmd) {
	return m.open_service_picker(wt, pickerStopService)
}

// open_service_picker builds a picker of services filtered by state.
// For pickerStartService: shows services with any stopped process.
// For pickerStopService: shows services with any running process.
func (m Model) open_service_picker(wt worktree.Worktree, mode string) (Model, tea.Cmd) {
	debug_log("[svc_picker] open: mode=%s alias=%s services=%d cfg=%v", mode, wt.Alias, len(m.services), m.cfg != nil)
	if m.cfg == nil || len(m.cfg.Dash.Services.List) == 0 {
		m.activity = "No services configured"
		return m, nil
	}

	running := m.running_base_names(wt.Alias)
	want_running := mode == pickerStopService

	var actions []ui.PickerAction
	idx := 0
	for _, entry := range m.cfg.Dash.Services.List {
		match := false
		for _, b := range entry.BaseProcesses() {
			if running[b] == want_running {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		if idx >= 26 {
			break
		}
		key := string(rune('a' + idx))
		actions = append(actions, ui.PickerAction{
			Key:   key,
			Label: entry.Name,
			Desc:  fmt.Sprintf("port %d", entry.Port),
		})
		idx++
	}

	debug_log("[svc_picker] %s: %d services to offer", mode, len(actions))
	if len(actions) == 0 {
		if want_running {
			m.activity = "No services are running"
		} else {
			m.activity = "All services are already running"
		}
		return m, nil
	}

	m.picker_actions = actions
	m.picker_cursor = 0
	m.picker_open = true
	m.picker_context = mode
	m.recalc_layout()
	return m, nil
}

func (m Model) execute_start_service_action(action ui.PickerAction) (Model, tea.Cmd) {
	debug_log("[start_svc] execute: label=%s key=%s", action.Label, action.Key)
	wt := m.selected_worktree()
	if wt == nil {
		return m, nil
	}

	// Find the config entry to get the actual PM2 process names
	var pm2_names []string
	for _, entry := range m.cfg.Dash.Services.List {
		if entry.Name == action.Label {
			pm2_names = pm2_process_names(entry, wt.Name)
			break
		}
	}

	if len(pm2_names) == 0 {
		return m, nil
	}

	m.activity = fmt.Sprintf("Starting %s...", action.Label)

	// For isolated PM2, regenerate ecosystem once then start all processes
	if wt.IsolatedPM2 {
		return m, cmd_start_isolated_services(*wt, pm2_names, m.cfg)
	}

	// Non-isolated: start each process individually
	var cmds []tea.Cmd
	for _, name := range pm2_names {
		svc := worktree.Service{Name: name, DisplayName: name}
		cmds = append(cmds, cmd_service_action("start", *wt, svc, m.cfg))
	}
	return m, tea.Batch(cmds...)
}

// cmd_start_isolated_services regenerates the ecosystem config once, then
// starts all named processes via pm2 --only.
func cmd_start_isolated_services(wt worktree.Worktree, pm2_names []string, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		// Use the project's own ecosystem config (same one pnpm dev uses)
		eco_name := ""
		if cfg != nil {
			eco_name = cfg.PM2EcosystemConfig()
		}
		if eco_name == "" {
			eco_name = "ecosystem.dev.config.js"
		}
		ecosystem := filepath.Join(wt.Path, eco_name)
		env := pm2.HomeEnv(wt.PM2Home())

		var last_out string
		var last_err error
		for _, name := range pm2_names {
			debug_log("[start_svc] start via ecosystem: %s --only %s", ecosystem, name)
			last_out, last_err = run_host_cmd_env_dir(wt.Path, env, "pm2", "start", ecosystem, "--only", name, "--update-env")
			debug_log("[start_svc] start %s: out=%q err=%v", name, last_out, last_err)
		}
		return MsgActionOutput{Output: last_out, Err: last_err}
	}
}

func (m Model) execute_stop_service_action(action ui.PickerAction) (Model, tea.Cmd) {
	debug_log("[stop_svc] execute: label=%s key=%s", action.Label, action.Key)
	wt := m.selected_worktree()
	if wt == nil {
		return m, nil
	}

	var pm2_names []string
	for _, entry := range m.cfg.Dash.Services.List {
		if entry.Name == action.Label {
			pm2_names = pm2_process_names(entry, wt.Name)
			break
		}
	}

	if len(pm2_names) == 0 {
		return m, nil
	}

	m.activity = fmt.Sprintf("Stopping %s...", action.Label)

	var cmds []tea.Cmd
	for _, name := range pm2_names {
		svc := worktree.Service{Name: name, DisplayName: name}
		cmds = append(cmds, cmd_service_action("stop", *wt, svc, m.cfg))
	}
	return m, tea.Batch(cmds...)
}
