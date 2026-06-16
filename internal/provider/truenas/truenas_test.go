package truenas

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func fixture(t *testing.T) (*TrueNAS, *capture) {
	t.Helper()
	cap := &capture{}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer apikey" {
			t.Errorf("bad auth header on %s: %q", r.URL.Path, r.Header.Get("Authorization"))
			http.Error(w, "unauthorized", 401)
			return false
		}
		return true
	}
	mux.HandleFunc("/api/v2.0/system/info", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			_, _ = w.Write([]byte(`{"version":"TrueNAS-SCALE-24.04"}`))
		}
	})
	mux.HandleFunc("/api/v2.0/pool", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			_, _ = w.Write([]byte(`[{"id":1,"name":"tank","status":"ONLINE","healthy":true,"size":8000000000000,"allocated":2000000000000,"free":6000000000000}]`))
		}
	})
	mux.HandleFunc("/api/v2.0/pool/dataset", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			_, _ = w.Write([]byte(`[{"id":"tank","name":"tank","type":"FILESYSTEM","used":{"parsed":2000000000000},"available":{"parsed":6000000000000}},
				{"id":"tank/media","name":"tank/media","type":"FILESYSTEM","used":{"parsed":1500000000000},"available":{"parsed":6000000000000}}]`))
		}
	})
	mux.HandleFunc("/api/v2.0/sharing/smb", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			_, _ = w.Write([]byte(`[{"id":1,"name":"media","path":"/mnt/tank/media","enabled":true}]`))
		}
	})
	mux.HandleFunc("/api/v2.0/sharing/nfs", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			_, _ = w.Write([]byte(`[{"id":3,"path":"/mnt/tank/backups","enabled":false}]`))
		}
	})
	mux.HandleFunc("/api/v2.0/sharing/smb/id/1", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &cap.lastBody)
		cap.lastPath = r.URL.Path
		_, _ = w.Write([]byte(`{"id":1,"enabled":false}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := New()
	if err := p.Connect(context.Background(), provider.ConnConfig{Endpoint: srv.URL, Token: "apikey"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return p, cap
}

type capture struct {
	lastPath string
	lastBody map[string]any
}

func TestRegistered(t *testing.T) {
	if _, err := provider.New("truenas"); err != nil {
		t.Fatalf("not registered: %v", err)
	}
}

func TestEndpointSuffixAppended(t *testing.T) {
	// Connect should append /api/v2.0 when absent (verified implicitly: ping hits it).
	p, _ := fixture(t)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestListPools(t *testing.T) {
	p, _ := fixture(t)
	pools, err := p.List(context.Background(), provider.KindStorage)
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(pools))
	}
	if pools[0].Name != "tank" || pools[0].Status != provider.StatusOK {
		t.Errorf("unexpected pool: %+v", pools[0])
	}
	if pools[0].Fields["size"] != "7.3T" {
		t.Errorf("size format: %q", pools[0].Fields["size"])
	}
	if len(pools[0].Metrics) == 0 {
		t.Error("expected pool usage metric")
	}
}

func TestListDatasetsParentPool(t *testing.T) {
	p, _ := fixture(t)
	ds, err := p.List(context.Background(), provider.KindDataset)
	if err != nil {
		t.Fatalf("list datasets: %v", err)
	}
	if len(ds) != 2 {
		t.Fatalf("expected 2 datasets, got %d", len(ds))
	}
	for _, d := range ds {
		if d.Parent != "tank" {
			t.Errorf("dataset %s parent = %q, want tank", d.Name, d.Parent)
		}
	}
}

func TestListSharesProtocols(t *testing.T) {
	p, _ := fixture(t)
	shares, err := p.List(context.Background(), provider.KindShare)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(shares) != 2 {
		t.Fatalf("expected 2 shares, got %d", len(shares))
	}
	var smbOK, nfsDisabled bool
	for _, s := range shares {
		if s.ID == "smb:1" && s.Status == provider.StatusOK {
			smbOK = true
		}
		if s.ID == "nfs:3" && s.Status == provider.StatusStopped {
			nfsDisabled = true
		}
	}
	if !smbOK || !nfsDisabled {
		t.Errorf("share status mapping wrong: %+v", shares)
	}
}

func TestDisableShare(t *testing.T) {
	p, cap := fixture(t)
	res, err := p.Do(context.Background(), provider.Action{Verb: "disable_share", Kind: provider.KindShare, Target: "smb:1"})
	if err != nil {
		t.Fatalf("disable_share: %v", err)
	}
	if !res.OK {
		t.Errorf("expected OK, got %+v", res)
	}
	if cap.lastPath != "/api/v2.0/sharing/smb/id/1" {
		t.Errorf("unexpected PUT path: %q", cap.lastPath)
	}
	if v, ok := cap.lastBody["enabled"].(bool); !ok || v {
		t.Errorf("expected enabled=false in body, got %v", cap.lastBody)
	}
}

func TestBadShareTarget(t *testing.T) {
	p, _ := fixture(t)
	if _, err := p.Do(context.Background(), provider.Action{Verb: "enable_share", Target: "bogus"}); err == nil {
		t.Error("expected error for malformed share target")
	}
}
