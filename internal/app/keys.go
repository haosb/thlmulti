package app

import (
	"go.rockorager.dev/vaxis"

	"github.com/haosb/thlmulti/internal/clipboard"
)

func (a *app) handleKey(k vaxis.Key) {
	if a.status != "" {
		a.status = ""
		a.dirty = true
	}
	if a.rename != nil {
		// Every key belongs to the editor until it is done, so a tab chord
		// cannot switch tabs out from under a half-typed name.
		onPress(k, func() { a.editRename(k) })
		return
	}
	if act := a.keyAction(k); act != nil {
		onPress(k, act)
		return
	}
	a.current().input.push(k)
}

// keyAction is what k is bound to, or nil when the key belongs to the child.
func (a *app) keyAction(k vaxis.Key) func() {
	switch {
	case a.cfg.NewTab.Matches(k):
		return a.newTab
	case a.cfg.CloseTab.Matches(k):
		return a.closeActive
	case a.cfg.Paste.Matches(k):
		return a.paste
	case a.cfg.NextTab.Matches(k):
		return func() { a.selectTab(a.wrap(a.active + 1)) }
	case a.cfg.PrevTab.Matches(k):
		return func() { a.selectTab(a.wrap(a.active - 1)) }
	case a.cfg.AttentionTab.Matches(k):
		return a.jumpToAttention
	case a.cfg.MoveLeft.Matches(k):
		return func() { a.moveTab(-1) }
	case a.cfg.MoveRight.Matches(k):
		return func() { a.moveTab(1) }
	case a.cfg.RenameTab.Matches(k):
		return a.startRename
	}
	if i, ok := a.tabDigit(k); ok {
		return func() { a.selectTab(i) }
	}
	return nil
}

// onPress runs act for a press or repeat, and swallows the matching release so
// the child never sees half of a chord we consumed.
func onPress(k vaxis.Key, act func()) {
	if k.EventType == vaxis.EventPress || k.EventType == vaxis.EventRepeat {
		act()
	}
}

func (a *app) newTab() {
	if err := a.openTab(); err != nil {
		a.fail("new tab: %v", err)
	}
}

func (a *app) jumpToAttention() {
	if i, ok := nextAttention(a.tabs, a.active); ok {
		a.selectTab(i)
		return
	}
	a.notify("No tab is waiting", toastDuration)
}

func (a *app) startRename() {
	a.rename = &rename{tab: a.current(), text: []rune(a.current().name)}
	a.dirty = true
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

// rename is the name being typed for a tab, while it is being typed. It lives
// in the tab bar, in place of the tabs, until enter or escape.
type rename struct {
	tab  *tab
	text []rune
}

// edit applies one keypress. done reports that the editor is finished, and
// commit whether the name typed should be kept.
func (r *rename) edit(k vaxis.Key) (done, commit bool) {
	switch {
	case k.Matches(vaxis.KeyEsc):
		return true, false
	case k.Matches(vaxis.KeyEnter):
		return true, true
	case k.Matches(vaxis.KeyBackspace):
		if len(r.text) > 0 {
			r.text = r.text[:len(r.text)-1]
		}
	case k.Matches('u', vaxis.ModCtrl):
		r.text = r.text[:0]
	case k.Text != "" && k.Modifiers&(vaxis.ModCtrl|vaxis.ModAlt|vaxis.ModSuper) == 0:
		r.text = append(r.text, []rune(k.Text)...)
	}
	return false, false
}

func (a *app) editRename(k vaxis.Key) {
	done, commit := a.rename.edit(k)
	if done {
		if commit {
			// An empty name hands the label back to the program's title.
			a.rename.tab.name = string(a.rename.text)
		}
		a.rename = nil
	}
	a.dirty = true
}

// clipboardText brings the clipboard back to the loop once a tool has read it.
// It remembers which tab asked, so a paste lands where it was requested even
// if the tab in front has changed while the tool ran.
type clipboardText struct {
	tab  *tab
	text string
	err  error
}

// paste asks for the system clipboard. The reading happens off the loop: it
// runs a program, and a clipboard owner that does not answer used to take the
// whole window down with it.
func (a *app) paste() {
	t := a.current()
	go func() {
		text, err := clipboard.Read()
		a.vx.PostEventBlocking(clipboardText{tab: t, text: text, err: err})
	}()
}

// deliverPaste hands clipboard text to the tab that asked for it as a
// bracketed paste, so the child can tell it apart from typing.
func (a *app) deliverPaste(c clipboardText) {
	if c.err != nil {
		a.fail("paste: %v", c.err)
		return
	}
	if c.text == "" || a.index(c.tab) < 0 {
		return
	}
	in := c.tab.input
	in.push(vaxis.PasteStartEvent{})
	for _, r := range c.text {
		in.push(vaxis.Key{Keycode: r, Text: string(r), EventType: vaxis.EventPaste})
	}
	in.push(vaxis.PasteEndEvent{})
}
