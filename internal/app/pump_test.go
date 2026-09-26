package app

import (
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
)

// wait blocks until everything pushed so far has been applied, or the pump has
// been closed. It lets a test look at the emulator after the fact.
func (p *pump) wait() {
	p.mu.Lock()
	for (len(p.queue) > 0 || p.busy) && !p.closed {
		p.cond.Wait()
	}
	p.mu.Unlock()
}

// What goes into a tab has to come out in the order it went in: a paste is
// text, and text has an order.
func TestPumpKeepsOrder(t *testing.T) {
	var got []rune
	done := make(chan struct{})
	p := newPump(func(ev vaxis.Event) {
		got = append(got, ev.(vaxis.Key).Keycode)
		if len(got) == 5 {
			close(done)
		}
	})
	defer p.close()
	for _, r := range "hello" {
		p.push(vaxis.Key{Keycode: r})
	}
	<-done
	if string(got) != "hello" {
		t.Errorf("applied %q, want %q", string(got), "hello")
	}
}

// The whole point: a child that is not reading blocks its own pump and nothing
// else. Here apply blocks for as long as the test likes, and pushing thousands
// of events into it has to return at once anyway.
func TestPumpNeverBlocksThePusher(t *testing.T) {
	release := make(chan struct{})
	p := newPump(func(vaxis.Event) { <-release })
	defer p.close()

	pushed := make(chan struct{})
	go func() {
		for i := range 10000 {
			p.push(vaxis.Key{Keycode: rune(i)})
		}
		close(pushed)
	}()
	select {
	case <-pushed:
	case <-time.After(2 * time.Second):
		t.Fatal("pushing into a stalled pump blocked the pusher")
	}
	close(release)
	p.wait()
}

// Closing a pump lets its goroutine go and turns later pushes into no-ops, so
// a tab that has gone cannot be typed into by mistake.
func TestPumpClose(t *testing.T) {
	applied := 0
	p := newPump(func(vaxis.Event) { applied++ })
	p.push(vaxis.Key{Keycode: 'a'})
	p.wait()
	p.close()
	p.push(vaxis.Key{Keycode: 'b'})
	p.wait()
	if applied != 1 {
		t.Errorf("applied %d events, want 1: the one pushed before close", applied)
	}
}
