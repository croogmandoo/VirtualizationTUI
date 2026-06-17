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

func TestSelectFieldColumns(t *testing.T) {
	rows := []provider.Resource{
		{Kind: provider.KindVM, Fields: map[string]string{"cpu": "5%", "mem": "1G", "zzz": "x"}},
		{Kind: provider.KindDNSRecord, Fields: map[string]string{"type": "A", "value": "1.2.3.4", "ttl": "300"}},
	}
	cols := selectFieldColumns(rows)
	// Priority keys come first in priority order; cpu/mem before type/value/ttl.
	want := []string{"cpu", "mem", "type", "value", "ttl"}
	if len(cols) != len(want) {
		t.Fatalf("columns = %v, want %v", cols, want)
	}
	for i := range want {
		if cols[i] != want[i] {
			t.Fatalf("column %d = %q, want %q (full: %v)", i, cols[i], want[i], cols)
		}
	}
	// "zzz" is non-priority and should be dropped by the maxFieldCols cap.
	for _, c := range cols {
		if c == "zzz" {
			t.Errorf("low-priority field should have been capped out: %v", cols)
		}
	}
}

func TestAccumulateMetricsBuildsHistory(t *testing.T) {
	m, _ := newTestModel(t)
	m.cfg.UI.MetricsWindow = 3

	mk := func(v float64) []provider.Resource {
		return []provider.Resource{{
			ID:      "vm1",
			Metrics: []provider.Metric{{Name: "cpu", Value: v}},
		}}
	}

	rs := mk(10)
	m.accumulateMetrics(rs)
	if got := rs[0].Metrics[0].History; len(got) != 1 || got[0] != 10 {
		t.Fatalf("first poll history = %v, want [10]", got)
	}
	rs = mk(20)
	m.accumulateMetrics(rs)
	rs = mk(30)
	m.accumulateMetrics(rs)
	if got := rs[0].Metrics[0].History; len(got) != 3 {
		t.Fatalf("after 3 polls len = %d, want 3 (%v)", len(got), got)
	}
	// Fourth poll trims to the window of 3 (oldest dropped).
	rs = mk(40)
	m.accumulateMetrics(rs)
	got := rs[0].Metrics[0].History
	want := []float64{20, 30, 40}
	if len(got) != 3 || got[0] != want[0] || got[2] != want[2] {
		t.Fatalf("windowed history = %v, want %v", got, want)
	}
}

func TestAccumulateMetricsRespectsProviderHistory(t *testing.T) {
	m, _ := newTestModel(t)
	rs := []provider.Resource{{
		ID:      "vm1",
		Metrics: []provider.Metric{{Name: "cpu", Value: 9, History: []float64{1, 2, 3}}},
	}}
	m.accumulateMetrics(rs)
	if got := rs[0].Metrics[0].History; len(got) != 3 {
		t.Fatalf("provider-managed history should be left intact, got %v", got)
	}
}

func TestAccumulateMetricsPrunesStaleResources(t *testing.T) {
	m, _ := newTestModel(t)
	m.accumulateMetrics([]provider.Resource{{ID: "a", Metrics: []provider.Metric{{Name: "cpu", Value: 1}}}})
	m.accumulateMetrics([]provider.Resource{{ID: "b", Metrics: []provider.Metric{{Name: "cpu", Value: 1}}}})
	if _, ok := m.metricHist["a"]; ok {
		t.Error("resource 'a' absent from the latest poll should be pruned from history")
	}
}

func TestThemeCycleKey(t *testing.T) {
	m, _ := newTestModel(t)
	before := m.theme
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(Model)
	if m.theme == before {
		t.Errorf("theme should change after pressing t (still %q)", before)
	}
	if !strings.Contains(m.status, "theme:") {
		t.Errorf("status should report the new theme, got %q", m.status)
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
