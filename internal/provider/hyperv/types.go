package hyperv

import (
	"fmt"
	"strings"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

// vm mirrors the projected Get-VM properties (see vmSelect).
type vm struct {
	Name           string `json:"Name"`
	Id             string `json:"Id"`
	State          string `json:"State"` // Running / Off / Paused / Saved
	CPUUsage       int    `json:"CPUUsage"`
	MemoryAssigned int64  `json:"MemoryAssigned"` // bytes
	Uptime         string `json:"Uptime"`
	Status         string `json:"Status"`
}

// vmHost mirrors the projected Get-VMHost properties.
type vmHost struct {
	Name                  string `json:"Name"`
	LogicalProcessorCount int    `json:"LogicalProcessorCount"`
	MemoryCapacity        int64  `json:"MemoryCapacity"` // bytes
}

func mapVMState(s string) provider.Status {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "running":
		return provider.StatusRunning
	case "off", "stopped":
		return provider.StatusStopped
	case "paused":
		return provider.StatusPaused
	case "saved":
		return provider.StatusPaused
	default:
		return provider.StatusUnknown
	}
}

const (
	kib = 1024
	mib = 1024 * kib
	gib = 1024 * mib
	tib = 1024 * gib
)

func humanBytes(b int64) string {
	switch {
	case b >= tib:
		return fmt.Sprintf("%.1fT", float64(b)/float64(tib))
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
