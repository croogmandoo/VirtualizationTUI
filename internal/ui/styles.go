package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

// Styles bundles the reusable Lip Gloss styles for the app shell. It carries the
// Theme it was built from so status colouring stays consistent with the palette.
type Styles struct {
	Theme Theme

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

// NewStyles builds the style set for a theme.
func NewStyles(t Theme) Styles {
	return Styles{
		Theme: t,
		Title: lipgloss.NewStyle().Bold(true).Foreground(t.Fg).
			Background(t.Accent).Padding(0, 1),
		Sidebar: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Muted).Padding(0, 1),
		SidebarItem: lipgloss.NewStyle().Foreground(t.Dim),
		SidebarSel: lipgloss.NewStyle().Bold(true).Foreground(t.Fg).
			Background(t.Accent),
		Pane: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Muted).Padding(0, 1),
		StatusBar: lipgloss.NewStyle().Foreground(t.Dim).Background(t.Bg).Padding(0, 1),
		Help:      lipgloss.NewStyle().Foreground(t.Muted),
		Key:       lipgloss.NewStyle().Bold(true).Foreground(t.Accent),
		Muted:     lipgloss.NewStyle().Foreground(t.Muted),
		OverlayBox: lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).
			BorderForeground(t.Accent).Padding(1, 2),
	}
}

// Status returns a colour-coded style + glyph for a normalized status, using the
// style set's theme palette.
func (s Styles) Status(st provider.Status) (lipgloss.Style, string) {
	t := s.Theme
	switch st {
	case provider.StatusRunning, provider.StatusOK:
		return lipgloss.NewStyle().Foreground(t.OK), "●"
	case provider.StatusStopped:
		return lipgloss.NewStyle().Foreground(t.Muted), "○"
	case provider.StatusPaused, provider.StatusDegraded:
		return lipgloss.NewStyle().Foreground(t.Warn), "◐"
	case provider.StatusError:
		return lipgloss.NewStyle().Foreground(t.Err), "✗"
	default:
		return lipgloss.NewStyle().Foreground(t.Muted), "?"
	}
}
