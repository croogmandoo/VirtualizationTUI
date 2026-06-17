package ui

import (
	"sort"

	"github.com/charmbracelet/lipgloss"
)

// Theme is a named colour palette. Every colour the UI paints flows from a Theme,
// so switching themes (config.UI.Theme or the in-app cycle key) restyles the whole
// shell without touching call sites.
type Theme struct {
	Name   string         // identifier used in config and the theme cycle
	Accent lipgloss.Color // selections, titles, key hints
	Fg     lipgloss.Color // primary/bright text
	Dim    lipgloss.Color // secondary text
	Muted  lipgloss.Color // labels, borders, de-emphasised glyphs
	OK     lipgloss.Color // running / healthy
	Warn   lipgloss.Color // paused / degraded
	Err    lipgloss.Color // error / failed
	Bg     lipgloss.Color // status-bar background
}

// themes is the built-in registry, keyed by Theme.Name.
var themes = map[string]Theme{
	"default": {
		Name: "default", Accent: "63", Fg: "231", Dim: "252", Muted: "244",
		OK: "42", Warn: "214", Err: "196", Bg: "236",
	},
	"nord": {
		Name: "nord", Accent: "110", Fg: "231", Dim: "252", Muted: "245",
		OK: "108", Warn: "222", Err: "167", Bg: "236",
	},
	"dracula": {
		Name: "dracula", Accent: "141", Fg: "231", Dim: "253", Muted: "244",
		OK: "84", Warn: "215", Err: "203", Bg: "235",
	},
	"gruvbox": {
		Name: "gruvbox", Accent: "172", Fg: "223", Dim: "250", Muted: "245",
		OK: "142", Warn: "214", Err: "167", Bg: "236",
	},
	"solarized-light": {
		Name: "solarized-light", Accent: "33", Fg: "235", Dim: "240", Muted: "244",
		OK: "64", Warn: "136", Err: "160", Bg: "254",
	},
}

// DefaultTheme is the fallback when a configured name is unknown.
const DefaultTheme = "default"

// ThemeByName returns the named theme, falling back to the default for unknown
// names so a typo in config never leaves the UI unstyled.
func ThemeByName(name string) Theme {
	if t, ok := themes[name]; ok {
		return t
	}
	return themes[DefaultTheme]
}

// ThemeNames returns the registered theme names in stable, sorted order.
func ThemeNames() []string {
	names := make([]string, 0, len(themes))
	for n := range themes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// NextTheme returns the name of the theme after current in the sorted cycle,
// wrapping around. Unknown names start the cycle from the beginning.
func NextTheme(current string) string {
	names := ThemeNames()
	for i, n := range names {
		if n == current {
			return names[(i+1)%len(names)]
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return DefaultTheme
}
