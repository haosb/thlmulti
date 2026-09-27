package app

import (
	"testing"

	"go.rockorager.dev/vaxis"

	"github.com/haosb/thlmulti/internal/config"
)

func key(code rune, mods vaxis.ModifierMask) vaxis.Key {
	return vaxis.Key{Keycode: code, Modifiers: mods, EventType: vaxis.EventPress}
}

func TestTabDigits(t *testing.T) {
	a := &app{cfg: config.Default()}

	for _, tc := range []struct {
		name string
		key  vaxis.Key
		want int
		ok   bool
	}{
		{"ctrl+1 is the first tab", key('1', vaxis.ModCtrl), 0, true},
		{"ctrl+9 is the ninth tab", key('9', vaxis.ModCtrl), 8, true},
		{"ctrl+0 is the tenth tab", key('0', vaxis.ModCtrl), 9, true},
		{"a bare digit is typed, not a tab switch", key('1', 0), 0, false},
		{"a non-configured modifier is not a tab switch", key('1', vaxis.ModAlt), 0, false},
		{"a letter is not a tab switch", key('a', vaxis.ModCtrl), 0, false},
	} {
		got, ok := a.tabDigit(tc.key)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%s: tabDigit() = (%d, %v), want (%d, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func typed(s string) []vaxis.Key {
	var keys []vaxis.Key
	for _, r := range s {
		keys = append(keys, vaxis.Key{Keycode: r, Text: string(r), EventType: vaxis.EventPress})
	}
	return keys
}

func TestRenameEditing(t *testing.T) {
	r := &rename{text: []rune("old")}
	for _, k := range typed("-new") {
		if done, _ := r.edit(k); done {
			t.Fatal("typing a letter finished the editor")
		}
	}
	if string(r.text) != "old-new" {
		t.Fatalf("after typing, text = %q", string(r.text))
	}

	r.edit(key(vaxis.KeyBackspace, 0))
	if string(r.text) != "old-ne" {
		t.Errorf("after backspace, text = %q", string(r.text))
	}

	// A chord is not text, even when the host reports text for it.
	r.edit(vaxis.Key{Keycode: 'x', Text: "x", Modifiers: vaxis.ModAlt, EventType: vaxis.EventPress})
	if string(r.text) != "old-ne" {
		t.Errorf("alt+x was typed into the name: %q", string(r.text))
	}

	done, commit := r.edit(key(vaxis.KeyEnter, 0))
	if !done || !commit {
		t.Errorf("enter: done=%v commit=%v, want both", done, commit)
	}

	r = &rename{text: []rune("keep me")}
	done, commit = r.edit(key(vaxis.KeyEsc, 0))
	if !done || commit {
		t.Errorf("escape: done=%v commit=%v, want done and not committed", done, commit)
	}

	r = &rename{text: []rune("all of this")}
	r.edit(key('u', vaxis.ModCtrl))
	if len(r.text) != 0 {
		t.Errorf("ctrl+u left %q", string(r.text))
	}
}

// The label is what the user said, else what the program said, else the shell.
func TestTabLabel(t *testing.T) {
	tb := &tab{shell: "zsh"}
	if tb.label() != "zsh" {
		t.Errorf("fresh tab labelled %q", tb.label())
	}
	tb.title = "✳ Claude Code"
	if tb.label() != "✳ Claude Code" {
		t.Errorf("titled tab labelled %q", tb.label())
	}
	tb.name = "auth service"
	if tb.label() != "auth service" {
		t.Errorf("named tab labelled %q, the name should win", tb.label())
	}
	tb.name = ""
	if tb.label() != "✳ Claude Code" {
		t.Errorf("clearing the name did not hand the label back to the title: %q", tb.label())
	}
}

// A message in the bar stays until you do something. Letting go of the chord
// that caused it is not doing something: under the Kitty keyboard protocol
// every release arrives as a key of its own, modifiers included, and clearing
// on those took the message down before it was ever drawn.
func TestStatusOutlivesTheRelease(t *testing.T) {
	a := &app{cfg: config.Default()}
	a.tabs = []*tab{testTab(a)}
	a.status = "tab 1 is running a job"
	for _, k := range []vaxis.Key{
		{Keycode: 'w', Modifiers: vaxis.ModCtrl | vaxis.ModShift, EventType: vaxis.EventRelease},
		{Keycode: vaxis.KeyLeftShift, EventType: vaxis.EventRelease},
	} {
		a.handleKey(k)
		if a.status == "" {
			t.Fatalf("releasing %+v cleared the message", k)
		}
	}
	a.handleKey(key('x', 0))
	if a.status != "" {
		t.Error("a keypress left the message up")
	}
}
