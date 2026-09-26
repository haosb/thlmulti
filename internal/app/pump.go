package app

import (
	"sync"

	"go.rockorager.dev/vaxis"
)

// pump is the one goroutine that speaks to a tab's emulator on the user's
// behalf. Everything typed, clicked or pasted into a tab goes through it, in
// order, and the event loop never waits for any of it.
//
// It exists because writing to a pty blocks. The kernel buffers about four
// kilobytes of input for a program that is not reading — one in the middle of
// a build, a sleep, a long computation — and a paste larger than that stalls
// the writer until the program gets round to reading. When the writer was the
// event loop, that stalled every tab, the tab bar and the mouse with it: the
// whole window froze until the child came back, and a child that never came
// back froze it for good. Now it stalls the pump of that one tab, and nothing
// else notices.
type pump struct {
	apply func(vaxis.Event)

	mu   sync.Mutex
	cond *sync.Cond
	// queue holds what has been pushed and not yet applied. It is unbounded
	// on purpose: it grows to the size of whatever arrived while the child was
	// not reading, which is at most the size of a paste, and dropping typed
	// characters instead would be worse than any amount of memory.
	queue []vaxis.Event
	// busy is true while apply runs. Only the tests read it, to know when
	// everything pushed has been applied: see wait in pump_test.go.
	busy   bool
	closed bool
}

func newPump(apply func(vaxis.Event)) *pump {
	p := &pump{apply: apply}
	p.cond = sync.NewCond(&p.mu)
	go p.run()
	return p
}

// push queues an event. It never blocks.
func (p *pump) push(ev vaxis.Event) {
	p.mu.Lock()
	if !p.closed {
		p.queue = append(p.queue, ev)
		p.cond.Broadcast()
	}
	p.mu.Unlock()
}

// close drops whatever has not been applied and lets the goroutine go. Events
// pushed afterwards are ignored.
func (p *pump) close() {
	p.mu.Lock()
	p.closed = true
	p.queue = nil
	p.cond.Broadcast()
	p.mu.Unlock()
}

func (p *pump) run() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		for len(p.queue) == 0 && !p.closed {
			p.cond.Wait()
		}
		if p.closed {
			return
		}
		batch := p.queue
		p.queue = nil
		p.busy = true
		p.mu.Unlock()
		for _, ev := range batch {
			p.apply(ev)
		}
		p.mu.Lock()
		p.busy = false
		p.cond.Broadcast()
	}
}
