package unraid

import (
	"fmt"
	"strings"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

type arrayDisk struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"` // kilobytes
	Status string `json:"status"`
	Type   string `json:"type"` // Data / Parity / Cache
}

type container struct {
	ID    string   `json:"id"`
	Names []string `json:"names"`
	Image string   `json:"image"`
	State string   `json:"state"` // RUNNING / EXITED
}

func (c container) displayName() string {
	if len(c.Names) > 0 && c.Names[0] != "" {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return c.ID
}

type domain struct {
	UUID  string `json:"uuid"`
	Name  string `json:"name"`
	State string `json:"state"` // RUNNING / SHUTOFF / PAUSED
}

type share struct {
	Name string `json:"name"`
	Free int64  `json:"free"` // kilobytes
	Size int64  `json:"size"` // kilobytes
}

// --- mapping / formatting ---

func mapDiskStatus(s string) provider.Status {
	switch strings.ToUpper(s) {
	case "DISK_OK", "OK":
		return provider.StatusOK
	case "DISK_DSBL", "DISK_INVALID", "DISK_WRONG":
		return provider.StatusError
	case "DISK_NP", "DISK_NP_MISSING":
		return provider.StatusStopped
	default:
		return provider.StatusUnknown
	}
}

func mapContainerState(s string) provider.Status {
	switch strings.ToUpper(s) {
	case "RUNNING":
		return provider.StatusRunning
	case "EXITED", "STOPPED":
		return provider.StatusStopped
	case "PAUSED":
		return provider.StatusPaused
	default:
		return provider.StatusUnknown
	}
}

func mapVMState(s string) provider.Status {
	switch strings.ToUpper(s) {
	case "RUNNING":
		return provider.StatusRunning
	case "SHUTOFF", "STOPPED":
		return provider.StatusStopped
	case "PAUSED":
		return provider.StatusPaused
	default:
		return provider.StatusUnknown
	}
}

// humanKBytes formats a kilobyte count (Unraid reports sizes in KiB).
func humanKBytes(kb int64) string {
	const (
		mb = 1024
		gb = 1024 * mb
		tb = 1024 * gb
	)
	switch {
	case kb >= tb:
		return fmt.Sprintf("%.1fT", float64(kb)/float64(tb))
	case kb >= gb:
		return fmt.Sprintf("%.1fG", float64(kb)/float64(gb))
	case kb >= mb:
		return fmt.Sprintf("%.0fM", float64(kb)/float64(mb))
	default:
		return fmt.Sprintf("%dK", kb)
	}
}
