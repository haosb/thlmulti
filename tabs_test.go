package main

import "testing"

// Tabs are numbered by position, so closing one renumbers the rest. These are
// the cases where the active tab has to move to keep pointing at the same
// terminal.
func TestNextActive(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		active, removed, remaining int
		want                       int
	}{
		{"closing to the left shifts down", 2, 0, 3, 1},
		{"closing to the right stays put", 0, 2, 3, 0},
		{"closing the active tab keeps the position", 1, 1, 3, 1},
		{"closing the last active tab steps back", 3, 3, 3, 2},
		{"closing the only other tab", 1, 1, 1, 0},
	} {
		if got := nextActive(tc.active, tc.removed, tc.remaining); got != tc.want {
			t.Errorf("%s: nextActive(%d, %d, %d) = %d, want %d",
				tc.name, tc.active, tc.removed, tc.remaining, got, tc.want)
		}
	}
}
