package main

import (
	"os"
	"testing"
)

func TestProcCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	got := procCwd(os.Getpid())
	// macOS and some sandboxes hand back a symlinked temp dir, so compare what
	// the OS itself reports rather than the path we asked for.
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("procCwd(self) = %q, want %q", got, want)
	}
}

func TestProcCwdOfNothing(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if got := procCwd(pid); got != "" {
			t.Errorf("procCwd(%d) = %q, want empty", pid, got)
		}
	}
}
