package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// NotifyState describes what the notification area is showing.
type NotifyState int

const (
	NotifyIdle    NotifyState = iota // compact "No notifications" box
	NotifyMessage                    // title + message (3 rows)
	NotifyPicker                     // interactive picker
	NotifyConfirm                    // yes/no confirm
	NotifyInput                      // text input
)

// NotifyHeight returns the number of rows the notification area needs.
func NotifyHeight(state NotifyState, picker_count int) int {
	switch state {
	case NotifyIdle:
		return 2 // top border + bottom border
	case NotifyMessage:
		return 3
	case NotifyPicker:
		h := picker_count + 2 // items + top/bottom border
		if h < 3 {
			h = 3
		}
		return h
	case NotifyConfirm:
		return 5 // border + prompt + blank + hints + border
	case NotifyInput:
		return 3
	default:
		return 2
	}
}

// RenderNotifyIdle renders the compact "Notifications" box (2 rows).
// Built manually because lipgloss PanelStyle minimum inner height is 1 (3 rows).
func RenderNotifyIdle(width int) string {
	inner := width - 2
	if inner < 1 {
		inner = 1
	}

	border := lipgloss.NewStyle().Foreground(BorderColor)
	title := lipgloss.NewStyle().Foreground(DimTextColor).Render(" Notifications ")
	title_w := lipgloss.Width(title)

	fill := inner - 1 - title_w
	if fill < 1 {
		fill = 1
	}

	top := border.Render("╭─") + title + border.Render(strings.Repeat("─", fill)+"╮")
	bottom := border.Render("╰" + strings.Repeat("─", inner) + "╯")

	return top + "\n" + bottom
}

// RenderNotifyMessage renders a notification with title and message.
func RenderNotifyMessage(title, message string, width, height int) string {
	style := PanelStyle(width, height, false).BorderForeground(HintColor)

	title_rendered := lipgloss.NewStyle().
		Foreground(HintColor).
		Render(fmt.Sprintf(" %s ", title))

	msg_rendered := lipgloss.NewStyle().
		Foreground(DimTextColor).
		Width(width - 4).
		Render(message)

	rendered := style.Render(msg_rendered)
	return inject_title(rendered, title_rendered)
}

// RenderNotifyPicker renders the picker inline at the top of the view.
func RenderNotifyPicker(actions []PickerAction, cursor int, width int, title string) string {
	height := len(actions) + 2
	return RenderPicker(actions, cursor, width, height, title)
}

// RenderNotifyConfirm renders a confirmation dialog inline at the top.
func RenderNotifyConfirm(prompt string, width, height int) string {
	style := PanelStyle(width, height, false).BorderForeground(FocusBorderColor)

	title_rendered := lipgloss.NewStyle().
		Bold(true).
		Foreground(FocusBorderColor).
		Render(" Confirm ")

	prompt_style := lipgloss.NewStyle().
		Foreground(DimTextColor)

	dim_style := lipgloss.NewStyle().Foreground(DimTextColor)
	key_style := lipgloss.NewStyle().Foreground(HintColor)
	hint_line := key_style.Render("Enter") + dim_style.Render(": confirm  ") + key_style.Render("Esc") + dim_style.Render(": cancel")

	content := strings.Join([]string{
		prompt_style.Render(prompt),
		"",
		hint_line,
	}, "\n")

	rendered := style.Render(content)
	return inject_title(rendered, title_rendered)
}

// RenderNotifyInput renders a text input dialog inline at the top.
func RenderNotifyInput(prompt, value string, width, height int) string {
	style := PanelStyle(width, height, false).BorderForeground(FocusBorderColor)

	title_rendered := lipgloss.NewStyle().
		Bold(true).
		Foreground(FocusBorderColor).
		Render(" Input ")

	cursor_style := lipgloss.NewStyle().Background(lipgloss.Color("240"))
	prompt_style := lipgloss.NewStyle().Foreground(DimTextColor)

	line := prompt_style.Render(prompt+" ") + value + cursor_style.Render(" ")

	rendered := style.Render(line)
	return inject_title(rendered, title_rendered)
}
