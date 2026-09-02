package lock

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
)

// Running reads the process table.
//
// Our own process is excluded on purpose: this tool's command line contains the
// very names the rules match on ("--gradle", a path with "chrome" in it), so
// including it would let the tool lock itself out of everything it was asked to
// clean.
func Running() []Process {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	self := os.Getpid()
	parent := os.Getppid()

	var out []Process
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self || pid == parent {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		// Arguments are NUL-separated; rules are written against a normal
		// space-separated command line.
		cmd := string(bytes.TrimRight(bytes.ReplaceAll(raw, []byte{0}, []byte{' '}), " "))
		if cmd == "" {
			continue
		}
		out = append(out, Process{PID: pid, Cmd: cmd})
	}
	return out
}
