package main

import (
	"runtime/debug"
	"runtime/metrics"
	"time"

	"go.rockorager.dev/vaxis"
)

// Scrollback is the only thing this program spends real memory on, and it
// spends it twice.
//
// Once on the history itself: widgets/term keeps whole rows at full width,
// bytesPerCell each (see scrollback.go), in pages of 64 rows that are allocated
// and thrown away as the history slides. Twenty thousand lines at 200 columns
// is 350 MB of live cells, and there is nothing to be done about that from out
// here — it is the emulator's data structure.
//
// And once on everything the history has already been: sixty thousand lines
// pushed through a twenty-thousand-line window allocates three times the window
// and drops two thirds of it, and Go hangs on to what it drops. Measured on one
// tab at 200 columns: 397 MB of live history, 1006 MB of RSS. That second
// number is the one people see, and better than half of it is heap the
// collector is holding for a burst that already ended.
//
// So we ask for it back. Not while anything is happening — FreeOSMemory is a
// full collection plus a walk of the heap handing pages to the kernel, 27 ms at
// that size, and a keystroke should never wait behind it — but once the
// terminal has been quiet, when 27 ms costs nobody anything. RSS at 20000 lines
// goes from 1006 MB to 397 MB, and the default 2000 from 144 MB to 57 MB.

const (
	// idleReclaim is how long nothing may happen before we hand memory back.
	// It is deliberately longer than a pause for thought in the middle of
	// typing: the point is to catch the quiet after a build or a log, not to
	// run a collection between two keystrokes.
	idleReclaim = 5 * time.Second

	// reclaimFloor is how much has to have been allocated since the last time
	// we did this for it to be worth doing again. A terminal that has been
	// sitting still has nothing to hand back, and the collection would be pure
	// cost.
	reclaimFloor = 32 << 20
)

// reclaimMemory asks the loop to hand unused heap back to the operating system.
// It goes through the loop rather than firing on the timer's own goroutine so
// that it cannot land in the middle of a draw.
type reclaimMemory struct{}

// reclaimer decides when that is worth doing. It holds no memory-management
// machinery of its own: arm, churn and free are the three things it needs from
// the outside, which is also what makes its timing testable without a terminal
// or a heap to measure.
type reclaimer struct {
	// arm schedules one reclaimMemory event, idleReclaim from now.
	arm func()
	// churn reports how many bytes have ever been allocated. Only differences
	// are ever looked at.
	churn func() uint64
	// free hands unused heap back to the kernel.
	free func()

	armed bool
	// busy records that something happened while the timer was running, which
	// is the one thing the timer cannot tell us itself.
	busy      bool
	lastChurn uint64
}

func newReclaimer(vx *vaxis.Vaxis) *reclaimer {
	return &reclaimer{
		arm:   func() { time.AfterFunc(idleReclaim, func() { vx.PostEvent(reclaimMemory{}) }) },
		churn: heapChurn,
		free:  debug.FreeOSMemory,
	}
}

// note records that something happened, and starts the clock if it is not
// already running. Called for every event the loop handles except the reclaim
// itself, so "quiet" means quiet from the shell as well as from the keyboard.
func (r *reclaimer) note() {
	r.busy = true
	if !r.armed {
		r.armed = true
		r.arm()
	}
}

// fire runs when the timer goes off, and reports whether it freed anything.
//
// The timer is armed once and not reset per event, so its going off does not by
// itself mean the terminal was quiet — only that idleReclaim has passed since
// the first event of the burst. If anything happened in between it is armed
// again, which makes the wait for quiet between idleReclaim and twice it.
func (r *reclaimer) fire() bool {
	r.armed = false
	if r.busy {
		r.busy = false
		r.armed = true
		r.arm()
		return false
	}
	churn := r.churn()
	if churn-r.lastChurn < reclaimFloor {
		return false
	}
	r.lastChurn = churn
	r.free()
	return true
}

// heapChurn is every byte the process has ever allocated on the heap. Rising by
// a lot since the last reclaim is what "there is garbage to collect" looks like
// from outside the collector: it counts what has been thrown away as well as
// what is still held, which the free-page counters do not do until a collection
// has already run.
func heapChurn() uint64 {
	sample := []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}}
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return sample[0].Value.Uint64()
}
