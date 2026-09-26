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

// Paste more than the kernel will buffer into a program that is not reading
// its terminal, and the write to that pty blocks until the program reads. When
// the event loop did the writing, the whole window froze with it: no tab
// switched, no key was answered, until the child got round to reading. This
// runs the real binary, pastes into a sleep, and then asks for a new tab. The
// tab has to appear while the sleep is still asleep.
func TestPasteIntoAStalledChildDoesNotFreezeTheWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary under a pty")
	}
	host := startBinary(t, 100, 12)

	// A shell command that reads nothing for a long while.
	typeLine(host, "sleep 60")
	waitFor(t, host, "sleep 60 to echo", func(rows []string) bool {
		return strings.Contains(strings.Join(rows, "\n"), "sleep 60")
	})
	time.Sleep(300 * time.Millisecond) // let the shell start it

	// Far more than the pty's input buffer holds.
	text := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 800)
	host.Update(vaxis.PasteStartEvent{})
	for _, r := range text {
		host.Update(vaxis.Key{Keycode: r, Text: string(r), EventType: vaxis.EventPaste})
	}
	host.Update(vaxis.PasteEndEvent{})

	// The window has to answer while the paste is stuck in the sleep's pty.
	host.Update(vaxis.Key{Keycode: 't', Modifiers: vaxis.ModCtrl, EventType: vaxis.EventPress})
	host.Update(vaxis.Key{Keycode: 't', Modifiers: vaxis.ModCtrl, EventType: vaxis.EventRelease})
	waitFor(t, host, "a second tab on the bar", func(rows []string) bool {
		return len(rows) > 0 && strings.Contains(rows[0], " 2 ")
	})
}

// startBinary builds thlmulti, runs it under an emulator standing in for the
// host terminal, and waits for the tab bar to appear.
func startBinary(t *testing.T, cols, rows int) *term.Model {
	t.Helper()
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
	if err := host.StartWithSize(cmd, cols, rows); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Close)

	waitFor(t, host, "the tab bar", func(rows []string) bool {
		return len(rows) > 1 && strings.HasPrefix(rows[1], "─")
	})
	return host
}

func typeLine(host *term.Model, s string) {
	for _, r := range s {
		host.Update(vaxis.Key{Keycode: r, Text: string(r), EventType: vaxis.EventPress})
	}
	host.Update(vaxis.Key{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPress})
}

// waitFor polls the host's screen until ok says so, and fails with the screen
// if it never does.
func waitFor(t *testing.T, host *term.Model, what string, ok func(rows []string) bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if ok(host.Rows()) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waited 8s for %s; the screen shows:\n%s", what, strings.Join(host.Rows(), "\n"))
}
