package main

import (
	"fmt"
	"os"
)

// procCwd returns the working directory of a running process, or "" if it
// cannot be read because the process is gone or belongs to someone else.
//
// The alternative is OSC 7, where the shell reports its directory as it
// changes. That is the portable answer, but it only works when the shell has
// been set up to send it, and oh-my-zsh does not do so by default. Reading
// /proc needs no cooperation from the shell at all.
func procCwd(pid int) string {
	if pid <= 0 {
		return ""
	}
	dir, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return ""
	}
	return dir
}
