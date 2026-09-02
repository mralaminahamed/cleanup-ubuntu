package lock

import (
	"os"
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

func TestRunningFindsProcesses(t *testing.T) {
	procs := Running()
	if len(procs) == 0 {
		t.Skip("no readable process table here")
	}
	for _, p := range procs {
		if p.PID <= 0 {
			t.Errorf("bad pid %d", p.PID)
		}
	}
}
