package technitium

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func fixture(t *testing.T) *Technitium {
	t.Helper()
	mux := http.NewServeMux()
	check := func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Query().Get("token") != "tkn" {
			t.Errorf("missing/incorrect token on %s: %q", r.URL.Path, r.URL.RawQuery)
			http.Error(w, "no token", 403)
			return false
		}
		return true
	}
	mux.HandleFunc("/api/zones/list", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","response":{"zones":[
			{"name":"example.com","type":"Primary","disabled":false,"dnssecStatus":"Unsigned"},
			{"name":"internal.lan","type":"Primary","disabled":true,"dnssecStatus":"Unsigned"}
		]}}`))
	})
	mux.HandleFunc("/api/zones/records/get", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		zone := r.URL.Query().Get("zone")
		if zone == "example.com" {
			_, _ = w.Write([]byte(`{"status":"ok","response":{"records":[
				{"name":"example.com","type":"A","ttl":3600,"disabled":false,"rData":{"ipAddress":"203.0.113.5"}},
				{"name":"www.example.com","type":"CNAME","ttl":300,"disabled":false,"rData":{"cname":"example.com"}}
			]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","response":{"records":[]}}`))
	})
	mux.HandleFunc("/api/zones/records/add", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		if r.URL.Query().Get("ipAddress") != "203.0.113.9" {
			t.Errorf("expected ipAddress mapped from data, got %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"status":"ok","response":{}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := New()
	if err := p.Connect(context.Background(), provider.ConnConfig{Endpoint: srv.URL, Token: "tkn"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return p
}

func TestRegistered(t *testing.T) {
	if _, err := provider.New("technitium"); err != nil {
		t.Fatalf("not registered: %v", err)
	}
}

func TestListZones(t *testing.T) {
	p := fixture(t)
	zones, err := p.List(context.Background(), provider.KindDNSZone)
	if err != nil {
		t.Fatalf("list zones: %v", err)
	}
	if len(zones) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(zones))
	}
	if zones[0].Name != "example.com" || zones[0].Status != provider.StatusOK {
		t.Errorf("unexpected zone[0]: %+v", zones[0])
	}
	if zones[1].Status != provider.StatusStopped {
		t.Errorf("disabled zone should map to stopped, got %s", zones[1].Status)
	}
}

func TestListRecordsAcrossZones(t *testing.T) {
	p := fixture(t)
	recs, err := p.List(context.Background(), provider.KindDNSRecord)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	var sawCNAME bool
	for _, r := range recs {
		if r.Parent != "example.com" {
			t.Errorf("record %s missing zone parent: %q", r.Name, r.Parent)
		}
		if r.Fields["type"] == "CNAME" && r.Fields["data"] == "example.com" {
			sawCNAME = true
		}
	}
	if !sawCNAME {
		t.Error("expected CNAME record with data example.com")
	}
}

func TestCreateRecordMapsData(t *testing.T) {
	p := fixture(t)
	res, err := p.Do(context.Background(), provider.Action{
		Verb:   "create_record",
		Kind:   provider.KindDNSRecord,
		Target: "example.com",
		Params: map[string]any{"zone": "example.com", "domain": "api.example.com", "type": "A", "data": "203.0.113.9", "ttl": "3600"},
	})
	if err != nil {
		t.Fatalf("create_record: %v", err)
	}
	if !res.OK {
		t.Errorf("expected OK result, got %+v", res)
	}
}

func TestErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"error","errorMessage":"invalid token"}`))
	}))
	t.Cleanup(srv.Close)
	p := New()
	err := p.Connect(context.Background(), provider.ConnConfig{Endpoint: srv.URL, Token: "x"})
	if err == nil {
		t.Fatal("expected error from error envelope")
	}
}
