package main

import (
	"testing"
	"time"
)

func TestShouldDraw(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dirty  bool
		queued int
		since  time.Duration
		want   bool
	}{
		{"nothing changed, nothing to draw", false, 0, time.Second, false},
		{"a change with an empty queue draws at once", true, 0, 0, true},

		// The paste this exists for: a bracketed paste arrives one key event
		// per character, and painting each one turned a 200-line paste into
		// eight seconds of full repaints. While the queue still holds events,
		// the paint waits and the whole burst lands in one frame.
		{"a change with events still queued waits", true, 500, time.Millisecond, false},

		// But it cannot wait forever. A child that never stops printing keeps
		// the queue full, and a screen that never repaints is worse than one
		// that repaints a little too often.
		{"a queue that never empties still gets a frame", true, 500, drawDeadline, true},
		{"and keeps getting them", true, 1, 5 * drawDeadline, true},
	} {
		if got := shouldDraw(tc.dirty, tc.queued, tc.since); got != tc.want {
			t.Errorf("%s: shouldDraw(%v, %d, %v) = %v, want %v",
				tc.name, tc.dirty, tc.queued, tc.since, got, tc.want)
		}
	}
}
