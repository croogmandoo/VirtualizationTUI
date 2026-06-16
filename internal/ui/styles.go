package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

// Palette holds the colours used across the UI.
var (
	colAccent = lipgloss.Color("63")  // indigo
	colMuted  = lipgloss.Color("244") // grey
	colOK     = lipgloss.Color("42")  // green
	colWarn   = lipgloss.Color("214") // amber
	colErr    = lipgloss.Color("196") // red
	colBg     = lipgloss.Color("236")
)

// Styles bundles the reusable Lip Gloss styles for the app shell.
type Styles struct {
	Title       lipgloss.Style
	Sidebar     lipgloss.Style
	SidebarItem lipgloss.Style
	SidebarSel  lipgloss.Style
	Pane        lipgloss.Style
	StatusBar   lipgloss.Style
	Help        lipgloss.Style
	Key         lipgloss.Style
	Muted       lipgloss.Style
	OverlayBox  lipgloss.Style
}

// NewStyles builds the default style set.
func NewStyles() Styles {
	return Styles{
		Title: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).
			Background(colAccent).Padding(0, 1),
		Sidebar: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(colMuted).Padding(0, 1),
		SidebarItem: lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		SidebarSel: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).
			Background(colAccent),
		Pane: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(colMuted).Padding(0, 1),
		StatusBar: lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(colBg).Padding(0, 1),
		Help:      lipgloss.NewStyle().Foreground(colMuted),
		Key:       lipgloss.NewStyle().Bold(true).Foreground(colAccent),
		Muted:     lipgloss.NewStyle().Foreground(colMuted),
		OverlayBox: lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).
			BorderForeground(colAccent).Padding(1, 2),
	}
}

// StatusStyle returns a colour-coded style + glyph for a normalized status.
func StatusStyle(s provider.Status) (lipgloss.Style, string) {
	switch s {
	case provider.StatusRunning, provider.StatusOK:
		return lipgloss.NewStyle().Foreground(colOK), "●"
	case provider.StatusStopped:
		return lipgloss.NewStyle().Foreground(colMuted), "○"
	case provider.StatusPaused, provider.StatusDegraded:
		return lipgloss.NewStyle().Foreground(colWarn), "◐"
	case provider.StatusError:
		return lipgloss.NewStyle().Foreground(colErr), "✗"
	default:
		return lipgloss.NewStyle().Foreground(colMuted), "?"
	}
}
