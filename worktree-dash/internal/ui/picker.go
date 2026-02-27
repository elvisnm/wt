package ui

import (
	"fmt"
	"strings"

	"github.com/elvisnm/wt/internal/config"

	"github.com/charmbracelet/lipgloss"
)

// PickerAction represents a selectable action in the picker overlay
type PickerAction struct {
	Key   string
	Label string
	Desc  string
}

var WorktreeActions = []PickerAction{
	{Key: "b", Label: "Shell", Desc: "Open shell in container"},
	{Key: "z", Label: "Zsh", Desc: "Open host shell in worktree"},
	{Key: "c", Label: "Claude", Desc: "Open Claude Code"},
	{Key: "r", Label: "Restart", Desc: "Restart container"},
	{Key: "d", Label: "Stop", Desc: "Stop container"},
	{Key: "D", Label: "Remove", Desc: "Remove worktree"},
}

var StoppedActions = []PickerAction{
	{Key: "u", Label: "Start", Desc: "Start container"},
	{Key: "z", Label: "Zsh", Desc: "Open host shell in worktree"},
	{Key: "c", Label: "Claude", Desc: "Open Claude Code"},
	{Key: "D", Label: "Remove", Desc: "Remove worktree"},
}

var DatabaseActions = []PickerAction{
	{Key: "s", Label: "Seed", Desc: "Copy shared db into worktree db"},
	{Key: "d", Label: "Drop", Desc: "Drop the worktree database"},
	{Key: "r", Label: "Reset", Desc: "Drop + re-seed (fresh copy)"},
	{Key: "f", Label: "Fix Images", Desc: "Fix fakes3 image URLs"},
}

var MaintenanceActions = []PickerAction{
	{Key: "p", Label: "Prune", Desc: "Remove orphaned Docker volumes"},
	{Key: "s", Label: "Autostop", Desc: "Stop idle containers"},
	{Key: "r", Label: "Rebuild", Desc: "Rebuild base Docker image"},
}

var LocalActions = []PickerAction{
	{Key: "u", Label: "Start", Desc: "Start dev server (pnpm dev)"},
	{Key: "b", Label: "Shell", Desc: "Open shell in worktree"},
	{Key: "c", Label: "Claude", Desc: "Open Claude Code"},
	{Key: "n", Label: "Create", Desc: "Create Docker container"},
	{Key: "i", Label: "Info", Desc: "Show worktree info"},
	{Key: "D", Label: "Remove", Desc: "Remove worktree"},
}

var HostBuildRunningActions = []PickerAction{
	{Key: "e", Label: "Build", Desc: "Start esbuild watch on host"},
	{Key: "b", Label: "Shell", Desc: "Open shell in container"},
	{Key: "z", Label: "Zsh", Desc: "Open host shell in worktree"},
	{Key: "c", Label: "Claude", Desc: "Open Claude Code"},
	{Key: "r", Label: "Restart", Desc: "Restart container"},
	{Key: "d", Label: "Stop", Desc: "Stop container"},
	{Key: "D", Label: "Remove", Desc: "Remove worktree"},
}

var HostBuildStoppedActions = []PickerAction{
	{Key: "u", Label: "Start + Build", Desc: "Start container + esbuild watch"},
	{Key: "z", Label: "Zsh", Desc: "Open host shell in worktree"},
	{Key: "c", Label: "Claude", Desc: "Open Claude Code"},
	{Key: "D", Label: "Remove", Desc: "Remove worktree"},
}

var RemoveActions = []PickerAction{
	{Key: "n", Label: "Normal", Desc: "Remove worktree (fails if dirty)"},
	{Key: "f", Label: "Force", Desc: "Force remove (even if dirty)"},
}

var LocalRunningActions = []PickerAction{
	{Key: "b", Label: "Shell", Desc: "Open shell in worktree"},
	{Key: "c", Label: "Claude", Desc: "Open Claude Code"},
	{Key: "l", Label: "Logs", Desc: "View PM2 logs"},
	{Key: "r", Label: "Restart", Desc: "Restart all services"},
	{Key: "s", Label: "Services", Desc: "Manage PM2 services"},
	{Key: "i", Label: "Info", Desc: "Show worktree info"},
	{Key: "d", Label: "Stop", Desc: "Stop dev server"},
	{Key: "D", Label: "Remove", Desc: "Remove worktree"},
}

// FilterDatabaseActions returns DatabaseActions filtered by config feature flags.
// When cfg is nil, returns the full list.
func FilterDatabaseActions(cfg *config.Config) []PickerAction {
	if cfg == nil {
		return DatabaseActions
	}
	var actions []PickerAction
	for _, a := range DatabaseActions {
		switch a.Key {
		case "f": // Fix Images
			if cfg.FeatureEnabled("imagesFix") {
				actions = append(actions, a)
			}
		default:
			actions = append(actions, a)
		}
	}
	return actions
}

// FilterMaintenanceActions returns MaintenanceActions filtered by config feature flags.
// When cfg is nil, returns the full list.
func FilterMaintenanceActions(cfg *config.Config) []PickerAction {
	if cfg == nil {
		return MaintenanceActions
	}
	var actions []PickerAction
	for _, a := range MaintenanceActions {
		switch a.Key {
		case "p": // Prune
			if cfg.FeatureEnabled("prune") {
				actions = append(actions, a)
			}
		case "s": // Autostop
			if cfg.FeatureEnabled("autostop") {
				actions = append(actions, a)
			}
		case "r": // Rebuild base
			if cfg.FeatureEnabled("rebuildBase") {
				actions = append(actions, a)
			}
		default:
			actions = append(actions, a)
		}
	}
	return actions
}

func RenderPicker(actions []PickerAction, cursor int, width, height int, title string) string {
	picker_style := lipgloss.NewStyle().
		Width(width - 2).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(FocusBorderColor)

	title_rendered := lipgloss.NewStyle().
		Bold(true).
		Foreground(FocusBorderColor).
		Render(fmt.Sprintf(" %s ", title))

	var lines []string
	inner_w := width - 4
	for i, a := range actions {
		if i == cursor {
			key_plain := lipgloss.NewStyle().Width(3).Render(a.Key)
			label_plain := lipgloss.NewStyle().Width(14).Render(a.Label)
			line := fmt.Sprintf(" %s %s %s", key_plain, label_plain, a.Desc)
			line = lipgloss.NewStyle().
				Background(SelectedBgColor).
				Foreground(lipgloss.Color("255")).
				Bold(true).
				Width(inner_w).
				Render(line)
			lines = append(lines, line)
			continue
		}

		key_rendered := lipgloss.NewStyle().
			Bold(true).
			Foreground(FocusBorderColor).
			Width(3).
			Render(a.Key)

		label_rendered := lipgloss.NewStyle().
			Width(14).
			Render(a.Label)

		desc_rendered := lipgloss.NewStyle().
			Foreground(DimTextColor).
			Render(a.Desc)

		line := fmt.Sprintf(" %s %s %s", key_rendered, label_rendered, desc_rendered)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	styled := picker_style.Render(content)
	styled = inject_title(styled, title_rendered)

	return styled
}
