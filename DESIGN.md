# VirtualizationTUI — Design & Architecture

> A single terminal application for managing a homelab / small-cloud fleet:
> VMware vSphere, Proxmox VE, Microsoft Hyper-V, TrueNAS, Unraid,
> Technitium DNS, and Caddy reverse-proxy routing — from one cohesive TUI.

**Status:** Design proposal (no application code yet)
**Stack decision:** Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea)
**Reference integration:** Proxmox VE (built first, establishes the provider pattern)

---

## 1. Goals & Non-Goals

### Goals
- One keyboard-driven TUI to observe and control a heterogeneous fleet.
- A clean **provider abstraction** so each platform is an interchangeable plugin
  implementing a common interface.
- Ship as a **single static binary** per OS/arch (no runtime to install).
- Safe-by-default: read operations are free; mutating operations (start/stop/
  delete/apply) require explicit confirmation.
- Secrets handled responsibly (OS keyring first, encrypted file fallback).
- Responsive UI: all network I/O is async and never blocks the render loop.

### Non-Goals (initial versions)
- Not a replacement for each platform's full web UI; we cover the
  80% of day-to-day operations (inventory, power, status, basic edits).
- No agent/daemon installed on managed hosts — we use each platform's API only.
- No multi-user server mode; this is a local operator tool.
- No provisioning-as-code (Terraform/Ansible) engine — we may *shell out* later,
  but the core is live API management.

---

## 2. Why Go + Bubble Tea

- **Single static binary** — trivial distribution to jump boxes and servers.
- **The Elm Architecture (TEA)** — Model/Update/View gives a predictable,
  testable state machine that scales to many screens.
- **Mature ecosystem for these platforms:**
  - VMware: [`govmomi`](https://github.com/vmware/govmomi) (official Go SDK).
  - Proxmox: REST API is simple; thin client or `go-proxmox`.
  - TrueNAS: REST 2.0 / WebSocket JSON-RPC.
  - Caddy / Technitium / Unraid: plain HTTP/JSON.
  - Hyper-V: WinRM/PowerShell remoting or WMI/CIM.
- **Charm libraries** ([Lip Gloss](https://github.com/charmbracelet/lipgloss),
  [Bubbles](https://github.com/charmbracelet/bubbles)) give tables, viewports,
  spinners, text inputs, and styling out of the box.

---

## 3. High-Level Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                          TUI Layer                            │
│   Bubble Tea root model → screen models (list/detail/forms)   │
│   Lip Gloss styling · Bubbles widgets · key-map / help        │
└───────────────▲───────────────────────────────▲──────────────┘
                │ tea.Msg (async results)        │ tea.Cmd (actions)
┌───────────────┴───────────────────────────────┴──────────────┐
│                       Application Core                        │
│   • Session/registry: active connections per provider         │
│   • Command bus: UI intent → provider call → result msg       │
│   • Caching & polling scheduler (refresh inventory)           │
│   • Config + secrets manager                                  │
└───────────────▲───────────────────────────────────────────────┘
                │ Provider interface (Go)
┌───────────────┴───────────────────────────────────────────────┐
│                    Provider Plugins                           │
│  proxmox │ vsphere │ hyperv │ truenas │ unraid │ technitium │ │
│                              caddy                            │
│  each: Connect · Capabilities · Resources · Actions          │
└────────────────────────────────────────────────────────────────┘
```

### Key idea: capability-driven UI
Providers advertise **capabilities** (e.g. `VMPower`, `Snapshots`, `DNSZones`,
`Routes`). The UI renders actions/columns generically based on the capabilities
and resource kinds a provider returns, so adding a provider rarely means
touching the TUI layer.

---

## 4. Provider Abstraction

A provider is anything that can be connected to and yields **resources** of one
or more **kinds**, each kind supporting a set of **actions**.

```go
// Package provider defines the contract every integration implements.
package provider

type Kind string // "vm", "container", "host", "dataset", "dns_zone", "route", ...

type Capability string
const (
    CapPowerControl Capability = "power"      // start/stop/reboot
    CapSnapshots    Capability = "snapshots"
    CapConsole      Capability = "console"     // open console / serial
    CapDNSRecords   Capability = "dns_records"
    CapRoutes       Capability = "routes"
    CapMetrics      Capability = "metrics"
)

// Provider is the top-level integration.
type Provider interface {
    // Identity & static metadata.
    Type() string                 // "proxmox", "vsphere", ...
    Capabilities() []Capability

    // Lifecycle.
    Connect(ctx context.Context, cfg ConnConfig) error
    Ping(ctx context.Context) error
    Close() error

    // Inventory (read).
    Kinds() []Kind
    List(ctx context.Context, kind Kind) ([]Resource, error)
    Get(ctx context.Context, kind Kind, id string) (Resource, error)

    // Mutations (write) — routed through a typed action.
    Do(ctx context.Context, action Action) (ActionResult, error)
}

// Resource is the normalized, UI-facing representation.
type Resource struct {
    ID      string
    Kind    Kind
    Name    string
    Status  string            // normalized: running/stopped/degraded/ok/error
    Parent  string            // node/host/pool grouping
    Fields  map[string]string // kind-specific columns (cpu, mem, ip, ...)
    Raw     any               // original object for the detail view
}

// Action is an intent the UI dispatches; providers validate & execute.
type Action struct {
    Verb   string            // "start", "stop", "snapshot", "create_record"
    Kind   Kind
    Target string            // resource ID (may be empty for "create")
    Params map[string]any
}
```

**Notes**
- `Resource.Fields` keeps the core decoupled from platform schemas; the table
  view renders whatever columns a kind declares.
- `Do` returning a typed result lets the UI show progress/toasts and refresh.
- Long-running tasks (clone, migrate) return a task handle the scheduler polls.

---

## 5. Per-Platform Integration Notes

| Platform | Protocol | Auth | Library / Approach | Primary kinds |
|---|---|---|---|---|
| **Proxmox VE** | REST `/api2/json` | **API token** (preferred) or ticket+CSRF | thin REST client / `go-proxmox` | node, vm (qemu), container (lxc), storage |
| **VMware vSphere** | SOAP/vAPI | session | `govmomi` | datacenter, host, vm, datastore, network |
| **Hyper-V** | WinRM / PowerShell remoting (or WMI/CIM) | NTLM/Kerberos/cred | `masterzen/winrm` + PS scripts | host, vm, vswitch |
| **TrueNAS** | REST 2.0 + WebSocket JSON-RPC | API key | HTTP client | pool, dataset, share, app, system |
| **Unraid** | GraphQL (Connect API) / HTTP | API key / cookie | HTTP/GraphQL client | array, disk, docker, vm, share |
| **Technitium DNS** | HTTP API | token | HTTP client | dns_zone, dns_record |
| **Caddy** | Admin REST API (`/config`, `/load`) **+ on-disk Caddyfile/JSON** | local socket / token | HTTP client | route, upstream, tls_policy |

### Proxmox (reference implementation — built first)
- **Auth:** API token (`PVEAPIToken=user@realm!tokenid=secret`). No password
  stored; tokens are revocable and scopable.
- **Inventory:** `GET /nodes`, `/nodes/{node}/qemu`, `/nodes/{node}/lxc`,
  `/cluster/resources?type=vm` for a fast fleet-wide list.
- **Actions:** `POST /nodes/{node}/qemu/{vmid}/status/{start|stop|shutdown|reboot}`,
  snapshots under `/snapshot`, config via `/config`.
- **Tasks:** mutations return a UPID; poll `/nodes/{node}/tasks/{upid}/status`.
- **TLS:** self-signed common in homelabs → explicit "trust this fingerprint"
  setting per connection (never silent `InsecureSkipVerify`).

### Caddy & Technitium
- Lowest-complexity HTTP/JSON; good candidates for the *second/third* providers
  to prove the abstraction generalizes beyond compute (DNS + routing kinds).
- **Caddy manages both the live Admin API *and* an on-disk `Caddyfile`/JSON**
  (decision): edits apply immediately via `/load`, then persist to disk so they
  survive restarts and fit GitOps/version-control workflows. The provider tracks
  drift between the live config and the on-disk file and surfaces it in the UI.

### Hyper-V
- Highest friction (no clean REST). Plan: a small set of vetted PowerShell
  commands executed over WinRM, parsing `ConvertTo-Json` output. Deferred to a
  later phase.

---

## 6. TUI / UX Design

### Layout
```
┌ VirtualizationTUI ──────────────────────────[ ? help ][ q quit ]┐
│ Connections        │  qemu/lxc on pve-01                         │
│ ▸ pve-01  (prox)   │  ┌ ID ─ Name ───── Status ─ CPU ─ Mem ───┐ │
│   esxi-a  (vsphere)│  │ 100  web-01     ● running  3%   2.1G   │ │
│   nas     (truenas)│  │ 101  db-01      ● running  41%  7.8G   │ │
│   dns     (techn.) │  │ 102  ci-runner  ○ stopped   –     –    │ │
│   edge    (caddy)  │  └────────────────────────────────────────┘ │
│                    │  [enter] details  [s] start  [x] stop  [/]   │
├────────────────────┴─────────────────────────────────────────────┤
│ status: connected · 3 VMs · last refresh 2s ago        ⠋ syncing  │
└──────────────────────────────────────────────────────────────────┘
```

- **Left pane:** connections tree (provider instances), grouped by type.
- **Right pane:** context-sensitive — resource table → detail → action forms.
- **Command palette** (`/` or `:`): fuzzy-jump to a host/VM/zone and run actions.
- **Modal confirmations** for any mutation; destructive ones require typing the
  resource name.
- **Toasts / status bar** for async task progress and errors.
- **Help overlay** (`?`) driven by a central keymap; vim-style nav (`j/k/g/G`).
- **Metrics (decision): polling + inline sparklines.** The detail view keeps a
  short rolling window (~60s) per resource and renders Unicode sparklines for
  CPU / memory / network alongside point-in-time values, e.g.
  `CPU ▁▂▅▇▆▃ 41%`. A background poller (see §3 scheduler) feeds the history;
  the window length and poll interval are configurable.

### Bubble Tea model tree
```
rootModel
 ├─ connectionsModel      (sidebar list)
 ├─ resourceListModel     (bubbles/table)
 ├─ detailModel           (bubbles/viewport)
 ├─ formModel             (bubbles/textinput, for create/edit)
 ├─ paletteModel          (fuzzy command palette)
 └─ overlay: help / confirm / toast
```
State flows one way; providers are called via `tea.Cmd` and return results as
`tea.Msg`, keeping the render loop non-blocking.

---

## 7. Configuration & Secrets

> **Auth decision: tokens/keys only.** Every provider authenticates with an API
> token/key wherever the platform supports it — we do **not** store reusable
> passwords. The single exception is **Hyper-V**, which requires WinRM
> credentials (username/password or Kerberos). vSphere uses a session login but
> the credential is exchanged for a session and not persisted long-term.


- **Config file:** `~/.config/virttui/config.yaml` (XDG). Declares connections:
  ```yaml
  connections:
    - name: pve-01
      type: proxmox
      endpoint: https://10.0.0.10:8006
      auth: { kind: token, ref: keyring }   # secret stored in OS keyring
      tls:  { fingerprint: "AA:BB:..." }     # pinned self-signed cert
  ```
- **Secrets:** never in the YAML. Order of preference:
  1. OS keyring via [`zalando/go-keyring`](https://github.com/zalando/go-keyring)
     (Keychain / libsecret / WinCred).
  2. Encrypted file fallback (age/secretbox) with a passphrase prompt.
  3. Environment variables for CI/headless use.
- **TLS:** per-connection trust store; pin fingerprints for self-signed homelab
  certs. No global insecure mode.
- **Read-only mode flag** to disable all mutating actions for a session.

---

## 8. Proposed Repository Layout

```
VirtualizationTUI/
├── cmd/virttui/main.go            # entrypoint, flag parsing, bootstrap
├── internal/
│   ├── app/                       # Bubble Tea root model + wiring
│   ├── ui/                        # screens, components, styles, keymap
│   ├── core/                      # session registry, command bus, scheduler
│   ├── config/                    # config load/save, validation
│   ├── secrets/                   # keyring + encrypted fallback
│   └── provider/
│       ├── provider.go            # interfaces (Provider, Resource, Action…)
│       ├── registry.go            # type → constructor
│       ├── proxmox/               # reference implementation
│       ├── vsphere/
│       ├── hyperv/
│       ├── truenas/
│       ├── unraid/
│       ├── technitium/
│       └── caddy/
├── docs/                          # per-provider notes, screenshots
├── DESIGN.md
├── go.mod
└── README.md
```

---

## 9. Phased Roadmap

| Phase | Deliverable | Notes |
|---|---|---|
| **0** | This design doc | ✅ you are here |
| **1** | App shell + config/secrets + provider interface ✅ | Navigation, sidebar, table, help, mock provider |
| **2** | **Proxmox provider (end-to-end)** ✅ | REST client, token auth, TLS pinning, node/qemu/lxc inventory, power/snapshot actions, UPID task polling |
| **3** | Technitium DNS + Caddy ✅ | DNS zones/records (token API) + Caddy routes with live Admin API ↔ on-disk JSON persistence &amp; drift detection |
| **4** | TrueNAS ✅ | REST v2.0, API-key auth; pools/datasets/shares; enable/disable shares |
| **5** | VMware vSphere ✅ | `govmomi`; VMs/hosts/datastores + VM power ops; tested with vcsim |
| **6** | Unraid | GraphQL Connect API |
| **7** | Hyper-V | WinRM/PowerShell; most friction, last |
| **8** | Polish | Command palette, theming, metrics/sparklines, packaging/releases |

---

## 10. Testing & Quality

- **Provider unit tests** against recorded HTTP fixtures (`httptest` + golden
  JSON) so we never need a live cluster in CI.
- **Contract test suite**: every provider runs the same conformance tests for
  the `Provider` interface (List returns valid Resources, Actions validate, …).
- **UI tests** with Bubble Tea's `teatest` for key flows (navigate, confirm,
  cancel).
- **`golangci-lint`** + `go vet` in CI; `go test ./...` on Linux/macOS/Windows.
- **Releases** via GoReleaser → static binaries + checksums per OS/arch.

---

## 11. Decisions & Remaining Questions

### Resolved
1. **Auth scope — tokens/keys only.** Standardize on API tokens/keys, stored in
   the OS keyring; no reusable passwords. Hyper-V (WinRM) is the only exception.
   *(See §7.)*
2. **Metrics depth — polling + sparklines.** Rolling ~60s history with inline
   Unicode sparklines for CPU/mem/net in the detail view. *(See §6.)*
3. **Caddy target — Admin API + on-disk Caddyfile/JSON.** Apply live via `/load`
   and persist to disk; surface drift between the two. *(See §5.)*
4. **Workflow — `main` is the stable base; changes land via PRs into `main`.**

### Still open (not blocking Phase 1/2)
5. **Hyper-V transport:** WinRM/PowerShell vs. a future REST shim — confirmed OK
   to defer Hyper-V to Phase 7.
6. **Unraid API:** official Connect GraphQL API (account-linked) vs. local
   management endpoints — decide before Phase 6.
7. **Packaging:** Homebrew tap + scoop + raw binaries — priority order, decided
   before Phase 8.

> §4 (provider interface), §5 (platform approaches), and §9 (roadmap) stand as
> the agreed plan. Next up: Phase 1 (app shell + provider interface) and
> Phase 2 (Proxmox end-to-end).
