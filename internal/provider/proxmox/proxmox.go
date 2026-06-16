// Package proxmox implements the provider.Provider contract for Proxmox VE.
//
// It is the reference real-world integration (DESIGN.md §5): a thin REST client
// over /api2/json, authenticated with an API token, exposing nodes, QEMU VMs and
// LXC containers as normalized resources with power/snapshot actions and UPID
// task polling.
package proxmox

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func init() {
	provider.Register("proxmox", func() provider.Provider { return New() })
}

// Proxmox is a Proxmox VE provider instance.
type Proxmox struct {
	cli *client

	mu sync.RWMutex
	// guestNode maps a guest's VMID to the node and guest-kind ("qemu"/"lxc") it
	// lives on, populated during List so Do/actions can address it without the UI
	// needing to track placement.
	guestNode map[string]guestRef
}

type guestRef struct {
	node  string
	gtype string // "qemu" or "lxc"
}

// New returns an unconnected Proxmox provider.
func New() *Proxmox {
	return &Proxmox{guestNode: map[string]guestRef{}}
}

func (p *Proxmox) Type() string { return "proxmox" }

func (p *Proxmox) Capabilities() []provider.Capability {
	return []provider.Capability{provider.CapPowerControl, provider.CapSnapshots, provider.CapMetrics}
}

func (p *Proxmox) Kinds() []provider.Kind {
	return []provider.Kind{provider.KindNode, provider.KindVM, provider.KindContainer}
}

func (p *Proxmox) Connect(ctx context.Context, cfg provider.ConnConfig) error {
	cli, err := newClient(cfg)
	if err != nil {
		return err
	}
	p.cli = cli
	return p.Ping(ctx)
}

// Ping verifies connectivity and credentials via the version endpoint.
func (p *Proxmox) Ping(ctx context.Context) error {
	if p.cli == nil {
		return fmt.Errorf("proxmox: not connected")
	}
	var v struct {
		Version string `json:"version"`
	}
	return p.cli.get(ctx, "/version", &v)
}

func (p *Proxmox) Close() error {
	p.cli = nil
	return nil
}

// List returns inventory for the given kind.
func (p *Proxmox) List(ctx context.Context, kind provider.Kind) ([]provider.Resource, error) {
	if p.cli == nil {
		return nil, fmt.Errorf("proxmox: not connected")
	}
	switch kind {
	case provider.KindNode:
		return p.listNodes(ctx)
	case provider.KindVM:
		return p.listGuests(ctx, "qemu", provider.KindVM)
	case provider.KindContainer:
		return p.listGuests(ctx, "lxc", provider.KindContainer)
	default:
		return nil, fmt.Errorf("proxmox: unsupported kind %q", kind)
	}
}

func (p *Proxmox) listNodes(ctx context.Context) ([]provider.Resource, error) {
	var nodes []nodeEntry
	if err := p.cli.get(ctx, "/nodes", &nodes); err != nil {
		return nil, err
	}
	out := make([]provider.Resource, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, provider.Resource{
			ID:     n.Node,
			Kind:   provider.KindNode,
			Name:   n.Node,
			Status: mapNodeStatus(n.Status),
			Parent: "",
			Fields: map[string]string{
				"cpu":    formatPct(n.CPU),
				"mem":    formatMem(n.Mem, n.MaxMem),
				"uptime": formatUptime(n.Uptime),
			},
			Metrics: nodeMetrics(n),
			Raw:     n,
		})
	}
	return out, nil
}

func (p *Proxmox) listGuests(ctx context.Context, gtype string, kind provider.Kind) ([]provider.Resource, error) {
	var res []clusterResource
	if err := p.cli.get(ctx, "/cluster/resources?type=vm", &res); err != nil {
		return nil, err
	}
	out := make([]provider.Resource, 0)
	for _, r := range res {
		if r.Type != gtype {
			continue
		}
		id := strconv.Itoa(r.VMID)
		p.mu.Lock()
		p.guestNode[id] = guestRef{node: r.Node, gtype: gtype}
		p.mu.Unlock()

		name := r.Name
		if name == "" {
			name = id
		}
		out = append(out, provider.Resource{
			ID:     id,
			Kind:   kind,
			Name:   name,
			Status: mapGuestStatus(r.Status),
			Parent: r.Node,
			Fields: map[string]string{
				"cpu": formatPct(r.CPU),
				"mem": formatMem(r.Mem, r.MaxMem),
			},
			Metrics: guestMetrics(r),
			Raw:     r,
		})
	}
	return out, nil
}

// Get fetches a single resource by re-listing its kind (the cluster endpoint is a
// single round-trip, so this stays cheap).
func (p *Proxmox) Get(ctx context.Context, kind provider.Kind, id string) (provider.Resource, error) {
	items, err := p.List(ctx, kind)
	if err != nil {
		return provider.Resource{}, err
	}
	for _, r := range items {
		if r.ID == id {
			return r, nil
		}
	}
	return provider.Resource{}, fmt.Errorf("proxmox: %s/%s not found", kind, id)
}

// Do executes a mutating action against a guest.
func (p *Proxmox) Do(ctx context.Context, a provider.Action) (provider.ActionResult, error) {
	if p.cli == nil {
		return provider.ActionResult{}, fmt.Errorf("proxmox: not connected")
	}
	ref, ok := p.guestRef(a.Target)
	if !ok {
		return provider.ActionResult{}, fmt.Errorf("proxmox: unknown target %q (list resources first)", a.Target)
	}
	base := fmt.Sprintf("/nodes/%s/%s/%s", ref.node, ref.gtype, a.Target)

	var path string
	params := url.Values{}
	switch a.Verb {
	case "start", "stop", "shutdown", "reboot", "suspend", "resume":
		path = base + "/status/" + a.Verb
	case "snapshot":
		name, _ := a.Params["name"].(string)
		if name == "" {
			name = "snap-" + a.Target
		}
		path = base + "/snapshot"
		params.Set("snapname", name)
	default:
		return provider.ActionResult{}, fmt.Errorf("proxmox: unsupported verb %q", a.Verb)
	}

	var upid string
	if err := p.cli.post(ctx, path, params, &upid); err != nil {
		return provider.ActionResult{}, err
	}
	return provider.ActionResult{
		OK:      true,
		Message: fmt.Sprintf("%s %s queued", a.Verb, a.Target),
		TaskID:  upid,
	}, nil
}

// TaskStatus implements provider.TaskTracker by polling a UPID's task status. The
// node is parsed from the UPID itself (UPID:node:...).
func (p *Proxmox) TaskStatus(ctx context.Context, taskID string) (provider.TaskState, error) {
	if p.cli == nil {
		return provider.TaskFailed, fmt.Errorf("proxmox: not connected")
	}
	node, err := nodeFromUPID(taskID)
	if err != nil {
		return provider.TaskFailed, err
	}
	var st taskStatus
	if err := p.cli.get(ctx, fmt.Sprintf("/nodes/%s/tasks/%s/status", node, url.PathEscape(taskID)), &st); err != nil {
		return provider.TaskFailed, err
	}
	switch {
	case st.Status == "running":
		return provider.TaskRunning, nil
	case st.ExitStatus == "OK":
		return provider.TaskDone, nil
	default:
		return provider.TaskFailed, nil
	}
}

func (p *Proxmox) guestRef(id string) (guestRef, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ref, ok := p.guestNode[id]
	return ref, ok
}

// nodeFromUPID extracts the node component of a Proxmox UPID string, whose format
// is "UPID:node:pid:pstart:starttime:type:id:user:".
func nodeFromUPID(upid string) (string, error) {
	parts := strings.Split(upid, ":")
	if len(parts) < 2 || parts[0] != "UPID" || parts[1] == "" {
		return "", fmt.Errorf("proxmox: malformed UPID %q", upid)
	}
	return parts[1], nil
}
