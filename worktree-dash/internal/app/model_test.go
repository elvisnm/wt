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

func TestEnterOpensPicker(t *testing.T) {
	m := test_model()

	enter_msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ := m.Update(enter_msg)
	updated := result.(Model)

	// Enter on a running docker worktree should open the picker
	if !updated.picker_open {
		t.Errorf("Expected picker_open=true after Enter on running worktree")
	}
}

func TestEnterOnLocalOpensPicker(t *testing.T) {
	m := test_model()
	m.cursor = 1 // local worktree

	enter_msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ := m.Update(enter_msg)
	updated := result.(Model)

	if !updated.picker_open {
		t.Errorf("Expected picker_open=true after Enter on local worktree")
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
