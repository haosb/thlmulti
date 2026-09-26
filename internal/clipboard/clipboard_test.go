package clipboard

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeTools puts a wl-paste and an xclip on PATH that print their own name, and
// nothing else, so a test sees which one was asked.
func fakeTools(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"wl-paste", "xclip"} {
		script := "#!/bin/sh\nprintf " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

// The tool is picked by the session, not by what happens to be installed.
// wl-clipboard installed on an X11 desktop used to win, fail to reach a
// Wayland server, and have that failure read as an empty clipboard: paste
// silently did nothing.
func TestReadClipboardPicksTheSessionsTool(t *testing.T) {
	for _, tc := range []struct {
		name             string
		wayland, display string
		want             string
	}{
		{"wayland", "wayland-0", "", "wl-paste"},
		{"wayland with xwayland", "wayland-0", ":0", "wl-paste"},
		{"x11 with wl-clipboard installed", "", ":0", "xclip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeTools(t)
			t.Setenv("WAYLAND_DISPLAY", tc.wayland)
			t.Setenv("DISPLAY", tc.display)
			got, err := Read()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("Read() asked %q, want %q", got, tc.want)
			}
		})
	}
}

// No display at all, over ssh say, is worth saying so rather than pasting
// nothing.
func TestReadClipboardWithoutADisplay(t *testing.T) {
	fakeTools(t)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	if _, err := Read(); err == nil {
		t.Error("Read() with no display returned no error")
	}
}

// A session whose tool is not installed names the tool it looked for.
func TestReadClipboardWithoutTheTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", "")
	_, err := Read()
	if err == nil || err.Error() != "no clipboard tool found, tried wl-paste" {
		t.Errorf("Read() error = %v, want it to name wl-paste", err)
	}
}
