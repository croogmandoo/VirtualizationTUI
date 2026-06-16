package caddy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

const fullConfig = `{"apps":{"http":{"servers":{"srv0":{"listen":[":443"],"routes":[
	{"match":[{"host":["example.com"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"localhost:8080"}]}]},
	{"match":[{"host":["api.example.com"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"localhost:9000"}]}]}
]}}}}}`

const serversConfig = `{"srv0":{"listen":[":443"],"routes":[
	{"match":[{"host":["example.com"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"localhost:8080"}]}]},
	{"match":[{"host":["api.example.com"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"localhost:9000"}]}]}
]}}`

func fixture(t *testing.T, configFile string) *Caddy {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/config/apps/http/servers", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(serversConfig))
	})
	mux.HandleFunc("/config/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(fullConfig))
	})
	mux.HandleFunc("/load", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST to /load, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := New()
	extra := map[string]string{}
	if configFile != "" {
		extra["config_file"] = configFile
	}
	if err := p.Connect(context.Background(), provider.ConnConfig{Endpoint: srv.URL, Extra: extra}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return p
}

func TestRegistered(t *testing.T) {
	if _, err := provider.New("caddy"); err != nil {
		t.Fatalf("not registered: %v", err)
	}
}

func TestListRoutes(t *testing.T) {
	p := fixture(t, "")
	routes, err := p.List(context.Background(), provider.KindRoute)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
	r0 := routes[0]
	if r0.Name != "example.com" {
		t.Errorf("expected host name example.com, got %q", r0.Name)
	}
	if r0.Fields["upstreams"] != "localhost:8080" {
		t.Errorf("unexpected upstreams: %q", r0.Fields["upstreams"])
	}
	if r0.Fields["handler"] != "reverse_proxy" {
		t.Errorf("unexpected handler: %q", r0.Fields["handler"])
	}
}

func TestPersistAndDriftDetection(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "caddy.json")
	p := fixture(t, cfgFile)

	// File missing -> drift, routes marked degraded.
	routes, err := p.List(context.Background(), provider.KindRoute)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if routes[0].Status != provider.StatusDegraded {
		t.Errorf("expected degraded status on drift, got %s", routes[0].Status)
	}

	// Persist live -> disk; now there should be no drift.
	if _, err := p.Do(context.Background(), provider.Action{Verb: "persist"}); err != nil {
		t.Fatalf("persist: %v", err)
	}
	res, err := p.Do(context.Background(), provider.Action{Verb: "diff"})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if res.Message == "" || res.Message[:4] != "live" {
		t.Errorf("unexpected diff message: %q", res.Message)
	}
	routes, _ = p.List(context.Background(), provider.KindRoute)
	if routes[0].Status != provider.StatusOK {
		t.Errorf("expected OK status after persist, got %s", routes[0].Status)
	}
}

func TestLoadFromDisk(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "caddy.json")
	p := fixture(t, cfgFile)
	if _, err := p.Do(context.Background(), provider.Action{Verb: "persist"}); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if _, err := p.Do(context.Background(), provider.Action{Verb: "load"}); err != nil {
		t.Fatalf("load: %v", err)
	}
}

func TestPersistWithoutFileErrors(t *testing.T) {
	p := fixture(t, "")
	if _, err := p.Do(context.Background(), provider.Action{Verb: "persist"}); err == nil {
		t.Error("expected error persisting with no config_file")
	}
}
