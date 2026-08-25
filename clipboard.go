package main

import (
	"errors"
	"os/exec"
	"strings"
)

// readClipboard returns the system clipboard.
//
// Copying goes out over OSC 52, but reading cannot: terminals refuse OSC 52
// reads because they would let any program on any pty exfiltrate the
// clipboard. So paste has to ask the compositor directly.
func readClipboard() (string, error) {
	candidates := [][]string{
		{"wl-paste", "--no-newline"},
		{"xclip", "-selection", "clipboard", "-out"},
	}
	var missing []string
	for _, argv := range candidates {
		path, err := exec.LookPath(argv[0])
		if err != nil {
			missing = append(missing, argv[0])
			continue
		}
		out, err := exec.Command(path, argv[1:]...).Output()
		if err != nil {
			// An empty clipboard makes wl-paste exit non-zero. That is not a
			// failure worth reporting, and there is nothing to paste.
			return "", nil
		}
		return string(out), nil
	}
	return "", errors.New("no clipboard tool found, tried " + strings.Join(missing, ", "))
}
