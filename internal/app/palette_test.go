package app

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func slashKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")} }
func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }
func escKey() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyEsc} }
func downKey() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyDown} }

// typeRunes feeds a string into the model one key event at a time.
func typeRunes(m Model, s string) Model {
	for _, r := range s {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	return m
}

func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"", "anything", true},
		{"cyt", "cycle theme", true},
		{"theme", "theme: nord", true},
		{"nord", "theme: nord", true},
		{"xyz", "cycle theme", false},
		{"emet", "theme", false}, // order matters: not a subsequence
	}
	for _, c := range cases {
		if got := fuzzyMatch(c.pattern, c.s); got != c.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}

func TestPaletteOpensAndRenders(t *testing.T) {
	m, _ := newTestModel(t)
	next, _ := m.Update(slashKey())
	m = next.(Model)
	if !m.palette {
		t.Fatal("slash should open the command palette")
	}
	if !strings.Contains(m.View(), "Command palette") {
		t.Error("palette overlay did not render")
	}
	// Esc closes it.
	next, _ = m.Update(escKey())
	m = next.(Model)
	if m.palette {
		t.Error("esc should close the palette")
	}
}

func TestPaletteFiltersByQuery(t *testing.T) {
	m, _ := newTestModel(t)
	next, _ := m.Update(slashKey())
	m = next.(Model)
	all := len(m.paletteShown)
	m = typeRunes(m, "theme: nord")
	if len(m.paletteShown) != 1 {
		t.Fatalf("query 'theme: nord' should match exactly one command, got %d", len(m.paletteShown))
	}
	if all <= len(m.paletteShown) {
		t.Errorf("filtering should narrow the list (was %d, now %d)", all, len(m.paletteShown))
	}
}

func TestPaletteRunsThemeCommand(t *testing.T) {
	m, _ := newTestModel(t)
	if m.theme == "nord" {
		t.Fatal("precondition: default theme should not be nord")
	}
	next, _ := m.Update(slashKey())
	m = next.(Model)
	m = typeRunes(m, "theme: nord")
	next, _ = m.Update(enterKey())
	m = next.(Model)
	if m.palette {
		t.Error("running a command should close the palette")
	}
	if m.theme != "nord" {
		t.Errorf("palette theme command should switch theme to nord, got %q", m.theme)
	}
}

func TestPaletteActionOpensConfirm(t *testing.T) {
	m, session := newTestModel(t)
	rs, _ := session.List(context.Background(), "demo", provider.KindVM)
	next, _ := m.Update(inventoryMsg{conn: "demo", resources: rs})
	m = next.(Model)
	m.focus = focusTable

	next, _ = m.Update(slashKey())
	m = next.(Model)
	m = typeRunes(m, "stop selected")
	next, _ = m.Update(enterKey())
	m = next.(Model)
	if !m.confirm {
		t.Fatal("a power action from the palette should open the confirm modal")
	}
	if m.pending.Verb != "stop" {
		t.Errorf("pending verb = %q, want stop", m.pending.Verb)
	}
}

func TestPaletteQuitCommand(t *testing.T) {
	m, _ := newTestModel(t)
	next, _ := m.Update(slashKey())
	m = next.(Model)
	m = typeRunes(m, "quit")
	_, cmd := m.Update(enterKey())
	if cmd == nil {
		t.Fatal("quit command should return a tea.Cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("quit command should produce a QuitMsg")
	}
}

func TestPaletteCursorNavigation(t *testing.T) {
	m, _ := newTestModel(t)
	next, _ := m.Update(slashKey())
	m = next.(Model)
	if m.paletteCursor != 0 {
		t.Fatalf("cursor should start at 0, got %d", m.paletteCursor)
	}
	next, _ = m.Update(downKey())
	m = next.(Model)
	if m.paletteCursor != 1 {
		t.Errorf("down should advance cursor to 1, got %d", m.paletteCursor)
	}
}
