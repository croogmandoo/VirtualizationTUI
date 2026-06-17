package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/croogmandoo/virtualizationtui/internal/ui"
)

// paletteCmd is a single entry in the command palette: a title to match and show,
// a short description, and the effect to run when chosen. run receives the model
// by pointer so a command can mutate UI state (open the confirm modal, switch
// theme, …) and/or return a tea.Cmd for async work.
type paletteCmd struct {
	title string
	desc  string
	run   func(m *Model) tea.Cmd
}

// openPalette builds the context-sensitive command set and focuses the input.
func (m *Model) openPalette() {
	ti := textinput.New()
	ti.Placeholder = "type a command…"
	ti.Prompt = "› "
	ti.Focus()
	m.paletteInput = ti
	m.paletteAll = buildPaletteCommands(m)
	m.paletteCursor = 0
	m.palette = true
	m.refilterPalette()
}

func (m *Model) closePalette() {
	m.palette = false
	m.paletteInput.Blur()
	m.paletteCursor = 0
}

// refilterPalette recomputes the shown subset for the current query, fuzzy-matching
// against command titles and keeping the cursor in range.
func (m *Model) refilterPalette() {
	q := m.paletteInput.Value()
	m.paletteShown = m.paletteShown[:0]
	for i, c := range m.paletteAll {
		if q == "" || fuzzyMatch(q, c.title) {
			m.paletteShown = append(m.paletteShown, i)
		}
	}
	if m.paletteCursor >= len(m.paletteShown) {
		m.paletteCursor = len(m.paletteShown) - 1
	}
	if m.paletteCursor < 0 {
		m.paletteCursor = 0
	}
}

// handlePaletteKey drives the palette while it is open.
func (m Model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closePalette()
		return m, nil
	case "enter":
		// Capture the chosen command before closing (which clears selection state),
		// then run it so its mutations land on the returned model.
		var run func(*Model) tea.Cmd
		if len(m.paletteShown) > 0 {
			run = m.paletteAll[m.paletteShown[m.paletteCursor]].run
		}
		m.closePalette()
		var cmd tea.Cmd
		if run != nil {
			cmd = run(&m)
		}
		return m, cmd
	case "up", "ctrl+p":
		if m.paletteCursor > 0 {
			m.paletteCursor--
		}
		return m, nil
	case "down", "ctrl+n":
		if m.paletteCursor < len(m.paletteShown)-1 {
			m.paletteCursor++
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.paletteInput, cmd = m.paletteInput.Update(msg)
	m.refilterPalette()
	return m, cmd
}

// renderPalette draws the palette overlay box.
func (m Model) renderPalette() string {
	var b strings.Builder
	b.WriteString(m.styles.Key.Render("Command palette") + "\n")
	b.WriteString(m.paletteInput.View() + "\n\n")

	if len(m.paletteShown) == 0 {
		b.WriteString(m.styles.Muted.Render("no matching commands"))
		return m.styles.OverlayBox.Width(56).Render(b.String())
	}

	const maxVisible = 10
	start := 0
	if m.paletteCursor >= maxVisible {
		start = m.paletteCursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.paletteShown) {
		end = len(m.paletteShown)
	}
	for i := start; i < end; i++ {
		c := m.paletteAll[m.paletteShown[i]]
		title := fmt.Sprintf("%-22s", c.title)
		if i == m.paletteCursor {
			b.WriteString(m.styles.SidebarSel.Render("▸ "+title) + " " + m.styles.Muted.Render(c.desc) + "\n")
		} else {
			b.WriteString("  " + title + " " + m.styles.Muted.Render(c.desc) + "\n")
		}
	}
	b.WriteString("\n" + m.styles.Muted.Render(fmt.Sprintf("%d match · enter run · esc cancel", len(m.paletteShown))))
	return m.styles.OverlayBox.Width(56).Render(b.String())
}

// fuzzyMatch reports whether pattern is a (case-insensitive) subsequence of s,
// the classic command-palette match: "cyt" matches "cycle theme".
func fuzzyMatch(pattern, s string) bool {
	pattern = strings.ToLower(pattern)
	s = strings.ToLower(s)
	pi := 0
	for i := 0; i < len(s) && pi < len(pattern); i++ {
		if s[i] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}

// buildPaletteCommands assembles the palette for the current context: global
// commands, theme switches, connection navigation, and — when a resource is
// selected — its detail view and (outside read-only mode) power actions.
func buildPaletteCommands(m *Model) []paletteCmd {
	var cmds []paletteCmd

	cmds = append(cmds, paletteCmd{"refresh", "reload inventory", func(m *Model) tea.Cmd {
		m.status = "refreshing…"
		return m.loadCmd(m.currentConnName())
	}})
	cmds = append(cmds, paletteCmd{"cycle theme", "next colour theme", func(m *Model) tea.Cmd {
		m.cycleTheme()
		return nil
	}})
	for _, tn := range ui.ThemeNames() {
		tn := tn
		cmds = append(cmds, paletteCmd{"theme: " + tn, "switch theme", func(m *Model) tea.Cmd {
			m.setTheme(tn)
			return nil
		}})
	}

	for i, c := range m.conns {
		i, cn := i, c.Cfg.Name
		cmds = append(cmds, paletteCmd{"go: " + cn, "switch connection", func(m *Model) tea.Cmd {
			return m.selectConn(i)
		}})
	}

	if _, ok := m.selectedResource(); ok {
		cmds = append(cmds, paletteCmd{"details", "open detail view", func(m *Model) tea.Cmd {
			if r, ok := m.selectedResource(); ok {
				m.mode = viewDetail
				m.renderDetail(r)
			}
			return nil
		}})
		if !m.session.ReadOnly() {
			for _, a := range []struct{ verb, label string }{
				{"start", "Start"}, {"stop", "Stop"}, {"reboot", "Reboot"}, {"snapshot", "Snapshot"},
			} {
				a := a
				cmds = append(cmds, paletteCmd{a.verb + " selected", a.label + " the selected resource", func(m *Model) tea.Cmd {
					if r, ok := m.selectedResource(); ok {
						m.queueAction(r, a.verb, a.label+" "+r.Name+"?")
					}
					return nil
				}})
			}
		}
	}

	cmds = append(cmds, paletteCmd{"help", "show keybindings", func(m *Model) tea.Cmd {
		m.showHelp = true
		return nil
	}})
	cmds = append(cmds, paletteCmd{"quit", "exit the app", func(m *Model) tea.Cmd {
		return tea.Quit
	}})

	return cmds
}
