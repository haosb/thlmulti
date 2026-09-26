// Package config reads ~/.config/thlmulti/config: the keys, the shell, the
// history limit, and the colours of the tab bar.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.rockorager.dev/vaxis"
)

// Config is the whole of thlmulti's configuration: which keys do what, which
// shell to start, how much history to keep, and what the tab bar looks like.
// Nothing else is configurable because nothing else was asked for.
type Config struct {
	NewTab   Binding
	CloseTab Binding
	Paste    Binding
	// NextTab and PrevTab step through the tabs, wrapping at the ends.
	NextTab Binding
	PrevTab Binding
	// AttentionTab jumps to the tab that wants you: one that rang its bell,
	// or failing that one with output you have not seen.
	AttentionTab Binding
	// MoveLeft and MoveRight shift the tab in front along the bar.
	MoveLeft  Binding
	MoveRight Binding
	// RenameTab gives the tab in front a name of your choosing.
	RenameTab Binding
	// TabMods combined with 1..9 and 0 selects a tab by position.
	TabMods vaxis.ModifierMask
	Shell   []string
	// Scrollback is how many lines of history each tab keeps. Zero keeps none.
	Scrollback int
	// Theme picks the tab bar palette; Colors overrides parts of it by slot
	// name. Only slots the config named are present.
	Theme  ThemeMode
	Colors map[string]vaxis.Color
}

// defaultScrollback is deliberately far below the 10000 lines the emulator
// would otherwise keep. History is stored as whole rows of full width, so at a
// wide window 10000 lines is a few hundred megabytes for every tab, and ten
// tabs of that is real memory. 2000 is what tmux keeps, and it is enough to
// scroll back over what a command just printed. Raise it if you need to.
const defaultScrollback = 2000

// Binding is a single chord, such as ctrl+alt+v. The zero value never matches,
// which is how a binding gets disabled.
type Binding struct {
	Key  rune
	Mods vaxis.ModifierMask
}

// Matches reports whether k is this chord.
func (b Binding) Matches(k vaxis.Key) bool {
	return b.Key != 0 && k.Matches(b.Key, b.Mods)
}

// String writes the chord back the way the config spells it, for messages.
func (b Binding) String() string {
	if b.Key == 0 {
		return "none"
	}
	var parts []string
	for _, mod := range modifierNames {
		if b.Mods&mod.mask != 0 {
			parts = append(parts, mod.name)
		}
	}
	return strings.Join(append(parts, keyName(b.Key)), "+")
}

// modifierNames are the modifiers in the order a chord is written back.
var modifierNames = []struct {
	mask vaxis.ModifierMask
	name string
}{
	{vaxis.ModCtrl, "ctrl"},
	{vaxis.ModAlt, "alt"},
	{vaxis.ModShift, "shift"},
	{vaxis.ModSuper, "super"},
}

// keyNames are the keys a chord can end in that are not a single character.
// Where a key has two spellings, the first is the one written back.
var keyNames = []struct {
	name string
	code rune
}{
	{"tab", vaxis.KeyTab}, {"enter", vaxis.KeyEnter}, {"esc", vaxis.KeyEsc}, {"escape", vaxis.KeyEsc},
	{"space", vaxis.KeySpace}, {"backspace", vaxis.KeyBackspace},
	{"up", vaxis.KeyUp}, {"down", vaxis.KeyDown}, {"left", vaxis.KeyLeft}, {"right", vaxis.KeyRight},
	{"pgup", vaxis.KeyPgUp}, {"pageup", vaxis.KeyPgUp}, {"pgdown", vaxis.KeyPgDown}, {"pagedown", vaxis.KeyPgDown},
	{"home", vaxis.KeyHome}, {"end", vaxis.KeyEnd}, {"insert", vaxis.KeyInsert}, {"delete", vaxis.KeyDelete},
	{"f1", vaxis.KeyF01}, {"f2", vaxis.KeyF02}, {"f3", vaxis.KeyF03}, {"f4", vaxis.KeyF04},
	{"f5", vaxis.KeyF05}, {"f6", vaxis.KeyF06}, {"f7", vaxis.KeyF07}, {"f8", vaxis.KeyF08},
	{"f9", vaxis.KeyF09}, {"f10", vaxis.KeyF10}, {"f11", vaxis.KeyF11}, {"f12", vaxis.KeyF12},
}

func keyByName(name string) (rune, bool) {
	for _, k := range keyNames {
		if k.name == name {
			return k.code, true
		}
	}
	return 0, false
}

func keyName(code rune) string {
	for _, k := range keyNames {
		if k.code == code {
			return k.name
		}
	}
	return string(code)
}

// Default is the configuration with no config file.
func Default() Config {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return Config{
		NewTab: Binding{Key: 't', Mods: vaxis.ModCtrl},
		// Not ctrl+w: that deletes a word in zsh's line editor and is far too
		// useful to take. Shift makes it distinct, and if the host cannot speak
		// the Kitty keyboard protocol the chord arrives as a plain ctrl+w, this
		// binding does not match, and deleting a word still works.
		CloseTab: Binding{Key: 'w', Mods: vaxis.ModCtrl | vaxis.ModShift},
		// Not ctrl+shift+v: that is a default Alacritty binding, so Alacritty
		// pastes on its own and the key never reaches us. We forward the
		// bracketed paste it produces, and own ctrl+alt+v as well — which
		// leaves plain ctrl+v untouched for vim's visual block.
		Paste: Binding{Key: 'v', Mods: vaxis.ModCtrl | vaxis.ModAlt},
		// ctrl+tab is what browsers do, and it only exists as a distinct key
		// under the Kitty keyboard protocol; without it the chord is a plain
		// tab and reaches the shell, which is the right failure.
		NextTab:      Binding{Key: vaxis.KeyTab, Mods: vaxis.ModCtrl},
		PrevTab:      Binding{Key: vaxis.KeyTab, Mods: vaxis.ModCtrl | vaxis.ModShift},
		AttentionTab: Binding{Key: 'a', Mods: vaxis.ModCtrl | vaxis.ModShift},
		// ctrl+shift+pgup/pgdown move tabs in Firefox, and Alacritty leaves
		// them alone: its own scrolling is on shift+pgup without ctrl.
		MoveLeft:   Binding{Key: vaxis.KeyPgUp, Mods: vaxis.ModCtrl | vaxis.ModShift},
		MoveRight:  Binding{Key: vaxis.KeyPgDown, Mods: vaxis.ModCtrl | vaxis.ModShift},
		RenameTab:  Binding{Key: 'r', Mods: vaxis.ModCtrl | vaxis.ModShift},
		TabMods:    vaxis.ModCtrl,
		Shell:      []string{shell},
		Scrollback: defaultScrollback,
		Theme:      ThemeAuto,
	}
}

// filePath is ~/.config/thlmulti/config. A missing file is not an error.
func filePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "thlmulti", "config"), nil
}

// Load reads the config file over the defaults. A missing file is not an
// error; a line that does not parse is, and names the file and line.
func Load() (Config, error) {
	cfg := Default()
	path, err := filePath()
	if err != nil {
		return cfg, nil // no config dir: defaults are fine
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer f.Close() //nolint:errcheck // opened read-only, nothing to flush

	scan := bufio.NewScanner(f)
	for line := 1; scan.Scan(); line++ {
		text := strings.TrimSpace(scan.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		name, value, ok := strings.Cut(text, "=")
		if !ok {
			return cfg, fmt.Errorf("%s:%d: expected name = value", path, line)
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if err := cfg.set(name, value); err != nil {
			return cfg, fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
	return cfg, scan.Err()
}

// bindingSettings maps a config setting to the binding it sets.
var bindingSettings = map[string]func(*Config) *Binding{
	"new_tab":       func(c *Config) *Binding { return &c.NewTab },
	"close_tab":     func(c *Config) *Binding { return &c.CloseTab },
	"paste":         func(c *Config) *Binding { return &c.Paste },
	"next_tab":      func(c *Config) *Binding { return &c.NextTab },
	"prev_tab":      func(c *Config) *Binding { return &c.PrevTab },
	"attention_tab": func(c *Config) *Binding { return &c.AttentionTab },
	"move_left":     func(c *Config) *Binding { return &c.MoveLeft },
	"move_right":    func(c *Config) *Binding { return &c.MoveRight },
	"rename_tab":    func(c *Config) *Binding { return &c.RenameTab },
}

func (c *Config) set(name, value string) error {
	if slot, ok := bindingSettings[name]; ok {
		b, err := parseBinding(value)
		if err != nil {
			return err
		}
		*slot(c) = b
		return nil
	}
	if _, ok := themeSlots[name]; ok {
		color, err := parseColor(value)
		if err != nil {
			return err
		}
		if c.Colors == nil {
			c.Colors = map[string]vaxis.Color{}
		}
		c.Colors[name] = color
		return nil
	}

	switch name {
	case "tab_mods":
		mods, err := parseMods(strings.Split(value, "+"))
		if err != nil {
			return err
		}
		// Without a modifier, a bare digit would select a tab instead of being
		// typed. A typo here would make the terminal unusable, so refuse it.
		if mods == 0 {
			return fmt.Errorf("tab_mods needs at least one modifier, got %q", value)
		}
		c.TabMods = mods
		return nil
	case "shell":
		if fields := strings.Fields(value); len(fields) > 0 {
			c.Shell = fields
		}
		return nil
	case "scrollback":
		lines, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("scrollback: %q is not a number of lines", value)
		}
		if lines < 0 {
			return fmt.Errorf("scrollback cannot be negative, got %d", lines)
		}
		c.Scrollback = lines
		return nil
	case "theme":
		mode, err := parseThemeMode(value)
		if err != nil {
			return err
		}
		c.Theme = mode
		return nil
	default:
		// A typo in a keybind is worth failing on: silently ignoring it means
		// the key just doesn't work and you go looking in the wrong place.
		return fmt.Errorf("unknown setting %q", name)
	}
}

// parseBinding reads a chord like "ctrl+alt+v" or "ctrl+shift+tab". The key
// is a single character or one of the names in keyNames. "none" disables the
// binding.
func parseBinding(s string) (Binding, error) {
	if s == "" || s == "none" {
		return Binding{}, nil
	}
	parts := strings.Split(strings.ToLower(s), "+")
	name := parts[len(parts)-1]
	key, ok := keyByName(name)
	if !ok {
		runes := []rune(name)
		if len(runes) != 1 {
			return Binding{}, fmt.Errorf("%q: the key must be a single character or a key name like tab or pgup, got %q", s, name)
		}
		key = runes[0]
	}
	mods, err := parseMods(parts[:len(parts)-1])
	if err != nil {
		return Binding{}, fmt.Errorf("%q: %w", s, err)
	}
	return Binding{Key: key, Mods: mods}, nil
}

func parseMods(names []string) (vaxis.ModifierMask, error) {
	var mods vaxis.ModifierMask
	for _, name := range names {
		switch strings.TrimSpace(strings.ToLower(name)) {
		case "":
		case "ctrl", "control":
			mods |= vaxis.ModCtrl
		case "alt", "meta":
			mods |= vaxis.ModAlt
		case "shift":
			mods |= vaxis.ModShift
		case "super", "cmd":
			mods |= vaxis.ModSuper
		default:
			return 0, fmt.Errorf("unknown modifier %q", name)
		}
	}
	return mods, nil
}
