package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasTerminfo(t *testing.T) {
	// xterm-256color is part of the base ncurses data on any system that has
	// terminfo at all. If this fails, the search itself is broken.
	if !hasTerminfo("xterm-256color") {
		t.Error("xterm-256color should be found")
	}
	if hasTerminfo("no-such-terminal-entry") {
		t.Error("a made-up name should not be found")
	}
	if hasTerminfo("") {
		t.Error("an empty name should not be found")
	}
}

func TestHasTerminfoUsesTERMINFO(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "z"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "z", "zzz-made-up"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMINFO", dir)
	if !hasTerminfo("zzz-made-up") {
		t.Error("an entry under $TERMINFO should be found")
	}
}

// The whole point of childTERM: never hand a child a TERM with no terminfo
// entry behind it, because zsh's line editor then redraws blind.
func TestChildTERMAlwaysResolves(t *testing.T) {
	if got := childTERM(); !hasTerminfo(got) {
		t.Errorf("childTERM() = %q, which has no terminfo entry", got)
	}
}
