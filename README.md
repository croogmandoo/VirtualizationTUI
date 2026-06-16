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

> **Status:** early design phase. The architecture, provider abstraction, and
> phased roadmap are documented in **[DESIGN.md](./DESIGN.md)** — please start
> there. No application code has been written yet; Proxmox VE will be the first
> fully working integration and establishes the provider pattern.

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
