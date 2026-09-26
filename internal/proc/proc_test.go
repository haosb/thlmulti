package proc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	got := Cwd(os.Getpid())
	// macOS and some sandboxes hand back a symlinked temp dir, so compare what
	// the OS itself reports rather than the path we asked for.
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("Cwd(self) = %q, want %q", got, want)
	}
}

// A shell standing in a directory that has since been removed still has a
// cwd, and /proc reports it as "<path> (deleted)". A new tab started there
// fails to chdir, so there is no directory to hand on.
func TestProcCwdOfRemovedDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if got := Cwd(os.Getpid()); got != "" {
		t.Errorf("Cwd(self) in a removed directory = %q, want empty", got)
	}
}

func TestProcCwdOfNothing(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if got := Cwd(pid); got != "" {
			t.Errorf("Cwd(%d) = %q, want empty", pid, got)
		}
	}
}

// The command name in /proc/<pid>/stat is in parentheses and can contain
// anything, spaces and parentheses included. The fields after it have to be
// counted from the last closing parenthesis, or a process called "a) b" shifts
// every number over.
func TestForegroundGroup(t *testing.T) {
	for _, tc := range []struct {
		name string
		stat string
		want int
		ok   bool
	}{
		{"a shell at its prompt", "1234 (zsh) S 1 1234 1234 34816 1234 4194304 0", 1234, true},
		{"a shell with a job in front", "1234 (zsh) S 1 1234 1234 34816 5678 4194304 0", 5678, true},
		{"no controlling terminal", "1234 (zsh) S 1 1234 1234 0 -1 4194304 0", -1, true},
		{"parentheses in the name", "1234 (a) b (c)) S 1 1234 1234 34816 5678 4194304 0", 5678, true},
		{"too short", "1234 (zsh) S 1", 0, false},
		{"garbage", "", 0, false},
	} {
		got, ok := foregroundGroup(tc.stat)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: foregroundGroup() = (%d, %v), want (%d, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestProcRunningOfNothing(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if Running(pid) {
			t.Errorf("Running(%d) = true, want false", pid)
		}
	}
}
