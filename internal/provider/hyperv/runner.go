package hyperv

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/masterzen/winrm"
)

// CommandRunner executes a PowerShell script on the Hyper-V host and returns its
// stdout. Abstracting the transport lets the provider's command/parse logic be
// unit-tested with a fake runner, with WinRM (or another transport) used in prod.
type CommandRunner interface {
	Run(ctx context.Context, script string) ([]byte, error)
	Close() error
}

// winrmRunner runs PowerShell over WinRM using the masterzen/winrm client.
type winrmRunner struct {
	client *winrm.Client
}

// newWinRMRunner builds a WinRM-backed runner from an endpoint and credentials.
// Endpoint forms accepted: "host", "host:5985", "http://host:5985",
// "https://host:5986". https (5986) is used when the scheme is https or the port
// is 5986; TLS verification is skipped only when insecure is set.
func newWinRMRunner(endpoint, user, password string, insecure bool) (*winrmRunner, error) {
	host, port, https, err := parseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	ep := winrm.NewEndpoint(host, port, https, insecure, nil, nil, nil, 60*time.Second)
	c, err := winrm.NewClient(ep, user, password)
	if err != nil {
		return nil, err
	}
	return &winrmRunner{client: c}, nil
}

func (r *winrmRunner) Run(ctx context.Context, script string) ([]byte, error) {
	// winrm.Powershell base64-encodes the script and invokes powershell.exe.
	stdout, stderr, code, err := r.client.RunWithContextWithString(ctx, winrm.Powershell(script), "")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = fmt.Sprintf("exit code %d", code)
		}
		return nil, fmt.Errorf("powershell: %s", firstLine(msg))
	}
	return []byte(stdout), nil
}

func (r *winrmRunner) Close() error { return nil }

func parseEndpoint(endpoint string) (host string, port int, https bool, err error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", 0, false, fmt.Errorf("hyperv: endpoint is required")
	}
	if strings.Contains(endpoint, "://") {
		u, perr := url.Parse(endpoint)
		if perr != nil {
			return "", 0, false, fmt.Errorf("hyperv: parse endpoint: %w", perr)
		}
		https = u.Scheme == "https"
		host = u.Hostname()
		if p := u.Port(); p != "" {
			port, _ = strconv.Atoi(p)
		}
	} else if h, p, ok := strings.Cut(endpoint, ":"); ok {
		host = h
		port, _ = strconv.Atoi(p)
	} else {
		host = endpoint
	}
	if host == "" {
		return "", 0, false, fmt.Errorf("hyperv: could not determine host from %q", endpoint)
	}
	if port == 0 {
		if https {
			port = 5986
		} else {
			port = 5985
		}
	}
	if port == 5986 {
		https = true
	}
	return host, port, https, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
