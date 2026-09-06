package lock

import (
	"strconv"
	"strings"
)

// parsePS reads "pid command..." lines as produced by "ps -Ao pid=,command=".
//
// Split out from the caller so it can be tested against real ps output rather
// than a fixture, on any platform that has ps -- which includes the one this is
// written for and the one it is developed on.
func parsePS(out string, skip ...int) []Process {
	skipped := make(map[int]bool, len(skip))
	for _, p := range skip {
		skipped[p] = true
	}

	var procs []Process
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		field, rest, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(field)
		if err != nil || skipped[pid] {
			continue
		}
		cmd := strings.TrimSpace(rest)
		if cmd == "" {
			continue
		}
		procs = append(procs, Process{PID: pid, Cmd: cmd})
	}
	return procs
}
