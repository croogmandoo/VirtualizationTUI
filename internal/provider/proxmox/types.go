package proxmox

import (
	"fmt"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

// nodeEntry is an item from GET /nodes.
type nodeEntry struct {
	Node   string  `json:"node"`
	Status string  `json:"status"` // "online" / "offline" / "unknown"
	CPU    float64 `json:"cpu"`    // fraction 0..1
	MaxCPU int     `json:"maxcpu"`
	Mem    int64   `json:"mem"`
	MaxMem int64   `json:"maxmem"`
	Uptime int64   `json:"uptime"` // seconds
}

// clusterResource is an item from GET /cluster/resources?type=vm.
type clusterResource struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"` // "qemu" / "lxc"
	Node     string  `json:"node"`
	VMID     int     `json:"vmid"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`   // "running" / "stopped"
	Template int     `json:"template"` // 1 for VM/CT templates (not runnable guests)
	CPU      float64 `json:"cpu"`      // fraction 0..1
	MaxCPU   float64 `json:"maxcpu"`
	Mem      int64   `json:"mem"`
	MaxMem   int64   `json:"maxmem"`
	Uptime   int64   `json:"uptime"`
}

// taskStatus is the result of GET /nodes/{node}/tasks/{upid}/status.
type taskStatus struct {
	Status     string `json:"status"`     // "running" / "stopped"
	ExitStatus string `json:"exitstatus"` // "OK" or an error string when stopped
}

// --- status mapping ---

func mapNodeStatus(s string) provider.Status {
	switch s {
	case "online":
		return provider.StatusOK
	case "offline":
		return provider.StatusError
	default:
		return provider.StatusUnknown
	}
}

func mapGuestStatus(s string) provider.Status {
	switch s {
	case "running":
		return provider.StatusRunning
	case "stopped":
		return provider.StatusStopped
	case "paused", "suspended":
		return provider.StatusPaused
	default:
		return provider.StatusUnknown
	}
}

// --- field formatting ---

func formatPct(fraction float64) string {
	return fmt.Sprintf("%.0f%%", fraction*100)
}

const (
	kib = 1024
	mib = 1024 * kib
	gib = 1024 * mib
)

func formatMem(used, max int64) string {
	if max <= 0 {
		return humanBytes(used)
	}
	return fmt.Sprintf("%s/%s", humanBytes(used), humanBytes(max))
}

func humanBytes(b int64) string {
	switch {
	case b >= gib:
		return fmt.Sprintf("%.1fG", float64(b)/float64(gib))
	case b >= mib:
		return fmt.Sprintf("%.0fM", float64(b)/float64(mib))
	case b >= kib:
		return fmt.Sprintf("%.0fK", float64(b)/float64(kib))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

func formatUptime(sec int64) string {
	if sec <= 0 {
		return "-"
	}
	d := sec / 86400
	h := (sec % 86400) / 3600
	switch {
	case d > 0:
		return fmt.Sprintf("%dd%dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", sec/60)
	}
}

// --- metrics (point-in-time seed; the scheduler accumulates history) ---

func nodeMetrics(n nodeEntry) []provider.Metric {
	memPct := 0.0
	if n.MaxMem > 0 {
		memPct = float64(n.Mem) / float64(n.MaxMem) * 100
	}
	return []provider.Metric{
		{Name: "cpu", Value: n.CPU * 100, Unit: "%", History: []float64{n.CPU * 100}},
		{Name: "mem", Value: memPct, Unit: "%", History: []float64{memPct}},
	}
}

func guestMetrics(r clusterResource) []provider.Metric {
	memPct := 0.0
	if r.MaxMem > 0 {
		memPct = float64(r.Mem) / float64(r.MaxMem) * 100
	}
	return []provider.Metric{
		{Name: "cpu", Value: r.CPU * 100, Unit: "%", History: []float64{r.CPU * 100}},
		{Name: "mem", Value: memPct, Unit: "%", History: []float64{memPct}},
	}
}
