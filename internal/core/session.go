// Package core is the application core: it owns the live provider connections and
// mediates between the UI (intents) and providers (calls). It deliberately has no
// dependency on the TUI layer so it can be exercised in tests headlessly.
package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/croogmandoo/virtualizationtui/internal/config"
	"github.com/croogmandoo/virtualizationtui/internal/provider"
	"github.com/croogmandoo/virtualizationtui/internal/secrets"
)

// Connection pairs a configured connection with its (lazily) connected provider.
type Connection struct {
	Cfg       config.Connection
	Provider  provider.Provider
	Connected bool
	LastErr   error
}

// Session owns all connections for a running app instance.
type Session struct {
	mu       sync.RWMutex
	conns    []*Connection
	byName   map[string]*Connection
	secrets  secrets.Store
	readOnly bool
}

// NewSession builds a session from config, constructing (but not yet connecting)
// a provider for each connection. Unknown provider types are recorded as a
// LastErr on the connection rather than failing the whole session.
func NewSession(cfg config.Config, store secrets.Store) *Session {
	s := &Session{
		byName:   map[string]*Connection{},
		secrets:  store,
		readOnly: cfg.UI.ReadOnly,
	}
	for _, c := range cfg.Connections {
		conn := &Connection{Cfg: c}
		p, err := provider.New(c.Type)
		if err != nil {
			conn.LastErr = err
		} else {
			conn.Provider = p
		}
		s.conns = append(s.conns, conn)
		s.byName[c.Name] = conn
	}
	return s
}

// Connections returns the connections in configuration order.
func (s *Session) Connections() []*Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Connection, len(s.conns))
	copy(out, s.conns)
	return out
}

// ReadOnly reports whether mutating actions are disabled for this session.
func (s *Session) ReadOnly() bool { return s.readOnly }

// Get returns the connection with the given name.
func (s *Session) Get(name string) (*Connection, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.byName[name]
	return c, ok
}

// Connect resolves the connection's secret and connects its provider.
func (s *Session) Connect(ctx context.Context, name string) error {
	c, ok := s.Get(name)
	if !ok {
		return fmt.Errorf("core: no connection %q", name)
	}
	if c.Provider == nil {
		return fmt.Errorf("core: connection %q has no provider: %w", name, c.LastErr)
	}

	cc := provider.ConnConfig{
		Name:     c.Cfg.Name,
		Endpoint: c.Cfg.Endpoint,
		Username: c.Cfg.Auth.Username,
		TLS:      provider.TLSConfig{Fingerprint: c.Cfg.TLS.Fingerprint, Insecure: c.Cfg.TLS.Insecure},
		Extra:    c.Cfg.Extra,
	}
	// Resolve the secret unless the provider type needs no credentials (e.g. mock).
	if secret, err := s.secrets.Resolve(c.Cfg); err == nil {
		if c.Cfg.Auth.Kind == config.AuthPassword {
			cc.Password = secret
		} else {
			cc.Token = secret
		}
	} else if c.Cfg.Type != "mock" {
		s.setErr(c, err)
		return err
	}

	if err := c.Provider.Connect(ctx, cc); err != nil {
		s.setErr(c, err)
		return err
	}
	s.mu.Lock()
	c.Connected = true
	c.LastErr = nil
	s.mu.Unlock()
	return nil
}

// List returns inventory of a kind from a named connection.
func (s *Session) List(ctx context.Context, name string, kind provider.Kind) ([]provider.Resource, error) {
	c, ok := s.Get(name)
	if !ok {
		return nil, fmt.Errorf("core: no connection %q", name)
	}
	if !c.Connected {
		return nil, fmt.Errorf("core: connection %q not connected", name)
	}
	return c.Provider.List(ctx, kind)
}

// Do dispatches an action to a named connection, honouring read-only mode.
func (s *Session) Do(ctx context.Context, name string, a provider.Action) (provider.ActionResult, error) {
	if s.readOnly {
		return provider.ActionResult{}, fmt.Errorf("core: session is read-only; %q on %q blocked", a.Verb, a.Target)
	}
	c, ok := s.Get(name)
	if !ok {
		return provider.ActionResult{}, fmt.Errorf("core: no connection %q", name)
	}
	if !c.Connected {
		return provider.ActionResult{}, fmt.Errorf("core: connection %q not connected", name)
	}
	return c.Provider.Do(ctx, a)
}

// Close closes every connected provider, returning the first error encountered.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for _, c := range s.conns {
		if c.Provider != nil && c.Connected {
			if err := c.Provider.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
			c.Connected = false
		}
	}
	return firstErr
}

func (s *Session) setErr(c *Connection, err error) {
	s.mu.Lock()
	c.Connected = false
	c.LastErr = err
	s.mu.Unlock()
}
