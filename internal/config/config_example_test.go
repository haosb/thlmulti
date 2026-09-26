package config

import (
	"os"
	"path/filepath"
	"testing"
)

// config.example must parse, every line of it, or the starter config task
// hands people a file that refuses to load.
func TestConfigExampleLoads(t *testing.T) {
	src, err := os.ReadFile("../../config.example")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "thlmulti"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "thlmulti", "config"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	if _, err := Load(); err != nil {
		t.Fatalf("config.example does not load: %v", err)
	}
}
