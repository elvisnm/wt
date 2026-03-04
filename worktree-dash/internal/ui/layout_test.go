package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestLayoutResize(t *testing.T) {
	tests := []struct {
		name   string
		w, h   int
		checks func(t *testing.T, l Layout)
	}{
		{
			name: "standard size",
			w:    40, h: 50,
			checks: func(t *testing.T, l Layout) {
				t.Helper()
				if l.Width != 40 {
					t.Errorf("Width: got %d, want 40", l.Width)
				}
				if l.Height != 50 {
					t.Errorf("Height: got %d, want 50", l.Height)
				}
				total := l.TabsHeight + l.WorktreeHeight + l.ServicesHeight + l.DetailsHeight
				if total != 50 {
					t.Errorf("panel heights sum to %d, want 50", total)
				}
			},
		},
		{
			name: "tiny terminal",
			w:    20, h: 8,
			checks: func(t *testing.T, l Layout) {
				t.Helper()
				if l.TabsHeight < 4 {
					t.Errorf("TabsHeight %d < minimum 4", l.TabsHeight)
				}
			},
		},
		{
			name: "very small height enforces minimum",
			w:    40, h: 3,
			checks: func(t *testing.T, l Layout) {
				t.Helper()
				// panels_h should be at least 8
				total := l.TabsHeight + l.WorktreeHeight + l.ServicesHeight + l.DetailsHeight
				if total < 8 {
					t.Errorf("panel heights sum to %d, want >= 8", total)
				}
			},
		},
		{
			name: "status height is zero",
			w:    80, h: 40,
			checks: func(t *testing.T, l Layout) {
				t.Helper()
				if l.StatusHeight != 0 {
					t.Errorf("StatusHeight: got %d, want 0", l.StatusHeight)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Layout{}.Resize(tt.w, tt.h, ResizeOpts{DetailsVisible: true})
			tt.checks(t, l)
		})
	}
}

func TestVisibleWindow(t *testing.T) {
	tests := []struct {
		name                   string
		total, cursor, max     int
		want_start, want_end   int
	}{
		{"fits entirely", 5, 2, 10, 0, 5},
		{"fits exactly", 5, 2, 5, 0, 5},
		{"scroll needed at start", 10, 0, 5, 0, 5},
		{"scroll to middle", 10, 6, 5, 2, 7},
		{"scroll to end", 10, 9, 5, 5, 10},
		{"cursor at boundary", 10, 4, 5, 0, 5},
		{"cursor past boundary", 10, 5, 5, 1, 6},
		{"single visible line", 5, 3, 1, 3, 4},
		{"zero total", 0, 0, 5, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := visible_window(tt.total, tt.cursor, tt.max)
			if start != tt.want_start || end != tt.want_end {
				t.Errorf("visible_window(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tt.total, tt.cursor, tt.max, start, end, tt.want_start, tt.want_end)
			}
		})
	}
}

func TestOverlayScrollbar_NoScrollNeeded(t *testing.T) {
	rendered := "╭──╮\n│ab│\n│cd│\n╰──╯"
	result := OverlayScrollbar(rendered, 2, 2, 0, false)
	if result != rendered {
		t.Errorf("Expected no change when content fits, got:\n%s", result)
	}
}

func TestOverlayScrollbar_AddsScrollbar(t *testing.T) {
	rendered := "╭──╮\n│ab│\n│cd│\n╰──╯"
	result := OverlayScrollbar(rendered, 10, 2, 0, false)
	if result == rendered {
		t.Error("Expected scrollbar to be added when content overflows")
	}
	// Should contain the thumb character
	if !strings.Contains(result, "█") {
		t.Error("Expected thumb character █ in scrollbar")
	}
}

func TestRenderTabsPanel_Empty(t *testing.T) {
	result := RenderTabsPanel(nil, 0, 40, 10, false)
	if !strings.Contains(result, "No sessions") {
		t.Error("Expected placeholder text for empty tabs")
	}
}

func TestRenderTabsPanel_WithTabs(t *testing.T) {
	tabs := []TabInfo{
		{Index: 1, Label: "Shell — test", Active: true, Alive: true},
		{Index: 2, Label: "Logs — test", Active: false, Alive: true},
	}
	result := RenderTabsPanel(tabs, 0, 40, 10, true)

	if !strings.Contains(result, "Shell") {
		t.Error("Expected tab label 'Shell' in output")
	}
	if !strings.Contains(result, "Logs") {
		t.Error("Expected tab label 'Logs' in output")
	}
}

func TestRenderTabsPanel_DeadTab(t *testing.T) {
	tabs := []TabInfo{
		{Index: 1, Label: "Create", Active: true, Alive: false},
	}
	result := RenderTabsPanel(tabs, 0, 40, 10, false)
	if !strings.Contains(result, "dead") {
		t.Error("Expected 'dead' indicator for non-alive tab")
	}
}

func TestRenderTabsPanel_Height(t *testing.T) {
	tabs := []TabInfo{
		{Index: 1, Label: "Shell", Active: true, Alive: true},
	}
	height := 8
	result := RenderTabsPanel(tabs, 0, 40, height, false)
	rendered_h := lipgloss.Height(result)
	if rendered_h != height {
		t.Errorf("Expected height %d, got %d", height, rendered_h)
	}
}

func TestFilterDatabaseActions_NilConfig(t *testing.T) {
	actions := FilterDatabaseActions(nil)
	if len(actions) != len(DatabaseActions) {
		t.Errorf("Expected %d actions with nil config, got %d", len(DatabaseActions), len(actions))
	}
}

func TestFilterMaintenanceActions_NilConfig(t *testing.T) {
	actions := FilterMaintenanceActions(nil)
	if len(actions) != len(MaintenanceActions) {
		t.Errorf("Expected %d actions with nil config, got %d", len(MaintenanceActions), len(actions))
	}
}

func TestRenderPicker_ActionCount(t *testing.T) {
	actions := []PickerAction{
		{Key: "b", Label: "Bash", Desc: "Open shell"},
		{Key: "l", Label: "Logs", Desc: "View logs"},
	}
	result := RenderPicker(actions, 0, 60, 6, "Test")
	// Both actions should appear
	if !strings.Contains(result, "Bash") {
		t.Error("Expected 'Bash' in picker output")
	}
	if !strings.Contains(result, "Logs") {
		t.Error("Expected 'Logs' in picker output")
	}
}

func TestRenderPicker_CorrectHeight(t *testing.T) {
	actions := []PickerAction{
		{Key: "a", Label: "Alpha", Desc: "First"},
		{Key: "b", Label: "Beta", Desc: "Second"},
		{Key: "c", Label: "Gamma", Desc: "Third"},
	}
	height := 7 // 3 actions + 2 borders + 2 padding
	result := RenderPicker(actions, 1, 60, height, "Pick")
	rendered_h := lipgloss.Height(result)
	if rendered_h != height {
		t.Errorf("Expected height %d, got %d", height, rendered_h)
	}
}

func TestTabStatusIndicatorPlain(t *testing.T) {
	tests := []struct {
		name string
		tab  TabInfo
		want string
	}{
		{"alive active", TabInfo{Alive: true, Active: true}, "●"},
		{"alive inactive", TabInfo{Alive: true, Active: false}, "●"},
		{"dead", TabInfo{Alive: false}, "○"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tab_status_indicator_plain(tt.tab)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLayoutResizeWithUsage(t *testing.T) {
	l := Layout{}.Resize(40, 50, ResizeOpts{DetailsVisible: true, UsageVisible: true})

	if l.UsageHeight != 5 {
		t.Errorf("UsageHeight: got %d, want 5", l.UsageHeight)
	}

	total := l.TabsHeight + l.WorktreeHeight + l.ServicesHeight + l.DetailsHeight + l.UsageHeight
	if total != 50 {
		t.Errorf("panel heights sum to %d, want 50", total)
	}
}

func TestLayoutResizeWithoutUsage(t *testing.T) {
	l := Layout{}.Resize(40, 50, ResizeOpts{DetailsVisible: true})

	if l.UsageHeight != 0 {
		t.Errorf("UsageHeight: got %d, want 0", l.UsageHeight)
	}

	total := l.TabsHeight + l.WorktreeHeight + l.ServicesHeight + l.DetailsHeight
	if total != 50 {
		t.Errorf("panel heights sum to %d, want 50", total)
	}
}

func TestLayoutResizeUsageCapped(t *testing.T) {
	// With very small height, usage should be capped at panels_h/4
	l := Layout{}.Resize(40, 10, ResizeOpts{DetailsVisible: true, UsageVisible: true})

	if l.UsageHeight > 10/4 {
		t.Errorf("UsageHeight %d should be capped at %d for small terminals", l.UsageHeight, 10/4)
	}
}

func TestLayoutResizeDetailsHidden(t *testing.T) {
	l := Layout{}.Resize(40, 50, ResizeOpts{})

	if l.DetailsHeight != 0 {
		t.Errorf("DetailsHeight: got %d, want 0", l.DetailsHeight)
	}

	total := l.TabsHeight + l.WorktreeHeight + l.ServicesHeight
	if total != 50 {
		t.Errorf("panel heights sum to %d, want 50", total)
	}
}
