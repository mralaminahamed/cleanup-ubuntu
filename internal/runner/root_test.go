package runner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func tmpTree(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "blob"), make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRootUnitsSkippedWithAReasonWhenElevationFails(t *testing.T) {
	// Failing to get sudo must say so. The old behaviour printed
	// "exit status 1" with no cause and then listed the unit as reclaimed.
	sys := &unit.Unit{ID: "system-apt", Kind: unit.KindCmd, Command: "apt-get clean", NeedsRoot: true}
	usr := &unit.Unit{ID: "npm", Kind: unit.KindPaths, Paths: []string{tmpTree(t, 100)}, Bytes: 100}

	ran := false
	r := &Runner{
		Apply:     true,
		RootCheck: func() error { return errors.New("sudo: a password is required") },
		Exec:      func(string) error { ran = true; return nil },
	}
	res := r.Run([]*unit.Unit{sys, usr})

	if ran {
		t.Error("a root unit was executed despite elevation failing")
	}
	if res[0].Err == nil {
		t.Fatal("root unit reported no error")
	}
	if !strings.Contains(res[0].Err.Error(), "password") {
		t.Errorf("error %q does not carry the reason", res[0].Err)
	}
	if res[0].Freed != 0 {
		t.Errorf("skipped unit reported %d bytes freed", res[0].Freed)
	}
	// the ordinary unit must still run
	if res[1].Err != nil {
		t.Errorf("non-root unit failed: %v", res[1].Err)
	}
}

func TestElevationIsCheckedOncePerRun(t *testing.T) {
	// Three system units must not mean three password prompts.
	calls := 0
	units := []*unit.Unit{
		{ID: "a", Kind: unit.KindCmd, Command: "x", NeedsRoot: true},
		{ID: "b", Kind: unit.KindCmd, Command: "y", NeedsRoot: true},
		{ID: "c", Kind: unit.KindCmd, Command: "z", NeedsRoot: true},
	}
	r := &Runner{
		Apply:     true,
		RootCheck: func() error { calls++; return nil },
		Exec:      func(string) error { return nil },
	}
	r.Run(units)
	if calls != 1 {
		t.Errorf("RootCheck called %d times, want exactly 1", calls)
	}
}

func TestElevationNotRequestedForADryRun(t *testing.T) {
	// A dry run executes nothing, so it must never prompt for a password.
	called := false
	r := &Runner{
		Apply:     false,
		RootCheck: func() error { called = true; return nil },
	}
	r.Run([]*unit.Unit{{ID: "a", Kind: unit.KindCmd, Command: "x", NeedsRoot: true}})
	if called {
		t.Error("dry run asked for elevation")
	}
}

func TestElevationNotRequestedWhenNoUnitNeedsIt(t *testing.T) {
	called := false
	r := &Runner{
		Apply:     true,
		RootCheck: func() error { called = true; return nil },
		Exec:      func(string) error { return nil },
	}
	r.Run([]*unit.Unit{{ID: "a", Kind: unit.KindCmd, Command: "x"}})
	if called {
		t.Error("elevation requested for a run with no root units")
	}
}

func TestCommandFailureCarriesItsOutput(t *testing.T) {
	// "exit status 1" alone is not actionable; the tool's own stderr is.
	u := &unit.Unit{ID: "boom", Kind: unit.KindCmd, Command: "echo 'no such file' >&2; exit 3"}
	r := &Runner{Apply: true}
	res := r.Run([]*unit.Unit{u})

	if res[0].Err == nil {
		t.Fatal("failing command reported no error")
	}
	if !strings.Contains(res[0].Err.Error(), "no such file") {
		t.Errorf("error %q does not include the command's output", res[0].Err)
	}
}
