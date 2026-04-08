package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPickerRender(t *testing.T) {
	actions := []PickerAction{
		{Key: "b", Label: "Bash", Desc: "Open shell"},
		{Key: "l", Label: "Logs", Desc: "View logs"},
		{Key: "r", Label: "Restart", Desc: "Restart container"},
	}

	width := 60
	height := 5 // len(actions) + 2

	result := RenderPicker(actions, 0, width, height, "Actions — test")
	lines := strings.Split(result, "\n")

	t.Logf("Picker rendered %d lines (expected %d)", len(lines), height)
	t.Logf("Picker visual height: %d", lipgloss.Height(result))
	for i, line := range lines {
		t.Logf("Line %d [w=%d]: %q", i, lipgloss.Width(line), line)
	}

	rendered_h := lipgloss.Height(result)
	if rendered_h != height {
		t.Errorf("Expected height %d, got %d", height, rendered_h)
	}
}

