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

> **Status: Phase 1 complete — the app shell is here.** The architecture,
> provider abstraction, and phased roadmap are documented in
> **[DESIGN.md](./DESIGN.md)**. Phase 1 ships the navigable TUI (connections
> sidebar, resource table, detail view with live sparklines, action
> confirmations, help overlay), the provider interface, and config/secrets
> handling — running against an in-memory **mock** provider so the UI is usable
> before real integrations land. **Proxmox VE** is next (Phase 2) and establishes
> the real provider pattern.

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
`?` help · `q` quit.

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
8. Polish, packaging, releases

## License

TBD.
