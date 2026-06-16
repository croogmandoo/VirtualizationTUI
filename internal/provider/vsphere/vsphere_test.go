package vsphere

import (
	"context"
	"testing"

	"github.com/vmware/govmomi/simulator"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

// withSim runs fn against a vcsim-backed connected provider, exercising real
// govmomi code paths without a live vCenter.
func withSim(t *testing.T, fn func(p *VSphere)) {
	t.Helper()
	model := simulator.VPX()
	if err := model.Create(); err != nil {
		t.Fatalf("create model: %v", err)
	}
	defer model.Remove()
	server := model.Service.NewServer()
	defer server.Close()

	p := New()
	// server.URL embeds simulator credentials; pass them through our config.
	pw, _ := server.URL.User.Password()
	if err := p.Connect(context.Background(), provider.ConnConfig{
		Endpoint: server.URL.String(),
		Username: server.URL.User.Username(),
		Password: pw,
		TLS:      provider.TLSConfig{Insecure: true},
	}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer p.Close()
	fn(p)
}

func TestRegistered(t *testing.T) {
	if _, err := provider.New("vsphere"); err != nil {
		t.Fatalf("not registered: %v", err)
	}
}

func TestConnectValidation(t *testing.T) {
	p := New()
	if err := p.Connect(context.Background(), provider.ConnConfig{Endpoint: "https://x"}); err == nil {
		t.Error("expected error when credentials missing")
	}
}

func TestPingAndListVMs(t *testing.T) {
	withSim(t, func(p *VSphere) {
		if err := p.Ping(context.Background()); err != nil {
			t.Fatalf("ping: %v", err)
		}
		vms, err := p.List(context.Background(), provider.KindVM)
		if err != nil {
			t.Fatalf("list vms: %v", err)
		}
		if len(vms) == 0 {
			t.Fatal("expected simulator VMs")
		}
		for _, vm := range vms {
			if vm.Kind != provider.KindVM || vm.ID == "" {
				t.Errorf("bad vm resource: %+v", vm)
			}
			if vm.Status != provider.StatusRunning && vm.Status != provider.StatusStopped {
				t.Errorf("unexpected vm status %q", vm.Status)
			}
		}
	})
}

func TestListHostsAndDatastores(t *testing.T) {
	withSim(t, func(p *VSphere) {
		hosts, err := p.List(context.Background(), provider.KindHost)
		if err != nil {
			t.Fatalf("list hosts: %v", err)
		}
		if len(hosts) == 0 {
			t.Fatal("expected simulator hosts")
		}
		ds, err := p.List(context.Background(), provider.KindStorage)
		if err != nil {
			t.Fatalf("list datastores: %v", err)
		}
		if len(ds) == 0 {
			t.Fatal("expected simulator datastores")
		}
		if _, ok := ds[0].Fields["size"]; !ok {
			t.Error("datastore missing size field")
		}
	})
}

func TestPowerCycleVM(t *testing.T) {
	withSim(t, func(p *VSphere) {
		vms, err := p.List(context.Background(), provider.KindVM)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		target := vms[0].ID

		// Power off then on; the simulator executes the tasks synchronously.
		if _, err := p.Do(context.Background(), provider.Action{Verb: "stop", Kind: provider.KindVM, Target: target}); err != nil {
			t.Fatalf("stop: %v", err)
		}
		got, _ := p.Get(context.Background(), provider.KindVM, target)
		if got.Status != provider.StatusStopped {
			t.Fatalf("expected stopped after power off, got %s", got.Status)
		}
		if _, err := p.Do(context.Background(), provider.Action{Verb: "start", Kind: provider.KindVM, Target: target}); err != nil {
			t.Fatalf("start: %v", err)
		}
		got, _ = p.Get(context.Background(), provider.KindVM, target)
		if got.Status != provider.StatusRunning {
			t.Fatalf("expected running after power on, got %s", got.Status)
		}
	})
}

func TestDoUnknownTarget(t *testing.T) {
	withSim(t, func(p *VSphere) {
		if _, err := p.Do(context.Background(), provider.Action{Verb: "start", Target: "vm-does-not-exist"}); err == nil {
			t.Error("expected error for unknown VM target")
		}
	})
}
