package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis/widgets/term"
)

// fakeReclaimer is a reclaimer with the clock and the heap taken away from it,
// so its timing can be checked without waiting five seconds or allocating a
// gigabyte. arms and frees count what it asked for.
type fakeReclaimer struct {
	*reclaimer
	arms, frees int
}

func newFakeReclaimer(churn *uint64) *fakeReclaimer {
	f := &fakeReclaimer{reclaimer: &reclaimer{}}
	f.reclaimer.arm = func() { f.arms += 1 }
	f.reclaimer.churn = func() uint64 { return *churn }
	f.reclaimer.free = func() { f.frees += 1 }
	return f
}

// The timer is armed once per burst rather than reset per event, so it can go
// off in the middle of one. When it does, it has to try again instead of
// collecting while the shell is still printing.
func TestReclaimerWaitsForQuiet(t *testing.T) {
	churn := uint64(reclaimFloor)
	r := newFakeReclaimer(&churn)

	r.note()
	r.note()
	r.note()
	if r.arms != 1 {
		t.Errorf("a burst of three events armed the timer %d times, want 1", r.arms)
	}

	churn += reclaimFloor
	if r.fire() {
		t.Error("freed memory in the middle of a burst")
	}
	if r.arms != 2 {
		t.Fatalf("armed %d times after firing during a burst, want 2", r.arms)
	}

	if !r.fire() {
		t.Fatal("did not free memory after a quiet interval")
	}
	if r.frees != 1 {
		t.Errorf("freed %d times, want 1", r.frees)
	}
	if r.armed {
		t.Error("still armed after freeing: an idle terminal should stay idle")
	}

	// And the next thing that happens starts the clock again.
	r.note()
	if r.arms != 3 {
		t.Errorf("armed %d times after activity resumed, want 3", r.arms)
	}
}

// A terminal that has been sitting still has nothing to hand back, and the
// collection is not free. Sitting still must not cost anything.
func TestReclaimerSkipsAnUnchangedHeap(t *testing.T) {
	churn := uint64(reclaimFloor)
	r := newFakeReclaimer(&churn)

	r.note()
	churn += reclaimFloor
	r.fire() // during the burst: rearms
	if !r.fire() {
		t.Fatal("did not free after the churn of a burst")
	}

	r.note()
	churn += reclaimFloor / 2
	r.fire()
	if r.fire() {
		t.Errorf("freed memory for %d bytes of churn, under a floor of %d", reclaimFloor/2, reclaimFloor)
	}
}

// The reclaim itself is not activity. Counting it as such would keep arming the
// timer forever, so an idle terminal would collect its heap every five seconds
// for as long as it was left alone.
func TestReclaimEventIsNotActivity(t *testing.T) {
	churn := uint64(0)
	f := newFakeReclaimer(&churn)
	a := &app{mem: f.reclaimer}

	a.handle(toastExpiry{gen: -1}) // a no-op event, but an event
	if f.arms != 1 {
		t.Fatalf("an ordinary event armed the timer %d times, want 1", f.arms)
	}
	// The first reclaim lands during the burst that armed it and asks for one
	// more interval. From there on nothing is happening, and nothing more
	// should be scheduled.
	a.handle(reclaimMemory{})
	a.handle(reclaimMemory{})
	a.handle(reclaimMemory{})
	if f.arms != 2 {
		t.Errorf("reclaims armed the timer %d times: an idle terminal would never settle", f.arms)
	}
	if f.reclaimer.armed {
		t.Error("still armed with nothing to do")
	}
}

// rssMB is what the process is actually costing the machine, which is the
// number this whole file exists to bring down. Resident size, not heap: the
// point is the memory the kernel has handed us and not taken back.
func rssMB(t *testing.T) float64 {
	t.Helper()
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Skipf("no /proc/self/status: %v", err)
	}
	for _, line := range strings.Split(string(status), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		kb, err := strconv.Atoi(strings.Fields(line)[1])
		if err != nil {
			t.Fatalf("VmRSS: %v", err)
		}
		return float64(kb) / 1024
	}
	t.Skip("no VmRSS in /proc/self/status")
	return 0
}

// floodHistory fills one tab's history the way a build log does: more lines
// than the history can hold, most of the row left blank.
func floodHistory(t *testing.T, limit int, cols int) *term.Model {
	t.Helper()
	vt := term.New()
	vt.Resize(cols, 50)
	if err := setScrollback(vt, limit); err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 39)
	for i := range 60000 {
		vt.WriteString(fmt.Sprintf("%6d %s\r\n", i, line))
	}
	return vt
}

// The claim in the README, checked: what the history costs, and how much of
// what the process costs is not the history at all. Run it with -v to print the
// table that section quotes.
func TestReclaimHandsHistoryBackToTheOS(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a gigabyte and collects it")
	}
	const cols = 200
	for _, limit := range []int{0, 2000, 20000} {
		vt := floodHistory(t, limit, cols)

		before := rssMB(t)
		debug.FreeOSMemory()
		after := rssMB(t)

		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		live := float64(m.HeapAlloc) / (1 << 20)
		history := float64(limit*cols*bytesPerCell) / (1 << 20)
		t.Logf("scrollback=%5d: RSS %4.0f MB -> %4.0f MB after the reclaim (live heap %4.0f MB, of which history %4.0f MB)",
			limit, before, after, live, history)

		switch {
		case limit == 0:
			if after > 32 {
				t.Errorf("no history at all still costs %.0f MB", after)
			}
		default:
			// The history is the floor: what comes back is everything above it.
			if after > 0.6*before {
				t.Errorf("scrollback=%d: reclaim gave back %.0f MB of %.0f MB, expected better than 40%%",
					limit, before-after, before)
			}
			if after < history {
				t.Errorf("scrollback=%d: %.0f MB resident is less than the %.0f MB of history being held; "+
					"bytesPerCell or the emulator's history has changed", limit, after, history)
			}
		}
		runtime.KeepAlive(vt)
	}
}
