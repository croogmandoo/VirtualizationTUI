package vsphere

import (
	"fmt"

	"github.com/vmware/govmomi/vim25/types"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func mapPowerState(s types.VirtualMachinePowerState) provider.Status {
	switch s {
	case types.VirtualMachinePowerStatePoweredOn:
		return provider.StatusRunning
	case types.VirtualMachinePowerStatePoweredOff:
		return provider.StatusStopped
	case types.VirtualMachinePowerStateSuspended:
		return provider.StatusPaused
	default:
		return provider.StatusUnknown
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func pct(part, whole int64) float64 {
	if whole <= 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
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
