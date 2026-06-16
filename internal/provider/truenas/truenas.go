// Package truenas implements the provider.Provider contract for TrueNAS
// (SCALE/CORE) via its REST API v2.0 (DESIGN.md §5). It exposes storage pools,
// datasets and shares, authenticating with an API key (Bearer token).
package truenas

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func init() {
	provider.Register("truenas", func() provider.Provider { return New() })
}

// TrueNAS is a TrueNAS provider instance.
type TrueNAS struct {
	base string // e.g. https://nas.local/api/v2.0
	key  string // API key
	http *http.Client
}

// New returns an unconnected provider.
func New() *TrueNAS { return &TrueNAS{} }

func (n *TrueNAS) Type() string { return "truenas" }

func (n *TrueNAS) Capabilities() []provider.Capability {
	return []provider.Capability{provider.CapMetrics}
}

func (n *TrueNAS) Kinds() []provider.Kind {
	return []provider.Kind{provider.KindStorage, provider.KindDataset, provider.KindShare}
}

func (n *TrueNAS) Connect(ctx context.Context, cfg provider.ConnConfig) error {
	base := strings.TrimRight(cfg.Endpoint, "/")
	if base == "" {
		return fmt.Errorf("truenas: endpoint is required")
	}
	if !strings.HasSuffix(base, "/api/v2.0") {
		base += "/api/v2.0"
	}
	if cfg.Token == "" {
		return fmt.Errorf("truenas: API key is required")
	}
	n.base = base
	n.key = cfg.Token
	n.http = &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig(cfg.TLS)},
	}
	return n.Ping(ctx)
}

func tlsConfig(t provider.TLSConfig) *tls.Config {
	if t.Insecure {
		return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit per-connection opt-out
	}
	return &tls.Config{}
}

// Ping verifies connectivity/credentials via the system/info endpoint.
func (n *TrueNAS) Ping(ctx context.Context) error {
	if n.http == nil {
		return fmt.Errorf("truenas: not connected")
	}
	return n.do(ctx, http.MethodGet, "/system/info", nil, nil)
}

func (n *TrueNAS) Close() error {
	n.http = nil
	return nil
}

func (n *TrueNAS) List(ctx context.Context, kind provider.Kind) ([]provider.Resource, error) {
	if n.http == nil {
		return nil, fmt.Errorf("truenas: not connected")
	}
	switch kind {
	case provider.KindStorage:
		return n.listPools(ctx)
	case provider.KindDataset:
		return n.listDatasets(ctx)
	case provider.KindShare:
		return n.listShares(ctx)
	default:
		return nil, fmt.Errorf("truenas: unsupported kind %q", kind)
	}
}

func (n *TrueNAS) listPools(ctx context.Context) ([]provider.Resource, error) {
	var pools []pool
	if err := n.do(ctx, http.MethodGet, "/pool", nil, &pools); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(pools))
	for _, p := range pools {
		usedPct := pct(p.Allocated, p.Size)
		res = append(res, provider.Resource{
			ID:     fmt.Sprintf("%d", p.ID),
			Kind:   provider.KindStorage,
			Name:   p.Name,
			Status: mapPoolStatus(p.Status, p.Healthy),
			Fields: map[string]string{
				"size": humanBytes(p.Size),
				"used": fmt.Sprintf("%s (%.0f%%)", humanBytes(p.Allocated), usedPct),
				"free": humanBytes(p.Free),
			},
			Metrics: []provider.Metric{{Name: "used", Value: usedPct, Unit: "%", History: []float64{usedPct}}},
			Raw:     p,
		})
	}
	return res, nil
}

func (n *TrueNAS) listDatasets(ctx context.Context) ([]provider.Resource, error) {
	var ds []dataset
	if err := n.do(ctx, http.MethodGet, "/pool/dataset", nil, &ds); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(ds))
	for _, d := range ds {
		used := d.Used.Parsed
		avail := d.Available.Parsed
		res = append(res, provider.Resource{
			ID:     d.ID,
			Kind:   provider.KindDataset,
			Name:   d.Name,
			Status: provider.StatusOK,
			Parent: poolOf(d.Name),
			Fields: map[string]string{
				"type":      d.Type,
				"used":      humanBytes(used),
				"available": humanBytes(avail),
			},
			Raw: d,
		})
	}
	return res, nil
}

func (n *TrueNAS) listShares(ctx context.Context) ([]provider.Resource, error) {
	var res []provider.Resource

	var smb []smbShare
	if err := n.do(ctx, http.MethodGet, "/sharing/smb", nil, &smb); err != nil {
		return nil, err
	}
	for _, s := range smb {
		name := s.Name
		if name == "" {
			name = s.Path
		}
		res = append(res, shareResource("smb", s.ID, name, s.Path, s.Enabled))
	}

	var nfs []nfsShare
	if err := n.do(ctx, http.MethodGet, "/sharing/nfs", nil, &nfs); err != nil {
		return nil, err
	}
	for _, s := range nfs {
		res = append(res, shareResource("nfs", s.ID, s.Path, s.Path, s.Enabled))
	}
	return res, nil
}

func shareResource(proto string, id int, name, path string, enabled bool) provider.Resource {
	status := provider.StatusOK
	if !enabled {
		status = provider.StatusStopped
	}
	return provider.Resource{
		ID:     fmt.Sprintf("%s:%d", proto, id),
		Kind:   provider.KindShare,
		Name:   name,
		Status: status,
		Parent: strings.ToUpper(proto),
		Fields: map[string]string{
			"proto":   proto,
			"path":    path,
			"enabled": fmt.Sprintf("%t", enabled),
		},
	}
}

func (n *TrueNAS) Get(ctx context.Context, kind provider.Kind, id string) (provider.Resource, error) {
	items, err := n.List(ctx, kind)
	if err != nil {
		return provider.Resource{}, err
	}
	for _, r := range items {
		if r.ID == id {
			return r, nil
		}
	}
	return provider.Resource{}, fmt.Errorf("truenas: %s/%s not found", kind, id)
}

// Do supports enabling/disabling shares. The target encodes the protocol and id
// as "smb:123" / "nfs:7" (as produced by listShares).
func (n *TrueNAS) Do(ctx context.Context, a provider.Action) (provider.ActionResult, error) {
	if n.http == nil {
		return provider.ActionResult{}, fmt.Errorf("truenas: not connected")
	}
	switch a.Verb {
	case "enable_share", "disable_share":
		proto, id, ok := strings.Cut(a.Target, ":")
		if !ok || (proto != "smb" && proto != "nfs") {
			return provider.ActionResult{}, fmt.Errorf("truenas: bad share target %q (want smb:ID or nfs:ID)", a.Target)
		}
		enabled := a.Verb == "enable_share"
		body, _ := json.Marshal(map[string]any{"enabled": enabled})
		if err := n.do(ctx, http.MethodPut, fmt.Sprintf("/sharing/%s/id/%s", proto, id), body, nil); err != nil {
			return provider.ActionResult{}, err
		}
		state := "disabled"
		if enabled {
			state = "enabled"
		}
		return provider.ActionResult{OK: true, Message: fmt.Sprintf("%s share %s %s", proto, id, state)}, nil
	default:
		return provider.ActionResult{}, fmt.Errorf("truenas: unsupported verb %q", a.Verb)
	}
}

// do performs a request with Bearer auth and decodes a JSON response into out.
func (n *TrueNAS) do(ctx context.Context, method, path string, body []byte, out any) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, n.base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+n.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("truenas: %s %s: HTTP %d: %s", method, path, resp.StatusCode, firstLine(string(data)))
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
