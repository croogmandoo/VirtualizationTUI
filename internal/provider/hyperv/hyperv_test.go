package hyperv

import (
	"context"
	"strings"
	"testing"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

// fakeRunner returns canned output based on the script, and records the last
// script run so power actions can be asserted.
type fakeRunner struct {
	lastScript string
	failPing   bool
}

func (f *fakeRunner) Run(ctx context.Context, script string) ([]byte, error) {
	f.lastScript = script
	switch {
	case strings.Contains(script, "Get-VMHost).Name"):
		if f.failPing {
			return nil, errString("winrm unreachable")
		}
		return []byte("HV-HOST\r\n"), nil
	case strings.Contains(script, "Get-VM |"):
		// Two VMs; PowerShell would emit an array here.
		return []byte(`[
			{"Name":"web01","Id":"11111111-1111-1111-1111-111111111111","State":"Running","CPUUsage":5,"MemoryAssigned":2147483648,"Uptime":"1.02:03:04","Status":"Operating normally"},
			{"Name":"db01","Id":"22222222-2222-2222-2222-222222222222","State":"Off","CPUUsage":0,"MemoryAssigned":0,"Uptime":"00:00:00","Status":"Operating normally"}
		]`), nil
	case strings.Contains(script, "Get-VMHost |"):
		// Single object: ConvertTo-Json renders one host as an object, not array.
		return []byte(`{"Name":"HV-HOST","LogicalProcessorCount":16,"MemoryCapacity":68719476736}`), nil
	case strings.Contains(script, "Get-VM -Id"):
		return []byte(""), nil // power actions produce no stdout
	default:
		return []byte(""), nil
	}
}

func (f *fakeRunner) Close() error { return nil }

type errString string

func (e errString) Error() string { return string(e) }

func withFake(t *testing.T) (*Hyperv, *fakeRunner) {
	t.Helper()
	f := &fakeRunner{}
	return &Hyperv{runner: f}, f
}

func TestRegistered(t *testing.T) {
	if _, err := provider.New("hyperv"); err != nil {
		t.Fatalf("not registered: %v", err)
	}
}

func TestPing(t *testing.T) {
	p, f := withFake(t)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	f.failPing = true
	if err := p.Ping(context.Background()); err == nil {
		t.Error("expected ping failure to propagate")
	}
}

func TestListVMs(t *testing.T) {
	p, _ := withFake(t)
	vms, err := p.List(context.Background(), provider.KindVM)
	if err != nil {
		t.Fatalf("list vms: %v", err)
	}
	if len(vms) != 2 {
		t.Fatalf("expected 2 VMs, got %d", len(vms))
	}
	if vms[0].Name != "web01" || vms[0].Status != provider.StatusRunning {
		t.Errorf("unexpected vm[0]: %+v", vms[0])
	}
	if vms[0].Fields["mem"] != "2.0G" {
		t.Errorf("mem format: %q", vms[0].Fields["mem"])
	}
	if vms[1].Status != provider.StatusStopped {
		t.Errorf("vm db01 should be stopped, got %s", vms[1].Status)
	}
}

func TestListHostSingleObject(t *testing.T) {
	p, _ := withFake(t)
	hosts, err := p.List(context.Background(), provider.KindHost)
	if err != nil {
		t.Fatalf("list host: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host (single-object JSON normalized), got %d", len(hosts))
	}
	if hosts[0].Fields["cpus"] != "16" || hosts[0].Fields["mem"] != "64.0G" {
		t.Errorf("unexpected host fields: %+v", hosts[0].Fields)
	}
}

func TestPowerActionBuildsScript(t *testing.T) {
	p, f := withFake(t)
	id := "11111111-1111-1111-1111-111111111111"
	res, err := p.Do(context.Background(), provider.Action{Verb: "stop", Kind: provider.KindVM, Target: id})
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !res.OK {
		t.Errorf("expected OK, got %+v", res)
	}
	if !strings.Contains(f.lastScript, "Get-VM -Id '"+id+"'") || !strings.Contains(f.lastScript, "Stop-VM -Force") {
		t.Errorf("unexpected power script: %q", f.lastScript)
	}
}

func TestDoRejectsNonGUID(t *testing.T) {
	p, _ := withFake(t)
	if _, err := p.Do(context.Background(), provider.Action{Verb: "start", Target: "web01; Remove-VM"}); err == nil {
		t.Error("expected rejection of non-GUID target (injection guard)")
	}
}

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		in        string
		wantHost  string
		wantPort  int
		wantHTTPS bool
	}{
		{"host", "host", 5985, false},
		{"host:5985", "host", 5985, false},
		{"https://host:5986", "host", 5986, true},
		{"host:5986", "host", 5986, true},
		{"http://host", "host", 5985, false},
	}
	for _, c := range cases {
		host, port, https, err := parseEndpoint(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if host != c.wantHost || port != c.wantPort || https != c.wantHTTPS {
			t.Errorf("%s -> host=%q port=%d https=%v; want %q/%d/%v", c.in, host, port, https, c.wantHost, c.wantPort, c.wantHTTPS)
		}
	}
}
