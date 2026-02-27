package ui

import (
	"fmt"
	"strings"

	"github.com/elvisnm/wt/internal/worktree"

	"github.com/charmbracelet/lipgloss"
)

func RenderServicesPanel(services []worktree.Service, cursor int, width, height int, focused bool) string {
	title := TitleStyle(focused).Render(" s - Services ")
	style := PanelStyle(width, height, focused)

	if len(services) == 0 {
		placeholder := lipgloss.NewStyle().
			Foreground(DimTextColor).
			Render("No services")
		styled := style.Render(placeholder)
		return inject_title(styled, title)
	}

	inner_w := width - 4
	inner_h := height - 2

	var lines []string
	for i, svc := range services {
		line := format_service_line(svc, inner_w, i == cursor, focused)
		lines = append(lines, line)
	}

	total := len(lines)
	start, end := visible_window(total, cursor, inner_h)
	lines = lines[start:end]

	content := strings.Join(lines, "\n")
	styled := style.Render(content)
	styled = OverlayScrollbar(styled, total, inner_h, start, focused)
	return inject_title(styled, title)
}

func format_service_line(svc worktree.Service, width int, selected bool, panel_focused bool) string {
	var status_icon string
	switch svc.Status {
	case "online":
		status_icon = "●"
	case "stopped":
		status_icon = "○"
	default:
		status_icon = "?"
	}

	var right string
	if svc.Status == "online" {
		mem_mb := svc.Memory / (1024 * 1024)
		right = fmt.Sprintf("%.0f%% %dMB", svc.CPU, mem_mb)
	} else {
		right = svc.Status
	}

	name := svc.DisplayName
	label := fmt.Sprintf(" %s %s", status_icon, name)
	right_len := len(right)
	pad := width - lipgloss.Width(label) - right_len - 1
	if pad < 1 {
		pad = 1
	}

	line := label + strings.Repeat(" ", pad) + right + " "

	if selected && panel_focused {
		return lipgloss.NewStyle().
			Bold(true).
			Background(SelectedBgColor).
			Foreground(lipgloss.Color("255")).
			Width(width).
			Render(line)
	}

	if selected {
		return lipgloss.NewStyle().
			Bold(true).
			Width(width).
			Render(line)
	}

	// Color the status icon for non-selected lines
	var colored_icon string
	switch svc.Status {
	case "online":
		colored_icon = lipgloss.NewStyle().Foreground(RunningColor).Render("●")
	case "stopped":
		colored_icon = lipgloss.NewStyle().Foreground(StoppedColor).Render("○")
	default:
		colored_icon = lipgloss.NewStyle().Foreground(DimTextColor).Render("?")
	}
	label = fmt.Sprintf(" %s %s", colored_icon, name)
	pad = width - lipgloss.Width(label) - right_len - 1
	if pad < 1 {
		pad = 1
	}
	line = label + strings.Repeat(" ", pad) + right + " "
	return lipgloss.NewStyle().Width(width).Render(line)
}
