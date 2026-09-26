package app

import "go.rockorager.dev/vaxis"

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
	// The selection, if this release finished one, is picked up by the pump
	// once the emulator has seen the release: see applyInput.
	a.current().input.push(m)
}
