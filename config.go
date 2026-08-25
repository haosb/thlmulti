package main

import (
	"bufio"
	"fmt"
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

func (b Binding) matches(k vaxis.Key) bool {
	return b.Key != 0 && k.Matches(b.Key, b.Mods)
}

func defaultConfig() Config {
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
		Paste:      Binding{Key: 'v', Mods: vaxis.ModCtrl | vaxis.ModAlt},
		TabMods:    vaxis.ModCtrl,
		Shell:      []string{shell},
		Scrollback: defaultScrollback,
		Theme:      ThemeAuto,
	}
}

// configPath is ~/.config/thlmulti/config. A missing file is not an error.
func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "thlmulti", "config"), nil
}

func loadConfig() (Config, error) {
	cfg := defaultConfig()
	path, err := configPath()
	if err != nil {
		return cfg, nil // no config dir: defaults are fine
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer f.Close()

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

func (c *Config) set(name, value string) error {
	switch name {
	case "new_tab":
		b, err := parseBinding(value)
		c.NewTab = b
		return err
	case "close_tab":
		b, err := parseBinding(value)
		c.CloseTab = b
		return err
	case "paste":
		b, err := parseBinding(value)
		c.Paste = b
		return err
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
		c.Theme = mode
		return err
	default:
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
		// A typo in a keybind is worth failing on: silently ignoring it means
		// the key just doesn't work and you go looking in the wrong place.
		return fmt.Errorf("unknown setting %q", name)
	}
}

// parseBinding reads a chord like "ctrl+alt+v". "none" disables the binding.
func parseBinding(s string) (Binding, error) {
	if s == "" || s == "none" {
		return Binding{}, nil
	}
	parts := strings.Split(strings.ToLower(s), "+")
	key := parts[len(parts)-1]
	runes := []rune(key)
	if len(runes) != 1 {
		return Binding{}, fmt.Errorf("%q: the key must be a single character, got %q", s, key)
	}
	mods, err := parseMods(parts[:len(parts)-1])
	if err != nil {
		return Binding{}, fmt.Errorf("%q: %w", s, err)
	}
	return Binding{Key: runes[0], Mods: mods}, nil
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
