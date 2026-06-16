package app

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/croogmandoo/virtualizationtui/internal/config"
	"github.com/croogmandoo/virtualizationtui/internal/core"
	"github.com/croogmandoo/virtualizationtui/internal/provider"

	_ "github.com/croogmandoo/virtualizationtui/internal/provider/mock"
)

// fakeStore is a secrets.Store that needs no OS keyring.
type fakeStore struct{}

func (fakeStore) Resolve(config.Connection) (string, error) { return "tok", nil }
func (fakeStore) Set(string, string) error                  { return nil }
func (fakeStore) Delete(string) error                       { return nil }

func newTestModel(t *testing.T) (Model, *core.Session) {
	t.Helper()
	cfg := config.Default()
	session := core.NewSession(cfg, fakeStore{})
	if err := session.Connect(context.Background(), "demo"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	m := New(session, cfg)
	// Establish layout.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(Model), session
}

func TestModelRendersInventory(t *testing.T) {
	m, session := newTestModel(t)
	rs, err := session.List(context.Background(), "demo", provider.KindVM)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	next, _ := m.Update(inventoryMsg{conn: "demo", resources: rs})
	m = next.(Model)

	out := m.View()
	if !strings.Contains(out, "VirtualizationTUI") {
		t.Error("header missing from view")
	}
	if !strings.Contains(out, "web-01") {
		t.Errorf("expected a seeded VM in the view; got:\n%s", out)
	}
}

func TestHelpOverlayToggles(t *testing.T) {
	m, _ := newTestModel(t)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = next.(Model)
	if !strings.Contains(m.View(), "Keybindings") {
		t.Error("help overlay did not render")
	}
}

func TestQuitKey(t *testing.T) {
	m, _ := newTestModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("quit key should return a command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("quit command should produce a message")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", msg)
	}
}

func TestReadOnlyBlocksActions(t *testing.T) {
	cfg := config.Default()
	cfg.UI.ReadOnly = true
	session := core.NewSession(cfg, fakeStore{})
	if err := session.Connect(context.Background(), "demo"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	m := New(session, cfg)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	rs, _ := session.List(context.Background(), "demo", provider.KindVM)
	next, _ = m.Update(inventoryMsg{conn: "demo", resources: rs})
	m = next.(Model)
	m.focus = focusTable

	// Attempt a stop; read-only should set status and not open the confirm modal.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = next.(Model)
	if m.confirm {
		t.Error("read-only mode should not open a confirm modal")
	}
	if !strings.Contains(m.status, "read-only") {
		t.Errorf("expected read-only status, got %q", m.status)
	}
}
