package truenas

import (
	"fmt"
	"strings"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

// pool mirrors the subset of GET /pool we surface.
type pool struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"` // ONLINE / DEGRADED / OFFLINE / ...
	Healthy   bool   `json:"healthy"`
	Size      int64  `json:"size"`
	Allocated int64  `json:"allocated"`
	Free      int64  `json:"free"`
}

// dataset mirrors the subset of GET /pool/dataset we surface. TrueNAS reports
// sizes as objects with a parsed numeric value.
type dataset struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // FILESYSTEM / VOLUME
	Used      sizeValue `json:"used"`
	Available sizeValue `json:"available"`
}

type sizeValue struct {
	Parsed int64 `json:"parsed"`
}

type smbShare struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

type nfsShare struct {
	ID      int    `json:"id"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

// --- mapping / formatting ---

func mapPoolStatus(status string, healthy bool) provider.Status {
	switch strings.ToUpper(status) {
	case "ONLINE":
		if healthy {
			return provider.StatusOK
		}
		return provider.StatusDegraded
	case "DEGRADED":
		return provider.StatusDegraded
	case "OFFLINE", "FAULTED", "UNAVAIL":
		return provider.StatusError
	default:
		return provider.StatusUnknown
	}
}

// poolOf returns the pool (root) component of a dataset name like "tank/media".
func poolOf(name string) string {
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return name
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
