package proxmox

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

// TestLiveProxmox is an opt-in smoke test against a real Proxmox VE host. It is
// skipped unless PVE_ENDPOINT and PVE_TOKEN are set, so it stays a no-op in CI
// and only runs from a machine that can actually reach the host.
//
// Read-only by default — it connects, pings, and lists nodes / VMs / containers,
// logging what it found and flagging anything that decodes oddly (missing
// id/name, unmapped status). To additionally exercise a power action and task
// polling, point PVE_TEST_VMID at a throwaway guest and set PVE_TEST_ACTION.
//
// Example (run from a host on the same LAN as the Proxmox box):
//
//	PVE_ENDPOINT=https://172.20.200.250:8006 \
//	PVE_TOKEN='root@pam!tui=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx' \
//	PVE_INSECURE=1 \
//	go test ./internal/provider/proxmox -run TestLiveProxmox -v
//
// Pin the self-signed certificate with PVE_FINGERPRINT (a SHA-256 hex, with or
// without colons) instead of PVE_INSECURE=1 for a verified connection.
func TestLiveProxmox(t *testing.T) {
	endpoint := os.Getenv("PVE_ENDPOINT")
	token := os.Getenv("PVE_TOKEN")
	if endpoint == "" || token == "" {
		t.Skip("set PVE_ENDPOINT and PVE_TOKEN to run the live Proxmox smoke test")
	}

	cfg := provider.ConnConfig{
		Name:     "live",
		Endpoint: endpoint,
		Token:    token,
		TLS: provider.TLSConfig{
			Fingerprint: os.Getenv("PVE_FINGERPRINT"),
			Insecure:    os.Getenv("PVE_INSECURE") == "1",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p := New()
	if err := p.Connect(ctx, cfg); err != nil {
		t.Fatalf("connect/ping failed: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	for _, kind := range []provider.Kind{provider.KindNode, provider.KindVM, provider.KindContainer} {
		rs, err := p.List(ctx, kind)
		if err != nil {
			t.Fatalf("list %s: %v", kind, err)
		}
		t.Logf("%s: %d resource(s)", kind, len(rs))
		for _, r := range rs {
			if r.ID == "" || r.Name == "" {
				t.Errorf("%s resource missing id/name: %+v", kind, r)
			}
			if r.Status == provider.StatusUnknown {
				t.Errorf("%s %q has an unmapped status (Raw=%+v) — extend the status mapping", kind, r.Name, r.Raw)
			}
			t.Logf("  %-10s id=%-6s %-16s %-8s cpu=%s mem=%s", r.Kind, r.ID, r.Name, r.Status, r.Fields["cpu"], r.Fields["mem"])
		}
	}

	// Optional, opt-in mutation against a throwaway guest.
	vmid := os.Getenv("PVE_TEST_VMID")
	verb := os.Getenv("PVE_TEST_ACTION")
	if vmid == "" || verb == "" {
		t.Log("read-only run complete; set PVE_TEST_VMID + PVE_TEST_ACTION (e.g. reboot) to also test a power action")
		return
	}

	res, err := p.Do(ctx, provider.Action{Verb: verb, Target: vmid})
	if err != nil {
		t.Fatalf("action %s on %s: %v", verb, vmid, err)
	}
	t.Logf("action queued: %s (task %s)", res.Message, res.TaskID)
	if res.TaskID == "" {
		return
	}

	// Poll the task to completion (bounded).
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		st, err := p.TaskStatus(ctx, res.TaskID)
		if err != nil {
			t.Fatalf("task status: %v", err)
		}
		t.Logf("task state: %s", st)
		if st != provider.TaskRunning {
			if st == provider.TaskFailed {
				t.Errorf("task %s reported failure", res.TaskID)
			}
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Log("task still running after 60s; not waiting further")
}
