package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func tree(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "blob"), make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDryRunDeletesNothing(t *testing.T) {
	dir := tree(t, 4096)
	u := &unit.Unit{ID: "u", Kind: unit.KindPaths, Paths: []string{dir}, Bytes: 4096}

	r := &Runner{Apply: false}
	res := r.Run([]*unit.Unit{u})

	if _, err := os.Stat(filepath.Join(dir, "blob")); err != nil {
		t.Fatalf("dry run deleted the file: %v", err)
	}
	if res[0].Freed != 4096 {
		t.Errorf("Freed = %d, want the probed 4096 reported as would-free", res[0].Freed)
	}
}

func TestApplyDeletesPathsAndReportsFreed(t *testing.T) {
	dir := tree(t, 4096)
	u := &unit.Unit{ID: "u", Kind: unit.KindPaths, Paths: []string{dir}, Bytes: 4096}

	r := &Runner{Apply: true}
	res := r.Run([]*unit.Unit{u})

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("apply did not remove the path: %v", err)
	}
	if res[0].Freed != 4096 {
		t.Errorf("Freed = %d, want 4096", res[0].Freed)
	}
}

func TestApplyRunsCommandUnits(t *testing.T) {
	var got string
	u := &unit.Unit{ID: "c", Kind: unit.KindCmd, Command: "npm cache clean --force"}

	r := &Runner{Apply: true, Exec: func(cmd string) error { got = cmd; return nil }}
	r.Run([]*unit.Unit{u})

	if got != "npm cache clean --force" {
		t.Errorf("Exec got %q, want the unit's command", got)
	}
}

func TestDryRunNeverRunsCommands(t *testing.T) {
	called := false
	u := &unit.Unit{ID: "c", Kind: unit.KindCmd, Command: "rm -rf /"}

	r := &Runner{Apply: false, Exec: func(string) error { called = true; return nil }}
	r.Run([]*unit.Unit{u})

	if called {
		t.Fatal("dry run executed a command unit")
	}
}

func TestStopsEarlyOnceTargetIsMet(t *testing.T) {
	// Freeing more than asked is a cost with no benefit, so the run stops the
	// moment the target is reached.
	a := &unit.Unit{ID: "a", Kind: unit.KindPaths, Paths: []string{tree(t, 10)}, Bytes: 10}
	b := &unit.Unit{ID: "b", Kind: unit.KindPaths, Paths: []string{tree(t, 10)}, Bytes: 10}
	c := &unit.Unit{ID: "c", Kind: unit.KindPaths, Paths: []string{tree(t, 10)}, Bytes: 10}

	avail := int64(0)
	r := &Runner{
		Apply:       true,
		TargetBytes: 20,
		TargetPath:  "/",
		Avail: func(string) (int64, error) {
			avail += 10 // each completed unit frees ten
			return avail, nil
		},
	}
	res := r.Run([]*unit.Unit{a, b, c})

	if len(res) != 2 {
		t.Fatalf("ran %d units, want 2 before the target was met", len(res))
	}
	if !r.StoppedEarly {
		t.Error("StoppedEarly = false, want true")
	}
}

func TestNoTargetRunsEverything(t *testing.T) {
	a := &unit.Unit{ID: "a", Kind: unit.KindPaths, Paths: []string{tree(t, 10)}, Bytes: 10}
	b := &unit.Unit{ID: "b", Kind: unit.KindPaths, Paths: []string{tree(t, 10)}, Bytes: 10}

	r := &Runner{Apply: true}
	if res := r.Run([]*unit.Unit{a, b}); len(res) != 2 {
		t.Fatalf("ran %d units, want 2", len(res))
	}
	if r.StoppedEarly {
		t.Error("StoppedEarly = true with no target set")
	}
}

func TestFailureOnOneUnitDoesNotAbortTheRest(t *testing.T) {
	bad := &unit.Unit{ID: "bad", Kind: unit.KindCmd, Command: "boom"}
	good := &unit.Unit{ID: "good", Kind: unit.KindPaths, Paths: []string{tree(t, 10)}, Bytes: 10}

	r := &Runner{Apply: true, Exec: func(cmd string) error {
		if cmd == "boom" {
			return os.ErrPermission
		}
		return nil
	}}
	res := r.Run([]*unit.Unit{bad, good})

	if len(res) != 2 {
		t.Fatalf("ran %d units, want both attempted", len(res))
	}
	if res[0].Err == nil {
		t.Error("failing unit reported no error")
	}
}

func TestRefusesToDeleteDangerousPaths(t *testing.T) {
	// A malformed unit must never be able to aim the deleter at the filesystem
	// root or a home directory.
	for _, p := range []string{"/", "/home", os.Getenv("HOME"), "", "/usr"} {
		if p == "" {
			continue
		}
		u := &unit.Unit{ID: "danger", Kind: unit.KindPaths, Paths: []string{p}, Bytes: 1}
		r := &Runner{Apply: true}
		res := r.Run([]*unit.Unit{u})
		if res[0].Err == nil {
			t.Errorf("deleting %q was permitted", p)
		}
	}
}
