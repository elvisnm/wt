package app

import (
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

	// AWS keys: tracks when the aws-keys script is running
	aws_keys_running bool

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
	return func() tea.Msg {
		wts := worktree.Discover(m.worktrees_dir, m.worktrees, m.cfg)
		wts = worktree.SortWorktrees(wts)
		wts = docker.FetchContainerStatus(wts, m.cfg)
		wts = docker.FetchContainerStats(wts, m.cfg)
		wts = mark_local_running(wts)
		wts = worktree.SortWorktrees(wts)
		return MsgDiscovered{Worktrees: wts}
	}
}

func cmd_fetch_status(wt_dir string, wts []worktree.Worktree, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		fresh := worktree.Discover(wt_dir, wts, cfg)
		fresh = docker.FetchContainerStatus(fresh, cfg)
		fresh = mark_local_running(fresh)
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

func cmd_fetch_services(container string, wt_name string) tea.Cmd {
	return func() tea.Msg {
		svcs := docker.FetchServices(container, wt_name)
		return MsgServicesUpdated{Services: svcs}
	}
}

func cmd_fetch_local_services(wt_path string) tea.Cmd {
	return func() tea.Msg {
		svcs := pm2.FetchServices(wt_path)
		return MsgServicesUpdated{Services: svcs}
	}
}

// mark_local_running checks PM2 for running local worktrees and sets Running=true
func mark_local_running(wts []worktree.Worktree) []worktree.Worktree {
	// Build map of path -> name for local worktrees
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
