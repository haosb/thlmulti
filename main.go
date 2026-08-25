// thlmulti is a terminal multiplexer with tabs, and nothing else.
//
// It draws a tab bar on the top rows of the host terminal and gives every tab a
// VT emulator of its own. Two host-terminal features carry most of the weight:
// the Kitty keyboard protocol, without which ctrl+<digit> is indistinguishable
// from a bare digit, and OSC 52, which carries copied text to the system
// clipboard. Vaxis negotiates both, and falls back on its own when the host
// cannot do them.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"
)

// The tab bar owns the top two rows: the labels, and a rule under them that
// separates the bar from the terminal. Every terminal is that much shorter.
const tabBarHeight = 2

const (
	copiedToast = "Copied to clipboard"

	toastDuration = 1500 * time.Millisecond
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "thlmulti:", err)
		os.Exit(1)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "thlmulti:", err)
		os.Exit(1)
	}
}

type app struct {
	vx  *vaxis.Vaxis
	cfg Config
	// term is what children see in TERM. See childTERM for why it is not
	// simply left to the emulator's default.
	term string
	// theme is the palette the tab bar is drawn in, and mode is the palette it
	// came from, resolved: mode is never ThemeAuto.
	theme Theme
	mode  ThemeMode

	tabs   []*tab
	active int

	// hits maps tab bar columns back to tabs, rebuilt on every draw.
	hits []hit
	// dragging is true between a left press in the terminal and its release,
	// so the drag is not lost if the pointer crosses onto the tab bar.
	dragging bool
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

// toastExpiry asks the loop to clear a toast. It carries the generation it was
// scheduled for, so a toast that has since been replaced is left alone.
type toastExpiry struct{ gen int }

type tab struct {
	vt    *term.Model
	title string
	// pid is the shell in this tab, used to find its working directory when a
	// new tab is opened from it.
	pid int
}

type hit struct {
	start, end int // [start, end) columns in the tab bar
	index      int
}

// tabEvent wraps an event from one tab's emulator so the loop knows which tab
// it came from. Without this, a tab exiting is indistinguishable from any
// other tab exiting.
type tabEvent struct {
	tab *tab
	ev  vaxis.Event
}

func run(cfg Config) error {
	vx, err := vaxis.New(vaxis.Options{})
	if err != nil {
		return err
	}
	defer vx.Close()

	a := &app{vx: vx, cfg: cfg, term: childTERM(), mode: cfg.Theme, mem: newReclaimer(vx)}
	if a.mode == ThemeAuto {
		// Dark until the host says otherwise: it is what almost every terminal
		// is, and it is what we fall back to if the host will not answer.
		a.mode = ThemeDark
		go queryBackground(vx)
	}
	a.theme = cfg.theme(a.mode)

	if err := a.openTab(); err != nil {
		return err
	}
	a.draw()

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

// queryBackground asks the host what colour it is painted and hands the answer
// to the loop. A terminal that does not answer leaves us on the dark palette,
// so the wait is bounded rather than forever.
func queryBackground(vx *vaxis.Vaxis) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	vx.PostEvent(backgroundColor(vx.QueryBackgroundContext(ctx)))
}

func (a *app) current() *tab { return a.tabs[a.active] }

// setThemeMode repaints the bar in a different palette, keeping whatever the
// config pinned by hand.
func (a *app) setThemeMode(mode ThemeMode) {
	if mode == a.mode {
		return
	}
	a.mode = mode
	a.theme = a.cfg.theme(mode)
	a.dirty = true
}

func (a *app) openTab() error {
	cols, rows := a.vx.Window().Size()
	t := &tab{title: filepath.Base(a.cfg.Shell[0])}
	t.vt = term.New(term.WithVaxis(a.vx))
	t.vt.TERM = a.term
	// Route this emulator's events into the single event loop, tagged so we
	// can tell which tab produced them.
	t.vt.Attach(func(ev vaxis.Event) {
		a.vx.PostEvent(tabEvent{tab: t, ev: ev})
	})

	cmd := exec.Command(a.cfg.Shell[0], a.cfg.Shell[1:]...)
	// A new tab starts where the tab it was opened from is standing. An empty
	// Dir means "inherit ours", which is the right answer for the first tab.
	if len(a.tabs) > 0 {
		cmd.Dir = procCwd(a.current().pid)
	}
	if err := t.vt.StartWithSize(cmd, cols, terminalRows(rows)); err != nil {
		return err
	}
	t.pid = cmd.Process.Pid
	if err := setScrollback(t.vt, a.cfg.Scrollback); err != nil {
		a.fail("scrollback: %v", err)
	}

	if len(a.tabs) > 0 {
		a.current().vt.Blur()
	}
	a.tabs = append(a.tabs, t)
	a.active = len(a.tabs) - 1
	t.vt.Focus()
	a.dirty = true
	return nil
}

func (a *app) selectTab(i int) {
	if i < 0 || i >= len(a.tabs) || i == a.active {
		return
	}
	a.current().vt.Blur()
	a.active = i
	a.current().vt.Focus()
	a.dirty = true
}

// closeTab drops a tab and renumbers by position, so the tabs are always
// 1..N with no gaps. Reports whether any tab is left.
func (a *app) closeTab(t *tab) bool {
	for i, existing := range a.tabs {
		if existing != t {
			continue
		}
		t.vt.Close()
		a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
		if len(a.tabs) == 0 {
			return false
		}
		next := nextActive(a.active, i, len(a.tabs))
		if next != a.active {
			a.active = next
			a.current().vt.Focus()
		}
		a.dirty = true
		return true
	}
	return len(a.tabs) > 0
}

// closeGrace is how long a shell gets to shut down on its own after SIGHUP
// before the tab is taken away from it.
const closeGrace = 2 * time.Second

// forceClose asks the loop to take a tab down if it is still there.
type forceClose struct{ tab *tab }

// closeActive hangs up on the tab in front, the way closing a terminal window
// does. The shell then exits on its own and reports EventClosed, which is the
// same path a tab takes when you type exit — so history is written and exit
// hooks run. Model.Close, which is what actually removes the tab, kills with
// SIGKILL and would skip all of that, so it is only the fallback for a shell
// that ignores the hangup.
func (a *app) closeActive() {
	t := a.current()
	if t.pid > 0 {
		// Negative pid: the whole process group, so a foreground job is hung up
		// on too. The shell is the group leader because it was started with
		// its own session.
		_ = syscall.Kill(-t.pid, syscall.SIGHUP)
	}
	time.AfterFunc(closeGrace, func() { a.vx.PostEvent(forceClose{tab: t}) })
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

// handle processes one event and reports whether to keep running.
func (a *app) handle(ev vaxis.Event) bool {
	// The reclaim is the one event that is not activity: counting it as such
	// would keep the terminal awake forever, checking a heap nothing is
	// touching. Everything else, from a keystroke to a line of output, is.
	if _, ok := ev.(reclaimMemory); ok {
		a.mem.fire()
		return true
	}
	a.mem.note()

	switch ev := ev.(type) {
	case tabEvent:
		return a.handleTabEvent(ev)
	case toastExpiry:
		if ev.gen == a.toastGen {
			a.toast = ""
			a.dirty = true
		}
	case forceClose:
		// Still here after the grace period, so the shell did not take the
		// hint. Nothing arrives for a tab that already went, because closeTab
		// removed it from the list.
		for _, t := range a.tabs {
			if t == ev.tab {
				return a.closeTab(t)
			}
		}
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
			_ = setScrollback(t.vt, a.cfg.Scrollback)
		}
		a.dirty = true
	case backgroundColor:
		if mode, ok := modeForBackground(vaxis.Color(ev)); ok && a.cfg.Theme == ThemeAuto {
			a.setThemeMode(mode)
		}
	case vaxis.ColorThemeUpdate:
		if a.cfg.Theme == ThemeAuto {
			switch ev.Mode {
			case vaxis.DarkMode:
				a.setThemeMode(ThemeDark)
			case vaxis.LightMode:
				a.setThemeMode(ThemeLight)
			}
		}
		a.current().vt.Update(ev)
		a.dirty = true
	case vaxis.Key:
		a.handleKey(ev)
	case vaxis.Mouse:
		a.handleMouse(ev)
	default:
		if _, ok := ev.(vaxis.FocusOut); ok {
			// A drag cannot still be in progress in a window that just lost
			// focus, and the release for it will never arrive.
			a.dragging = false
		}
		// Paste brackets, focus changes, colour scheme reports: all belong to
		// whichever child is on screen.
		a.current().vt.Update(ev)
		a.dirty = true
	}
	return true
}

func (a *app) handleTabEvent(ev tabEvent) bool {
	switch inner := ev.ev.(type) {
	case term.EventClosed:
		return a.closeTab(ev.tab)
	case term.EventTitle:
		if title := strings.TrimSpace(string(inner)); title != "" {
			ev.tab.title = title
			a.dirty = true
		}
	case vaxis.Redraw:
		// A background tab redrawing is not a reason to repaint: only the
		// active tab is on screen. This matters when a background tab is
		// streaming, which is the common case for a tail or a log follow.
		if ev.tab == a.current() {
			a.dirty = true
		}
	}
	return true
}

func (a *app) handleKey(k vaxis.Key) {
	if a.status != "" {
		a.status = ""
		a.dirty = true
	}
	switch {
	case a.cfg.NewTab.matches(k):
		a.onPress(k, func() {
			if err := a.openTab(); err != nil {
				a.fail("new tab: %v", err)
			}
		})
		return
	case a.cfg.CloseTab.matches(k):
		a.onPress(k, a.closeActive)
		return
	case a.cfg.Paste.matches(k):
		a.onPress(k, a.paste)
		return
	}
	if i, ok := a.tabDigit(k); ok {
		a.onPress(k, func() { a.selectTab(i) })
		return
	}
	a.current().vt.Update(k)
	a.dirty = true
}

// onPress runs act for a press or repeat, and swallows the matching release so
// the child never sees half of a chord we consumed.
func (a *app) onPress(k vaxis.Key, act func()) {
	if k.EventType == vaxis.EventPress || k.EventType == vaxis.EventRepeat {
		act()
	}
}

// tabDigit maps the configured modifier plus 1..9 or 0 to a tab position.
func (a *app) tabDigit(k vaxis.Key) (int, bool) {
	for d := range 10 {
		if !k.Matches(rune('0'+d), a.cfg.TabMods) {
			continue
		}
		if d == 0 {
			return 9, true // ctrl+0 is the tenth tab
		}
		return d - 1, true
	}
	return 0, false
}

// mouseTarget is where a mouse event is delivered.
type mouseTarget int

const (
	targetTerminal mouseTarget = iota
	targetTabBar
)

// routeMouse decides who gets a mouse event.
//
// Two rules, and both exist because of a way this can go wrong:
//
// The wheel always reaches the terminal, wherever the pointer is. A wheel that
// stops scrolling because of where you last clicked is the one thing that must
// never happen.
//
// A drag that began in the terminal stays with the terminal even when the
// pointer wanders up onto the tab bar. Otherwise the release lands on the bar,
// the emulator never learns the drag ended, and the selection is stuck
// mid-drag: nothing gets copied and the next click extends the old selection.
func routeMouse(row int, wheel, dragging bool) mouseTarget {
	if row >= tabBarHeight || wheel || dragging {
		return targetTerminal
	}
	return targetTabBar
}

func (a *app) handleMouse(m vaxis.Mouse) {
	wheel := m.Button == vaxis.MouseWheelUp || m.Button == vaxis.MouseWheelDown

	if routeMouse(m.Row, wheel, a.dragging) == targetTabBar {
		if m.EventType == vaxis.EventPress && m.Button == vaxis.MouseLeftButton {
			for _, h := range a.hits {
				if m.Col >= h.start && m.Col < h.end {
					a.selectTab(h.index)
					break
				}
			}
		}
		return
	}

	switch {
	case m.EventType == vaxis.EventPress && m.Button == vaxis.MouseLeftButton:
		a.dragging = true
	case m.EventType == vaxis.EventRelease, m.Button == vaxis.MouseNoButton:
		// Also clear on a no-button event, not just on release: if the release
		// never arrives — focus lost mid-drag, a click swallowed by the
		// compositor — a latched flag would send every later tab bar click to
		// the terminal, and clicking tabs would stop working for good.
		a.dragging = false
	}

	m.Row = max(0, m.Row-tabBarHeight)
	t := a.current()
	t.vt.Update(m)

	// Copy on select, mirroring selection.save_to_clipboard in Alacritty. The
	// emulator only owns the selection when the child has not grabbed the
	// mouse, so this stays quiet inside vim and k9s.
	if m.EventType == vaxis.EventRelease && t.vt.HasSelection() {
		if sel := t.vt.Selection(); sel != "" {
			a.vx.ClipboardPush(sel)
			a.notify(copiedToast, toastDuration)
		}
	}
	a.dirty = true
}

// paste reads the system clipboard and hands it to the focused child as a
// bracketed paste, so the child can tell it apart from typing.
func (a *app) paste() {
	text, err := readClipboard()
	if err != nil {
		a.fail("paste: %v", err)
		return
	}
	if text == "" {
		return
	}
	vt := a.current().vt
	vt.Update(vaxis.PasteStartEvent{})
	for _, r := range text {
		vt.Update(vaxis.Key{
			Keycode:   r,
			Text:      string(r),
			EventType: vaxis.EventPaste,
		})
	}
	vt.Update(vaxis.PasteEndEvent{})
	a.dirty = true
}

// fitLabel renders " <n> <title> " inside width columns, trimming the front of
// the title rather than the back: with titles like "user@host:~/some/project"
// the tail is what tells two tabs apart.
func (a *app) fitLabel(n int, title string, width int) string {
	prefix := fmt.Sprintf(" %d ", n)
	room := width - a.vx.RenderedWidth(prefix) - 1
	if room < 1 {
		return a.clip(prefix, width)
	}
	return prefix + a.clipFront(title, room) + " "
}

// clipFront drops columns from the start of s, marking the cut with an ellipsis.
func (a *app) clipFront(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if a.vx.RenderedWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && a.vx.RenderedWidth("…"+string(runes)) > width {
		runes = runes[1:]
	}
	return "…" + string(runes)
}

// clip shortens s to at most width columns, measured the way the host terminal
// will render it rather than by counting bytes or runes.
func (a *app) clip(s string, width int) string {
	if a.vx.RenderedWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && a.vx.RenderedWidth(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// notify puts a message in the tab bar for d, then takes it down. The timer
// posts an event rather than touching state directly: everything the loop owns
// is only ever read and written from the loop.
func (a *app) notify(text string, d time.Duration) {
	a.toast = text
	a.toastGen++
	gen := a.toastGen
	time.AfterFunc(d, func() { a.vx.PostEvent(toastExpiry{gen: gen}) })
	a.dirty = true
}

func (a *app) fail(format string, args ...any) {
	a.status = fmt.Sprintf(format, args...)
	a.dirty = true
}

func terminalRows(windowRows int) int {
	return max(1, windowRows-tabBarHeight)
}

func (a *app) draw() {
	win := a.vx.Window()
	win.Clear()
	cols, rows := win.Size()

	a.drawTabBar(win.New(0, 0, cols, tabBarHeight))
	a.current().vt.Draw(win.New(0, tabBarHeight, cols, terminalRows(rows)))
	a.vx.Render()
}

func (a *app) drawTabBar(win vaxis.Window) {
	a.hits = a.hits[:0]
	cols, _ := win.Size()
	if cols <= 0 {
		return
	}
	if a.status != "" {
		win.New(0, 0, cols, 1).Print(vaxis.Segment{Text: " " + a.status + " ", Style: a.theme.failure()})
		a.drawRule(win.New(0, 1, cols, 1))
		return
	}

	// The toast takes the right end of the row, and the tabs get what is left,
	// so a message can never cover a tab or steal its click target.
	labelCols := cols
	if a.toast != "" {
		text := a.clip(" "+a.toast+" ", cols)
		width := a.vx.RenderedWidth(text)
		win.New(cols-width, 0, width, 1).Print(vaxis.Segment{Text: text, Style: a.theme.toast()})
		labelCols = max(0, cols-width)
	}

	a.drawTabs(win.New(0, 0, labelCols, 1))
	a.drawRule(win.New(0, 1, cols, 1))
}

func (a *app) drawTabs(win vaxis.Window) {
	cols, _ := win.Size()
	// Every tab gets a fair share of the row, minus the separators between
	// them. Without this, one long title — zsh sets it to the whole working
	// directory — fills the entire row, and the other tabs can be neither seen
	// nor clicked.
	budget := (cols - (len(a.tabs) - 1)) / max(1, len(a.tabs))
	col := 0
	for i, t := range a.tabs {
		if col >= cols {
			break
		}
		style := a.theme.tab()
		if i == a.active {
			style = a.theme.active()
		}
		// Measure and clip before printing: a label that wrapped would put the
		// click target on the wrong tab.
		label := a.fitLabel(i+1, t.title, min(budget, cols-col))
		width := a.vx.RenderedWidth(label)
		win.New(col, 0, width, 1).Print(vaxis.Segment{Text: label, Style: style})
		a.hits = append(a.hits, hit{start: col, end: col + width, index: i})
		col += width

		if i < len(a.tabs)-1 && col < cols {
			win.New(col, 0, 1, 1).Print(vaxis.Segment{Text: "│", Style: a.theme.tab()})
			col++
		}
	}
}

// drawRule underlines the bar, dropping a junction below each separator so the
// labels read as tabs rather than as a run of text.
func (a *app) drawRule(win vaxis.Window) {
	win.Fill(vaxis.Cell{Character: vaxis.Character{Grapheme: "─", Width: 1}, Style: a.theme.tab()})
	for _, h := range a.hits {
		if h.index == len(a.tabs)-1 {
			continue
		}
		win.SetCell(h.end, 0, vaxis.Cell{Character: vaxis.Character{Grapheme: "┴", Width: 1}, Style: a.theme.tab()})
	}
}
