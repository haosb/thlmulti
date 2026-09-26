package config

import (
	"testing"

	"go.rockorager.dev/vaxis"
)

func key(code rune, mods vaxis.ModifierMask) vaxis.Key {
	return vaxis.Key{Keycode: code, Modifiers: mods, EventType: vaxis.EventPress}
}

// The paste binding must not swallow plain ctrl+v, or vim loses visual block.
func TestPasteBindingLeavesCtrlVAlone(t *testing.T) {
	paste := Default().Paste

	if !paste.Matches(key('v', vaxis.ModCtrl|vaxis.ModAlt)) {
		t.Error("ctrl+alt+v should trigger paste")
	}
	if paste.Matches(key('v', vaxis.ModCtrl)) {
		t.Error("ctrl+v must reach the child: it is vim's visual block")
	}
	if paste.Matches(key('v', 0)) {
		t.Error("bare v must reach the child")
	}
}

// The close binding must not swallow plain ctrl+w, which deletes a word in
// zsh's line editor. Without the Kitty keyboard protocol the chord arrives as a
// bare ctrl+w, and this is what keeps that case from closing the tab.
func TestCloseBindingLeavesCtrlWAlone(t *testing.T) {
	closeTab := Default().CloseTab

	if !closeTab.Matches(key('w', vaxis.ModCtrl|vaxis.ModShift)) {
		t.Error("ctrl+shift+w should close the tab")
	}
	if closeTab.Matches(key('w', vaxis.ModCtrl)) {
		t.Error("ctrl+w must reach the shell: it deletes a word")
	}
	if closeTab.Matches(key('w', 0)) {
		t.Error("bare w must reach the shell")
	}
}

func TestDisabledBindingNeverMatches(t *testing.T) {
	b, err := parseBinding("none")
	if err != nil {
		t.Fatal(err)
	}
	for _, mods := range []vaxis.ModifierMask{0, vaxis.ModCtrl, vaxis.ModCtrl | vaxis.ModAlt} {
		if b.Matches(key('v', mods)) {
			t.Errorf("disabled binding matched mods %v", mods)
		}
	}
}

func TestSetRejectsBadInput(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		// A bare digit would select a tab instead of being typed.
		{"tab_mods", ""},
		{"tab_mods", "hyper"},
		{"paste", "ctrl+alt+shift"},     // no key, only modifiers
		{"paste", "ctrl+bogus"},         // not a key name
		{"new_tab", "hyper+t"},          // unknown modifier
		{"colour_scheme", "catppuccin"}, // unknown setting
		{"scrollback", "lots"},
		{"scrollback", "-1"},
		{"theme", "solarized"},
		{"active_bg", "#12345"},
	} {
		cfg := Default()
		if err := cfg.set(tc.name, tc.value); err == nil {
			t.Errorf("set(%q, %q) should have failed", tc.name, tc.value)
		}
	}
}

func TestSetAcceptsGoodInput(t *testing.T) {
	cfg := Default()
	for name, value := range map[string]string{
		"new_tab":    "ctrl+n",
		"close_tab":  "ctrl+alt+q",
		"paste":      "super+v",
		"next_tab":   "ctrl+pgdown",
		"prev_tab":   "ctrl+shift+Tab",
		"rename_tab": "none",
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
	if cfg.NextTab != (Binding{Key: vaxis.KeyPgDown, Mods: vaxis.ModCtrl}) {
		t.Errorf("next_tab = %+v", cfg.NextTab)
	}
	if cfg.PrevTab != (Binding{Key: vaxis.KeyTab, Mods: vaxis.ModCtrl | vaxis.ModShift}) {
		t.Errorf("prev_tab = %+v", cfg.PrevTab)
	}
	if cfg.RenameTab != (Binding{}) {
		t.Errorf("rename_tab = %+v, want disabled", cfg.RenameTab)
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
	theme := cfg.Palette(cfg.Theme)
	if theme.TabFg != vaxis.IndexColor(8) || theme.ActiveBg != vaxis.RGBColor(0x1d, 0x20, 0x21) {
		t.Errorf("colours = %+v", theme)
	}
}

// A chord is written back the way the config spells it, so a message that
// names a key names the one in the file.
func TestBindingString(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ctrl+shift+w", "ctrl+shift+w"},
		{"Shift+Ctrl+W", "ctrl+shift+w"},
		{"ctrl+tab", "ctrl+tab"},
		{"ctrl+shift+pageup", "ctrl+shift+pgup"},
		{"none", "none"},
	} {
		b, err := parseBinding(tc.in)
		if err != nil {
			t.Fatalf("parseBinding(%q): %v", tc.in, err)
		}
		if got := b.String(); got != tc.want {
			t.Errorf("parseBinding(%q).String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ctrl+tab only exists as a chord under the Kitty keyboard protocol. Without
// it the host sends a plain tab, and that has to reach the shell, not us.
func TestNextTabLeavesPlainTabAlone(t *testing.T) {
	cfg := Default()
	if !cfg.NextTab.Matches(key(vaxis.KeyTab, vaxis.ModCtrl)) {
		t.Error("ctrl+tab should switch tabs")
	}
	if !cfg.PrevTab.Matches(key(vaxis.KeyTab, vaxis.ModCtrl|vaxis.ModShift)) {
		t.Error("ctrl+shift+tab should switch tabs")
	}
	for _, k := range []vaxis.Key{key(vaxis.KeyTab, 0), key(vaxis.KeyTab, vaxis.ModShift)} {
		if cfg.NextTab.Matches(k) || cfg.PrevTab.Matches(k) {
			t.Errorf("%+v must reach the shell: it completes a word", k)
		}
	}
}
