package app

import (
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
)

// chanPoster is a loop whose queue is always full: nothing is taken off it
// until the test reads.
type chanPoster chan vaxis.Event

func (c chanPoster) PostEventBlocking(ev vaxis.Event) { c <- ev }

// A timer that fires while the queue is full has to wait its turn, not give
// up: its event is the only thing that will ever clear a toast, lapse a close
// confirmation, or rearm the reclaimer.
func TestPostAfterWaitsForAFullQueue(t *testing.T) {
	loop := make(chanPoster)
	postAfter(loop, time.Millisecond, toastExpiry{gen: 7})

	// Let the timer fire and find nobody reading.
	time.Sleep(50 * time.Millisecond)

	select {
	case ev := <-loop:
		if got, ok := ev.(toastExpiry); !ok || got.gen != 7 {
			t.Fatalf("got %#v, want toastExpiry{gen: 7}", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("the event was dropped while the queue was full")
	}
}
