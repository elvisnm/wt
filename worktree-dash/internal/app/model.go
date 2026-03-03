package app

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/docker"
	"github.com/elvisnm/wt/internal/pm2"
	"github.com/elvisnm/wt/internal/terminal"
	"github.com/elvisnm/wt/internal/ui"
	"github.com/elvisnm/wt/internal/worktree"

	tea "github.com/charmbracelet/bubbletea"
)

// Panel focus targets
type Panel int

const (
	PanelTerminal Panel = iota
	PanelWorktrees
	PanelServices
	PanelDetails
)

const PanelCount = 4

// Model is the root Bubbletea model
type Model struct {
	width  int
	height int

	focus      Panel
	prev_focus Panel
	worktrees  []worktree.Worktree
	cursor     int
	ready      bool // window size received
	discovered bool // initial worktree discovery complete

	// Overlay state
	help_open       bool
	confirm_open    bool
	confirm_prompt  string
	confirm_action  func(*Model) (Model, tea.Cmd)
	picker_open     bool
	picker_cursor   int
	picker_actions  []ui.PickerAction
	picker_context  string // "worktree", "db", "maintenance"

	// Details panel scroll
	details_scroll int

	// Services for selected worktree
	services       []worktree.Service
	service_cursor int

	// Terminal
	term_mgr        *terminal.Manager
	terminal_output string // static output (for non-PTY actions)
	pane_layout     *terminal.PaneLayout

	// Preview: standalone log session shown in right panel without a tab
	preview_session  *terminal.Session
	preview_svc_name string

	// Status bar input mode
	input_active   bool
	input_prompt   string
	input_value    string
	input_callback func(string) tea.Cmd
	result_text    string

	// Activity status shown below terminal
	activity    string
	spin_frame  int

	// Worktrees with in-progress actions (e.g. removing, starting)
	// Prevents periodic discovery from overwriting their state.
	actions_pending map[string]bool

	// AWS keys: tracks when the aws-keys script is running
	aws_keys_running bool

	// Skip-worktree: tracks when the skip-worktree script is running
	skip_worktree_running bool

	// HeiHei easter egg
	heihei_audio   []byte
	heihei_tmpfile string
	heihei_playing bool

	repo_root     string
	worktrees_dir string
	cfg           *config.Config

	layout ui.Layout
}

func NewModel() Model {
	repo_root, err := worktree.FindRepoRoot()
	if err != nil {
		repo_root = ""
	}

	// Load config; nil means legacy mode (ignore error)
	var cfg *config.Config
	if repo_root != "" {
		cfg, _ = config.Load(repo_root)
	}

	wt_dir := ""
	if repo_root != "" {
		wt_dir = worktree.ResolveWorktreesDir(repo_root, cfg)
	}

	return Model{
		focus:         PanelWorktrees,
		cursor:        0,
		repo_root:     repo_root,
		worktrees_dir: wt_dir,
		cfg:           cfg,
		term_mgr:      terminal.NewManager(),
	}
}

// NewModelWithLayout creates a Model for inner mode with an existing tmux server and pane layout.
func NewModelWithLayout(server *terminal.TmuxServer, pl *terminal.PaneLayout) Model {
	repo_root, err := worktree.FindRepoRoot()
	if err != nil {
		repo_root = ""
	}

	var cfg *config.Config
	if repo_root != "" {
		cfg, _ = config.Load(repo_root)
	}

	wt_dir := ""
	if repo_root != "" {
		wt_dir = worktree.ResolveWorktreesDir(repo_root, cfg)
	}

	mgr := terminal.NewManagerWithServer(server)
	mgr.SetPaneLayout(pl)

	return Model{
		focus:         PanelWorktrees,
		cursor:        0,
		repo_root:     repo_root,
		worktrees_dir: wt_dir,
		cfg:           cfg,
		term_mgr:      mgr,
		pane_layout:   pl,
	}
}

func (m *Model) SetHeiHeiAudio(data []byte) {
	m.heihei_audio = data
}

func (m Model) Init() tea.Cmd {
	debug_init()
	cfg_name := ""
	cfg_strategy := ""
	if m.cfg != nil {
		cfg_name = m.cfg.Name
		cfg_strategy = m.cfg.Docker.ComposeStrategy
	}
	debug_log("[init] repo_root=%s", m.repo_root)
	debug_log("[init] worktrees_dir=%s", m.worktrees_dir)
	debug_log("[init] config name=%q strategy=%q", cfg_name, cfg_strategy)
	return tea.Batch(
		m.cmd_discover(),
	)
}

// Messages
type MsgDiscovered struct{ Worktrees []worktree.Worktree }
type MsgStatusUpdated struct{ Worktrees []worktree.Worktree }
type MsgStatsUpdated struct{ Worktrees []worktree.Worktree }
type MsgServicesUpdated struct{ Services []worktree.Service }
type MsgTick struct{ Kind string }
type MsgSessionOpened struct{ Err error }
type MsgResultClear struct{}
type MsgOpenBuildAfterStart struct{ WtName string }

// Commands

func (m Model) cmd_discover() tea.Cmd {
	cfg := m.cfg
	term_mgr := m.term_mgr
	return func() tea.Msg {
		debug_log("[discovery] starting discovery in %s", m.worktrees_dir)
		wts := worktree.Discover(m.worktrees_dir, m.worktrees, cfg)
		debug_log("[discovery] found %d worktrees", len(wts))
		for _, wt := range wts {
			debug_log("[discovery]   %s type=%v alias=%s path=%s", wt.Name, wt.Type, wt.Alias, wt.Path)
		}
		wts = worktree.SortWorktrees(wts)
		debug_log("[discovery] fetching container status")
		wts = docker.FetchContainerStatus(wts, cfg)
		debug_log("[discovery] fetching container stats")
		wts = docker.FetchContainerStats(wts, cfg)
		debug_log("[discovery] checking local running state")
		wts = mark_local_running(wts, cfg, term_mgr)
		wts = worktree.SortWorktrees(wts)
		debug_log("[discovery] complete: %d worktrees", len(wts))
		return MsgDiscovered{Worktrees: wts}
	}
}

func cmd_fetch_status(wt_dir string, wts []worktree.Worktree, cfg *config.Config, term_mgr *terminal.Manager) tea.Cmd {
	return func() tea.Msg {
		debug_log("[tick] fetch_status: %d worktrees", len(wts))
		fresh := worktree.Discover(wt_dir, wts, cfg)
		fresh = docker.FetchContainerStatus(fresh, cfg)
		fresh = mark_local_running(fresh, cfg, term_mgr)
		fresh = worktree.SortWorktrees(fresh)
		return MsgStatusUpdated{Worktrees: fresh}
	}
}

func cmd_fetch_stats(wts []worktree.Worktree, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		debug_log("[tick] fetch_stats: %d worktrees", len(wts))
		updated := docker.FetchContainerStats(wts, cfg)
		return MsgStatsUpdated{Worktrees: updated}
	}
}

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
	manager := "pm2"
	if cfg != nil {
		manager = cfg.ServiceManager()
	}
	return func() tea.Msg {
		debug_log("[services] fetch_local_services: %s manager=%s path=%s", wt.Alias, manager, wt.Path)
		var svcs []worktree.Service
		switch manager {
		case "static":
			svcs = build_static_services(cfg, &wt)
			// Merge PM2 runtime data: mark matching services as online
			// using config's processes field for name mapping.
			pm2_svcs := pm2.FetchServices(wt.Path)
			svcs = merge_pm2_into_static(svcs, pm2_svcs, cfg)
		default:
			svcs = pm2.FetchServices(wt.Path)
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

	all_online := true
	for _, entry := range cfg.Dash.Services.List {
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
				port = entry.Port
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
			if len(entry.Processes) > 0 {
				for _, proc := range entry.Processes {
					pm2_to_static[proc] = static_idx
				}
			} else {
				pm2_to_static[entry.Name] = static_idx
			}
		}
	}

	// Update matching static entries with PM2 status
	for pm2_name, pm2_svc := range pm2_map {
		idx, ok := pm2_to_static[pm2_name]
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
func mark_local_running(wts []worktree.Worktree, cfg *config.Config, term_mgr *terminal.Manager) []worktree.Worktree {
	check := "pm2"
	if cfg != nil {
		check = cfg.Dash.Services.RunningCheck
	}
	debug_log("[discovery] mark_local_running: method=%s", check)

	switch check {
	case "devTab":
		for i := range wts {
			if wts[i].Type == worktree.TypeLocal && term_mgr != nil {
				dev_label := fmt.Sprintf("Dev — %s", wts[i].Alias)
				create_label := fmt.Sprintf("Create — %s", wts[i].Alias)
				wts[i].Running = term_mgr.IsLabelAlive(dev_label) ||
					term_mgr.IsLabelAlive(create_label) ||
					term_mgr.IsLabelAlive(LabelCreate)
				debug_log("[discovery]   %s running=%v (devTab)", wts[i].Alias, wts[i].Running)
			}
		}
	default: // "pm2"
		local_paths := make(map[string]string)
		for _, wt := range wts {
			if wt.Type == worktree.TypeLocal {
				local_paths[wt.Path] = wt.Name
			}
		}
		if len(local_paths) == 0 {
			return wts
		}

		running := pm2.FetchRunningWorktrees(local_paths)
		if len(running) == 0 {
			for i := range wts {
				if wts[i].Type == worktree.TypeLocal {
					wts[i].Running = false
				}
			}
			return wts
		}
		for i := range wts {
			if wts[i].Type == worktree.TypeLocal {
				wts[i].Running = running[wts[i].Name]
			}
		}
	}
	return wts
}

// right_pane_dimensions returns the width and height of the right tmux pane.
// Used when creating new sessions that need to know their initial size.
func (m Model) right_pane_dimensions() (int, int) {
	if m.pane_layout != nil {
		return m.pane_layout.RightPaneDimensions()
	}
	// Fallback for non-pane mode
	return 80, 24
}
