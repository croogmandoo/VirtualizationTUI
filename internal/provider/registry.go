package provider

import (
	"fmt"
	"sort"
	"sync"
)

// Constructor builds a fresh, unconnected Provider instance.
type Constructor func() Provider

var (
	regMu    sync.RWMutex
	registry = map[string]Constructor{}
)

// Register associates a provider type name with its constructor. It is intended
// to be called from provider packages' init() functions. Registering the same
// type twice panics, since that indicates a programming error.
func Register(typeName string, c Constructor) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := registry[typeName]; exists {
		panic(fmt.Sprintf("provider: type %q already registered", typeName))
	}
	registry[typeName] = c
}

// New constructs a provider of the given type, or an error if unknown.
func New(typeName string) (Provider, error) {
	regMu.RLock()
	c, ok := registry[typeName]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("provider: unknown type %q", typeName)
	}
	return c(), nil
}

// Types returns the sorted list of registered provider type names.
func Types() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
