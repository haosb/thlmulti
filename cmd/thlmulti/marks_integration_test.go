package main

import (
	"strings"
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"
)

// ● means a tab behind you printed something. Leaving a tab is not output: the
// blur it is sent redraws its cursor, and that redraw used to arrive once the
// tab was already behind and mark it. Worse, the emulator reports a redraw only
// once until it is drawn, so the output that came later went unmarked.
func TestLeavingATabDoesNotMarkItUnseen(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the binary under a pty")
	}
	host := startBinary(t, 80, 12)

	// A second tab, then back to the first with the mouse: the host here does
	// not speak the Kitty keyboard protocol, so ctrl+1 would be a bare 1.
	host.Update(vaxis.Key{Keycode: 't', Modifiers: vaxis.ModCtrl, EventType: vaxis.EventPress})
	waitFor(t, host, "a second tab", func(rows []string) bool {
		return strings.Contains(rows[0], " 2 ")
	})
	typeLine(host, "sleep 2; echo done")
	time.Sleep(300 * time.Millisecond)
	clickTab(host, 1)

	// Quiet for longer than a redraw takes to arrive: tab 2 must not be marked.
	time.Sleep(time.Second)
	if bar := host.Rows()[0]; strings.Contains(bar, "●") {
		t.Fatalf("tab 2 printed nothing yet but is marked unseen: %q", bar)
	}
	// And when it does print, it is.
	waitFor(t, host, "tab 2 marked unseen once it printed", func(rows []string) bool {
		return strings.Contains(rows[0], "●")
	})
}

// clickTab clicks the label of tab n, found by its number in the bar.
func clickTab(host *term.Model, n int) {
	col := strings.Index(host.Rows()[0], " "+string(rune('0'+n))+" ") + 1
	for _, ev := range []vaxis.EventType{vaxis.EventPress, vaxis.EventRelease} {
		host.Update(vaxis.Mouse{Col: col, Row: 0, Button: vaxis.MouseLeftButton, EventType: ev})
	}
}
