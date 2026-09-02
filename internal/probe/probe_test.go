package probe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func fixture(t *testing.T) (string, *unit.Registry) {
	t.Helper()
	dir := t.TempDir()
	mk := func(name string, n int) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	one := mk("one/blob", 1000)
	two := mk("two/blob", 2000)
	// A directory name containing a space must survive the round trip.
	sp := mk("with space/blob", 3000)

	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "one", Kind: unit.KindPaths, Paths: []string{filepath.Dir(one)}})
	r.Add(&unit.Unit{ID: "two", Kind: unit.KindPaths, Paths: []string{filepath.Dir(two)}})
	r.Add(&unit.Unit{ID: "space", Kind: unit.KindPaths, Paths: []string{filepath.Dir(sp)}})
	r.Add(&unit.Unit{ID: "multi", Kind: unit.KindPaths,
		Paths: []string{filepath.Dir(one), filepath.Dir(two)}})
	r.Add(&unit.Unit{ID: "absent", Kind: unit.KindPaths,
		Paths: []string{filepath.Join(dir, "does-not-exist")}})
	r.Add(&unit.Unit{ID: "cmd", Kind: unit.KindCmd, Command: "true", MountHint: dir})
	return dir, r
}

func TestProbeMeasuresEachUnit(t *testing.T) {
	_, r := fixture(t)
	All(r, 8)

	want := map[string]int64{
		"one": 1000, "two": 2000, "space": 3000,
		"multi":  3000, // one + two
		"absent": 0,
		"cmd":    0, // a command reclaims an unknown amount until it runs
	}
	for id, w := range want {
		u, ok := r.Get(id)
		if !ok {
			t.Fatalf("unit %q missing", id)
		}
		if u.Bytes != w {
			t.Errorf("unit %q Bytes = %d, want %d", id, u.Bytes, w)
		}
	}
}

func TestProbeDeletesNothing(t *testing.T) {
	// The single most important invariant in the tool: nothing reachable from
	// the probe phase may remove a byte.
	dir, r := fixture(t)
	var before []string
	filepath.Walk(dir, func(p string, _ os.FileInfo, _ error) error {
		before = append(before, p)
		return nil
	})

	All(r, 8)

	var after []string
	filepath.Walk(dir, func(p string, _ os.FileInfo, _ error) error {
		after = append(after, p)
		return nil
	})
	if len(before) != len(after) {
		t.Fatalf("probe changed the tree: %d entries before, %d after", len(before), len(after))
	}
}

func TestProbeResolvesMounts(t *testing.T) {
	_, r := fixture(t)
	All(r, 8)
	for _, u := range r.All() {
		if u.Mount == "" {
			t.Errorf("unit %q has no mount resolved", u.ID)
		}
	}
}

func TestParallelMatchesSerial(t *testing.T) {
	// Results must not depend on worker count, or the planner would pick
	// different units on a different machine.
	_, rSerial := fixture(t)
	All(rSerial, 1)
	_, rPar := fixture(t)
	All(rPar, 12)

	for _, s := range rSerial.All() {
		p, ok := rPar.Get(s.ID)
		if !ok {
			t.Fatalf("unit %q missing from parallel run", s.ID)
		}
		if s.Bytes != p.Bytes {
			t.Errorf("unit %q: serial %d bytes, parallel %d bytes", s.ID, s.Bytes, p.Bytes)
		}
	}
}

func TestEmptyRegistryDoesNotHang(t *testing.T) {
	All(unit.NewRegistry(), 8)
}

func TestZeroWorkersStillProbes(t *testing.T) {
	// A caller passing 0 must get a sane default rather than a deadlock.
	_, r := fixture(t)
	All(r, 0)
	u, _ := r.Get("one")
	if u.Bytes != 1000 {
		t.Errorf("Bytes = %d, want 1000", u.Bytes)
	}
}
