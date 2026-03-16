package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/elvisnm/wt/internal/beads"
	"github.com/elvisnm/wt/internal/claude"
	"github.com/elvisnm/wt/internal/config"
	"github.com/elvisnm/wt/internal/docker"
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
	PanelTasks
)

const PanelCount = 5

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
	notify_open bool
	picker_open     bool
	picker_cursor   int
	picker_actions  []ui.PickerAction
	picker_context  string // pickerWorktree, pickerDB, pickerMaintenance, pickerRemove

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

	// SSO: deferred action after SSO login completes
	pending_sso_action string             // "create" or "start"
	pending_sso_start  *worktree.Worktree // worktree to start (when action="start")

	// Skip-worktree: tracks when the skip-worktree script is running
	skip_worktree_running bool

	// Deferred esbuild: alias to open esbuild watch for after next discovery
	pending_esbuild_alias string

	// Deferred dev server: alias to start dev server for after next discovery (local worktrees)
	pending_dev_alias string

	// Details panel toggle
	details_visible bool

	// Claude usage panel
	usage_visible bool
	usage_data    *claude.Usage
	usage_err     error
	usage_token   string

	// Beads tasks panel
	tasks_visible       bool
	tasks_list          []beads.Task
	tasks_cursor        int
	tasks_detail        *beads.Task
	tasks_detail_scroll int
	tasks_err           error

	// HeiHei easter egg
	heihei_audio   []byte
	heihei_tmpfile string
	heihei_playing bool

	repo_root     string
	worktrees_dir string
	cfg           *config.Config

	layout ui.Layout
}

// recalc_layout recomputes layout dimensions with current visibility state.
func (m *Model) recalc_layout() {
	m.layout = m.layout.Resize(m.width, m.height, ui.ResizeOpts{
		DetailsVisible: m.details_visible,
		UsageVisible:   m.usage_visible,
		TasksVisible:   m.tasks_visible,
		TasksContent:   ui.TasksContentHeight(m.tasks_list, m.tasks_detail),
	})
}

// cleanup_temp_files removes any temporary files created during the session.
func (m *Model) cleanup_temp_files() {
	if m.heihei_tmpfile != "" {
		os.Remove(m.heihei_tmpfile)
		m.heihei_tmpfile = ""
	}
}

// quit_action is the shared confirm handler for both quit paths.
func quit_action(mdl *Model) (Model, tea.Cmd) {
	mdl.close_preview()
	if mdl.term_mgr.HasLiveSessions() {
		mdl.term_mgr.CloseAll()
	}
	mdl.cleanup_temp_files()
	return *mdl, tea.Quit
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
		focus:           PanelWorktrees,
		cursor:          0,
		repo_root:       repo_root,
		worktrees_dir:   wt_dir,
		cfg:             cfg,
		term_mgr:        mgr,
		pane_layout:     pl,
		// details_visible defaults to false (zero value)
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
type MsgUsageUpdated struct {
	Token string
	Usage *claude.Usage
	Err   error
}
type MsgTasksLoaded struct {
	Tasks []beads.Task
	Err   error
}
type MsgTaskDetailLoaded struct {
	Task *beads.Task
	Err  error
}
type MsgTaskActionDone struct{ Err error }
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
			debug_log("[discovery]   %s type=%v alias=%s offset=%d domain=%s path=%s", wt.Name, wt.Type, wt.Alias, wt.Offset, wt.Domain, wt.Path)
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

// cmd_fetch_usage fetches usage data. If token is empty, fetches it from keychain first.
// On 401, attempts to refresh the token and retry once.
func cmd_fetch_usage(token string) tea.Cmd {
	return func() tea.Msg {
		// Fetch full token set upfront so refresh token is available on 401
		var refresh_token string
		if token == "" {
			debug_log("[usage] no cached token, reading from keychain")
			tokens, err := claude.FetchTokens()
			if err != nil {
				debug_log("[usage] keychain read failed: %v", err)
				return MsgUsageUpdated{Err: err}
			}
			token = tokens.AccessToken
			refresh_token = tokens.RefreshToken
			debug_log("[usage] got token from keychain (expires_at=%d)", tokens.ExpiresAt)
		}

		debug_log("[usage] fetching usage data")
		usage, err := claude.FetchUsage(token)
		if !errors.Is(err, claude.ErrUnauthorized) {
			if err != nil {
				debug_log("[usage] fetch error: %v", err)
			} else {
				debug_log("[usage] fetch ok: 5h=%.1f%% 7d=%.1f%%", usage.FiveHour.Utilization*100, usage.SevenDay.Utilization*100)
			}
			return MsgUsageUpdated{Token: token, Usage: usage, Err: err}
		}

		// 401 — try refreshing the token
		debug_log("[usage] got 401, attempting token refresh")
		if refresh_token == "" {
			// Token was cached (not fetched this call) — read refresh token from keychain
			tokens, fetch_err := claude.FetchTokens()
			if fetch_err != nil {
				debug_log("[usage] keychain read for refresh failed: %v", fetch_err)
				return MsgUsageUpdated{Err: fmt.Errorf("token expired, keychain read failed: %w", fetch_err)}
			}
			refresh_token = tokens.RefreshToken
		}
		if refresh_token == "" {
			debug_log("[usage] no refresh token available")
			return MsgUsageUpdated{Err: fmt.Errorf("token expired, no refresh token available")}
		}

		refreshed, refresh_err := claude.RefreshAccessToken(refresh_token)
		if refresh_err != nil {
			debug_log("[usage] token refresh failed: %v", refresh_err)
			return MsgUsageUpdated{Err: fmt.Errorf("token refresh failed: %w", refresh_err)}
		}
		debug_log("[usage] token refreshed successfully")

		if write_err := claude.WriteTokens(refreshed); write_err != nil {
			debug_log("[usage] keychain write failed after token refresh: %v", write_err)
		}

		// Retry with the new access token
		usage, err = claude.FetchUsage(refreshed.AccessToken)
		if err != nil {
			debug_log("[usage] retry fetch error: %v", err)
		} else {
			debug_log("[usage] retry fetch ok: 5h=%.1f%% 7d=%.1f%%", usage.FiveHour.Utilization*100, usage.SevenDay.Utilization*100)
		}
		return MsgUsageUpdated{Token: refreshed.AccessToken, Usage: usage, Err: err}
	}
}

func cmd_fetch_tasks() tea.Cmd {
	return func() tea.Msg {
		tasks, err := beads.FetchTasks()
		return MsgTasksLoaded{Tasks: tasks, Err: err}
	}
}

func cmd_fetch_task_detail(id string) tea.Cmd {
	return func() tea.Msg {
		task, err := beads.FetchDetail(id)
		return MsgTaskDetailLoaded{Task: task, Err: err}
	}
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
