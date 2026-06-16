// Package vsphere implements the provider.Provider contract for VMware vSphere
// (vCenter / ESXi) using the official govmomi SDK (DESIGN.md §5). It exposes
// virtual machines, hosts and datastores, and performs VM power operations.
//
// vSphere uses a session login (username + password); the credential is exchanged
// for a session and not persisted by this app (DESIGN.md §7).
package vsphere

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func init() {
	provider.Register("vsphere", func() provider.Provider { return New() })
}

// VSphere is a vSphere provider instance.
type VSphere struct {
	client *govmomi.Client

	mu   sync.RWMutex
	refs map[string]types.ManagedObjectReference // VM id -> moref, for actions
}

// New returns an unconnected provider.
func New() *VSphere { return &VSphere{refs: map[string]types.ManagedObjectReference{}} }

func (v *VSphere) Type() string { return "vsphere" }

func (v *VSphere) Capabilities() []provider.Capability {
	return []provider.Capability{provider.CapPowerControl, provider.CapMetrics}
}

func (v *VSphere) Kinds() []provider.Kind {
	return []provider.Kind{provider.KindVM, provider.KindHost, provider.KindStorage}
}

func (v *VSphere) Connect(ctx context.Context, cfg provider.ConnConfig) error {
	if cfg.Endpoint == "" {
		return fmt.Errorf("vsphere: endpoint is required")
	}
	if cfg.Username == "" || cfg.Password == "" {
		return fmt.Errorf("vsphere: username and password are required (session login)")
	}
	u, err := soap.ParseURL(cfg.Endpoint)
	if err != nil {
		return fmt.Errorf("vsphere: parse endpoint: %w", err)
	}
	u.User = url.UserPassword(cfg.Username, cfg.Password)

	c, err := govmomi.NewClient(ctx, u, cfg.TLS.Insecure)
	if err != nil {
		return fmt.Errorf("vsphere: login: %w", err)
	}
	v.client = c
	return nil
}

// Ping verifies the session is still valid.
func (v *VSphere) Ping(ctx context.Context) error {
	if v.client == nil {
		return fmt.Errorf("vsphere: not connected")
	}
	active, err := v.client.SessionManager.SessionIsActive(ctx)
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("vsphere: session is not active")
	}
	return nil
}

func (v *VSphere) Close() error {
	if v.client != nil {
		_ = v.client.Logout(context.Background())
		v.client = nil
	}
	return nil
}

func (v *VSphere) List(ctx context.Context, kind provider.Kind) ([]provider.Resource, error) {
	if v.client == nil {
		return nil, fmt.Errorf("vsphere: not connected")
	}
	switch kind {
	case provider.KindVM:
		return v.listVMs(ctx)
	case provider.KindHost:
		return v.listHosts(ctx)
	case provider.KindStorage:
		return v.listDatastores(ctx)
	default:
		return nil, fmt.Errorf("vsphere: unsupported kind %q", kind)
	}
}

// containerView retrieves the named properties for all objects of a managed type.
func (v *VSphere) containerView(ctx context.Context, kind string, props []string, dst any) error {
	m := view.NewManager(v.client.Client)
	cv, err := m.CreateContainerView(ctx, v.client.ServiceContent.RootFolder, []string{kind}, true)
	if err != nil {
		return err
	}
	defer func() { _ = cv.Destroy(ctx) }()
	return cv.Retrieve(ctx, []string{kind}, props, dst)
}

func (v *VSphere) listVMs(ctx context.Context) ([]provider.Resource, error) {
	var vms []mo.VirtualMachine
	if err := v.containerView(ctx, "VirtualMachine", []string{"summary"}, &vms); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(vms))
	for _, vm := range vms {
		s := vm.Summary
		id := vm.Self.Value
		v.mu.Lock()
		v.refs[id] = vm.Self
		v.mu.Unlock()

		cpu := float64(s.QuickStats.OverallCpuUsage)  // MHz
		mem := float64(s.QuickStats.GuestMemoryUsage) // MB
		res = append(res, provider.Resource{
			ID:     id,
			Kind:   provider.KindVM,
			Name:   orDash(s.Config.Name),
			Status: mapPowerState(s.Runtime.PowerState),
			Fields: map[string]string{
				"cpu":   fmt.Sprintf("%.0fMHz", cpu),
				"mem":   fmt.Sprintf("%.0fMB", mem),
				"guest": orDash(s.Config.GuestFullName),
				"ip":    orDash(s.Guest.IpAddress),
			},
			Metrics: []provider.Metric{
				{Name: "cpu", Value: cpu, Unit: "MHz", History: []float64{cpu}},
				{Name: "mem", Value: mem, Unit: "MB", History: []float64{mem}},
			},
			Raw: s,
		})
	}
	return res, nil
}

func (v *VSphere) listHosts(ctx context.Context) ([]provider.Resource, error) {
	var hosts []mo.HostSystem
	if err := v.containerView(ctx, "HostSystem", []string{"summary"}, &hosts); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(hosts))
	for _, h := range hosts {
		s := h.Summary
		status := provider.StatusOK
		if s.Runtime.ConnectionState != types.HostSystemConnectionStateConnected {
			status = provider.StatusError
		} else if s.Runtime.InMaintenanceMode {
			status = provider.StatusDegraded
		}
		res = append(res, provider.Resource{
			ID:     h.Self.Value,
			Kind:   provider.KindHost,
			Name:   orDash(s.Config.Name),
			Status: status,
			Fields: map[string]string{
				"cpu":    fmt.Sprintf("%dMHz", s.QuickStats.OverallCpuUsage),
				"mem":    fmt.Sprintf("%dMB", s.QuickStats.OverallMemoryUsage),
				"vendor": orDash(s.Hardware.Vendor),
			},
			Raw: s,
		})
	}
	return res, nil
}

func (v *VSphere) listDatastores(ctx context.Context) ([]provider.Resource, error) {
	var ds []mo.Datastore
	if err := v.containerView(ctx, "Datastore", []string{"summary"}, &ds); err != nil {
		return nil, err
	}
	res := make([]provider.Resource, 0, len(ds))
	for _, d := range ds {
		s := d.Summary
		used := s.Capacity - s.FreeSpace
		usedPct := pct(used, s.Capacity)
		status := provider.StatusOK
		if !s.Accessible {
			status = provider.StatusError
		}
		res = append(res, provider.Resource{
			ID:     d.Self.Value,
			Kind:   provider.KindStorage,
			Name:   orDash(s.Name),
			Status: status,
			Fields: map[string]string{
				"type": s.Type,
				"size": humanBytes(s.Capacity),
				"used": fmt.Sprintf("%s (%.0f%%)", humanBytes(used), usedPct),
				"free": humanBytes(s.FreeSpace),
			},
			Metrics: []provider.Metric{{Name: "used", Value: usedPct, Unit: "%", History: []float64{usedPct}}},
			Raw:     s,
		})
	}
	return res, nil
}

func (v *VSphere) Get(ctx context.Context, kind provider.Kind, id string) (provider.Resource, error) {
	items, err := v.List(ctx, kind)
	if err != nil {
		return provider.Resource{}, err
	}
	for _, r := range items {
		if r.ID == id {
			return r, nil
		}
	}
	return provider.Resource{}, fmt.Errorf("vsphere: %s/%s not found", kind, id)
}

// Do performs VM power operations. The target is a VM moref value (as listed).
func (v *VSphere) Do(ctx context.Context, a provider.Action) (provider.ActionResult, error) {
	if v.client == nil {
		return provider.ActionResult{}, fmt.Errorf("vsphere: not connected")
	}
	v.mu.RLock()
	ref, ok := v.refs[a.Target]
	v.mu.RUnlock()
	if !ok {
		return provider.ActionResult{}, fmt.Errorf("vsphere: unknown VM %q (list VMs first)", a.Target)
	}
	vm := object.NewVirtualMachine(v.client.Client, ref)

	var task *object.Task
	var err error
	switch a.Verb {
	case "start":
		task, err = vm.PowerOn(ctx)
	case "stop":
		task, err = vm.PowerOff(ctx)
	case "reboot":
		task, err = vm.Reset(ctx)
	case "suspend":
		task, err = vm.Suspend(ctx)
	case "shutdown":
		// Guest-initiated; no task object is returned.
		if err = vm.ShutdownGuest(ctx); err != nil {
			return provider.ActionResult{}, err
		}
		return provider.ActionResult{OK: true, Message: "guest shutdown requested for " + a.Target}, nil
	default:
		return provider.ActionResult{}, fmt.Errorf("vsphere: unsupported verb %q", a.Verb)
	}
	if err != nil {
		return provider.ActionResult{}, err
	}
	if err := task.Wait(ctx); err != nil {
		return provider.ActionResult{}, err
	}
	return provider.ActionResult{OK: true, Message: fmt.Sprintf("%s %s completed", a.Verb, a.Target)}, nil
}
