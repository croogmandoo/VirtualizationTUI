package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	in := Default()
	in.UI.ReadOnly = true
	if err := Save(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, found, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if !out.UI.ReadOnly {
		t.Error("read_only did not round-trip")
	}
	if len(out.Connections) != 1 || out.Connections[0].Type != "mock" {
		t.Errorf("connections did not round-trip: %+v", out.Connections)
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	_, found, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for missing file")
	}
}

func TestValidateDuplicateNames(t *testing.T) {
	c := Config{Connections: []Connection{
		{Name: "a", Type: "mock"},
		{Name: "a", Type: "mock"},
	}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected duplicate-name error")
	}
}
