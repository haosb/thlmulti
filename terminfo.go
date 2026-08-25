package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// childTERM picks the TERM handed to child processes.
//
// widgets/term defaults this to xterm-kitty whenever the host speaks the Kitty
// keyboard protocol, which Alacritty does. But that terminfo entry ships with
// kitty itself and is absent on plenty of systems, and a TERM with no terminfo
// entry is far worse than a less capable one: zsh's line editor then redraws
// blind. The prompt renders as blank spaces and every backspace leaves another
// fragment on screen, one column further right than the last.
func childTERM() string {
	if hasTerminfo("xterm-kitty") {
		return "xterm-kitty"
	}
	return "xterm-256color"
}

// hasTerminfo reports whether an entry for name exists, searching where ncurses
// searches.
func hasTerminfo(name string) bool {
	if name == "" {
		return false
	}
	var dirs []string
	if dir := os.Getenv("TERMINFO"); dir != "" {
		dirs = append(dirs, dir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".terminfo"))
	}
	for _, dir := range filepath.SplitList(os.Getenv("TERMINFO_DIRS")) {
		if dir == "" {
			dir = "/usr/share/terminfo" // an empty entry means the default
		}
		dirs = append(dirs, dir)
	}
	dirs = append(dirs, "/etc/terminfo", "/lib/terminfo", "/usr/share/terminfo")

	// Entries live under their first letter, or under its hex code on
	// filesystems that dislike some of those characters.
	subdirs := []string{name[:1], fmt.Sprintf("%02x", name[0])}
	for _, dir := range dirs {
		for _, sub := range subdirs {
			if _, err := os.Stat(filepath.Join(dir, sub, name)); err == nil {
				return true
			}
		}
	}
	return false
}
