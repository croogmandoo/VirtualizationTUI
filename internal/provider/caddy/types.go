package caddy

import (
	"bytes"
	"encoding/json"
)

// server mirrors the subset of a Caddy HTTP server we read from
// /config/apps/http/servers.
type server struct {
	Listen []string `json:"listen"`
	Routes []route  `json:"routes"`
}

// route mirrors the subset of a Caddy route we surface.
type route struct {
	Match  []match   `json:"match"`
	Handle []handler `json:"handle"`
}

type match struct {
	Host []string `json:"host"`
}

type handler struct {
	Handler   string     `json:"handler"`
	Upstreams []upstream `json:"upstreams"`
}

type upstream struct {
	Dial string `json:"dial"`
}

func (r route) hosts() []string {
	var hosts []string
	for _, m := range r.Match {
		hosts = append(hosts, m.Host...)
	}
	return hosts
}

func (r route) upstreams() []string {
	var ups []string
	for _, h := range r.Handle {
		for _, u := range h.Upstreams {
			if u.Dial != "" {
				ups = append(ups, u.Dial)
			}
		}
	}
	return ups
}

// handlerKind returns the primary handler name for the route (e.g. reverse_proxy).
func (r route) handlerKind() string {
	if len(r.Handle) > 0 && r.Handle[0].Handler != "" {
		return r.Handle[0].Handler
	}
	return "-"
}

// --- JSON normalization for drift detection ---

// jsonEqual reports whether two JSON documents are semantically equal, ignoring
// formatting and key ordering.
func jsonEqual(a, b []byte) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	an, err1 := json.Marshal(av)
	bn, err2 := json.Marshal(bv)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(an, bn)
}

// indentJSON pretty-prints JSON for on-disk storage; on failure it returns the
// input unchanged.
func indentJSON(raw []byte) []byte {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return raw
	}
	return buf.Bytes()
}
