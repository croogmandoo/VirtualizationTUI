// Package secrets resolves connection credentials at runtime. Per the design
// decision (DESIGN.md §7) secrets are never written to the config file: they live
// in the OS keyring (preferred) or are supplied via environment variables for
// headless/CI use. An encrypted-file fallback is planned but not part of Phase 1.
package secrets

import (
	"fmt"
	"os"
	"strings"

	"github.com/croogmandoo/virtualizationtui/internal/config"
	"github.com/zalando/go-keyring"
)

// keyringService namespaces this app's entries in the OS keyring.
const keyringService = "virttui"

// Store abstracts secret resolution so it can be faked in tests.
type Store interface {
	// Resolve returns the secret for a connection based on its Auth config.
	Resolve(conn config.Connection) (string, error)
	// Set stores a secret for a connection in the OS keyring.
	Set(connName, secret string) error
	// Delete removes a connection's keyring entry.
	Delete(connName string) error
}

// OSStore resolves secrets from the OS keyring and environment variables.
type OSStore struct{}

// New returns the default OS-backed secret store.
func New() *OSStore { return &OSStore{} }

// Resolve returns the secret for conn. For RefEnv it reads the named environment
// variable; otherwise it reads the OS keyring under service "virttui".
func (s *OSStore) Resolve(conn config.Connection) (string, error) {
	switch conn.Auth.Ref {
	case config.RefEnv:
		name := conn.Auth.EnvVar
		if name == "" {
			name = envVarName(conn.Name)
		}
		v, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("secrets: env var %s not set for connection %q", name, conn.Name)
		}
		return v, nil
	case config.RefKeyring, "":
		v, err := keyring.Get(keyringService, conn.Name)
		if err != nil {
			return "", fmt.Errorf("secrets: keyring lookup for %q: %w", conn.Name, err)
		}
		return v, nil
	default:
		return "", fmt.Errorf("secrets: unknown ref %q", conn.Auth.Ref)
	}
}

// Set stores a secret in the OS keyring.
func (s *OSStore) Set(connName, secret string) error {
	return keyring.Set(keyringService, connName, secret)
}

// Delete removes a secret from the OS keyring. A missing entry is not an error.
func (s *OSStore) Delete(connName string) error {
	err := keyring.Delete(keyringService, connName)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}

// envVarName derives a conventional env var name from a connection name, e.g.
// "pve-01" -> "VIRTTUI_PVE_01_TOKEN".
func envVarName(connName string) string {
	up := strings.ToUpper(connName)
	up = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(up)
	return "VIRTTUI_" + up + "_TOKEN"
}
