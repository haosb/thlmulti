package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"
)

// Vaxis reports SIGWINCH but does not resize its own screen, so thlmulti has to
// do it. Forgetting that is invisible to every unit test here and ruinous in
// use: the program keeps painting into the area it started with, a grown window
// shows a band of blank, and anything that lays itself out to the real terminal
// width has its right-hand side cut off.
//
// Nothing short of running the real binary catches it, so this test does.
func TestResizeRepaintsAtTheNewSize(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary under a pty")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "thlmulti")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// A config of our own, so the test does not depend on the user's shell.
	cfgDir := filepath.Join(dir, "config", "thlmulti")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config"), []byte("shell = /bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	host := term.New()
	host.TERM = "xterm-256color"
	host.Attach(func(vaxis.Event) {})
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+filepath.Join(dir, "config"))
	if err := host.StartWithSize(cmd, 80, 12); err != nil {
		t.Fatal(err)
	}
	defer host.Close()

	if got := ruleWidth(t, host, 80); got != 80 {
		t.Fatalf("at startup the rule spans %d columns, want 80", got)
	}
	host.Resize(120, 20)
	if got := ruleWidth(t, host, 120); got != 120 {
		t.Errorf("after growing, the rule spans %d columns, want 120", got)
	}
	host.Resize(64, 10)
	if got := ruleWidth(t, host, 64); got != 64 {
		t.Errorf("after shrinking, the rule spans %d columns, want 64", got)
	}
}

// ruleWidth waits for the tab bar's rule row to reach want columns and returns
// what it settled on, so the test states a size rather than a delay.
func ruleWidth(t *testing.T, host *term.Model, want int) int {
	t.Helper()
	last := 0
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		rows := host.Rows()
		if len(rows) > 1 {
			rule := strings.TrimRight(rows[1], " ")
			if strings.HasPrefix(rule, "─") {
				last = len([]rune(rule))
				if last == want {
					return last
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return last
}
