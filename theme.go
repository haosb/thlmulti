package main

import (
	"fmt"
	"strconv"
	"strings"

	"go.rockorager.dev/vaxis"
)

// ThemeMode picks the built-in palette. Auto follows the host terminal: what it
// reports as its colour scheme, and failing that the brightness of its
// background colour.
type ThemeMode int

const (
	ThemeAuto ThemeMode = iota
	ThemeDark
	ThemeLight
)

// Theme is every colour thlmulti paints with, which is only ever the tab bar.
// The terminals inside the tabs are not themed: they paint themselves, in
// whatever colours the host terminal gives them.
type Theme struct {
	TabFg, TabBg       vaxis.Color // inactive tabs, the separators, the rule
	ActiveFg, ActiveBg vaxis.Color // the tab in front
	ToastFg, ToastBg   vaxis.Color // "Copied to clipboard"
	ErrorFg, ErrorBg   vaxis.Color // a failed action
}

func darkTheme() Theme {
	return Theme{
		TabFg: vaxis.IndexColor(8), TabBg: vaxis.ColorDefault,
		ActiveFg: vaxis.IndexColor(0), ActiveBg: vaxis.IndexColor(7),
		ToastFg: vaxis.IndexColor(0), ToastBg: vaxis.IndexColor(6),
		ErrorFg: vaxis.IndexColor(15), ErrorBg: vaxis.IndexColor(1),
	}
}

func lightTheme() Theme {
	return Theme{
		TabFg: vaxis.IndexColor(8), TabBg: vaxis.ColorDefault,
		ActiveFg: vaxis.IndexColor(15), ActiveBg: vaxis.IndexColor(0),
		ToastFg: vaxis.IndexColor(15), ToastBg: vaxis.IndexColor(4),
		ErrorFg: vaxis.IndexColor(15), ErrorBg: vaxis.IndexColor(1),
	}
}

// themeSlots maps a config setting to the field it fills in. A slot left out of
// the config keeps whatever the palette put there, so you can repaint one part
// of the bar without describing the other three.
var themeSlots = map[string]func(*Theme) *vaxis.Color{
	"tab_fg":    func(t *Theme) *vaxis.Color { return &t.TabFg },
	"tab_bg":    func(t *Theme) *vaxis.Color { return &t.TabBg },
	"active_fg": func(t *Theme) *vaxis.Color { return &t.ActiveFg },
	"active_bg": func(t *Theme) *vaxis.Color { return &t.ActiveBg },
	"toast_fg":  func(t *Theme) *vaxis.Color { return &t.ToastFg },
	"toast_bg":  func(t *Theme) *vaxis.Color { return &t.ToastBg },
	"error_fg":  func(t *Theme) *vaxis.Color { return &t.ErrorFg },
	"error_bg":  func(t *Theme) *vaxis.Color { return &t.ErrorBg },
}

// theme builds the palette for mode and paints the configured overrides on top.
// mode is the resolved one: ThemeAuto never reaches here.
func (c Config) theme(mode ThemeMode) Theme {
	t := darkTheme()
	if mode == ThemeLight {
		t = lightTheme()
	}
	for name, color := range c.Colors {
		if slot, ok := themeSlots[name]; ok {
			*slot(&t) = color
		}
	}
	return t
}

func (t Theme) tab() vaxis.Style {
	return vaxis.Style{Foreground: t.TabFg, Background: t.TabBg}
}

func (t Theme) active() vaxis.Style {
	return vaxis.Style{Foreground: t.ActiveFg, Background: t.ActiveBg}
}

func (t Theme) toast() vaxis.Style {
	return vaxis.Style{Foreground: t.ToastFg, Background: t.ToastBg}
}

func (t Theme) failure() vaxis.Style {
	return vaxis.Style{Foreground: t.ErrorFg, Background: t.ErrorBg}
}

// modeForBackground reads the host's background colour and says which palette
// belongs on it. The second result is false when the colour tells us nothing,
// which is what a terminal that will not answer the query looks like.
func modeForBackground(c vaxis.Color) (ThemeMode, bool) {
	rgb := c.Params()
	if len(rgb) != 3 {
		return ThemeAuto, false
	}
	// Rec. 601 luma: green carries most of the perceived brightness, blue
	// almost none.
	luma := 0.299*float64(rgb[0]) + 0.587*float64(rgb[1]) + 0.114*float64(rgb[2])
	if luma < 128 {
		return ThemeDark, true
	}
	return ThemeLight, true
}

func parseThemeMode(s string) (ThemeMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto":
		return ThemeAuto, nil
	case "dark":
		return ThemeDark, nil
	case "light":
		return ThemeLight, nil
	default:
		return ThemeAuto, fmt.Errorf("theme must be auto, dark or light, got %q", s)
	}
}

// baseColors are the sixteen names every terminal palette has. The numbers are
// the palette indexes, so the colours are the ones the user picked in their
// terminal rather than ones we decided on.
var baseColors = map[string]uint8{
	"black": 0, "red": 1, "green": 2, "yellow": 3,
	"blue": 4, "magenta": 5, "cyan": 6, "white": 7,
	"bright-black": 8, "bright-red": 9, "bright-green": 10, "bright-yellow": 11,
	"bright-blue": 12, "bright-magenta": 13, "bright-cyan": 14, "bright-white": 15,
}

// parseColor reads "default", a palette name, a palette index 0-255, or #rrggbb.
func parseColor(s string) (vaxis.Color, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "default" || s == "" {
		return vaxis.ColorDefault, nil
	}
	if index, ok := baseColors[s]; ok {
		return vaxis.IndexColor(index), nil
	}
	if hex, ok := strings.CutPrefix(s, "#"); ok {
		if len(hex) != 6 {
			return 0, fmt.Errorf("%q: a hex colour is six digits, like #1d2021", s)
		}
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return 0, fmt.Errorf("%q: not a hex colour", s)
		}
		return vaxis.RGBColor(uint8(n>>16), uint8(n>>8), uint8(n)), nil
	}
	if index, err := strconv.ParseUint(s, 10, 8); err == nil {
		return vaxis.IndexColor(uint8(index)), nil
	}
	return 0, fmt.Errorf("%q: expected default, a colour name, 0-255, or #rrggbb", s)
}
