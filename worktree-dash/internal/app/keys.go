package app

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	Enter      key.Binding
	Tab        key.Binding
	ShiftTab   key.Binding
	Escape     key.Binding
	Quit       key.Binding
	CtrlC      key.Binding
	Help       key.Binding
	PanelLeft  key.Binding
	PanelRight key.Binding
	TabPrev    key.Binding
	TabNext    key.Binding
}

var Keys = KeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup", "shift+up"),
		key.WithHelp("PgUp/S-↑", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown", "shift+down"),
		key.WithHelp("PgDn/S-↓", "page down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("↵", "select"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next panel"),
	),
	ShiftTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "prev panel"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	CtrlC: key.NewBinding(
		key.WithKeys("ctrl+c"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	PanelLeft: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←", "prev panel"),
	),
	PanelRight: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("→", "next panel"),
	),
	TabPrev: key.NewBinding(
		key.WithKeys("<"),
		key.WithHelp("<", "prev panel"),
	),
	TabNext: key.NewBinding(
		key.WithKeys(">"),
		key.WithHelp(">", "next panel"),
	),
}
