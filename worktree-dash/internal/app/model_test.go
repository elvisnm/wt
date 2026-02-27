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
	m.layout = m.layout.Resize(120, 40)
	return m
}

func TestEnterOpensPicker(t *testing.T) {
	m := test_model()

	// Simulate Enter key
	enter_msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ := m.Update(enter_msg)
	updated := result.(Model)

	if !updated.picker_open {
		t.Errorf("Expected picker_open=true after Enter, got false")
	}

	if len(updated.picker_actions) == 0 {
		t.Errorf("Expected picker_actions to be populated, got empty")
	}

	t.Logf("picker_open=%v, picker_actions=%d", updated.picker_open, len(updated.picker_actions))
	for _, a := range updated.picker_actions {
		t.Logf("  %s: %s", a.Key, a.Label)
	}
}

func TestEnterOnLocalOpensPicker(t *testing.T) {
	m := test_model()
	m.cursor = 1 // local worktree

	enter_msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ := m.Update(enter_msg)
	updated := result.(Model)

	if !updated.picker_open {
		t.Errorf("Expected picker_open=true for local worktree, got false")
	}
	t.Logf("picker_open=%v, picker_actions=%d", updated.picker_open, len(updated.picker_actions))
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
