package proxmox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

// fixtureServer returns an httptest server emulating the slice of the Proxmox API
// the provider uses, plus a connected provider pointed at it.
func fixtureServer(t *testing.T) (*Proxmox, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api2/json/version", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		writeJSON(w, `{"data":{"version":"8.1.4","release":"8.1"}}`)
	})
	mux.HandleFunc("/api2/json/nodes", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		writeJSON(w, `{"data":[{"node":"pve-01","status":"online","cpu":0.18,"maxcpu":8,"mem":24800000000,"maxmem":67000000000,"uptime":2678400}]}`)
	})
	mux.HandleFunc("/api2/json/cluster/resources", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.URL.Query().Get("type") != "vm" {
			t.Errorf("expected type=vm, got %q", r.URL.RawQuery)
		}
		writeJSON(w, `{"data":[
			{"id":"qemu/100","type":"qemu","node":"pve-01","vmid":100,"name":"web-01","status":"running","cpu":0.03,"maxcpu":2,"mem":2254857830,"maxmem":4294967296,"uptime":86400},
			{"id":"qemu/102","type":"qemu","node":"pve-01","vmid":102,"name":"ci-runner","status":"stopped","cpu":0,"maxmem":4294967296},
			{"id":"qemu/900","type":"qemu","node":"pve-01","vmid":900,"name":"debian-template","status":"stopped","template":1,"maxmem":2147483648},
			{"id":"lxc/200","type":"lxc","node":"pve-01","vmid":200,"name":"dns","status":"running","cpu":0.01,"mem":268435456,"maxmem":536870912}
		]}`)
	})
	mux.HandleFunc("/api2/json/nodes/pve-01/qemu/100/status/start", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		writeJSON(w, `{"data":"UPID:pve-01:00001234:00ABCDEF:65000000:qmstart:100:root@pam:"}`)
	})
	mux.HandleFunc("/api2/json/nodes/pve-01/tasks/", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		writeJSON(w, `{"data":{"status":"stopped","exitstatus":"OK"}}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := New()
	if err := p.Connect(context.Background(), provider.ConnConfig{
		Endpoint: srv.URL,
		Token:    "root@pam!test=secret-uuid",
	}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return p, srv
}

func assertAuth(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "PVEAPIToken=root@pam!test=secret-uuid" {
		t.Errorf("unexpected Authorization header: %q", got)
	}
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func TestRegistered(t *testing.T) {
	p, err := provider.New("proxmox")
	if err != nil {
		t.Fatalf("proxmox not registered: %v", err)
	}
	if p.Type() != "proxmox" {
		t.Fatalf("unexpected type %q", p.Type())
	}
}

func TestPingAndListNodes(t *testing.T) {
	p, _ := fixtureServer(t)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	nodes, err := p.List(context.Background(), provider.KindNode)
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.Name != "pve-01" || n.Status != provider.StatusOK {
		t.Errorf("unexpected node: %+v", n)
	}
	if n.Fields["cpu"] != "18%" {
		t.Errorf("cpu format: %q", n.Fields["cpu"])
	}
	if n.Fields["uptime"] != "31d0h" {
		t.Errorf("uptime format: %q", n.Fields["uptime"])
	}
}

func TestListGuestsFilters(t *testing.T) {
	p, _ := fixtureServer(t)
	vms, err := p.List(context.Background(), provider.KindVM)
	if err != nil {
		t.Fatalf("list vms: %v", err)
	}
	if len(vms) != 2 {
		t.Fatalf("expected 2 qemu VMs, got %d", len(vms))
	}
	ctrs, err := p.List(context.Background(), provider.KindContainer)
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	if len(ctrs) != 1 || ctrs[0].Name != "dns" {
		t.Fatalf("expected 1 lxc container named dns, got %+v", ctrs)
	}
	// A stopped VM should map correctly.
	var stopped bool
	for _, vm := range vms {
		if vm.ID == "102" && vm.Status == provider.StatusStopped {
			stopped = true
		}
	}
	if !stopped {
		t.Error("expected vm 102 to be stopped")
	}
}

func TestListGuestsSkipsTemplates(t *testing.T) {
	p, _ := fixtureServer(t)
	vms, err := p.List(context.Background(), provider.KindVM)
	if err != nil {
		t.Fatalf("list vms: %v", err)
	}
	for _, vm := range vms {
		if vm.ID == "900" {
			t.Errorf("template guest 900 should be excluded from the VM list: %+v", vm)
		}
	}
}

func TestSnapshotDefaultNameUnique(t *testing.T) {
	var snapname string
	mux := http.NewServeMux()
	mux.HandleFunc("/api2/json/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"data":{"version":"8.1.4"}}`)
	})
	mux.HandleFunc("/api2/json/cluster/resources", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"data":[{"id":"qemu/100","type":"qemu","node":"pve-01","vmid":100,"name":"web-01","status":"running"}]}`)
	})
	mux.HandleFunc("/api2/json/nodes/pve-01/qemu/100/snapshot", func(w http.ResponseWriter, r *http.Request) {
		snapname = r.FormValue("snapname")
		writeJSON(w, `{"data":"UPID:pve-01:00001234:00ABCDEF:65000000:qmsnapshot:100:root@pam:"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := New()
	if err := p.Connect(context.Background(), provider.ConnConfig{Endpoint: srv.URL, Token: "root@pam!test=secret-uuid"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := p.List(context.Background(), provider.KindVM); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := p.Do(context.Background(), provider.Action{Verb: "snapshot", Kind: provider.KindVM, Target: "100"}); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !regexp.MustCompile(`^snap-100-\d+$`).MatchString(snapname) {
		t.Errorf("default snapshot name %q should be unique (snap-<vmid>-<unixtime>)", snapname)
	}
}

func TestUnauthorizedErrorHint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api2/json/version", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("401 No ticket"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := New()
	err := p.Connect(context.Background(), provider.ConnConfig{Endpoint: srv.URL, Token: "bad"})
	if err == nil {
		t.Fatal("expected connect to fail on 401")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("401 error should hint at the token problem, got: %v", err)
	}
}

func TestDoStartReturnsTaskAndPolls(t *testing.T) {
	p, _ := fixtureServer(t)
	// Populate the placement cache.
	if _, err := p.List(context.Background(), provider.KindVM); err != nil {
		t.Fatalf("list: %v", err)
	}
	res, err := p.Do(context.Background(), provider.Action{Verb: "start", Kind: provider.KindVM, Target: "100"})
	if err != nil {
		t.Fatalf("do start: %v", err)
	}
	if !res.OK || res.TaskID == "" {
		t.Fatalf("expected OK result with task id, got %+v", res)
	}
	if !strings.HasPrefix(res.TaskID, "UPID:pve-01:") {
		t.Errorf("unexpected UPID: %q", res.TaskID)
	}
	state, err := p.TaskStatus(context.Background(), res.TaskID)
	if err != nil {
		t.Fatalf("task status: %v", err)
	}
	if state != provider.TaskDone {
		t.Errorf("expected TaskDone, got %s", state)
	}
}

func TestDoUnknownTarget(t *testing.T) {
	p, _ := fixtureServer(t)
	if _, err := p.Do(context.Background(), provider.Action{Verb: "start", Target: "999"}); err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func TestConnectValidation(t *testing.T) {
	p := New()
	if err := p.Connect(context.Background(), provider.ConnConfig{Endpoint: "https://x:8006"}); err == nil {
		t.Error("expected error when token missing")
	}
	if err := p.Connect(context.Background(), provider.ConnConfig{Token: "t"}); err == nil {
		t.Error("expected error when endpoint missing")
	}
}

func TestNodeFromUPID(t *testing.T) {
	node, err := nodeFromUPID("UPID:pve-02:00001234:00ABCDEF:65000000:qmstart:100:root@pam:")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if node != "pve-02" {
		t.Errorf("expected pve-02, got %q", node)
	}
	if _, err := nodeFromUPID("not-a-upid"); err == nil {
		t.Error("expected error for malformed UPID")
	}
}

func TestFormatHelpers(t *testing.T) {
	cases := []struct{ in, want string }{
		{humanBytes(2254857830), "2.1G"},
		{humanBytes(268435456), "256M"},
		{formatUptime(2678400), "31d0h"},
		{formatUptime(7200), "2h"},
		{formatPct(0.415), "42%"},
	}
	for _, c := range cases {
		if c.in != c.want {
			t.Errorf("got %q, want %q", c.in, c.want)
		}
	}
}
