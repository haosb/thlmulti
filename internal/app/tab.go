package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"

	"github.com/haosb/thlmulti/internal/proc"
	"github.com/haosb/thlmulti/internal/scrollback"
)

// tab is one terminal and what the bar needs to know about it.
type tab struct {
	vt *term.Model
	// input is the goroutine that feeds the emulator whatever the user does
	// in this tab. See pump.go for why it is not the event loop.
	input *pump
	// pid is the shell in this tab, used to find its working directory when a
	// new tab is opened from it, and to see whether it is running a job.
	pid int
	// shell is the program's name, the label of last resort.
	shell string
	// title is what the program in the tab called itself, over OSC 0 or 2.
	title string
	// name is what the user called the tab. It wins over the title.
	name string
	// running means a job holds the terminal: a command is in the foreground
	// rather than the shell's prompt. Read from /proc once a second.
	running bool
	// unseen means output arrived while the tab was not in front.
	unseen bool
	// bell means the tab rang, or sent a notification, while it was not in
	// front or while the window was not focused.
	bell bool
}

// label is what the bar prints for the tab: the name given by hand, else the
// title the program set, else the shell.
func (t *tab) label() string {
	switch {
	case t.name != "":
		return t.name
	case t.title != "":
		return t.title
	default:
		return t.shell
	}
}

// marker is the one-glyph state of a tab, shown next to its number.
type marker int

const (
	markNone marker = iota
	markRunning
	markUnseen
	markBell
)

func (m marker) String() string {
	switch m {
	case markBell:
		return "!"
	case markUnseen:
		return "●"
	case markRunning:
		return "▶"
	}
	return ""
}

// marker picks the one thing most worth knowing about a tab. A bell beats
// everything: the program asked for you. Output you have not seen beats a job
// that is merely still running, because it is the one that may have finished.
func (t *tab) marker() marker {
	switch {
	case t.bell:
		return markBell
	case t.unseen:
		return markUnseen
	case t.running:
		return markRunning
	}
	return markNone
}

// tabEvent wraps an event from one tab's emulator so the loop knows which tab
// it came from. Without this, a tab exiting is indistinguishable from any
// other tab exiting.
type tabEvent struct {
	tab *tab
	ev  vaxis.Event
}

// copied carries a finished mouse selection from a tab's pump to the loop,
// which owns the clipboard and the toast.
type copied struct{ text string }

func (a *app) current() *tab { return a.tabs[a.active] }

// index is the tab's position, or -1 once it has been closed. Events from a
// tab can still arrive after it has gone.
func (a *app) index(t *tab) int {
	for i, existing := range a.tabs {
		if existing == t {
			return i
		}
	}
	return -1
}

func (a *app) openTab() error {
	cols, rows := a.vx.Window().Size()
	t := &tab{shell: filepath.Base(a.cfg.Shell[0])}
	t.vt = term.New(term.WithVaxis(a.vx))
	t.vt.TERM = a.term
	// Route this emulator's events into the single event loop, tagged so we
	// can tell which tab produced them. The post blocks rather than drops: a
	// dropped redraw would leave the tab showing stale output until something
	// else happened, and a dropped exit would leave a dead tab on the bar. The
	// emulator holds no lock while it posts, and the loop never waits on the
	// emulator, so a full queue only ever pauses the reader until the loop
	// catches up — which is what a full queue ought to do.
	t.vt.Attach(func(ev vaxis.Event) {
		a.vx.PostEventBlocking(tabEvent{tab: t, ev: ev})
	})
	t.input = newPump(func(ev vaxis.Event) { a.applyInput(t, ev) })

	cmd := exec.Command(a.cfg.Shell[0], a.cfg.Shell[1:]...)
	// A new tab starts where the tab it was opened from is standing. An empty
	// Dir means "inherit ours", which is the right answer for the first tab,
	// and for one opened from a directory that has since been removed.
	if len(a.tabs) > 0 {
		cmd.Dir = proc.Cwd(a.current().pid)
	}
	if err := t.vt.StartWithSize(cmd, cols, terminalRows(rows)); err != nil {
		t.input.close()
		return err
	}
	t.pid = cmd.Process.Pid
	if err := scrollback.Set(t.vt, a.cfg.Scrollback); err != nil {
		a.fail("scrollback: %v", err)
	}

	if len(a.tabs) > 0 {
		a.current().input.push(vaxis.FocusOut{})
	}
	a.tabs = append(a.tabs, t)
	a.active = len(a.tabs) - 1
	a.focusCurrent()
	return nil
}

// applyInput runs on a tab's pump. It hands the event to the emulator and, when
// a mouse selection has just been finished, hands the text to the loop. That
// check has to happen here, after the emulator has seen the release, and not
// on the loop, which no longer knows when that is.
func (a *app) applyInput(t *tab, ev vaxis.Event) {
	t.vt.Update(ev)
	if m, ok := ev.(vaxis.Mouse); ok && m.EventType == vaxis.EventRelease && t.vt.HasSelection() {
		// Copy on select, mirroring selection.save_to_clipboard in Alacritty.
		// The emulator only owns the selection when the child has not grabbed
		// the mouse, so this stays quiet inside vim and k9s.
		if sel := t.vt.Selection(); sel != "" {
			a.vx.PostEventBlocking(copied{text: sel})
		}
	}
}

// focusCurrent tells the tab in front that it is, and marks it seen: whatever
// it wanted you for, you are looking at it now.
func (a *app) focusCurrent() {
	t := a.current()
	t.unseen, t.bell = false, false
	t.input.push(vaxis.FocusIn{})
	a.dirty = true
}

func (a *app) selectTab(i int) {
	if i < 0 || i >= len(a.tabs) || i == a.active {
		return
	}
	a.current().input.push(vaxis.FocusOut{})
	a.active = i
	a.focusCurrent()
}

// wrap brings a position back onto the bar from either end.
func (a *app) wrap(i int) int {
	n := len(a.tabs)
	return ((i % n) + n) % n
}

// moveTab shifts the tab in front by delta places along the bar, so the tabs
// can be put in the order the work is in rather than the order it was started.
func (a *app) moveTab(delta int) {
	to := a.active + delta
	if to < 0 || to >= len(a.tabs) {
		return
	}
	a.tabs[a.active], a.tabs[to] = a.tabs[to], a.tabs[a.active]
	a.active = to
	a.dirty = true
}

// nextAttention picks the tab to jump to: the nearest tab after active that
// rang its bell, or failing that the nearest with output not yet seen. The
// search goes all the way round, so the tab in front counts last — it can
// have rung while the window was unfocused.
func nextAttention(tabs []*tab, active int) (int, bool) {
	n := len(tabs)
	if n == 0 {
		return 0, false
	}
	for _, want := range []marker{markBell, markUnseen} {
		for step := 1; step <= n; step++ {
			i := (active + step) % n
			if tabs[i].marker() == want {
				return i, true
			}
		}
	}
	return 0, false
}

// closeTab drops a tab and renumbers by position, so the tabs are always
// 1..N with no gaps. Reports whether any tab is left.
func (a *app) closeTab(t *tab) bool {
	i := a.index(t)
	if i < 0 {
		return len(a.tabs) > 0
	}
	t.input.close()
	t.vt.Close()
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	if a.confirmClose == t {
		a.confirmClose = nil
	}
	if a.rename != nil && a.rename.tab == t {
		a.rename = nil
	}
	if len(a.tabs) == 0 {
		return false
	}
	wasActive := i == a.active
	a.active = nextActive(a.active, i, len(a.tabs))
	if wasActive {
		// The tab now in front was told to blur when it went behind, and
		// nothing has told it otherwise. Left like that, its cursor stays
		// hidden and it never reports focus to its child: you are typing into
		// a tab that does not know you are there.
		a.focusCurrent()
	}
	a.dirty = true
	return true
}

// nextActive gives the active position after the tab at removed was dropped
// from a list that now holds remaining tabs. Tabs are numbered by position, so
// closing one to the left of the active tab shifts the active tab down.
func nextActive(active, removed, remaining int) int {
	switch {
	case active > removed:
		return active - 1
	case active == removed:
		return min(removed, remaining-1)
	default:
		return active
	}
}

const (
	// closeGrace is how long a shell gets to shut down on its own after
	// SIGHUP before the tab is taken away from it.
	closeGrace = 2 * time.Second
	// confirmGrace is how long a second press of the close key counts as
	// confirming the first.
	confirmGrace = 3 * time.Second
)

// forceClose asks the loop to take a tab down if it is still there.
type forceClose struct{ tab *tab }

// confirmExpiry asks the loop to forget a pending close confirmation.
type confirmExpiry struct{ gen int }

// closeActive hangs up on the tab in front, the way closing a terminal window
// does. The shell then exits on its own and reports EventClosed, which is the
// same path a tab takes when you type exit — so history is written and exit
// hooks run. Model.Close, which is what actually removes the tab, kills with
// SIGKILL and would skip all of that, so it is only the fallback for a shell
// that ignores the hangup.
//
// A tab with a job running asks to be pressed twice. Hanging up on a shell
// that is sitting at its prompt loses nothing; hanging up on an agent halfway
// through a task loses the task, and the key is one row away from ctrl+w.
func (a *app) closeActive() {
	t := a.current()
	if t.running && a.confirmClose != t {
		a.confirmClose = t
		a.confirmGen++
		gen := a.confirmGen
		postAfter(a.vx, confirmGrace, confirmExpiry{gen: gen})
		a.fail("tab %d is running a job — %s again to close it", a.active+1, a.cfg.CloseTab)
		return
	}
	a.confirmClose = nil
	if t.pid > 0 {
		// Negative pid: the whole process group, so a foreground job is hung up
		// on too. The shell is the group leader because it was started with
		// its own session.
		_ = syscall.Kill(-t.pid, syscall.SIGHUP)
	}
	postAfter(a.vx, closeGrace, forceClose{tab: t})
}

// handleTabEvent deals with what a tab's emulator reports. behind is whether the
// tab is not the one on screen: what would be a redraw or a beep in front
// becomes a mark on the bar behind.
func (a *app) handleTabEvent(ev tabEvent) bool {
	t := ev.tab
	if a.index(t) < 0 {
		return true // already closed; a last event caught up with us
	}
	behind := t != a.current()
	switch inner := ev.ev.(type) {
	case term.EventClosed:
		return a.closeTab(t)
	case term.EventPanic:
		a.fail("tab %d crashed: %v", a.index(t)+1, error(inner))
		return a.closeTab(t)
	case term.EventTitle:
		if title := strings.TrimSpace(string(inner)); title != "" && title != t.title {
			t.title = title
			a.dirty = true
		}
	case term.EventBell:
		// The host terminal gets the bell whatever tab rang it, so whatever
		// Alacritty has been told to do about a bell — flash, run a command —
		// still happens.
		a.vx.Bell()
		if behind || !a.focused {
			t.bell = true
			a.dirty = true
		}
	case term.EventNotify:
		a.vx.Notify(inner.Title, inner.Body)
		if behind || !a.focused {
			t.bell = true
		}
		a.notify(fmt.Sprintf("%d: %s", a.index(t)+1, notifyText(inner)), notifyDuration)
	case term.EventMouseShape:
		if !behind {
			a.vx.SetMouseShape(inner.Shape)
			a.dirty = true
		}
	case vaxis.Redraw:
		// The emulator reports this once per burst of output and not again
		// until it has been drawn. A tab behind is not drawn, so behind it
		// means exactly one thing: there is output you have not seen.
		if !behind {
			a.dirty = true
		} else if !t.unseen {
			t.unseen = true
			a.dirty = true
		}
	}
	return true
}

// notifyText is the one line of a notification that fits in the bar.
func notifyText(n term.EventNotify) string {
	title, body := strings.TrimSpace(n.Title), strings.TrimSpace(n.Body)
	switch {
	case title != "" && body != "":
		return title + ": " + body
	case title != "":
		return title
	default:
		return body
	}
}

// pollEvery is how often the tabs are asked whether a job is running. Once a
// second is soon enough for a mark on the bar, and cheap enough — one small
// file under /proc per tab — to run for as long as the window is open.
const pollEvery = time.Second

// tick asks the loop to look at the tabs' jobs. It is not activity: see handle.
type tick struct{}

// poll refreshes what /proc knows about every tab, and puts the history limit
// back where a child's reset may have moved it.
func (a *app) poll() {
	for _, t := range a.tabs {
		if running := proc.Running(t.pid); running != t.running {
			t.running = running
			a.dirty = true
		}
		_ = scrollback.Set(t.vt, a.cfg.Scrollback)
	}
}
