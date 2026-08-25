package main

import (
	"testing"

	"go.rockorager.dev/vaxis"
)

func TestParseColor(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want vaxis.Color
	}{
		{"default", vaxis.ColorDefault},
		{"", vaxis.ColorDefault},
		{"red", vaxis.IndexColor(1)},
		{"bright-black", vaxis.IndexColor(8)},
		{" Cyan ", vaxis.IndexColor(6)},
		{"0", vaxis.IndexColor(0)},
		{"255", vaxis.IndexColor(255)},
		{"#1d2021", vaxis.RGBColor(0x1d, 0x20, 0x21)},
		{"#FFFFFF", vaxis.RGBColor(255, 255, 255)},
	} {
		got, err := parseColor(tc.in)
		if err != nil {
			t.Errorf("parseColor(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseColor(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}

	// A colour that does not parse has to be refused rather than silently
	// dropped: the bar would come up in the wrong colours and the config would
	// look right.
	for _, in := range []string{"#12345", "#gggggg", "256", "-1", "puce"} {
		if _, err := parseColor(in); err == nil {
			t.Errorf("parseColor(%q) should have failed", in)
		}
	}
}

func TestParseThemeMode(t *testing.T) {
	for in, want := range map[string]ThemeMode{
		"auto": ThemeAuto, "dark": ThemeDark, "LIGHT": ThemeLight,
	} {
		got, err := parseThemeMode(in)
		if err != nil || got != want {
			t.Errorf("parseThemeMode(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := parseThemeMode("solarized"); err == nil {
		t.Error("an unknown theme should be refused")
	}
}

func TestModeForBackground(t *testing.T) {
	if mode, ok := modeForBackground(vaxis.RGBColor(0x1d, 0x20, 0x21)); !ok || mode != ThemeDark {
		t.Errorf("a near-black background = %v, %v; want dark", mode, ok)
	}
	if mode, ok := modeForBackground(vaxis.RGBColor(0xfb, 0xf1, 0xc7)); !ok || mode != ThemeLight {
		t.Errorf("a cream background = %v, %v; want light", mode, ok)
	}
	// What a terminal that will not answer the query looks like. Guessing from
	// it would flip the bar to the wrong palette for no reason.
	if _, ok := modeForBackground(vaxis.ColorDefault); ok {
		t.Error("no answer should not resolve to a theme")
	}
	if _, ok := modeForBackground(vaxis.IndexColor(4)); ok {
		t.Error("a palette index says nothing about brightness")
	}
}

// Overriding one slot must not disturb the other three, and it must survive the
// theme changing under it.
func TestThemeOverridesOneSlot(t *testing.T) {
	cfg := Config{Colors: map[string]vaxis.Color{"active_bg": vaxis.RGBColor(1, 2, 3)}}

	dark := cfg.theme(ThemeDark)
	if dark.ActiveBg != vaxis.RGBColor(1, 2, 3) {
		t.Errorf("ActiveBg = %v, want the override", dark.ActiveBg)
	}
	if dark.TabFg != darkTheme().TabFg || dark.ToastBg != darkTheme().ToastBg {
		t.Error("an override leaked into the slots it did not name")
	}

	if light := cfg.theme(ThemeLight); light.ActiveBg != vaxis.RGBColor(1, 2, 3) {
		t.Error("the override did not survive the light palette")
	} else if light.ActiveFg != lightTheme().ActiveFg {
		t.Error("the light palette was not used for the slots left alone")
	}
}
