// Package clipboard reads the system clipboard, which a terminal program
// cannot ask its terminal for.
package clipboard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// timeout bounds how long a clipboard tool may take. wl-paste waits
// on the owner of the clipboard to hand the data over, and an owner that has
// hung or gone away never does; without a limit that wait was ours too.
const timeout = 3 * time.Second

// tools are the programs that can read the clipboard, each with the
// variable that says its display server is there. A tool is only asked when
// its session is: wl-paste on an X11 desktop cannot reach a Wayland server,
// and its failure looks just like an empty clipboard. Wayland comes first,
// because under XWayland both are set and xclip sees only the X11 side.
var tools = []struct {
	env  string
	argv []string
}{
	{"WAYLAND_DISPLAY", []string{"wl-paste", "--no-newline"}},
	{"DISPLAY", []string{"xclip", "-selection", "clipboard", "-out"}},
}

// Read returns the system clipboard.
//
// Copying goes out over OSC 52, but reading cannot: terminals refuse OSC 52
// reads because they would let any program on any pty exfiltrate the
// clipboard. So paste has to ask the compositor directly. It is called off the
// event loop, because it runs a program and waits for it.
func Read() (string, error) {
	var missing []string
	for _, tool := range tools {
		if os.Getenv(tool.env) == "" {
			continue
		}
		path, err := exec.LookPath(tool.argv[0])
		if err != nil {
			missing = append(missing, tool.argv[0])
			continue
		}
		return run(path, tool.argv)
	}
	if len(missing) == 0 {
		return "", errors.New("no display to read the clipboard from: neither WAYLAND_DISPLAY nor DISPLAY is set")
	}
	return "", errors.New("no clipboard tool found, tried " + strings.Join(missing, ", "))
}

// run runs the tool at path with argv[1:] and returns what it
// printed, giving up after timeout.
func run(path string, argv []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, argv[1:]...).Output()
	if ctx.Err() != nil {
		return "", errors.New(argv[0] + " did not answer")
	}
	if err != nil {
		// An empty clipboard makes wl-paste exit non-zero. That is not a
		// failure worth reporting, and there is nothing to paste.
		return "", nil
	}
	return string(out), nil
}
