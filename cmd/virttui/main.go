// Command virttui is the entrypoint for VirtualizationTUI — a single terminal
// application for managing a heterogeneous homelab/small-cloud fleet.
//
// It wires the app shell (navigation, sidebar, content-driven resource table,
// detail view with sparklines, command palette, theming, action confirmations,
// help overlay) to the provider abstraction and config/secrets handling, then
// registers the built-in providers (Proxmox, Technitium, Caddy, TrueNAS,
// vSphere, Unraid, Hyper-V, and an in-memory mock) and runs the Bubble Tea
// program. Build metadata is injected at release time (see --version).
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/croogmandoo/virtualizationtui/internal/app"
	"github.com/croogmandoo/virtualizationtui/internal/config"
	"github.com/croogmandoo/virtualizationtui/internal/core"
	"github.com/croogmandoo/virtualizationtui/internal/secrets"

	// Register built-in providers via their init() functions.
	_ "github.com/croogmandoo/virtualizationtui/internal/provider/caddy"
	_ "github.com/croogmandoo/virtualizationtui/internal/provider/hyperv"
	_ "github.com/croogmandoo/virtualizationtui/internal/provider/mock"
	_ "github.com/croogmandoo/virtualizationtui/internal/provider/proxmox"
	_ "github.com/croogmandoo/virtualizationtui/internal/provider/technitium"
	_ "github.com/croogmandoo/virtualizationtui/internal/provider/truenas"
	_ "github.com/croogmandoo/virtualizationtui/internal/provider/unraid"
	_ "github.com/croogmandoo/virtualizationtui/internal/provider/vsphere"
)

// Build metadata, injected at release time via -ldflags by GoReleaser. They keep
// their defaults for `go install`/`go build` (which carries no version stamp).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var (
		cfgPath     = flag.String("config", "", "path to config file (default: XDG config dir)")
		readOnly    = flag.Bool("read-only", false, "disable all mutating actions for this session")
		initCfg     = flag.Bool("init-config", false, "write a default config file and exit")
		showVersion = flag.Bool("version", false, "print version information and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("virttui %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	path := *cfgPath
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			fail("resolve config path: %v", err)
		}
		path = p
	}

	if *initCfg {
		if err := config.Save(path, config.Default()); err != nil {
			fail("write config: %v", err)
		}
		fmt.Printf("wrote default config to %s\n", path)
		return
	}

	cfg, found, err := config.Load(path)
	if err != nil {
		fail("load config: %v", err)
	}
	if !found {
		fmt.Fprintf(os.Stderr, "no config at %s — using built-in defaults (run with --init-config to create one)\n", path)
	}
	if *readOnly {
		cfg.UI.ReadOnly = true
	}

	session := core.NewSession(cfg, secrets.New())
	defer session.Close()

	p := tea.NewProgram(app.New(session, cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fail("run: %v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "virttui: "+format+"\n", args...)
	os.Exit(1)
}
