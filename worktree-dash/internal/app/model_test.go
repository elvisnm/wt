package app

import (
	"testing"

	"github.com/elvisnm/wt/internal/ui"
	"github.com/elvisnm/wt/internal/worktree"

	tea "github.com/charmbracelet/bubbletea"
)

func test_model() Model {
	m := Model{
		focus:  PanelWorktrees,
		cursor: 0,
		ready:      true,
		discovered: true,
		worktrees: []worktree.Worktree{
			{Name: "test-wt", Alias: "test", Branch: "feat/test", Type: worktree.TypeDocker, Running: true, Health: "healthy"},
			{Name: "local-wt", Alias: "local", Branch: "fix/local", Type: worktree.TypeLocal},
		},
	}
	m.layout = m.layout.Resize(120, 40, ui.ResizeOpts{DetailsVisible: true})
	return m
}

func TestEnterWithoutPaneLayout(t *testing.T) {
	m := test_model()
	// No pane_layout — open_panel_picker returns early (no-op)

	enter_msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ := m.Update(enter_msg)
	updated := result.(Model)

	// Without pane_layout, picker should NOT open
	if updated.picker_open {
		t.Errorf("Expected picker_open=false without pane_layout")
	}
	if updated.panel_picker_open {
		t.Errorf("Expected panel_picker_open=false without pane_layout")
	}
}

func TestEnterOnLocalWithoutPaneLayout(t *testing.T) {
	m := test_model()
	m.cursor = 1 // local worktree

	enter_msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ := m.Update(enter_msg)
	updated := result.(Model)

	if updated.picker_open || updated.panel_picker_open {
		t.Errorf("Expected no picker without pane_layout")
	}
}

func TestPickerEscCloses(t *testing.T) {
	m := test_model()
	m.picker_open = true
	m.picker_actions = []ui.PickerAction{{Key: "b", Label: "Bash", Desc: "test"}}

	esc_msg := tea.KeyMsg{Type: tea.KeyEscape}
	result, _ := m.Update(esc_msg)
	updated := result.(Model)

	if updated.picker_open {
		t.Errorf("Expected picker_open=false after Esc, got true")
	}
}
