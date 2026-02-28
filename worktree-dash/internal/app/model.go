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
		wts := worktree.Discover(m.worktrees_dir, m.worktrees, cfg)
		wts = worktree.SortWorktrees(wts)
		wts = docker.FetchContainerStatus(wts, cfg)
		wts = docker.FetchContainerStats(wts, cfg)
		wts = mark_local_running(wts, cfg, term_mgr)
		wts = worktree.SortWorktrees(wts)
		return MsgDiscovered{Worktrees: wts}
	}
}

func cmd_fetch_status(wt_dir string, wts []worktree.Worktree, cfg *config.Config, term_mgr *terminal.Manager) tea.Cmd {
	return func() tea.Msg {
		fresh := worktree.Discover(wt_dir, wts, cfg)
		fresh = docker.FetchContainerStatus(fresh, cfg)
		fresh = mark_local_running(fresh, cfg, term_mgr)
		fresh = worktree.SortWorktrees(fresh)
		return MsgStatusUpdated{Worktrees: fresh}
	}
}

func cmd_fetch_stats(wts []worktree.Worktree, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
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
		var svcs []worktree.Service
		switch manager {
		case "static":
			svcs = build_static_services(cfg, &wt)
		default:
			svcs = docker.FetchServices(wt.Container, wt.Name)
		}
		return MsgServicesUpdated{Services: svcs}
	}
}

func cmd_fetch_local_services(wt worktree.Worktree, cfg *config.Config) tea.Cmd {
	manager := "pm2"
	if cfg != nil {
		manager = cfg.ServiceManager()
	}
	return func() tea.Msg {
		var svcs []worktree.Service
		switch manager {
		case "static":
			svcs = build_static_services(cfg, &wt)
		default:
			svcs = pm2.FetchServices(wt.Path)
		}
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
				addr := fmt.Sprintf("localhost:%d", port)
				conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
				if err == nil {
					conn.Close()
					svc.Status = "online"
				} else {
					svc.Status = "stopped"
					all_online = false
				}
			} else {
				svc.Status = "stopped"
				all_online = false
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

	switch check {
	case "devTab":
		for i := range wts {
			if wts[i].Type == worktree.TypeLocal && term_mgr != nil {
				dev_label := fmt.Sprintf("Dev — %s", wts[i].Alias)
				create_label := fmt.Sprintf("Create — %s", wts[i].Alias)
				wts[i].Running = term_mgr.IsLabelAlive(dev_label) ||
					term_mgr.IsLabelAlive(create_label) ||
					term_mgr.IsLabelAlive("Create")
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
