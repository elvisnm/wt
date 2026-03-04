package ui

import (
	"fmt"
	"strings"

	"github.com/elvisnm/wt/internal/beads"

	"github.com/charmbracelet/lipgloss"
)

// TasksContentHeight returns the number of inner lines needed to display tasks.
// Used by Layout.Resize to compute the panel height dynamically.
func TasksContentHeight(tasks []beads.Task, detail *beads.Task) int {
	if detail != nil {
		return task_detail_lines(detail)
	}
	n := len(tasks)
	if n == 0 {
		return 1 // "No open tasks"
	}
	return n
}

// RenderTasksPanel renders the tasks panel as a left-column section.
func RenderTasksPanel(tasks []beads.Task, cursor int, detail *beads.Task, detail_scroll int, width, height int, focused bool, err error) string {
	title := TitleStyle(focused).Render(" T - Tasks ")
	style := PanelStyle(width, height, focused)

	if err != nil {
		content := lipgloss.NewStyle().Foreground(StoppedColor).Render(fmt.Sprintf("Error: %v", err))
		styled := style.Render(content)
		return inject_title(styled, title)
	}

	if detail != nil {
		return render_task_detail_panel(detail, detail_scroll, width, height, focused, title, style)
	}
	return render_task_list_panel(tasks, cursor, width, height, focused, title, style)
}

func render_task_list_panel(tasks []beads.Task, cursor int, width, height int, focused bool, title string, style lipgloss.Style) string {
	inner_w := width - 4
	inner_h := height - 2

	var lines []string

	if len(tasks) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(DimTextColor).Render("No open tasks"))
	} else {
		start, end := visible_window(len(tasks), cursor, inner_h)
		for i := start; i < end; i++ {
			t := tasks[i]
			pri := priority_icon(t.Priority)
			typ := type_tag(t.IssueType)
			id := lipgloss.NewStyle().Foreground(DimTextColor).Render(t.ID)

			title_max := inner_w - lipgloss.Width(pri) - lipgloss.Width(typ) - lipgloss.Width(t.ID) - 5
			task_title := t.Title
			if title_max > 0 && len(task_title) > title_max {
				task_title = task_title[:title_max-1] + "~"
			}

			if i == cursor {
				line := fmt.Sprintf(" %s %s %s %s", pri, typ, id, task_title)
				line = lipgloss.NewStyle().
					Background(SelectedBgColor).
					Foreground(lipgloss.Color("255")).
					Bold(true).
					Width(inner_w).
					Render(line)
				lines = append(lines, line)
			} else {
				line := fmt.Sprintf(" %s %s %s %s", pri, typ, id, task_title)
				lines = append(lines, line)
			}
		}
	}

	content := strings.Join(lines, "\n")
	styled := style.Render(content)
	styled = inject_title(styled, title)

	if len(tasks) > inner_h {
		start, _ := visible_window(len(tasks), cursor, inner_h)
		styled = OverlayScrollbar(styled, len(tasks), inner_h, start, focused)
	}

	return styled
}

func render_task_detail_panel(task *beads.Task, scroll int, width, height int, focused bool, title string, style lipgloss.Style) string {
	inner_w := width - 4
	inner_h := height - 2
	wrap_w := inner_w - 2

	all_lines := build_task_detail_lines(task, wrap_w)

	total := len(all_lines)
	if scroll > total-inner_h {
		scroll = total - inner_h
	}
	if scroll < 0 {
		scroll = 0
	}

	end := scroll + inner_h
	if end > total {
		end = total
	}
	visible := all_lines[scroll:end]

	content := strings.Join(visible, "\n")

	detail_title := TitleStyle(focused).Render(fmt.Sprintf(" %s ", task.ID))
	styled := style.Render(content)
	styled = inject_title(styled, detail_title)

	if total > inner_h {
		styled = OverlayScrollbar(styled, total, inner_h, scroll, focused)
	}

	return styled
}

func build_task_detail_lines(task *beads.Task, wrap_w int) []string {
	var lines []string

	// Title
	title_style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	lines = append(lines, " "+title_style.Render(word_wrap(task.Title, wrap_w)))

	// Metadata
	meta := fmt.Sprintf(" %s %s  P%d  %s",
		priority_icon(task.Priority),
		type_tag(task.IssueType),
		task.Priority,
		lipgloss.NewStyle().Foreground(DimTextColor).Render(task.ID),
	)
	lines = append(lines, meta)

	// Status + Owner
	status_color := RunningColor
	if task.Status == "open" {
		status_color = HintColor
	}
	status := lipgloss.NewStyle().Foreground(status_color).Render(task.Status)
	owner_line := " Status: " + status
	if task.Owner != "" {
		owner_line += "  Owner: " + lipgloss.NewStyle().Foreground(DimTextColor).Render(task.Owner)
	}
	lines = append(lines, owner_line)
	lines = append(lines, "")

	// Description
	if task.Description != "" {
		label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
		lines = append(lines, " "+label.Render("Description"))
		for _, para := range strings.Split(task.Description, "\n") {
			if para == "" {
				lines = append(lines, "")
				continue
			}
			wrapped := word_wrap(para, wrap_w)
			for _, wl := range strings.Split(wrapped, "\n") {
				lines = append(lines, " "+lipgloss.NewStyle().Foreground(DimTextColor).Render(wl))
			}
		}
		lines = append(lines, "")
	}

	// Labels
	if len(task.Labels) > 0 {
		label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
		lines = append(lines, " "+label.Render("Labels"))
		for _, l := range task.Labels {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(DimTextColor).Render(l))
		}
		lines = append(lines, "")
	}

	// Dependencies
	if len(task.Dependencies) > 0 {
		label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
		lines = append(lines, " "+label.Render("Dependencies"))
		for _, dep := range task.Dependencies {
			dep_line := fmt.Sprintf("  %s %s (%s)",
				lipgloss.NewStyle().Foreground(DimTextColor).Render(dep.ID),
				dep.Title,
				dep.DependencyType,
			)
			lines = append(lines, dep_line)
		}
		lines = append(lines, "")
	}

	// Dep/Dependent counts (when no expanded dependencies)
	if len(task.Dependencies) == 0 && (task.DependencyCount > 0 || task.DependentCount > 0) {
		dim := lipgloss.NewStyle().Foreground(DimTextColor)
		lines = append(lines, " "+dim.Render(fmt.Sprintf("Blocks: %d  Blocked by: %d", task.DependentCount, task.DependencyCount)))
		lines = append(lines, "")
	}

	return lines
}

func task_detail_lines(task *beads.Task) int {
	return len(build_task_detail_lines(task, 50))
}

func priority_icon(p int) string {
	switch {
	case p <= 0:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("!!")
	case p == 1:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Render("! ")
	case p == 2:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("- ")
	case p == 3:
		return lipgloss.NewStyle().Foreground(DimTextColor).Render(". ")
	default:
		return lipgloss.NewStyle().Foreground(DimTextColor).Render("  ")
	}
}

func type_tag(t string) string {
	switch t {
	case "bug":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("bug")
	case "feature":
		return lipgloss.NewStyle().Foreground(RunningColor).Render("feat")
	default:
		return lipgloss.NewStyle().Foreground(DimTextColor).Render("task")
	}
}

func word_wrap(text string, max_w int) string {
	if max_w <= 0 {
		return text
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > max_w {
			lines = append(lines, line)
			line = w
		} else {
			line += " " + w
		}
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
}
