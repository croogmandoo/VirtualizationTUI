// Package config loads and saves the application configuration: the list of
// connections (provider instances) plus global UI/polling preferences. Secrets are
// never stored here — only a reference indicating where to resolve them at runtime
// (see internal/secrets).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// AuthKind identifies where a connection's secret is resolved from.
type AuthKind string

const (
	AuthToken    AuthKind = "token"    // API token / key
	AuthPassword AuthKind = "password" // username + password (e.g. Hyper-V WinRM)
)

// SecretRef indicates how to obtain a connection's secret.
type SecretRef string

const (
	RefKeyring SecretRef = "keyring" // OS keyring (preferred)
	RefEnv     SecretRef = "env"     // environment variable
)

// Auth describes a connection's authentication.
type Auth struct {
	Kind     AuthKind  `yaml:"kind"`
	Ref      SecretRef `yaml:"ref"`                // where the secret lives
	Username string    `yaml:"username,omitempty"` // for AuthPassword
	EnvVar   string    `yaml:"env_var,omitempty"`  // for RefEnv
}

// TLS captures per-connection trust settings.
type TLS struct {
	Fingerprint string `yaml:"fingerprint,omitempty"` // pinned SHA-256 cert fingerprint
	Insecure    bool   `yaml:"insecure,omitempty"`    // explicit per-connection opt-out
}

// Connection is one configured provider instance.
type Connection struct {
	Name     string            `yaml:"name"`
	Type     string            `yaml:"type"` // provider type, e.g. "proxmox"
	Endpoint string            `yaml:"endpoint"`
	Auth     Auth              `yaml:"auth"`
	TLS      TLS               `yaml:"tls,omitempty"`
	Extra    map[string]string `yaml:"extra,omitempty"`
}

// UI holds global preferences.
type UI struct {
	ReadOnly      bool          `yaml:"read_only"`      // disable all mutating actions
	PollInterval  time.Duration `yaml:"poll_interval"`  // inventory/metrics refresh cadence
	MetricsWindow int           `yaml:"metrics_window"` // sparkline rolling-window length
	Theme         string        `yaml:"theme,omitempty"`
}

// Config is the root document.
type Config struct {
	Connections []Connection `yaml:"connections"`
	UI          UI           `yaml:"ui"`
}

// Default returns a config with sensible defaults and a single mock connection so
// the Phase 1 shell is usable out of the box.
func Default() Config {
	return Config{
		Connections: []Connection{{
			Name:     "demo",
			Type:     "mock",
			Endpoint: "memory://demo",
			Auth:     Auth{Kind: AuthToken, Ref: RefKeyring},
		}},
		UI: UI{ReadOnly: false, PollInterval: 5 * time.Second, MetricsWindow: 24, Theme: "default"},
	}
}

// DefaultPath returns the XDG config path: $XDG_CONFIG_HOME/virttui/config.yaml
// (falling back to ~/.config/virttui/config.yaml).
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "virttui", "config.yaml"), nil
}

// Load reads config from path. If the file does not exist, Default() is returned
// with found=false so the caller can offer to write it.
func Load(path string) (cfg Config, found bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, true, err
	}
	return cfg, true, nil
}

// Save writes config to path, creating parent directories as needed.
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Validate checks for structural problems that would break startup.
func (c Config) Validate() error {
	seen := map[string]bool{}
	for i, conn := range c.Connections {
		if conn.Name == "" {
			return fmt.Errorf("connection %d: name is required", i)
		}
		if seen[conn.Name] {
			return fmt.Errorf("duplicate connection name %q", conn.Name)
		}
		seen[conn.Name] = true
		if conn.Type == "" {
			return fmt.Errorf("connection %q: type is required", conn.Name)
		}
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.UI.PollInterval <= 0 {
		c.UI.PollInterval = 5 * time.Second
	}
	if c.UI.MetricsWindow <= 0 {
		c.UI.MetricsWindow = 24
	}
	if c.UI.Theme == "" {
		c.UI.Theme = "default"
	}
}
