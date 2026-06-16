// Package unraid implements the provider.Provider contract for Unraid via its
// official GraphQL API (the Unraid Connect / Unraid API plugin, DESIGN.md §5).
// It authenticates with an API key (x-api-key header) and exposes the array and
// its disks, Docker containers, VMs and user shares.
//
// The GraphQL queries/mutations below target the documented Unraid API schema.
// They are exercised against fixtures in tests; when wiring a live server, field
// names may need minor adjustment for your Unraid version.
package unraid

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
	provider.Register("unraid", func() provider.Provider { return New() })
}

// Unraid is an Unraid provider instance.
type Unraid struct {
	endpoint string // full GraphQL endpoint, e.g. http://tower/graphql
	key      string // API key
	http     *http.Client
}

// New returns an unconnected provider.
func New() *Unraid { return &Unraid{} }

func (u *Unraid) Type() string { return "unraid" }

func (u *Unraid) Capabilities() []provider.Capability {
	return []provider.Capability{provider.CapPowerControl}
}

func (u *Unraid) Kinds() []provider.Kind {
	return []provider.Kind{provider.KindStorage, provider.KindContainer, provider.KindVM, provider.KindShare}
}

func (u *Unraid) Connect(ctx context.Context, cfg provider.ConnConfig) error {
	ep := strings.TrimRight(cfg.Endpoint, "/")
	if ep == "" {
		return fmt.Errorf("unraid: endpoint is required")
	}
	if !strings.HasSuffix(ep, "/graphql") {
		ep += "/graphql"
	}
	if cfg.Token == "" {
		return fmt.Errorf("unraid: API key is required")
	}
	u.endpoint = ep
	u.key = cfg.Token
	u.http = &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig(cfg.TLS)},
	}
	return u.Ping(ctx)
}

func tlsConfig(t provider.TLSConfig) *tls.Config {
	if t.Insecure {
		return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit per-connection opt-out
	}
	return &tls.Config{}
}

// Ping verifies connectivity/credentials with a minimal query.
func (u *Unraid) Ping(ctx context.Context) error {
	if u.http == nil {
		return fmt.Errorf("unraid: not connected")
	}
	var out struct {
		Array struct {
			State string `json:"state"`
		} `json:"array"`
	}
	return u.query(ctx, `query { array { state } }`, nil, &out)
}

func (u *Unraid) Close() error {
	u.http = nil
	return nil
}

func (u *Unraid) List(ctx context.Context, kind provider.Kind) ([]provider.Resource, error) {
	if u.http == nil {
		return nil, fmt.Errorf("unraid: not connected")
	}
	switch kind {
	case provider.KindStorage:
		return u.listArray(ctx)
	case provider.KindContainer:
		return u.listDocker(ctx)
	case provider.KindVM:
		return u.listVMs(ctx)
	case provider.KindShare:
		return u.listShares(ctx)
	default:
		return nil, fmt.Errorf("unraid: unsupported kind %q", kind)
	}
}

func (u *Unraid) listArray(ctx context.Context) ([]provider.Resource, error) {
	var out struct {
		Array struct {
			State string      `json:"state"`
			Disks []arrayDisk `json:"disks"`
		} `json:"array"`
	}
	if err := u.query(ctx, `query { array { state disks { name size status type } } }`, nil, &out); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(out.Array.Disks))
	for _, d := range out.Array.Disks {
		res = append(res, provider.Resource{
			ID:     d.Name,
			Kind:   provider.KindStorage,
			Name:   d.Name,
			Status: mapDiskStatus(d.Status),
			Parent: "array(" + out.Array.State + ")",
			Fields: map[string]string{
				"type":   strings.ToLower(d.Type),
				"size":   humanKBytes(d.Size),
				"status": d.Status,
			},
			Raw: d,
		})
	}
	return res, nil
}

func (u *Unraid) listDocker(ctx context.Context) ([]provider.Resource, error) {
	var out struct {
		Docker struct {
			Containers []container `json:"containers"`
		} `json:"docker"`
	}
	if err := u.query(ctx, `query { docker { containers { id names image state } } }`, nil, &out); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(out.Docker.Containers))
	for _, c := range out.Docker.Containers {
		res = append(res, provider.Resource{
			ID:     c.ID,
			Kind:   provider.KindContainer,
			Name:   c.displayName(),
			Status: mapContainerState(c.State),
			Parent: "docker",
			Fields: map[string]string{
				"image": c.Image,
				"state": strings.ToLower(c.State),
			},
			Raw: c,
		})
	}
	return res, nil
}

func (u *Unraid) listVMs(ctx context.Context) ([]provider.Resource, error) {
	var out struct {
		VMs struct {
			Domains []domain `json:"domains"`
		} `json:"vms"`
	}
	if err := u.query(ctx, `query { vms { domains { uuid name state } } }`, nil, &out); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(out.VMs.Domains))
	for _, d := range out.VMs.Domains {
		res = append(res, provider.Resource{
			ID:     d.UUID,
			Kind:   provider.KindVM,
			Name:   d.Name,
			Status: mapVMState(d.State),
			Parent: "vms",
			Fields: map[string]string{"state": strings.ToLower(d.State)},
			Raw:    d,
		})
	}
	return res, nil
}

func (u *Unraid) listShares(ctx context.Context) ([]provider.Resource, error) {
	var out struct {
		Shares []share `json:"shares"`
	}
	if err := u.query(ctx, `query { shares { name free size } }`, nil, &out); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(out.Shares))
	for _, s := range out.Shares {
		res = append(res, provider.Resource{
			ID:     s.Name,
			Kind:   provider.KindShare,
			Name:   s.Name,
			Status: provider.StatusOK,
			Parent: "shares",
			Fields: map[string]string{
				"size": humanKBytes(s.Size),
				"free": humanKBytes(s.Free),
			},
			Raw: s,
		})
	}
	return res, nil
}

func (u *Unraid) Get(ctx context.Context, kind provider.Kind, id string) (provider.Resource, error) {
	items, err := u.List(ctx, kind)
	if err != nil {
		return provider.Resource{}, err
	}
	for _, r := range items {
		if r.ID == id {
			return r, nil
		}
	}
	return provider.Resource{}, fmt.Errorf("unraid: %s/%s not found", kind, id)
}

// Do starts/stops Docker containers and VMs via GraphQL mutations. The target is
// the container id (KindContainer) or VM uuid (KindVM).
func (u *Unraid) Do(ctx context.Context, a provider.Action) (provider.ActionResult, error) {
	if u.http == nil {
		return provider.ActionResult{}, fmt.Errorf("unraid: not connected")
	}
	var mutation, field string
	switch {
	case a.Kind == provider.KindContainer && a.Verb == "start":
		mutation, field = `mutation($id:String!){ docker { start(id:$id){ id state } } }`, "start container"
	case a.Kind == provider.KindContainer && a.Verb == "stop":
		mutation, field = `mutation($id:String!){ docker { stop(id:$id){ id state } } }`, "stop container"
	case a.Kind == provider.KindVM && a.Verb == "start":
		mutation, field = `mutation($id:String!){ vm { start(id:$id) } }`, "start vm"
	case a.Kind == provider.KindVM && a.Verb == "stop":
		mutation, field = `mutation($id:String!){ vm { stop(id:$id) } }`, "stop vm"
	default:
		return provider.ActionResult{}, fmt.Errorf("unraid: unsupported %s on %s", a.Verb, a.Kind)
	}
	if err := u.query(ctx, mutation, map[string]any{"id": a.Target}, nil); err != nil {
		return provider.ActionResult{}, err
	}
	return provider.ActionResult{OK: true, Message: fmt.Sprintf("%s %s", field, a.Target)}, nil
}

// query executes a GraphQL operation and decodes data into out (which may be nil).
func (u *Unraid) query(ctx context.Context, op string, vars map[string]any, out any) error {
	reqBody, err := json.Marshal(map[string]any{"query": op, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", u.key)

	resp, err := u.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unraid: graphql: HTTP %d: %s", resp.StatusCode, firstLine(string(data)))
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("unraid: decode graphql: %w", err)
	}
	if len(env.Errors) > 0 {
		return fmt.Errorf("unraid: graphql error: %s", env.Errors[0].Message)
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
