// Package caddy implements the provider.Provider contract for the Caddy web
// server's Admin API (DESIGN.md §5). Per the design decision it manages both the
// live configuration (via /config and /load) and an on-disk JSON config file, and
// surfaces drift between the two.
package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func init() {
	provider.Register("caddy", func() provider.Provider { return New() })
}

// Caddy is a Caddy Admin API provider instance.
type Caddy struct {
	base       string
	configFile string // optional on-disk JSON config path (from Extra["config_file"])
	http       *http.Client
}

// New returns an unconnected provider.
func New() *Caddy { return &Caddy{} }

func (c *Caddy) Type() string { return "caddy" }

func (c *Caddy) Capabilities() []provider.Capability {
	return []provider.Capability{provider.CapRoutes}
}

func (c *Caddy) Kinds() []provider.Kind {
	return []provider.Kind{provider.KindRoute}
}

func (c *Caddy) Connect(ctx context.Context, cfg provider.ConnConfig) error {
	base := strings.TrimRight(cfg.Endpoint, "/")
	if base == "" {
		base = "http://localhost:2019"
	}
	c.base = base
	if cfg.Extra != nil {
		c.configFile = cfg.Extra["config_file"]
	}
	c.http = &http.Client{Timeout: 30 * time.Second}
	return c.Ping(ctx)
}

// Ping verifies the Admin API is reachable.
func (c *Caddy) Ping(ctx context.Context) error {
	if c.http == nil {
		return fmt.Errorf("caddy: not connected")
	}
	return c.getJSON(ctx, "/config/", nil)
}

func (c *Caddy) Close() error {
	c.http = nil
	return nil
}

func (c *Caddy) List(ctx context.Context, kind provider.Kind) ([]provider.Resource, error) {
	if c.http == nil {
		return nil, fmt.Errorf("caddy: not connected")
	}
	if kind != provider.KindRoute {
		return nil, fmt.Errorf("caddy: unsupported kind %q", kind)
	}
	servers, err := c.servers(ctx)
	if err != nil {
		return nil, err
	}
	drift, _ := c.drifted(ctx) // best-effort; drift status is informational

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	var res []provider.Resource
	for _, sName := range names {
		srv := servers[sName]
		for i, r := range srv.Routes {
			hosts := r.hosts()
			name := strings.Join(hosts, ", ")
			if name == "" {
				name = fmt.Sprintf("%s[%d]", sName, i)
			}
			status := provider.StatusOK
			if drift {
				status = provider.StatusDegraded // live config differs from on-disk file
			}
			res = append(res, provider.Resource{
				ID:     fmt.Sprintf("%s/%d", sName, i),
				Kind:   provider.KindRoute,
				Name:   name,
				Status: status,
				Parent: sName,
				Fields: map[string]string{
					"listen":    strings.Join(srv.Listen, ","),
					"upstreams": strings.Join(r.upstreams(), ", "),
					"handler":   r.handlerKind(),
				},
				Raw: r,
			})
		}
	}
	return res, nil
}

func (c *Caddy) Get(ctx context.Context, kind provider.Kind, id string) (provider.Resource, error) {
	items, err := c.List(ctx, kind)
	if err != nil {
		return provider.Resource{}, err
	}
	for _, r := range items {
		if r.ID == id {
			return r, nil
		}
	}
	return provider.Resource{}, fmt.Errorf("caddy: route %q not found", id)
}

// Do supports configuration-level operations bridging the live API and disk:
//   - persist: write the live config to the on-disk JSON file (live → disk)
//   - load:    POST the on-disk file to /load (disk → live)
//   - diff:    report whether live and on-disk configs differ
func (c *Caddy) Do(ctx context.Context, a provider.Action) (provider.ActionResult, error) {
	if c.http == nil {
		return provider.ActionResult{}, fmt.Errorf("caddy: not connected")
	}
	switch a.Verb {
	case "persist":
		return c.persist(ctx)
	case "load", "reload":
		return c.load(ctx)
	case "diff":
		drift, err := c.drifted(ctx)
		if err != nil {
			return provider.ActionResult{}, err
		}
		if drift {
			return provider.ActionResult{OK: true, Message: "live config differs from " + c.configFile}, nil
		}
		return provider.ActionResult{OK: true, Message: "live config matches on-disk file"}, nil
	default:
		return provider.ActionResult{}, fmt.Errorf("caddy: unsupported verb %q", a.Verb)
	}
}

func (c *Caddy) persist(ctx context.Context) (provider.ActionResult, error) {
	if c.configFile == "" {
		return provider.ActionResult{}, fmt.Errorf("caddy: no config_file configured")
	}
	raw, err := c.rawConfig(ctx)
	if err != nil {
		return provider.ActionResult{}, err
	}
	if err := os.WriteFile(c.configFile, indentJSON(raw), 0o600); err != nil {
		return provider.ActionResult{}, err
	}
	return provider.ActionResult{OK: true, Message: "live config written to " + c.configFile}, nil
}

func (c *Caddy) load(ctx context.Context) (provider.ActionResult, error) {
	if c.configFile == "" {
		return provider.ActionResult{}, fmt.Errorf("caddy: no config_file configured")
	}
	data, err := os.ReadFile(c.configFile)
	if err != nil {
		return provider.ActionResult{}, err
	}
	if err := c.postJSON(ctx, "/load", data); err != nil {
		return provider.ActionResult{}, err
	}
	return provider.ActionResult{OK: true, Message: "loaded " + c.configFile + " into live config"}, nil
}

// drifted reports whether the live config differs from the on-disk file. With no
// config file configured, there can be no drift.
func (c *Caddy) drifted(ctx context.Context) (bool, error) {
	if c.configFile == "" {
		return false, nil
	}
	live, err := c.rawConfig(ctx)
	if err != nil {
		return false, err
	}
	disk, err := os.ReadFile(c.configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil // file missing counts as drift
		}
		return false, err
	}
	return !jsonEqual(live, disk), nil
}

// --- HTTP helpers ---

func (c *Caddy) servers(ctx context.Context) (map[string]server, error) {
	servers := map[string]server{}
	if err := c.getJSON(ctx, "/config/apps/http/servers", &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

func (c *Caddy) rawConfig(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/config/", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("caddy: GET /config/: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

func (c *Caddy) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("caddy: GET %s: HTTP %d", path, resp.StatusCode)
	}
	if out == nil || len(bytes.TrimSpace(body)) == 0 || string(bytes.TrimSpace(body)) == "null" {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (c *Caddy) postJSON(ctx context.Context, path string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("caddy: POST %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}
