package lock

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Tested against real ps output rather than a fixture. The same invocation
// works on Linux, so the parser that macOS depends on is exercised by every
// run of this suite -- which matters, because nothing else here can be.
func TestParsePSReadsRealPsOutput(t *testing.T) {
	out, err := exec.Command("ps", "-Ao", "pid=,command=").Output()
	if err != nil {
		t.Skipf("no usable ps here: %v", err)
	}

	procs := parsePS(string(out))
	if len(procs) == 0 {
		t.Fatalf("parsed nothing from %d bytes of ps output", len(out))
	}
	for _, p := range procs {
		if p.PID <= 0 {
			t.Errorf("bad pid %d in %q", p.PID, p.Cmd)
		}
		if strings.TrimSpace(p.Cmd) == "" {
			t.Errorf("pid %d has an empty command", p.PID)
		}
	}

	// This test's own process must be in there, which is what proves the
	// parser is reading the table rather than producing plausible noise.
	self := os.Getpid()
	found := false
	for _, p := range procs {
		if p.PID == self {
			found = true
		}
	}
	if !found {
		t.Errorf("our own pid %d is missing from a table of %d processes", self, len(procs))
	}
}

func TestParsePSSkipsTheGivenPids(t *testing.T) {
	out := "  101 /usr/bin/chrome\n  102 /usr/bin/reclaim clean --gradle\n  103 /bin/zsh\n"

	procs := parsePS(out, 102)

	if len(procs) != 2 {
		t.Fatalf("got %d processes, want 2: %+v", len(procs), procs)
	}
	for _, p := range procs {
		if p.PID == 102 {
			t.Fatal("a skipped pid came through")
		}
	}
}

// A command line with spaces is the normal case, not an edge case: the rules
// match on things like "/Applications/Slack.app/Contents/MacOS/Slack".
func TestParsePSKeepsTheWholeCommandLine(t *testing.T) {
	out := "  501 /Applications/Slack.app/Contents/MacOS/Slack --enable-features=X\n"

	procs := parsePS(out)

	if len(procs) != 1 {
		t.Fatalf("got %+v", procs)
	}
	if procs[0].Cmd != "/Applications/Slack.app/Contents/MacOS/Slack --enable-features=X" {
		t.Fatalf("command truncated: %q", procs[0].Cmd)
	}
}

// ps prints a header unless every -o field ends in "=". If that is ever got
// wrong the header parses as a process, so the parser refuses it either way.
func TestParsePSIgnoresAHeaderLine(t *testing.T) {
	out := "  PID COMMAND\n  101 /usr/bin/chrome\n"

	if procs := parsePS(out); len(procs) != 1 || procs[0].PID != 101 {
		t.Fatalf("got %+v", procs)
	}
}

func TestParsePSIgnoresBlankAndMalformedLines(t *testing.T) {
	out := "\n  101 /usr/bin/chrome\n\nnotanumber\n  \n102\n"

	if procs := parsePS(out); len(procs) != 1 || procs[0].PID != 101 {
		t.Fatalf("got %+v", procs)
	}
}
