package app

import (
	"strings"
	"testing"

	"github.com/elvisnm/wt/internal/terminal"
	"github.com/elvisnm/wt/internal/worktree"

	tea "github.com/charmbracelet/bubbletea"
)

func test_model_full() Model {
	m := Model{
		focus:    PanelWorktrees,
		cursor:   0,
		ready:      true,
		discovered: true,
		width:    60,
		height:   40,
		term_mgr: terminal.NewManager(),
		worktrees: []worktree.Worktree{
			{Name: "test-wt", Alias: "test", Branch: "feat/test", Type: worktree.TypeDocker, Running: true, Health: "healthy", ContainerExists: true},
		},
	}
	m.layout = m.layout.Resize(60, 40)
	return m
}

func TestViewWithPickerOpen(t *testing.T) {
	m := test_model_full()

	// Open picker via Enter
	enter_msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ := m.Update(enter_msg)
	m = result.(Model)

	if !m.picker_open {
		t.Fatal("Picker should be open")
	}

	view := m.View()
	lines := strings.Split(view, "\n")
	t.Logf("View has %d lines", len(lines))

	// The picker should contain action labels (WorktreeActions for running docker)
	has_shell := strings.Contains(view, "Shell")
	has_restart := strings.Contains(view, "Restart")
	has_stop := strings.Contains(view, "Stop")

	if !has_shell {
		t.Error("View missing 'Shell' action in picker")
	}
	if !has_restart {
		t.Error("View missing 'Restart' action in picker")
	}
	if !has_stop {
		t.Error("View missing 'Stop' action in picker")
	}

	// Check "Actions" title appears
	has_title := strings.Contains(view, "Actions")
	if !has_title {
		t.Error("View missing 'Actions' title in picker")
	}
}

func TestViewLeftColumn(t *testing.T) {
	m := test_model_full()

	view := m.View()

	// Should contain the worktree alias "test"
	if !strings.Contains(view, "test") {
		t.Error("View missing worktree alias 'test'")
	}

	// Should contain panel titles
	if !strings.Contains(view, "Active Tabs") {
		for i, line := range strings.Split(view, "\n") {
			t.Logf("line %2d: %q", i, line)
		}
		t.Error("View missing 'Active Tabs' panel title")
	}
	if !strings.Contains(view, "Worktrees") {
		t.Error("View missing 'Worktrees' panel title")
	}

	// Should contain panel border elements
	has_border := strings.Contains(view, "╭") || strings.Contains(view, "│")
	if !has_border {
		t.Error("View missing border characters")
	}
}
