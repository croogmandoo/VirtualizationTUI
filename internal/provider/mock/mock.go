// Package mock provides an in-memory Provider used by the Phase 1 app shell so the
// UI can be developed and demonstrated before any real platform integration lands.
// It implements the full provider.Provider contract (including async tasks) with
// deterministic, mutable fake data.
package mock

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/croogmandoo/virtualizationtui/internal/provider"
)

func init() {
	provider.Register("mock", func() provider.Provider { return New() })
}

// Mock is a thread-safe, in-memory provider.
type Mock struct {
	mu        sync.Mutex
	connected bool
	name      string
	resources map[string]*provider.Resource // keyed by ID
	tasks     map[string]taskEntry
	rng       *rand.Rand
}

type taskEntry struct {
	done  time.Time
	state provider.TaskState
}

// New returns an unconnected mock provider seeded with sample inventory.
func New() *Mock {
	return &Mock{
		resources: map[string]*provider.Resource{},
		tasks:     map[string]taskEntry{},
		rng:       rand.New(rand.NewSource(42)),
	}
}

func (m *Mock) Type() string { return "mock" }

func (m *Mock) Capabilities() []provider.Capability {
	return []provider.Capability{provider.CapPowerControl, provider.CapMetrics, provider.CapSnapshots}
}

func (m *Mock) Kinds() []provider.Kind {
	return []provider.Kind{provider.KindNode, provider.KindVM, provider.KindContainer}
}

func (m *Mock) Connect(ctx context.Context, cfg provider.ConnConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.name = cfg.Name
	m.seed()
	m.connected = true
	return nil
}

func (m *Mock) Ping(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connected {
		return fmt.Errorf("mock: not connected")
	}
	return nil
}

func (m *Mock) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	return nil
}

func (m *Mock) List(ctx context.Context, kind provider.Kind) ([]provider.Resource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshMetricsLocked()
	var out []provider.Resource
	for _, r := range m.resources {
		if r.Kind == kind {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *Mock) Get(ctx context.Context, kind provider.Kind, id string) (provider.Resource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshMetricsLocked()
	if r, ok := m.resources[id]; ok && r.Kind == kind {
		return *r, nil
	}
	return provider.Resource{}, fmt.Errorf("mock: %s/%s not found", kind, id)
}

func (m *Mock) Do(ctx context.Context, a provider.Action) (provider.ActionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.resources[a.Target]
	if !ok {
		return provider.ActionResult{}, fmt.Errorf("mock: target %q not found", a.Target)
	}
	switch a.Verb {
	case "start":
		r.Status = provider.StatusRunning
	case "stop", "shutdown":
		r.Status = provider.StatusStopped
	case "reboot":
		// Simulate an async task that completes shortly.
		id := fmt.Sprintf("task-%d", time.Now().UnixNano())
		m.tasks[id] = taskEntry{done: time.Now().Add(1500 * time.Millisecond), state: provider.TaskRunning}
		return provider.ActionResult{OK: true, Message: "reboot scheduled", TaskID: id}, nil
	case "snapshot":
		return provider.ActionResult{OK: true, Message: "snapshot created"}, nil
	default:
		return provider.ActionResult{}, fmt.Errorf("mock: unsupported verb %q", a.Verb)
	}
	return provider.ActionResult{OK: true, Message: fmt.Sprintf("%s %s", a.Verb, r.Name)}, nil
}

// TaskStatus implements provider.TaskTracker.
func (m *Mock) TaskStatus(ctx context.Context, taskID string) (provider.TaskState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return provider.TaskFailed, fmt.Errorf("mock: task %q not found", taskID)
	}
	if time.Now().After(t.done) {
		t.state = provider.TaskDone
		m.tasks[taskID] = t
	}
	return t.state, nil
}

// --- internal helpers (caller holds m.mu) ---

func (m *Mock) seed() {
	if len(m.resources) > 0 {
		return
	}
	node := m.name
	if node == "" {
		node = "pve-01"
	}
	add := func(r *provider.Resource) {
		r.Parent = node
		for i := 0; i < 24; i++ {
			r.Metrics = m.bumpMetrics(r.Metrics)
		}
		m.resources[r.ID] = r
	}
	add(&provider.Resource{ID: node, Kind: provider.KindNode, Name: node, Status: provider.StatusOK,
		Fields: map[string]string{"cpu": "18%", "mem": "23.1/64G", "uptime": "31d"}})
	add(&provider.Resource{ID: "100", Kind: provider.KindVM, Name: "web-01", Status: provider.StatusRunning,
		Fields: map[string]string{"cpu": "3%", "mem": "2.1G", "ip": "10.0.0.21"}})
	add(&provider.Resource{ID: "101", Kind: provider.KindVM, Name: "db-01", Status: provider.StatusRunning,
		Fields: map[string]string{"cpu": "41%", "mem": "7.8G", "ip": "10.0.0.22"}})
	add(&provider.Resource{ID: "102", Kind: provider.KindVM, Name: "ci-runner", Status: provider.StatusStopped,
		Fields: map[string]string{"cpu": "-", "mem": "-", "ip": "-"}})
	add(&provider.Resource{ID: "200", Kind: provider.KindContainer, Name: "dns", Status: provider.StatusRunning,
		Fields: map[string]string{"cpu": "1%", "mem": "256M", "ip": "10.0.0.30"}})
	add(&provider.Resource{ID: "201", Kind: provider.KindContainer, Name: "caddy", Status: provider.StatusRunning,
		Fields: map[string]string{"cpu": "2%", "mem": "64M", "ip": "10.0.0.31"}})
}

// refreshMetricsLocked nudges metric histories so the sparkline view animates.
func (m *Mock) refreshMetricsLocked() {
	for _, r := range m.resources {
		if r.Status == provider.StatusRunning || r.Status == provider.StatusOK {
			r.Metrics = m.bumpMetrics(r.Metrics)
		}
	}
}

func (m *Mock) bumpMetrics(in []provider.Metric) []provider.Metric {
	const window = 24
	defs := []struct {
		name, unit string
		base, amp  float64
	}{
		{"cpu", "%", 25, 20},
		{"mem", "GiB", 6, 2},
		{"net", "Mbps", 40, 35},
	}
	if in == nil {
		in = make([]provider.Metric, len(defs))
		for i, d := range defs {
			in[i] = provider.Metric{Name: d.name, Unit: d.unit}
		}
	}
	for i := range in {
		d := defs[i%len(defs)]
		v := d.base + d.amp*math.Sin(float64(len(in[i].History))/3.0) + (m.rng.Float64()-0.5)*d.amp*0.4
		if v < 0 {
			v = 0
		}
		in[i].Value = v
		in[i].History = append(in[i].History, v)
		if len(in[i].History) > window {
			in[i].History = in[i].History[len(in[i].History)-window:]
		}
	}
	return in
}
