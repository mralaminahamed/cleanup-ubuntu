package lock

import (
	"os"
	"strings"
	"testing"
)

func TestRunningExcludesSelf(t *testing.T) {
	// Our own command line mentions the very app names we match on ("gradle"
	// appears in flags and paths), so including ourselves would let the tool
	// lock itself out of every unit it was asked to clean.
	self := os.Getpid()
	for _, p := range Running() {
		if p.PID == self {
			t.Fatalf("Running() included our own pid %d: %q", p.PID, p.Cmd)
		}
	}
}

// An empty process table is never a true answer on a machine that is running
// this test, so it is a failure and not a reason to skip.
//
// The skip this replaces is what let macOS go unnoticed: Running() read /proc,
// os.ReadDir failed, it returned nil, and the guard shrugged. Apply then
// iterated an empty list, marked nothing as locked, and every unit looked free
// to delete -- with no error, and a "Locked by running apps" section that read
// as "nothing is running".
func TestRunningFindsProcesses(t *testing.T) {
	procs := Running()
	if len(procs) == 0 {
		t.Fatal("empty process table: either the reader is broken or this platform has none")
	}
	for _, p := range procs {
		if p.PID <= 0 {
			t.Errorf("bad pid %d", p.PID)
		}
		if strings.TrimSpace(p.Cmd) == "" {
			t.Errorf("pid %d has an empty command line", p.PID)
		}
	}
}
