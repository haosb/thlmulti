package app

import (
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/term"
)

// Tabs are numbered by position, so closing one renumbers the rest. These are
// the cases where the active tab has to move to keep pointing at the same
// terminal.
func TestNextActive(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		active, removed, remaining int
		want                       int
	}{
		{"closing to the left shifts down", 2, 0, 3, 1},
		{"closing to the right stays put", 0, 2, 3, 0},
		{"closing the active tab keeps the position", 1, 1, 3, 1},
		{"closing the last active tab steps back", 3, 3, 3, 2},
		{"closing the only other tab", 1, 1, 1, 0},
	} {
		if got := nextActive(tc.active, tc.removed, tc.remaining); got != tc.want {
			t.Errorf("%s: nextActive(%d, %d, %d) = %d, want %d",
				tc.name, tc.active, tc.removed, tc.remaining, got, tc.want)
		}
	}
}

// One glyph per tab, and the one that matters most.
func TestMarker(t *testing.T) {
	for _, tc := range []struct {
		name string
		tab  tab
		want marker
	}{
		{"nothing going on", tab{}, markNone},
		{"a job in front", tab{running: true}, markRunning},
		{"finished, output unseen", tab{unseen: true}, markUnseen},
		{"still running, output unseen: unseen wins", tab{running: true, unseen: true}, markUnseen},
		{"the bell beats everything", tab{running: true, unseen: true, bell: true}, markBell},
	} {
		if got := tc.tab.marker(); got != tc.want {
			t.Errorf("%s: marker() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The attention key goes to the tab that wants you most, and the nearest one
// of those to the right, so pressing it repeatedly visits them all in order.
func TestNextAttention(t *testing.T) {
	tabs := func(states ...tab) []*tab {
		out := make([]*tab, len(states))
		for i := range states {
			out[i] = &states[i]
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		tabs   []*tab
		active int
		want   int
		ok     bool
	}{
		{"nothing waiting", tabs(tab{}, tab{}, tab{}), 0, 0, false},
		{"only running tabs are not waiting", tabs(tab{}, tab{running: true}), 0, 0, false},
		{"the next unseen tab", tabs(tab{}, tab{}, tab{unseen: true}), 0, 2, true},
		{"a bell beats a nearer unseen tab", tabs(tab{}, tab{unseen: true}, tab{bell: true}), 0, 2, true},
		{"the nearest bell to the right", tabs(tab{bell: true}, tab{}, tab{bell: true}), 1, 2, true},
		{"wrapping round", tabs(tab{unseen: true}, tab{}, tab{}), 2, 0, true},
		{"the tab in front, if it rang while you were away", tabs(tab{bell: true}), 0, 0, true},
		{"no tabs at all", nil, 0, 0, false},
	} {
		got, ok := nextAttention(tc.tabs, tc.active)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%s: nextAttention() = (%d, %v), want (%d, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMoveTab(t *testing.T) {
	first, second, third := &tab{}, &tab{}, &tab{}
	a := &app{tabs: []*tab{first, second, third}, active: 1}

	a.moveTab(1)
	if a.active != 2 || a.tabs[2] != second || a.tabs[1] != third {
		t.Errorf("after moving right: active=%d, order=%v", a.active, a.tabs)
	}
	a.moveTab(1)
	if a.active != 2 || a.tabs[2] != second {
		t.Error("moving the last tab right should do nothing")
	}
	a.moveTab(-2)
	if a.active != 0 || a.tabs[0] != second {
		t.Errorf("after moving left twice: active=%d, order=%v", a.active, a.tabs)
	}
}

// testTab is a tab with an emulator but no child: enough to see whether it
// believes it has focus, which is what its cursor is drawn from.
func testTab(a *app) *tab {
	tb := &tab{vt: term.New(), shell: "sh"}
	tb.vt.Resize(40, 5)
	tb.input = newPump(func(ev vaxis.Event) { a.applyInput(tb, ev) })
	return tb
}

func cursorShown(tb *tab) bool {
	tb.input.wait()
	return tb.vt.Snapshot().CursorVisible
}

// Close the tab in the middle while it is in front and the tab that takes its
// place must take its focus too. It was blurred when it went behind, and if
// nothing tells it otherwise it keeps its cursor hidden: you type into a tab
// that does not know you are there, and cannot see where the text is going.
func TestClosingTheActiveTabFocusesItsNeighbour(t *testing.T) {
	a := &app{}
	first, second, third := testTab(a), testTab(a), testTab(a)
	a.tabs = []*tab{first, second, third}
	a.active = 0
	a.focusCurrent()
	a.selectTab(1)
	a.selectTab(2)
	a.selectTab(1)
	if cursorShown(third) {
		t.Fatal("a tab behind shows its cursor")
	}
	if !cursorShown(second) {
		t.Fatal("the tab in front hides its cursor")
	}

	if !a.closeTab(second) {
		t.Fatal("two tabs left, but closeTab said none")
	}
	if a.current() != third {
		t.Fatalf("after closing the middle tab, tab %d is in front, want the one to its right", a.active+1)
	}
	if !cursorShown(third) {
		t.Error("the tab that took the closed tab's place has no cursor: it was never told it is in front")
	}

	// Closing a tab behind leaves the one in front alone.
	if !a.closeTab(first) {
		t.Fatal("one tab left, but closeTab said none")
	}
	if a.current() != third || !cursorShown(third) {
		t.Error("closing a tab behind disturbed the one in front")
	}
}

// Switching away marks nothing, switching to a tab marks it seen.
func TestSelectingATabMarksItSeen(t *testing.T) {
	a := &app{}
	first, second := testTab(a), testTab(a)
	a.tabs = []*tab{first, second}
	a.focusCurrent()

	second.unseen, second.bell = true, true
	a.selectTab(1)
	if second.unseen || second.bell {
		t.Error("a tab brought to the front still says it wants attention")
	}
	if first.unseen || first.bell {
		t.Error("switching away invented something to see")
	}
}
