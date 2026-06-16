// Package hyperv implements the provider.Provider contract for Microsoft Hyper-V
// (DESIGN.md §5). Hyper-V has no clean REST API, so this provider drives the
// PowerShell Hyper-V cmdlets over a pluggable CommandRunner (WinRM by default).
//
// Hyper-V is the one provider that needs username/password credentials (WinRM),
// the documented exception to the tokens-only rule (DESIGN.md §7).
package hyperv

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func init() {
	provider.Register("hyperv", func() provider.Provider { return New() })
}

// Hyperv is a Hyper-V provider instance.
type Hyperv struct {
	runner CommandRunner
}

// New returns an unconnected provider.
func New() *Hyperv { return &Hyperv{} }

func (h *Hyperv) Type() string { return "hyperv" }

func (h *Hyperv) Capabilities() []provider.Capability {
	return []provider.Capability{provider.CapPowerControl, provider.CapMetrics}
}

func (h *Hyperv) Kinds() []provider.Kind {
	return []provider.Kind{provider.KindVM, provider.KindHost}
}

func (h *Hyperv) Connect(ctx context.Context, cfg provider.ConnConfig) error {
	if cfg.Username == "" || cfg.Password == "" {
		return fmt.Errorf("hyperv: username and password are required (WinRM)")
	}
	r, err := newWinRMRunner(cfg.Endpoint, cfg.Username, cfg.Password, cfg.TLS.Insecure)
	if err != nil {
		return err
	}
	h.runner = r
	return h.Ping(ctx)
}

// Ping runs a trivial command to confirm the runner works.
func (h *Hyperv) Ping(ctx context.Context) error {
	if h.runner == nil {
		return fmt.Errorf("hyperv: not connected")
	}
	_, err := h.runner.Run(ctx, "(Get-VMHost).Name")
	return err
}

func (h *Hyperv) Close() error {
	if h.runner != nil {
		err := h.runner.Close()
		h.runner = nil
		return err
	}
	return nil
}

func (h *Hyperv) List(ctx context.Context, kind provider.Kind) ([]provider.Resource, error) {
	if h.runner == nil {
		return nil, fmt.Errorf("hyperv: not connected")
	}
	switch kind {
	case provider.KindVM:
		return h.listVMs(ctx)
	case provider.KindHost:
		return h.listHost(ctx)
	default:
		return nil, fmt.Errorf("hyperv: unsupported kind %q", kind)
	}
}

// vmSelect projects VM properties into JSON-friendly, stably-typed fields.
// Enums (State) and structs (Id, Uptime) are forced to strings so Windows
// PowerShell 5.1's ConvertTo-Json (which renders enums as integers) is consistent.
const vmSelect = `Get-VM | Select-Object Name,` +
	`@{N='Id';E={$_.Id.Guid}},` +
	`@{N='State';E={$_.State.ToString()}},` +
	`CPUUsage,MemoryAssigned,` +
	`@{N='Uptime';E={$_.Uptime.ToString()}},` +
	`Status | ConvertTo-Json -Depth 3`

func (h *Hyperv) listVMs(ctx context.Context) ([]provider.Resource, error) {
	out, err := h.runner.Run(ctx, vmSelect)
	if err != nil {
		return nil, err
	}
	var vms []vm
	if err := decodeJSONArray(out, &vms); err != nil {
		return nil, fmt.Errorf("hyperv: parse VMs: %w", err)
	}
	res := make([]provider.Resource, 0, len(vms))
	for _, m := range vms {
		res = append(res, provider.Resource{
			ID:     m.Id,
			Kind:   provider.KindVM,
			Name:   m.Name,
			Status: mapVMState(m.State),
			Fields: map[string]string{
				"cpu":    fmt.Sprintf("%d%%", m.CPUUsage),
				"mem":    humanBytes(m.MemoryAssigned),
				"uptime": m.Uptime,
				"status": m.Status,
			},
			Metrics: []provider.Metric{
				{Name: "cpu", Value: float64(m.CPUUsage), Unit: "%", History: []float64{float64(m.CPUUsage)}},
			},
			Raw: m,
		})
	}
	return res, nil
}

func (h *Hyperv) listHost(ctx context.Context) ([]provider.Resource, error) {
	const script = `Get-VMHost | Select-Object Name,LogicalProcessorCount,MemoryCapacity | ConvertTo-Json -Depth 2`
	out, err := h.runner.Run(ctx, script)
	if err != nil {
		return nil, err
	}
	var hosts []vmHost
	if err := decodeJSONArray(out, &hosts); err != nil {
		return nil, fmt.Errorf("hyperv: parse host: %w", err)
	}
	res := make([]provider.Resource, 0, len(hosts))
	for _, hh := range hosts {
		res = append(res, provider.Resource{
			ID:     hh.Name,
			Kind:   provider.KindHost,
			Name:   hh.Name,
			Status: provider.StatusOK,
			Fields: map[string]string{
				"cpus": fmt.Sprintf("%d", hh.LogicalProcessorCount),
				"mem":  humanBytes(hh.MemoryCapacity),
			},
			Raw: hh,
		})
	}
	return res, nil
}

func (h *Hyperv) Get(ctx context.Context, kind provider.Kind, id string) (provider.Resource, error) {
	items, err := h.List(ctx, kind)
	if err != nil {
		return provider.Resource{}, err
	}
	for _, r := range items {
		if r.ID == id {
			return r, nil
		}
	}
	return provider.Resource{}, fmt.Errorf("hyperv: %s/%s not found", kind, id)
}

var guidRe = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// Do performs VM power operations, addressing the VM by its GUID.
func (h *Hyperv) Do(ctx context.Context, a provider.Action) (provider.ActionResult, error) {
	if h.runner == nil {
		return provider.ActionResult{}, fmt.Errorf("hyperv: not connected")
	}
	if !guidRe.MatchString(a.Target) {
		return provider.ActionResult{}, fmt.Errorf("hyperv: target must be a VM GUID, got %q", a.Target)
	}
	var cmdlet string
	switch a.Verb {
	case "start":
		cmdlet = "Start-VM"
	case "stop":
		cmdlet = "Stop-VM -Force"
	case "shutdown":
		cmdlet = "Stop-VM" // graceful guest shutdown
	case "reboot":
		cmdlet = "Restart-VM -Force"
	case "suspend":
		cmdlet = "Suspend-VM"
	case "resume":
		cmdlet = "Resume-VM"
	default:
		return provider.ActionResult{}, fmt.Errorf("hyperv: unsupported verb %q", a.Verb)
	}
	script := fmt.Sprintf("Get-VM -Id '%s' | %s", a.Target, cmdlet)
	if _, err := h.runner.Run(ctx, script); err != nil {
		return provider.ActionResult{}, err
	}
	return provider.ActionResult{OK: true, Message: fmt.Sprintf("%s %s", a.Verb, a.Target)}, nil
}

// decodeJSONArray unmarshals PowerShell's ConvertTo-Json output into a slice.
// ConvertTo-Json renders a single object (not an array) when there is exactly one
// item, and emits nothing for zero items, so both cases are normalized here.
func decodeJSONArray(data []byte, dst any) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, "{") {
		trimmed = "[" + trimmed + "]"
	}
	return json.Unmarshal([]byte(trimmed), dst)
}
