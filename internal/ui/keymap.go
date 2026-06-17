package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap is the central key binding set, driving both input handling and the help
// overlay so they never drift apart.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Enter    key.Binding
	Back     key.Binding
	Refresh  key.Binding
	Start    key.Binding
	Stop     key.Binding
	Reboot   key.Binding
	Snapshot key.Binding
	Palette  key.Binding
	Theme    key.Binding
	Help     key.Binding
	Quit     key.Binding
}

// DefaultKeyMap returns the vim-flavoured default bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "connections")),
		Right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "resources")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Start:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start")),
		Stop:     key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop")),
		Reboot:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "reboot")),
		Snapshot: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "snapshot")),
		Palette:  key.NewBinding(key.WithKeys("/", ":"), key.WithHelp("/", "palette")),
		Theme:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "cycle theme")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// FullHelp returns the bindings grouped for the help overlay.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right, k.Enter, k.Back},
		{k.Start, k.Stop, k.Reboot, k.Snapshot},
		{k.Refresh, k.Palette, k.Theme, k.Help, k.Quit},
	}
}
