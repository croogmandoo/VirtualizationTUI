// Package technitium implements the provider.Provider contract for a Technitium
// DNS Server (DESIGN.md §5). It manages authoritative zones and their records over
// the Technitium HTTP API, authenticating with an API token.
package technitium

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func init() {
	provider.Register("technitium", func() provider.Provider { return New() })
}

// Technitium is a Technitium DNS provider instance.
type Technitium struct {
	base  string
	token string
	http  *http.Client
}

// New returns an unconnected provider.
func New() *Technitium { return &Technitium{} }

func (t *Technitium) Type() string { return "technitium" }

func (t *Technitium) Capabilities() []provider.Capability {
	return []provider.Capability{provider.CapDNSRecords}
}

func (t *Technitium) Kinds() []provider.Kind {
	return []provider.Kind{provider.KindDNSZone, provider.KindDNSRecord}
}

func (t *Technitium) Connect(ctx context.Context, cfg provider.ConnConfig) error {
	base := strings.TrimRight(cfg.Endpoint, "/")
	if base == "" {
		return fmt.Errorf("technitium: endpoint is required")
	}
	if cfg.Token == "" {
		return fmt.Errorf("technitium: API token is required")
	}
	t.base = base
	t.token = cfg.Token
	t.http = &http.Client{Timeout: 30 * time.Second}
	return t.Ping(ctx)
}

// Ping verifies connectivity/credentials by listing zones.
func (t *Technitium) Ping(ctx context.Context) error {
	if t.http == nil {
		return fmt.Errorf("technitium: not connected")
	}
	var out zonesList
	return t.call(ctx, "/api/zones/list", nil, &out)
}

func (t *Technitium) Close() error {
	t.http = nil
	return nil
}

func (t *Technitium) List(ctx context.Context, kind provider.Kind) ([]provider.Resource, error) {
	if t.http == nil {
		return nil, fmt.Errorf("technitium: not connected")
	}
	switch kind {
	case provider.KindDNSZone:
		return t.listZones(ctx)
	case provider.KindDNSRecord:
		return t.listAllRecords(ctx)
	default:
		return nil, fmt.Errorf("technitium: unsupported kind %q", kind)
	}
}

func (t *Technitium) listZones(ctx context.Context) ([]provider.Resource, error) {
	var out zonesList
	if err := t.call(ctx, "/api/zones/list", nil, &out); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(out.Zones))
	for _, z := range out.Zones {
		status := provider.StatusOK
		if z.Disabled {
			status = provider.StatusStopped
		}
		res = append(res, provider.Resource{
			ID:     z.Name,
			Kind:   provider.KindDNSZone,
			Name:   z.Name,
			Status: status,
			Fields: map[string]string{
				"type":   z.Type,
				"dnssec": z.DnssecStatus,
			},
			Raw: z,
		})
	}
	return res, nil
}

// listAllRecords enumerates records across every zone, tagging each with its zone
// via Parent so the table can group them.
func (t *Technitium) listAllRecords(ctx context.Context) ([]provider.Resource, error) {
	var zl zonesList
	if err := t.call(ctx, "/api/zones/list", nil, &zl); err != nil {
		return nil, err
	}
	var res []provider.Resource
	for _, z := range zl.Zones {
		recs, err := t.zoneRecords(ctx, z.Name)
		if err != nil {
			return nil, err
		}
		res = append(res, recs...)
	}
	return res, nil
}

func (t *Technitium) zoneRecords(ctx context.Context, zone string) ([]provider.Resource, error) {
	q := url.Values{"domain": {zone}, "zone": {zone}, "listZone": {"true"}}
	var out recordsGet
	if err := t.call(ctx, "/api/zones/records/get", q, &out); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(out.Records))
	for _, r := range out.Records {
		res = append(res, provider.Resource{
			ID:     recordID(zone, r),
			Kind:   provider.KindDNSRecord,
			Name:   r.Name,
			Status: statusFromDisabled(r.Disabled),
			Parent: zone,
			Fields: map[string]string{
				"type": r.Type,
				"ttl":  strconv.Itoa(r.TTL),
				"data": r.RData.summary(),
			},
			Raw: r,
		})
	}
	return res, nil
}

func (t *Technitium) Get(ctx context.Context, kind provider.Kind, id string) (provider.Resource, error) {
	items, err := t.List(ctx, kind)
	if err != nil {
		return provider.Resource{}, err
	}
	for _, r := range items {
		if r.ID == id {
			return r, nil
		}
	}
	return provider.Resource{}, fmt.Errorf("technitium: %s/%s not found", kind, id)
}

// Do supports zone and record mutations. Record verbs require zone/type/name/data
// in Params; the create/delete record endpoints are form-style query params.
func (t *Technitium) Do(ctx context.Context, a provider.Action) (provider.ActionResult, error) {
	if t.http == nil {
		return provider.ActionResult{}, fmt.Errorf("technitium: not connected")
	}
	switch a.Verb {
	case "create_zone":
		q := url.Values{"zone": {a.Target}, "type": {paramOr(a.Params, "type", "Primary")}}
		if err := t.call(ctx, "/api/zones/create", q, nil); err != nil {
			return provider.ActionResult{}, err
		}
		return provider.ActionResult{OK: true, Message: "zone " + a.Target + " created"}, nil
	case "delete_zone":
		q := url.Values{"zone": {a.Target}}
		if err := t.call(ctx, "/api/zones/delete", q, nil); err != nil {
			return provider.ActionResult{}, err
		}
		return provider.ActionResult{OK: true, Message: "zone " + a.Target + " deleted"}, nil
	case "create_record", "delete_record":
		return t.recordMutation(ctx, a)
	default:
		return provider.ActionResult{}, fmt.Errorf("technitium: unsupported verb %q", a.Verb)
	}
}

func (t *Technitium) recordMutation(ctx context.Context, a provider.Action) (provider.ActionResult, error) {
	zone := paramOr(a.Params, "zone", a.Target)
	domain := paramOr(a.Params, "domain", zone)
	rtype := paramOr(a.Params, "type", "A")
	q := url.Values{"domain": {domain}, "zone": {zone}, "type": {rtype}}
	if ttl := paramOr(a.Params, "ttl", ""); ttl != "" {
		q.Set("ttl", ttl)
	}
	// Map the record value to the parameter Technitium expects for its type.
	if data := paramOr(a.Params, "data", ""); data != "" {
		switch strings.ToUpper(rtype) {
		case "A", "AAAA":
			q.Set("ipAddress", data)
		case "CNAME":
			q.Set("cname", data)
		case "TXT":
			q.Set("text", data)
		case "NS":
			q.Set("nameServer", data)
		case "PTR":
			q.Set("ptrName", data)
		default:
			q.Set("rdata", data)
		}
	}
	path := "/api/zones/records/add"
	verb := "added"
	if a.Verb == "delete_record" {
		path = "/api/zones/records/delete"
		verb = "deleted"
	}
	if err := t.call(ctx, path, q, nil); err != nil {
		return provider.ActionResult{}, err
	}
	return provider.ActionResult{OK: true, Message: fmt.Sprintf("%s record %s %s", rtype, domain, verb)}, nil
}

// call performs a GET against the Technitium API, injecting the token, and decodes
// the {status,response} envelope into out (which may be nil).
func (t *Technitium) call(ctx context.Context, path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("token", t.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("technitium: %s: HTTP %d", path, resp.StatusCode)
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("technitium: decode %s: %w", path, err)
	}
	if env.Status != "ok" {
		msg := env.ErrorMessage
		if msg == "" {
			msg = env.Status
		}
		return fmt.Errorf("technitium: %s: %s", path, msg)
	}
	if out == nil || len(env.Response) == 0 {
		return nil
	}
	return json.Unmarshal(env.Response, out)
}

func paramOr(p map[string]any, key, def string) string {
	if p != nil {
		if v, ok := p[key].(string); ok && v != "" {
			return v
		}
	}
	return def
}

func statusFromDisabled(disabled bool) provider.Status {
	if disabled {
		return provider.StatusStopped
	}
	return provider.StatusOK
}

func recordID(zone string, r record) string {
	return fmt.Sprintf("%s|%s|%s|%s", zone, r.Name, r.Type, r.RData.summary())
}
