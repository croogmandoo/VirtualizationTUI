package unraid

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

type gqlReq struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func fixture(t *testing.T) (*Unraid, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key123" {
			t.Errorf("missing x-api-key, got %q", r.Header.Get("x-api-key"))
			http.Error(w, "unauthorized", 401)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req gqlReq
		_ = json.Unmarshal(body, &req)
		cap.lastQuery = req.Query
		cap.lastVars = req.Variables

		switch {
		case strings.Contains(req.Query, "mutation"):
			_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
		case strings.Contains(req.Query, "disks"):
			_, _ = w.Write([]byte(`{"data":{"array":{"state":"STARTED","disks":[
				{"name":"disk1","size":4000000000,"status":"DISK_OK","type":"Data"},
				{"name":"disk2","size":4000000000,"status":"DISK_DSBL","type":"Data"}]}}}`))
		case strings.Contains(req.Query, "containers"):
			_, _ = w.Write([]byte(`{"data":{"docker":{"containers":[
				{"id":"abc123","names":["/plex"],"image":"plexinc/pms","state":"RUNNING"},
				{"id":"def456","names":["/sonarr"],"image":"linuxserver/sonarr","state":"EXITED"}]}}}`))
		case strings.Contains(req.Query, "domains"):
			_, _ = w.Write([]byte(`{"data":{"vms":{"domains":[
				{"uuid":"uuid-1","name":"Windows11","state":"RUNNING"},
				{"uuid":"uuid-2","name":"Ubuntu","state":"SHUTOFF"}]}}}`))
		case strings.Contains(req.Query, "shares"):
			_, _ = w.Write([]byte(`{"data":{"shares":[{"name":"appdata","free":500000000,"size":1000000000}]}}`))
		default: // ping
			_, _ = w.Write([]byte(`{"data":{"array":{"state":"STARTED"}}}`))
		}
	}))
	t.Cleanup(srv.Close)

	p := New()
	if err := p.Connect(context.Background(), provider.ConnConfig{Endpoint: srv.URL, Token: "key123"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return p, cap
}

type capture struct {
	lastQuery string
	lastVars  map[string]any
}

func TestRegistered(t *testing.T) {
	if _, err := provider.New("unraid"); err != nil {
		t.Fatalf("not registered: %v", err)
	}
}

func TestEndpointSuffix(t *testing.T) {
	p, _ := fixture(t)
	if !strings.HasSuffix(p.endpoint, "/graphql") {
		t.Errorf("endpoint should end with /graphql, got %q", p.endpoint)
	}
}

func TestListArrayDisks(t *testing.T) {
	p, _ := fixture(t)
	disks, err := p.List(context.Background(), provider.KindStorage)
	if err != nil {
		t.Fatalf("list array: %v", err)
	}
	if len(disks) != 2 {
		t.Fatalf("expected 2 disks, got %d", len(disks))
	}
	if disks[0].Status != provider.StatusOK {
		t.Errorf("disk1 should be ok, got %s", disks[0].Status)
	}
	if disks[1].Status != provider.StatusError {
		t.Errorf("disabled disk2 should be error, got %s", disks[1].Status)
	}
}

func TestListDockerAndVMs(t *testing.T) {
	p, _ := fixture(t)
	ctrs, err := p.List(context.Background(), provider.KindContainer)
	if err != nil {
		t.Fatalf("list docker: %v", err)
	}
	if len(ctrs) != 2 || ctrs[0].Name != "plex" {
		t.Fatalf("unexpected containers: %+v", ctrs)
	}
	if ctrs[1].Status != provider.StatusStopped {
		t.Errorf("exited container should be stopped, got %s", ctrs[1].Status)
	}

	vms, err := p.List(context.Background(), provider.KindVM)
	if err != nil {
		t.Fatalf("list vms: %v", err)
	}
	if len(vms) != 2 || vms[0].Name != "Windows11" || vms[0].Status != provider.StatusRunning {
		t.Fatalf("unexpected vms: %+v", vms)
	}
}

func TestListShares(t *testing.T) {
	p, _ := fixture(t)
	shares, err := p.List(context.Background(), provider.KindShare)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(shares) != 1 || shares[0].Name != "appdata" {
		t.Fatalf("unexpected shares: %+v", shares)
	}
}

func TestStartContainerMutation(t *testing.T) {
	p, cap := fixture(t)
	res, err := p.Do(context.Background(), provider.Action{Verb: "start", Kind: provider.KindContainer, Target: "abc123"})
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	if !res.OK {
		t.Errorf("expected OK, got %+v", res)
	}
	if !strings.Contains(cap.lastQuery, "mutation") || !strings.Contains(cap.lastQuery, "start") {
		t.Errorf("expected a start mutation, got %q", cap.lastQuery)
	}
	if cap.lastVars["id"] != "abc123" {
		t.Errorf("expected id variable abc123, got %v", cap.lastVars)
	}
}

func TestGraphQLErrorSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"unauthorized api key"}]}`))
	}))
	t.Cleanup(srv.Close)
	p := New()
	err := p.Connect(context.Background(), provider.ConnConfig{Endpoint: srv.URL, Token: "x"})
	if err == nil || !strings.Contains(err.Error(), "unauthorized api key") {
		t.Fatalf("expected graphql error surfaced, got %v", err)
	}
}

func TestUnsupportedAction(t *testing.T) {
	p, _ := fixture(t)
	if _, err := p.Do(context.Background(), provider.Action{Verb: "snapshot", Kind: provider.KindContainer, Target: "x"}); err == nil {
		t.Error("expected error for unsupported action")
	}
}
