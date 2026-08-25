package main

import "testing"

func TestRouteMouse(t *testing.T) {
	for _, tc := range []struct {
		name            string
		row             int
		wheel, dragging bool
		want            mouseTarget
	}{
		{"a click in the terminal", 5, false, false, targetTerminal},
		{"a click on the tab bar switches tabs", 0, false, false, targetTabBar},
		// The bar is two rows now: labels, then the rule beneath them. The rule
		// belongs to the bar, so a click that lands a row low is not typed into
		// the shell.
		{"a click on the rule under the tabs belongs to the bar", 1, false, false, targetTabBar},
		{"the first terminal row is the one below the rule", 2, false, false, targetTerminal},

		// The bug this function exists for: dragging a selection upward past
		// the top line used to hand the release to the tab bar, so the
		// emulator never saw the drag end, nothing was copied, and the
		// selection stayed stuck mid-drag.
		{"a drag that wandered onto the tab bar stays with the terminal", 0, false, true, targetTerminal},

		// The wheel must never depend on where the pointer or the last click
		// was: a wheel that silently stops scrolling is the whole reason this
		// program exists.
		{"the wheel over the tab bar still scrolls", 0, true, false, targetTerminal},
		{"the wheel over the tab bar mid-drag still scrolls", 0, true, true, targetTerminal},
	} {
		if got := routeMouse(tc.row, tc.wheel, tc.dragging); got != tc.want {
			t.Errorf("%s: routeMouse(%d, wheel=%v, dragging=%v) = %v, want %v",
				tc.name, tc.row, tc.wheel, tc.dragging, got, tc.want)
		}
	}
}
