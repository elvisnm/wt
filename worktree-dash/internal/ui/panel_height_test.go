package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/elvisnm/wt/internal/worktree"
)

// TestLayoutTotalEqualsHeight verifies that the sum of all panel heights
// equals the terminal height for every combination of visible panels.
func TestLayoutTotalEqualsHeight(t *testing.T) {
	for _, h := range []int{30, 40, 50, 60, 74, 80} {
		for _, notify := range []int{2, 3, 5, 10} {
			for _, details := range []bool{false, true} {
				for _, usage := range []bool{false, true} {
					for _, tasks := range []bool{false, true} {
						l := Layout{}.Resize(38, h, ResizeOpts{
							NotifyHeight:   notify,
							DetailsVisible: details,
							UsageVisible:   usage,
							TasksVisible:   tasks,
							TasksContent:   10,
						})
						total := l.NotifyHeight + l.TabsHeight + l.WorktreeHeight +
							l.ServicesHeight + l.DetailsHeight + l.UsageHeight + l.TasksHeight
						if total > h {
							t.Errorf("h=%d notify=%d det=%v usage=%v tasks=%v: total=%d exceeds h",
								h, notify, details, usage, tasks, total)
						}
					}
				}
			}
		}
	}
}

// TestPanelStyleExactHeight verifies that PanelStyle renders exactly `height` lines.
func TestPanelStyleExactHeight(t *testing.T) {
	for _, h := range []int{3, 4, 5, 8, 10, 13, 16, 26} {
		for _, w := range []int{30, 38, 44, 60} {
			rendered := PanelStyle(w, h, false).Render("test content here")
			lines := strings.Count(rendered, "\n") + 1
			if lines != h {
				t.Errorf("PanelStyle(%d, %d): rendered %d lines, want %d", w, h, lines, h)
			}
		}
	}
}

// TestNotifyIdleExactHeight verifies the idle notification box is exactly 2 lines.
func TestNotifyIdleExactHeight(t *testing.T) {
	for _, w := range []int{30, 38, 44, 60, 80} {
		rendered := RenderNotifyIdle(w)
		lines := strings.Count(rendered, "\n") + 1
		if lines != 2 {
			t.Errorf("RenderNotifyIdle(%d): rendered %d lines, want 2", w, lines)
		}
	}
}

// TestNotifyMessageExactHeight verifies the message notification matches expected height.
func TestNotifyMessageExactHeight(t *testing.T) {
	h := NotifyHeight(NotifyMessage, 0) // 3
	for _, w := range []int{30, 38, 44, 60} {
		rendered := RenderNotifyMessage("Test", "hello world", w, h)
		lines := strings.Count(rendered, "\n") + 1
		if lines != h {
			t.Errorf("RenderNotifyMessage(%d, %d): rendered %d lines, want %d", w, h, lines, h)
		}
	}
}

// TestNotifyConfirmExactHeight verifies the confirm dialog matches expected height.
func TestNotifyConfirmExactHeight(t *testing.T) {
	h := NotifyHeight(NotifyConfirm, 0) // 5
	for _, w := range []int{30, 38, 44, 60} {
		rendered := RenderNotifyConfirm("Are you sure?", w, h)
		lines := strings.Count(rendered, "\n") + 1
		if lines != h {
			t.Errorf("RenderNotifyConfirm(%d, %d): rendered %d lines, want %d", w, h, lines, h)
		}
	}
}

// TestNotifyInputExactHeight verifies the input dialog matches expected height.
func TestNotifyInputExactHeight(t *testing.T) {
	h := NotifyHeight(NotifyInput, 0) // 3
	for _, w := range []int{30, 38, 44, 60} {
		rendered := RenderNotifyInput("Name:", "test-value", w, h)
		lines := strings.Count(rendered, "\n") + 1
		if lines != h {
			t.Errorf("RenderNotifyInput(%d, %d): rendered %d lines, want %d", w, h, lines, h)
		}
	}
}

// TestNotifyPickerExactHeight verifies the picker matches expected height.
func TestNotifyPickerExactHeight(t *testing.T) {
	actions := []PickerAction{
		{Key: "b", Label: "Shell", Desc: "Container shell"},
		{Key: "c", Label: "Claude", Desc: "Claude Code"},
		{Key: "r", Label: "Restart", Desc: "Restart container"},
	}
	h := NotifyHeight(NotifyPicker, len(actions)) // 5
	for _, w := range []int{30, 38, 44, 60} {
		rendered := RenderNotifyPicker(actions, 0, w, "Actions")
		lines := strings.Count(rendered, "\n") + 1
		if lines != h {
			t.Errorf("RenderNotifyPicker(%d): rendered %d lines, want %d", w, lines, h)
		}
	}
}

// TestDetailsPanelExactHeight verifies the details panel never exceeds its allocated height.
// This is the specific regression test for the off-by-one bug where detail_line wrapping
// caused the panel to render 1 extra line.
func TestDetailsPanelExactHeight(t *testing.T) {
	wts := []worktree.Worktree{
		{
			Name: "bulk-ship", Alias: "bulk-ship", Branch: "feat/bulk-ship",
			Type: worktree.TypeLocal, Mode: "minimal",
			Domain: "bulk-ship.localhost",
		},
		{
			Name: "my-feature", Alias: "my-feat", Branch: "feat/my-very-long-feature-branch-name",
			Type: worktree.TypeDocker, Running: true, Health: "healthy",
			Container: "skulabs-my-feat", Domain: "my-feat.localhost",
			DBName: "db_my_feat", CPU: "2.3%", Mem: "512MiB", Uptime: "3h",
			Mode: "minimal", HostBuild: true,
		},
	}

	for _, wt := range wts {
		// Test across various widths — narrow widths trigger detail_line wrapping
		for _, w := range []int{30, 36, 38, 40, 44, 50, 60} {
			for _, h := range []int{8, 10, 13, 16, 20} {
				rendered := RenderDetailsPanel(&wt, w, h, 0, 0, false, nil)
				lines := strings.Count(rendered, "\n") + 1
				if lines != h {
					t.Errorf("DetailsPanel(wt=%s, w=%d, h=%d): rendered %d lines, want %d",
						wt.Alias, w, h, lines, h)
				}
			}
		}
	}
}

// TestJoinVerticalTotalHeight verifies that JoinVertical of all panels
// produces exactly the terminal height.
func TestJoinVerticalTotalHeight(t *testing.T) {
	wts := []worktree.Worktree{
		{Name: "test-1", Alias: "t1", Branch: "feat/test", Type: worktree.TypeLocal, Mode: "minimal", Domain: "t1.localhost"},
		{Name: "test-2", Alias: "t2", Branch: "fix/bug", Type: worktree.TypeDocker, Running: true, Health: "healthy"},
	}

	for _, w := range []int{36, 38, 40, 44} {
		for _, h := range []int{40, 50, 60, 74} {
			for _, details := range []bool{false, true} {
				l := Layout{}.Resize(w, h, ResizeOpts{
					NotifyHeight:   2, // idle
					DetailsVisible: details,
					UsageVisible:   true,
					TasksVisible:   true,
					TasksContent:   8,
				})

				notify := RenderNotifyIdle(w)
				tabs := RenderTabsPanel(nil, -1, w, l.TabsHeight, false)
				wt := RenderWorktreePanel(wts, 0, w, l.WorktreeHeight, true, nil)
				svc := RenderServicesPanel(nil, 0, w, l.ServicesHeight, false)

				panels := []string{notify, tabs, wt, svc}

				if details {
					det := RenderDetailsPanel(&wts[0], w, l.DetailsHeight, 0, 0, false, nil)
					panels = append(panels, det)
				}

				usage := RenderUsagePanel(nil, nil, w, l.UsageHeight, 0)
				panels = append(panels, usage)

				tasks := RenderTasksPanel(nil, 0, nil, 0, w, l.TasksHeight, false, nil)
				panels = append(panels, tasks)

				joined := lipgloss.JoinVertical(lipgloss.Left, panels...)
				total := strings.Count(joined, "\n") + 1

				if total > h {
					t.Errorf("w=%d h=%d det=%v: JoinVertical produced %d lines, exceeds %d",
						w, h, details, total, h)
				}
			}
		}
	}
}
