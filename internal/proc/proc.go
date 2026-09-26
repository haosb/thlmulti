// Package proc reads what /proc knows about a tab's shell: where it is
// standing, and whether a job holds its terminal.
package proc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Cwd returns the working directory of a running process, or "" if it
// cannot be read because the process is gone or belongs to someone else, or
// if the directory itself is gone: /proc then reports "<path> (deleted)",
// which is no directory a new tab could start in.
//
// The alternative is OSC 7, where the shell reports its directory as it
// changes. That is the portable answer, but it only works when the shell has
// been set up to send it, and oh-my-zsh does not do so by default. Reading
// /proc needs no cooperation from the shell at all.
func Cwd(pid int) string {
	if pid <= 0 {
		return ""
	}
	dir, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return ""
	}
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

// Running reports whether the shell with this pid has handed its terminal
// to a job: a command is in the foreground rather than the prompt. That is
// what tells an agent still working apart from one that has finished and
// returned to the shell, without the shell having to say anything.
//
// It reads the foreground process group of the shell's terminal from
// /proc/<pid>/stat. The shell leads its own process group, so the terminal
// belongs to the shell exactly when that group is the shell's own pid.
func Running(pid int) bool {
	if pid <= 0 {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	tpgid, ok := foregroundGroup(string(stat))
	return ok && tpgid > 0 && tpgid != pid
}

// foregroundGroup pulls the terminal's foreground process group, field 8, out
// of a /proc/<pid>/stat line. The command name in field 2 is in parentheses
// and may itself contain spaces and parentheses, so the split has to start
// after the last closing one rather than at the first space.
func foregroundGroup(stat string) (int, bool) {
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, false
	}
	// After the name: state, ppid, pgrp, session, tty_nr, tpgid, ...
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 6 {
		return 0, false
	}
	tpgid, err := strconv.Atoi(fields[5])
	if err != nil {
		return 0, false
	}
	return tpgid, true
}
