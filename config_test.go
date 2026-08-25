package main

import (
	"testing"

	"go.rockorager.dev/vaxis"
)

func key(code rune, mods vaxis.ModifierMask) vaxis.Key {
	return vaxis.Key{Keycode: code, Modifiers: mods, EventType: vaxis.EventPress}
}

// The paste binding must not swallow plain ctrl+v, or vim loses visual block.
func TestPasteBindingLeavesCtrlVAlone(t *testing.T) {
	paste := defaultConfig().Paste

	if !paste.matches(key('v', vaxis.ModCtrl|vaxis.ModAlt)) {
		t.Error("ctrl+alt+v should trigger paste")
	}
	if paste.matches(key('v', vaxis.ModCtrl)) {
		t.Error("ctrl+v must reach the child: it is vim's visual block")
	}
	if paste.matches(key('v', 0)) {
		t.Error("bare v must reach the child")
	}
}

// The close binding must not swallow plain ctrl+w, which deletes a word in
// zsh's line editor. Without the Kitty keyboard protocol the chord arrives as a
// bare ctrl+w, and this is what keeps that case from closing the tab.
func TestCloseBindingLeavesCtrlWAlone(t *testing.T) {
	closeTab := defaultConfig().CloseTab

	if !closeTab.matches(key('w', vaxis.ModCtrl|vaxis.ModShift)) {
		t.Error("ctrl+shift+w should close the tab")
	}
	if closeTab.matches(key('w', vaxis.ModCtrl)) {
		t.Error("ctrl+w must reach the shell: it deletes a word")
	}
	if closeTab.matches(key('w', 0)) {
		t.Error("bare w must reach the shell")
	}
}

func TestDisabledBindingNeverMatches(t *testing.T) {
	b, err := parseBinding("none")
	if err != nil {
		t.Fatal(err)
	}
	for _, mods := range []vaxis.ModifierMask{0, vaxis.ModCtrl, vaxis.ModCtrl | vaxis.ModAlt} {
		if b.matches(key('v', mods)) {
			t.Errorf("disabled binding matched mods %v", mods)
		}
	}
}

func TestTabDigits(t *testing.T) {
	a := &app{cfg: defaultConfig()}

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

func TestSetRejectsBadInput(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		// A bare digit would select a tab instead of being typed.
		{"tab_mods", ""},
		{"tab_mods", "hyper"},
		{"paste", "ctrl+alt+shift"},     // no key, only modifiers
		{"paste", "ctrl+enter"},         // multi-character key name
		{"new_tab", "hyper+t"},          // unknown modifier
		{"colour_scheme", "catppuccin"}, // unknown setting
		{"scrollback", "lots"},
		{"scrollback", "-1"},
		{"theme", "solarized"},
		{"active_bg", "#12345"},
	} {
		cfg := defaultConfig()
		if err := cfg.set(tc.name, tc.value); err == nil {
			t.Errorf("set(%q, %q) should have failed", tc.name, tc.value)
		}
	}
}

func TestSetAcceptsGoodInput(t *testing.T) {
	cfg := defaultConfig()
	for name, value := range map[string]string{
		"new_tab":    "ctrl+n",
		"close_tab":  "ctrl+alt+q",
		"paste":      "super+v",
		"tab_mods":   "ctrl+shift",
		"shell":      "/bin/zsh --login",
		"scrollback": "500",
		"theme":      "light",
		"tab_fg":     "bright-black",
		"active_bg":  "#1d2021",
	} {
		if err := cfg.set(name, value); err != nil {
			t.Fatalf("set(%q, %q): %v", name, value, err)
		}
	}
	if cfg.NewTab != (Binding{Key: 'n', Mods: vaxis.ModCtrl}) {
		t.Errorf("new_tab = %+v", cfg.NewTab)
	}
	if cfg.CloseTab != (Binding{Key: 'q', Mods: vaxis.ModCtrl | vaxis.ModAlt}) {
		t.Errorf("close_tab = %+v", cfg.CloseTab)
	}
	if cfg.TabMods != vaxis.ModCtrl|vaxis.ModShift {
		t.Errorf("tab_mods = %v", cfg.TabMods)
	}
	if len(cfg.Shell) != 2 || cfg.Shell[0] != "/bin/zsh" || cfg.Shell[1] != "--login" {
		t.Errorf("shell = %q", cfg.Shell)
	}
	if cfg.Scrollback != 500 {
		t.Errorf("scrollback = %d", cfg.Scrollback)
	}
	if cfg.Theme != ThemeLight {
		t.Errorf("theme = %v", cfg.Theme)
	}
	theme := cfg.theme(cfg.Theme)
	if theme.TabFg != vaxis.IndexColor(8) || theme.ActiveBg != vaxis.RGBColor(0x1d, 0x20, 0x21) {
		t.Errorf("colours = %+v", theme)
	}
}
