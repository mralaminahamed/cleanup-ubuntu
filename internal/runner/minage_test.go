package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func aged(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

// The whole point of an age bound is the entry it does not take. A crash dump
// from this morning is the one someone is looking at.
func TestMinAgeUnitDeletesOldEntriesAndKeepsFreshOnes(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old")
	fresh := filepath.Join(dir, "fresh")
	aged(t, old, 30*24*time.Hour)
	aged(t, fresh, time.Hour)

	r := &Runner{Apply: true}
	r.Run([]*unit.Unit{{ID: "crash", Kind: unit.KindPaths, Paths: []string{dir},
		MinAge: 7 * 24 * time.Hour}})

	if _, err := os.Stat(old); err == nil {
		t.Error("old entry survived")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh entry was deleted: %v", err)
	}
}

// The directory is the thing that receives new records. Taking it would break
// whatever writes there, and is not what the unit asked for.
func TestMinAgeUnitLeavesTheDirectoryItself(t *testing.T) {
	dir := t.TempDir()
	aged(t, filepath.Join(dir, "old"), 30*24*time.Hour)

	r := &Runner{Apply: true}
	r.Run([]*unit.Unit{{ID: "crash", Kind: unit.KindPaths, Paths: []string{dir},
		MinAge: 7 * 24 * time.Hour}})

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the directory was removed: %v", err)
	}
}

func TestMinAgeUnitDeletesNothingInADryRun(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old")
	aged(t, old, 30*24*time.Hour)

	res := (&Runner{}).Run([]*unit.Unit{{ID: "crash", Kind: unit.KindPaths,
		Paths: []string{dir}, MinAge: 7 * 24 * time.Hour}})

	if _, err := os.Stat(old); err != nil {
		t.Fatalf("a dry run deleted something: %v", err)
	}
	if res[0].Freed == 0 {
		t.Error("a dry run reported nothing reclaimable")
	}
}

// The backstop is not something an age bound gets to route around.
func TestMinAgeUnitStillGoesThroughTheProtectedPathCheck(t *testing.T) {
	res := (&Runner{Apply: true}).Run([]*unit.Unit{{ID: "bad", Kind: unit.KindPaths,
		Paths: []string{"/"}, MinAge: time.Hour}})

	if res[0].Err == nil {
		t.Fatal("a unit aimed at / was accepted")
	}
}
