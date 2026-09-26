// Package app is thlmulti: a terminal multiplexer with tabs, and nothing else.
//
// It draws a tab bar on the top rows of the host terminal and gives every tab a
// VT emulator of its own. Two host-terminal features carry most of the weight:
// the Kitty keyboard protocol, without which ctrl+<digit> is indistinguishable
// from a bare digit, and OSC 52, which carries copied text to the system
// clipboard. Vaxis negotiates both, and falls back on its own when the host
// cannot do them.
//
// One rule holds the whole thing together: the event loop never waits on
// anything. Not on a child that has stopped reading its terminal, not on a
// clipboard tool, not on a tab that has gone quiet. Every tab has a goroutine
// of its own for what the user does in it (pump.go), everything that can block
// runs off the loop and reports back as an event, and the loop's only job is
// to route events and paint frames.
package app

import (
	"context"
	"time"

	"go.rockorager.dev/vaxis"

	"github.com/haosb/thlmulti/internal/config"
	"github.com/haosb/thlmulti/internal/scrollback"
	"github.com/haosb/thlmulti/internal/terminfo"
)

type app struct {
	vx  *vaxis.Vaxis
	cfg config.Config
	// term is what children see in TERM. See terminfo.ChildTERM for why it is not
	// simply left to the emulator's default.
	term string
	// theme is the palette the tab bar is drawn in, and mode is the palette it
	// came from, resolved: mode is never config.ThemeAuto.
	theme config.Theme
	mode  config.ThemeMode

	tabs   []*tab
	active int
	// focused is whether the host window has focus. A bell in the tab in
	// front only needs marking when nobody is looking at it.
	focused bool

	// hits maps tab bar columns back to tabs, rebuilt on every draw.
	hits []hit
	// dragging is true between a left press in the terminal and its release,
	// so the drag is not lost if the pointer crosses onto the tab bar.
	dragging bool
	// rename is the name being typed for a tab, or nil.
	rename *rename
	// confirmClose is a running tab the close key was pressed on once, and
	// confirmGen lets a later press outlive an earlier one's expiry.
	confirmClose *tab
	confirmGen   int
	// status shows why the last action failed, until the next keypress.
	status string
	// toast is a transient message in the right of the tab bar. toastGen lets
	// a later toast cancel an earlier one's expiry.
	toast    string
	toastGen int
	dirty    bool
	// mem hands the memory the scrollback churned through back to the
	// operating system, once nothing is happening. See memory.go.
	mem *reclaimer
}

// backgroundColor carries the host terminal's background colour back to the
// loop. The query has to run off the loop, because it waits for a reply that
// only arrives if the loop keeps reading events.
type backgroundColor vaxis.Color

// poster is the part of *vaxis.Vaxis that posts from off the loop.
type poster interface {
	PostEventBlocking(ev vaxis.Event)
}

// postAfter hands ev to the loop once d has passed.
//
// It posts blocking, never with PostEvent, which drops the event when the queue
// is full — and a paste or a burst of output fills it. A dropped expiry left a
// toast up for good, a close confirmation that never lapsed, a tab that ignored
// SIGHUP on the bar forever, and a reclaimer armed with no timer behind it. The
// timer runs on a goroutine of its own, so it can afford to wait for the loop.
func postAfter(vx poster, d time.Duration, ev vaxis.Event) {
	time.AfterFunc(d, func() { vx.PostEventBlocking(ev) })
}

// toastExpiry asks the loop to clear a toast. It carries the generation it was
// scheduled for, so a toast that has since been replaced is left alone.
type toastExpiry struct{ gen int }

// Run starts thlmulti in the terminal it was started from and returns when
// the last tab has closed.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	vx, err := vaxis.New(vaxis.Options{})
	if err != nil {
		return err
	}
	defer vx.Close()

	a := &app{
		vx:      vx,
		cfg:     cfg,
		term:    terminfo.ChildTERM(),
		mode:    cfg.Theme,
		mem:     newReclaimer(vx),
		focused: true,
	}
	if a.mode == config.ThemeAuto {
		// Dark until the host says otherwise: it is what almost every terminal
		// is, and it is what we fall back to if the host will not answer.
		a.mode = config.ThemeDark
		go queryBackground(vx)
	}
	a.theme = cfg.Palette(a.mode)

	if err := a.openTab(); err != nil {
		return err
	}
	a.draw()
	go func() {
		for range time.Tick(pollEvery) {
			// Unlike postAfter, dropping is fine here: another comes next second.
			vx.PostEvent(tick{})
		}
	}()

	events := vx.Events()
	lastDraw := time.Now()
	for ev := range events {
		if !a.handle(ev) {
			return nil
		}
		if !shouldDraw(a.dirty, len(events), time.Since(lastDraw)) {
			continue
		}
		a.draw()
		a.dirty = false
		lastDraw = time.Now()
	}
	return nil
}

// drawDeadline is the longest a repaint is put off while events keep arriving.
// A sixtieth of a second: short enough that a command streaming output still
// looks live, long enough that a burst coalesces into a few frames.
const drawDeadline = 16 * time.Millisecond

// shouldDraw reports whether to repaint now, given how many events are already
// waiting and how long it has been since the last repaint.
//
// Drawing costs far more than handling an event does: the whole screen is
// rendered and pushed to the host terminal, tens of kilobytes at a time. A
// bracketed paste arrives as one key event per character, so repainting on
// every event turns a 200-line paste into thousands of full repaints and locks
// the window up for seconds. Holding the paint back while events are still
// queued collapses that burst into a handful of frames, and the deadline keeps
// a stream that never lets up — a build log, a tail -f — from starving the
// screen of repaints entirely.
func shouldDraw(dirty bool, queued int, since time.Duration) bool {
	return dirty && (queued == 0 || since >= drawDeadline)
}

// backgroundTimeout bounds the wait for the host to report its background
// colour. A terminal that does not answer leaves us on the dark palette.
const backgroundTimeout = 2 * time.Second

// queryBackground asks the host what colour it is painted and hands the answer
// to the loop.
func queryBackground(vx *vaxis.Vaxis) {
	ctx, cancel := context.WithTimeout(context.Background(), backgroundTimeout)
	defer cancel()
	// Blocking for the same reason as postAfter: a dropped answer would leave a
	// light terminal on the dark palette.
	vx.PostEventBlocking(backgroundColor(vx.QueryBackgroundContext(ctx)))
}

// setThemeMode repaints the bar in a different palette, keeping whatever the
// config pinned by hand.
func (a *app) setThemeMode(mode config.ThemeMode) {
	if mode == a.mode {
		return
	}
	a.mode = mode
	a.theme = a.cfg.Palette(mode)
	a.dirty = true
}

// handle processes one event and reports whether to keep running.
func (a *app) handle(ev vaxis.Event) bool {
	// Two events are not activity: the reclaim, and the poll that asks the
	// tabs how they are doing. Counting either would keep the terminal awake
	// forever, checking a heap nothing is touching. Everything else, from a
	// keystroke to a line of output, is.
	switch ev.(type) {
	case reclaimMemory:
		a.mem.fire()
		return true
	case tick:
		a.poll()
		return true
	}
	a.mem.note()

	switch ev := ev.(type) {
	case tabEvent:
		return a.handleTabEvent(ev)
	case copied:
		a.vx.ClipboardPush(ev.text)
		a.notify(copiedToast, toastDuration)
	case clipboardText:
		a.deliverPaste(ev)
	case toastExpiry:
		if ev.gen == a.toastGen {
			a.toast = ""
			a.dirty = true
		}
	case confirmExpiry:
		if ev.gen == a.confirmGen {
			a.confirmClose = nil
		}
	case forceClose:
		// Still here after the grace period, so the shell did not take the
		// hint. Nothing happens for a tab that already went: closeTab looks
		// it up first.
		return a.closeTab(ev.tab)
	case vaxis.Resize:
		// Vaxis catches SIGWINCH and reports it, but does not resize its own
		// screen: that is the caller's job. Without this the whole program keeps
		// painting into the area it had at startup, so a grown window shows a
		// band of blank, and anything that lays itself out to the real width —
		// ls, a full-screen TUI — has its right-hand side cut off.
		a.vx.Resize(ev)
		for _, t := range a.tabs {
			t.vt.Resize(ev.Cols, terminalRows(ev.Rows))
			// A resize can rebuild the history buffer, and a rebuilt one comes
			// back with the library's limit rather than ours.
			_ = scrollback.Set(t.vt, a.cfg.Scrollback)
		}
		a.dirty = true
	case backgroundColor:
		if mode, ok := config.ModeForBackground(vaxis.Color(ev)); ok && a.cfg.Theme == config.ThemeAuto {
			a.setThemeMode(mode)
		}
	case vaxis.ColorThemeUpdate:
		if a.cfg.Theme == config.ThemeAuto {
			switch ev.Mode {
			case vaxis.DarkMode:
				a.setThemeMode(config.ThemeDark)
			case vaxis.LightMode:
				a.setThemeMode(config.ThemeLight)
			}
		}
		a.current().input.push(ev)
	case vaxis.SyncFunc:
		// Vaxis asks for a function to be run on the loop; this is the loop.
		ev()
		a.dirty = true
	case vaxis.FocusIn:
		a.focused = true
		// Whatever the tab in front wanted while you were away, you are
		// looking at it now.
		a.current().bell = false
		a.current().input.push(ev)
		a.dirty = true
	case vaxis.FocusOut:
		a.focused = false
		// A drag cannot still be in progress in a window that just lost
		// focus, and the release for it will never arrive.
		a.dragging = false
		a.current().input.push(ev)
	case vaxis.Key:
		a.handleKey(ev)
	case vaxis.Mouse:
		a.handleMouse(ev)
	default:
		// Paste brackets and the like: all belong to whichever child is on
		// screen.
		a.current().input.push(ev)
	}
	return true
}
