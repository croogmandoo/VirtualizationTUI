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

## Roadmap (high level)

1. App shell + config/secrets + provider interface
2. **Proxmox VE** end-to-end (reference implementation)
3. Technitium DNS + Caddy
4. TrueNAS → 5. VMware vSphere → 6. Unraid → 7. Hyper-V
8. Polish, packaging, releases

## License

TBD.
