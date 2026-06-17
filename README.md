# VirtualizationTUI

A single keyboard-driven terminal application for managing a heterogeneous
homelab / small-cloud fleet from one place:

- **VMware vSphere**
- **Proxmox VE**
- **Microsoft Hyper-V**
- **TrueNAS**
- **Unraid**
- **Technitium DNS** (zones & records)
- **Caddy** (reverse-proxy routing)

Built in **Go** with **[Bubble Tea](https://github.com/charmbracelet/bubbletea)**,
distributed as a single static binary.

> **Status: feature-complete (Phases 1–8).** All seven platform integrations —
> Proxmox VE, VMware vSphere, Hyper-V, TrueNAS, Unraid, Technitium DNS, and
> Caddy — plus the app shell and the Phase 8 polish (theming, command palette,
> content-driven columns, cross-poll sparklines, and GoReleaser packaging) are on
> `main`. The architecture, provider abstraction, and roadmap are documented in
> **[DESIGN.md](./DESIGN.md)**. Out of the box it runs against an in-memory
> **mock** provider, so the navigable TUI (connections sidebar, resource table,
> detail view with live sparklines, command palette, action confirmations, help
> overlay) works with zero configuration.

## Install

**Prebuilt binaries** (recommended): grab the archive for your OS/arch from the
[Releases](https://github.com/croogmandoo/VirtualizationTUI/releases) page, verify
it against `checksums.txt`, extract `virttui`, and put it on your `PATH`. Static,
cross-compiled binaries are published for Linux, macOS, and Windows (amd64 +
arm64) on every tagged release.

**From source** (needs Go 1.25+):

```sh
go install github.com/croogmandoo/virtualizationtui/cmd/virttui@latest
```

Check the build with `virttui --version`.

## Quick start

```sh
# Build
go build -o virttui ./cmd/virttui

# (optional) write a starter config to $XDG_CONFIG_HOME/virttui/config.yaml
./virttui --init-config

# Run — with no config it falls back to a built-in mock connection
./virttui

# Run without mutating actions enabled
./virttui --read-only
```

**Keys:** `↑/k ↓/j` move · `←/h →/l` switch sidebar/table · `enter` details ·
`esc` back · `s` start · `x` stop · `R` reboot · `S` snapshot · `r` refresh ·
`/` command palette · `t` cycle theme · `?` help · `q` quit.

Press `/` (or `:`) to open the **command palette** — a fuzzy launcher for every
action in context: refresh, switch theme or connection, open details, and (outside
read-only mode) power actions on the selected resource.

The resource table is **content-driven**: it shows whichever columns the current
inventory actually carries (CPU/Mem for hypervisors, type/value/TTL for DNS,
upstream/match for proxy routes), so no UI change is needed per platform. The
detail view's **sparklines accumulate across polls**, so even providers that only
report a point-in-time value build up rolling history while the app runs.

Themes are selectable with `t` or pinned in config (`ui.theme:` — one of
`default`, `nord`, `dracula`, `gruvbox`, `solarized-light`).

## How it works (in brief)

Each platform is a **provider plugin** implementing a common Go interface. A
provider exposes normalized **resources** (VMs, containers, datasets, DNS zones,
routes, …) and **actions** (start, stop, snapshot, create record, …). The TUI
renders generically from provider **capabilities**, so adding a platform rarely
touches the UI layer.

See [DESIGN.md](./DESIGN.md) for the full architecture, per-platform API notes,
configuration/secrets model, and roadmap.

## Connecting to Proxmox VE

Create an API token in Proxmox (Datacenter → Permissions → API Tokens), then add
a connection to `config.yaml` and store the token secret in your OS keyring:

```yaml
connections:
  - name: pve-01
    type: proxmox
    endpoint: https://10.0.0.10:8006
    auth:
      kind: token
      ref: keyring            # secret resolved from the OS keyring (never stored here)
    tls:
      fingerprint: "AA:BB:CC:…"  # pin the self-signed cert (recommended for homelabs)
```

The keyring entry uses service `virttui` and username = the connection name
(`pve-01`); its value is the full token credential `user@realm!tokenid=secret`.
For headless/CI use, set `auth.ref: env` and export `VIRTTUI_PVE_01_TOKEN`.

**Validate against a real host.** An opt-in smoke test exercises connect → ping →
list nodes/VMs/containers (and, optionally, a power action) against a live
Proxmox. It is skipped unless `PVE_ENDPOINT`/`PVE_TOKEN` are set, so run it from a
machine that can reach the host:

```sh
PVE_ENDPOINT=https://172.20.200.250:8006 \
PVE_TOKEN='root@pam!tui=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx' \
PVE_INSECURE=1 \
go test ./internal/provider/proxmox -run TestLiveProxmox -v
```

Pin the cert with `PVE_FINGERPRINT=<sha256>` instead of `PVE_INSECURE=1` for a
verified connection, and add `PVE_TEST_VMID=<id> PVE_TEST_ACTION=reboot` to also
exercise a power action and task polling against a throwaway guest.

## Connecting to Technitium DNS &amp; Caddy

```yaml
connections:
  - name: dns
    type: technitium
    endpoint: http://10.0.0.30:5380   # Technitium HTTP API
    auth: { kind: token, ref: keyring } # token = a Technitium API token

  - name: edge
    type: caddy
    endpoint: http://10.0.0.31:2019    # Caddy Admin API
    auth: { kind: token, ref: keyring } # token unused by Caddy admin; kept for uniformity
    extra:
      config_file: /etc/caddy/caddy.json  # enables live↔disk persistence &amp; drift detection
```

For Caddy, `persist` writes the live config to `config_file`, `load` pushes the
file back into the running server via `/load`, and `diff` reports drift between
the two.

## Connecting to VMware vSphere

vSphere uses a session login (username + password — the documented exception to
the tokens-only rule, since the credential is exchanged for a session and not
persisted):

```yaml
- name: vcenter
  type: vsphere
  endpoint: https://vcenter.lan      # /sdk appended automatically
  auth:
    kind: password
    ref: keyring                      # password resolved from the OS keyring
    username: administrator@vsphere.local
  tls:
    insecure: false                   # set true only for self-signed lab certs
```

## Connecting to Hyper-V

Hyper-V is driven via PowerShell cmdlets over **WinRM** (enable with
`Enable-PSRemoting` on the host). Like vSphere it uses username/password — the
documented exception to tokens-only:

```yaml
- name: hyperv
  type: hyperv
  endpoint: host01            # or host01:5986 / https://host01:5986 for WinRM-over-TLS
  auth:
    kind: password
    ref: keyring
    username: DOMAIN\\administrator
  tls:
    insecure: false           # set true to skip TLS verification on 5986
```

## Roadmap (high level)

1. ✅ App shell + config/secrets + provider interface
2. ✅ **Proxmox VE** end-to-end (reference implementation)
3. ✅ Technitium DNS + Caddy
4. ✅ **TrueNAS** (pools, datasets, shares) → 5. ✅ **VMware vSphere** (VMs, hosts, datastores) → 6. ✅ **Unraid** (array, docker, VMs, shares) → 7. ✅ **Hyper-V** (VMs, host; PowerShell/WinRM)
8. ✅ Polish — theming, content-driven columns, cross-poll sparklines, command palette, packaging/releases (GoReleaser)

## License

TBD.
