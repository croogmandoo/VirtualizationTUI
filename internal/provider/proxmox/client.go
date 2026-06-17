package proxmox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

// client is a thin Proxmox VE REST client. It speaks the /api2/json API and
// authenticates with an API token (DESIGN.md §5): no password is ever stored.
type client struct {
	base  string // e.g. https://10.0.0.10:8006
	token string // full token credential: user@realm!tokenid=secret
	http  *http.Client
}

// newClient builds a client from a resolved connection config, wiring TLS trust
// (fingerprint pinning for self-signed homelab certs, or an explicit insecure
// opt-out) per the design.
func newClient(cfg provider.ConnConfig) (*client, error) {
	base := strings.TrimRight(cfg.Endpoint, "/")
	if base == "" {
		return nil, fmt.Errorf("proxmox: endpoint is required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("proxmox: API token is required")
	}
	return &client{
		base:  base,
		token: cfg.Token,
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsConfig(cfg.TLS)},
		},
	}, nil
}

// tlsConfig builds a *tls.Config honouring fingerprint pinning / insecure opt-out.
func tlsConfig(t provider.TLSConfig) *tls.Config {
	if fp := normalizeFingerprint(t.Fingerprint); fp != "" {
		// Pin the leaf certificate by SHA-256 rather than trusting the chain.
		return &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // verified explicitly below
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return fmt.Errorf("proxmox: server presented no certificate")
				}
				sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
				if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, fp) {
					return fmt.Errorf("proxmox: certificate fingerprint mismatch (got %s)", colonize(got))
				}
				return nil
			},
		}
	}
	if t.Insecure {
		return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit per-connection opt-out
	}
	return &tls.Config{}
}

// apiEnvelope is the standard Proxmox response wrapper.
type apiEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors map[string]any  `json:"errors"`
}

// get performs a GET and decodes the data envelope into out (may be nil).
func (c *client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// post performs a form-encoded POST and decodes the data envelope into out.
func (c *client) post(ctx context.Context, path string, params url.Values, out any) error {
	return c.do(ctx, http.MethodPost, path, params, out)
}

func (c *client) do(ctx context.Context, method, path string, params url.Values, out any) error {
	var body io.Reader
	if params != nil {
		body = strings.NewReader(params.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api2/json"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.token)
	if params != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("proxmox: %s %s: authentication failed — check the API token "+
				"(expected the full credential user@realm!tokenid=secret) and that it has the "+
				"required privileges: %s", method, path, firstLine(msg))
		}
		return fmt.Errorf("proxmox: %s %s: %s", method, path, firstLine(msg))
	}
	if out == nil {
		return nil
	}
	var env apiEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("proxmox: decode %s: %w", path, err)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

// --- helpers ---

func normalizeFingerprint(s string) string {
	r := strings.NewReplacer(":", "", " ", "")
	return strings.ToLower(r.Replace(s))
}

// colonize formats a hex fingerprint with colon separators for display.
func colonize(hexStr string) string {
	var b bytes.Buffer
	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		end := i + 2
		if end > len(hexStr) {
			end = len(hexStr)
		}
		b.WriteString(strings.ToUpper(hexStr[i:end]))
	}
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
