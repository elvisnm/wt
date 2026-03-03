package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/elvisnm/wt/internal/claude"
)

// RenderUsagePanel renders the Claude API usage panel.
// This panel is display-only and does not participate in focus cycling.
func RenderUsagePanel(usage *claude.Usage, err error, width, height int) string {
	if height < 3 {
		return ""
	}

	style := PanelStyle(width, height, false)
	inner_w := width - 4 // borders + padding

	title := lipgloss.NewStyle().Foreground(DimTextColor).Render(" U - Usage ")

	var content string
	switch {
	case err != nil:
		err_style := lipgloss.NewStyle().Foreground(StoppedColor)
		content = err_style.Render(truncate(err.Error(), inner_w))
	case usage == nil:
		content = lipgloss.NewStyle().Foreground(DimTextColor).Render("Loading...")
	default:
		line_5h := render_usage_line("5h", usage.FiveHour.Utilization, usage.FiveHour.ResetsAt, inner_w)
		line_7d := render_usage_line("7d", usage.SevenDay.Utilization, usage.SevenDay.ResetsAt, inner_w)
		content = line_5h + "\n" + line_7d
	}

	rendered := style.Render(content)
	return inject_title(rendered, title)
}

func render_usage_line(label string, pct float64, resets_at time.Time, width int) string {
	countdown := format_countdown(resets_at)
	// "5h [████░░░░░░]  6% resets 4h32m"
	prefix := label + " "
	suffix := fmt.Sprintf(" %3.0f%% resets %s", pct, countdown)

	bar_w := width - len(prefix) - len(suffix)
	if bar_w < 4 {
		bar_w = 4
	}

	bar := render_bar(pct, bar_w)
	return prefix + bar + suffix
}

func render_bar(pct float64, width int) string {
	if width < 2 {
		return ""
	}
	// Reserve 2 chars for brackets
	inner := width - 2
	if inner < 1 {
		inner = 1
	}

	filled := int(pct / 100.0 * float64(inner))
	if filled > inner {
		filled = inner
	}
	if filled < 0 {
		filled = 0
	}

	var color lipgloss.Color
	switch {
	case pct >= 80:
		color = lipgloss.Color("196") // bright red (distinct from StoppedColor)
	case pct >= 50:
		color = StartingColor
	default:
		color = RunningColor
	}

	filled_style := lipgloss.NewStyle().Foreground(color)
	empty_style := lipgloss.NewStyle().Foreground(BorderColor)

	return "[" +
		filled_style.Render(strings.Repeat("\u2588", filled)) +
		empty_style.Render(strings.Repeat("\u2591", inner-filled)) +
		"]"
}

func format_countdown(t time.Time) string {
	d := time.Until(t)
	if d <= 0 {
		return "now"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
