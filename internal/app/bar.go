package app

import (
	"fmt"
	"time"

	"go.rockorager.dev/vaxis"

	"github.com/haosb/thlmulti/internal/config"
)

// The tab bar owns the top two rows: the labels, and a rule under them that
// separates the bar from the terminal. Every terminal is that much shorter.
const tabBarHeight = 2

const (
	copiedToast = "Copied to clipboard"

	toastDuration = 1500 * time.Millisecond
	// A notification is shown longer than a toast: it came from a program
	// that wanted you, and you may not have been looking.
	notifyDuration = 4 * time.Second
)

type hit struct {
	start, end int // [start, end) columns in the tab bar
	index      int
}

func terminalRows(windowRows int) int {
	return max(1, windowRows-tabBarHeight)
}

// notify puts a message in the tab bar for d, then takes it down. The timer
// posts an event rather than touching state directly: everything the loop owns
// is only ever read and written from the loop.
func (a *app) notify(text string, d time.Duration) {
	a.toast = text
	a.toastGen++
	gen := a.toastGen
	postAfter(a.vx, d, toastExpiry{gen: gen})
	a.dirty = true
}

func (a *app) fail(format string, args ...any) {
	a.status = fmt.Sprintf(format, args...)
	a.dirty = true
}

func (a *app) draw() {
	win := a.vx.Window()
	win.Clear()
	// Vaxis keeps the cursor where the last frame put it until something says
	// otherwise. Nothing did when a child hid its cursor — a full-screen
	// program drawing a frame, a shell mid-redraw — so the host cursor stayed
	// on, sitting on a cell the child had moved on from. Every frame starts
	// with no cursor, and the terminal puts it back if it has one to show.
	a.vx.HideCursor()
	cols, rows := win.Size()

	a.current().vt.Draw(win.New(0, tabBarHeight, cols, terminalRows(rows)))
	// The bar goes second so that, while a name is being typed, its cursor
	// is the one that wins.
	a.drawTabBar(win.New(0, 0, cols, tabBarHeight))
	a.vx.Render()
}

func (a *app) drawTabBar(win vaxis.Window) {
	a.hits = a.hits[:0]
	cols, _ := win.Size()
	if cols <= 0 {
		return
	}
	row := win.New(0, 0, cols, 1)
	switch {
	case a.status != "":
		row.Print(vaxis.Segment{Text: " " + a.status + " ", Style: a.theme.Failure()})
	case a.rename != nil:
		a.drawRename(row)
	default:
		// The toast takes the right end of the row, and the tabs get what is
		// left, so a message can never cover a tab or steal its click target.
		labelCols := cols
		if a.toast != "" {
			text := a.clip(" "+a.toast+" ", cols)
			width := a.vx.RenderedWidth(text)
			win.New(cols-width, 0, width, 1).Print(vaxis.Segment{Text: text, Style: a.theme.Toast()})
			labelCols = max(0, cols-width)
		}
		a.drawTabs(win.New(0, 0, labelCols, 1))
	}
	a.drawRule(win.New(0, 1, cols, 1))
}

// drawRename shows the name being typed, with a cursor at the end of it.
func (a *app) drawRename(win vaxis.Window) {
	cols, _ := win.Size()
	prompt := a.clip(fmt.Sprintf(" Rename tab %d: ", a.index(a.rename.tab)+1), cols-1)
	text := a.clipFront(string(a.rename.text), cols-1-a.vx.RenderedWidth(prompt))
	win.Fill(vaxis.Cell{Character: vaxis.Character{Grapheme: " ", Width: 1}, Style: a.theme.Toast()})
	col, _ := win.Print(vaxis.Segment{Text: prompt + text, Style: a.theme.Toast()})
	win.ShowCursor(col, 0, vaxis.CursorBeam)
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
		width := a.drawTab(win.New(col, 0, min(budget, cols-col), 1), i, t)
		a.hits = append(a.hits, hit{start: col, end: col + width, index: i})
		col += width

		if i < len(a.tabs)-1 && col < cols {
			win.New(col, 0, 1, 1).Print(vaxis.Segment{Text: "│", Style: a.theme.Tab()})
			col++
		}
	}
}

// drawTab prints one tab — " <n> ", its mark if it has one, and its label —
// into win, and reports how many columns it took. The label is trimmed at the
// front rather than the back: with titles like "user@host:~/some/project" the
// tail is what tells two tabs apart.
//
// Everything is measured and clipped before printing: a label that wrapped
// would put the click target on the wrong tab.
func (a *app) drawTab(win vaxis.Window, i int, t *tab) int {
	width, _ := win.Size()
	style := a.theme.Tab()
	if i == a.active {
		style = a.theme.Active()
	}
	mark := t.marker()

	prefix := fmt.Sprintf(" %d ", i+1)
	glyph := ""
	if mark != markNone {
		glyph = mark.String() + " "
	}
	room := width - a.vx.RenderedWidth(prefix) - a.vx.RenderedWidth(glyph) - 1
	if room < 1 {
		text := a.clip(prefix, width)
		win.Print(vaxis.Segment{Text: text, Style: style})
		return a.vx.RenderedWidth(text)
	}
	label := a.clipFront(t.label(), room) + " "
	win.Print(
		vaxis.Segment{Text: prefix, Style: style},
		vaxis.Segment{Text: glyph, Style: mark.style(a.theme, style)},
		vaxis.Segment{Text: label, Style: style},
	)
	// Measured, not taken from Print: a label that fills its window exactly
	// makes Print wrap and report column zero.
	return a.vx.RenderedWidth(prefix) + a.vx.RenderedWidth(glyph) + a.vx.RenderedWidth(label)
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

// drawRule underlines the bar, dropping a junction below each separator so the
// labels read as tabs rather than as a run of text.
func (a *app) drawRule(win vaxis.Window) {
	win.Fill(vaxis.Cell{Character: vaxis.Character{Grapheme: "─", Width: 1}, Style: a.theme.Tab()})
	for _, h := range a.hits {
		if h.index == len(a.tabs)-1 {
			continue
		}
		win.SetCell(h.end, 0, vaxis.Cell{Character: vaxis.Character{Grapheme: "┴", Width: 1}, Style: a.theme.Tab()})
	}
}

// style colours a tab's marker on top of the style the rest of the tab is
// drawn in, so the mark stands out without changing the tab's background.
func (m marker) style(t config.Theme, base vaxis.Style) vaxis.Style {
	switch m {
	case markBell:
		base.Foreground = t.BellFg
	case markUnseen:
		base.Foreground = t.UnseenFg
	}
	return base
}
