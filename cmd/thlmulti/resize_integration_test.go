package main

import (
	"strings"
	"testing"
	"time"

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
	host := startBinary(t, 80, 12)

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
