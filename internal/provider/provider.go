// Package provider defines the contract every platform integration implements.
//
// The TUI layer is deliberately decoupled from any specific platform: it renders
// generically from the Capabilities a provider advertises and the Resources it
// returns. Adding a new platform is implementing this interface and registering a
// constructor (see registry.go); it should rarely require touching the UI.
package provider

import "context"

// Kind identifies a category of resource a provider can expose.
type Kind string

const (
	KindNode      Kind = "node"
	KindVM        Kind = "vm"
	KindContainer Kind = "container"
	KindHost      Kind = "host"
	KindStorage   Kind = "storage"
	KindDataset   Kind = "dataset"
	KindShare     Kind = "share"
	KindDNSZone   Kind = "dns_zone"
	KindDNSRecord Kind = "dns_record"
	KindRoute     Kind = "route"
)

// Capability advertises an optional feature set a provider supports. The UI uses
// these to decide which actions and columns to surface.
type Capability string

const (
	CapPowerControl Capability = "power"       // start / stop / reboot
	CapSnapshots    Capability = "snapshots"   // create / rollback / delete snapshots
	CapConsole      Capability = "console"     // open console / serial
	CapDNSRecords   Capability = "dns_records" // manage DNS zones & records
	CapRoutes       Capability = "routes"      // manage reverse-proxy routes
	CapMetrics      Capability = "metrics"     // point-in-time + historical metrics
)

// Status is a normalized health/power state, mapped from each platform's native
// vocabulary so the UI can colour and sort consistently.
type Status string

const (
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusPaused   Status = "paused"
	StatusDegraded Status = "degraded"
	StatusOK       Status = "ok"
	StatusError    Status = "error"
	StatusUnknown  Status = "unknown"
)

// ConnConfig is the resolved connection configuration handed to a provider at
// connect time. Secrets are already resolved (from keyring/env) — providers never
// read the keyring themselves.
type ConnConfig struct {
	Name     string            // user-facing connection name, e.g. "pve-01"
	Endpoint string            // base URL or host, e.g. "https://10.0.0.10:8006"
	Token    string            // resolved API token / key (preferred auth)
	Username string            // only used where token auth is unavailable (e.g. Hyper-V WinRM)
	Password string            // only used where token auth is unavailable
	TLS      TLSConfig         // per-connection trust settings
	Extra    map[string]string // provider-specific knobs (realm, datacenter, ...)
}

// TLSConfig captures per-connection TLS trust. Self-signed certs (common in
// homelabs) are trusted by pinning a fingerprint rather than a global insecure mode.
type TLSConfig struct {
	Fingerprint string // pinned SHA-256 cert fingerprint, e.g. "AA:BB:..."
	Insecure    bool   // explicit opt-out of verification for a single connection
}

// Metric is a single named gauge with a short rolling history, feeding the
// sparkline detail view (see DESIGN.md §6).
type Metric struct {
	Name    string    // "cpu", "mem", "net_in", ...
	Value   float64   // current value
	Unit    string    // "%", "GiB", "Mbps", ...
	History []float64 // rolling window, oldest → newest
}

// Resource is the normalized, UI-facing representation of a managed object.
// Fields keeps the core decoupled from platform schemas: the table view renders
// whatever columns a Kind declares, and Raw holds the original object for details.
type Resource struct {
	ID      string
	Kind    Kind
	Name    string
	Status  Status
	Parent  string            // grouping: node / host / pool the resource belongs to
	Fields  map[string]string // kind-specific columns (cpu, mem, ip, ...)
	Metrics []Metric          // optional, when CapMetrics is advertised
	Raw     any               // original platform object, for the detail view
}

// Action is an intent dispatched from the UI; providers validate and execute it.
type Action struct {
	Verb   string         // "start", "stop", "snapshot", "create_record", ...
	Kind   Kind           // the resource kind acted upon
	Target string         // resource ID (may be empty for "create")
	Params map[string]any // verb-specific parameters
}

// ActionResult reports the outcome of an Action. For long-running platform tasks
// (clone, migrate, …) TaskID is set and the scheduler polls TaskStatus.
type ActionResult struct {
	OK      bool
	Message string
	TaskID  string // non-empty when the action spawned an async task
}

// TaskState is the normalized state of an asynchronous platform task.
type TaskState string

const (
	TaskRunning TaskState = "running"
	TaskDone    TaskState = "done"
	TaskFailed  TaskState = "failed"
)

// Provider is the top-level integration contract.
type Provider interface {
	// Identity & static metadata.
	Type() string // "proxmox", "vsphere", ...
	Capabilities() []Capability
	Kinds() []Kind

	// Lifecycle.
	Connect(ctx context.Context, cfg ConnConfig) error
	Ping(ctx context.Context) error
	Close() error

	// Inventory (read).
	List(ctx context.Context, kind Kind) ([]Resource, error)
	Get(ctx context.Context, kind Kind, id string) (Resource, error)

	// Mutations (write) — routed through a typed Action.
	Do(ctx context.Context, action Action) (ActionResult, error)
}

// TaskTracker is an optional interface for providers whose mutations spawn async
// tasks. The scheduler type-asserts for it after an ActionResult carries a TaskID.
type TaskTracker interface {
	TaskStatus(ctx context.Context, taskID string) (TaskState, error)
}

// Has reports whether the provider advertises the given capability.
func Has(p Provider, c Capability) bool {
	for _, have := range p.Capabilities() {
		if have == c {
			return true
		}
	}
	return false
}
