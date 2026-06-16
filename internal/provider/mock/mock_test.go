package mock

import (
	"context"
	"testing"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func TestMockRegistered(t *testing.T) {
	p, err := provider.New("mock")
	if err != nil {
		t.Fatalf("mock not registered: %v", err)
	}
	if p.Type() != "mock" {
		t.Fatalf("unexpected type %q", p.Type())
	}
}

func TestMockListAndConnect(t *testing.T) {
	m := New()
	ctx := context.Background()
	if err := m.Ping(ctx); err == nil {
		t.Fatal("expected ping to fail before connect")
	}
	if err := m.Connect(ctx, provider.ConnConfig{Name: "pve-01"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := m.Ping(ctx); err != nil {
		t.Fatalf("ping after connect: %v", err)
	}
	vms, err := m.List(ctx, provider.KindVM)
	if err != nil {
		t.Fatalf("list vms: %v", err)
	}
	if len(vms) == 0 {
		t.Fatal("expected seeded VMs")
	}
	for _, vm := range vms {
		if len(vm.Metrics) == 0 {
			t.Errorf("vm %s missing metrics", vm.Name)
		}
	}
}

func TestMockPowerAction(t *testing.T) {
	m := New()
	ctx := context.Background()
	_ = m.Connect(ctx, provider.ConnConfig{Name: "pve-01"})

	// 102 (ci-runner) starts stopped.
	res, err := m.Do(ctx, provider.Action{Verb: "start", Kind: provider.KindVM, Target: "102"})
	if err != nil || !res.OK {
		t.Fatalf("start failed: %v %+v", err, res)
	}
	got, _ := m.Get(ctx, provider.KindVM, "102")
	if got.Status != provider.StatusRunning {
		t.Fatalf("expected running after start, got %s", got.Status)
	}

	// Reboot returns an async task that eventually completes.
	res, err = m.Do(ctx, provider.Action{Verb: "reboot", Kind: provider.KindVM, Target: "102"})
	if err != nil || res.TaskID == "" {
		t.Fatalf("reboot should spawn a task: %v %+v", err, res)
	}
	if _, err := m.TaskStatus(ctx, res.TaskID); err != nil {
		t.Fatalf("task status: %v", err)
	}
}

func TestMockUnknownVerb(t *testing.T) {
	m := New()
	_ = m.Connect(context.Background(), provider.ConnConfig{Name: "x"})
	if _, err := m.Do(context.Background(), provider.Action{Verb: "explode", Target: "100"}); err == nil {
		t.Fatal("expected error for unknown verb")
	}
}
