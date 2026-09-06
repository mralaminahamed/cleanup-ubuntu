package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// "what ran" is answerable from the unit id. "what was removed" is not, and it
// is the question that matters after a --discover run, where the unit list was
// not knowable in advance.
func TestResultRecordsWhatWasRemoved(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "cache")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	res := (&Runner{Apply: true}).Run([]*unit.Unit{
		{ID: "u", Kind: unit.KindPaths, Paths: []string{sub}},
	})

	if len(res[0].Removed) != 1 || res[0].Removed[0] != sub {
		t.Fatalf("removed %v, want %q", res[0].Removed, sub)
	}
}

// An age-bounded unit removes entries, not the directory it was pointed at.
// Recording the directory would name something that is still there.
func TestResultRecordsTheEntriesAnAgeBoundTook(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old")
	aged(t, old, 30*24*time.Hour)
	aged(t, filepath.Join(dir, "fresh"), time.Hour)

	res := (&Runner{Apply: true}).Run([]*unit.Unit{
		{ID: "u", Kind: unit.KindPaths, Paths: []string{dir}, MinAge: 7 * 24 * time.Hour},
	})

	if len(res[0].Removed) != 1 || res[0].Removed[0] != old {
		t.Fatalf("removed %v, want only %q", res[0].Removed, old)
	}
}

// Removed means removed. A dry run deletes nothing, so it has nothing to name.
func TestDryRunRecordsNothingAsRemoved(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := (&Runner{}).Run([]*unit.Unit{
		{ID: "u", Kind: unit.KindPaths, Paths: []string{filepath.Join(dir, "cache")}},
	})

	if len(res[0].Removed) != 0 {
		t.Fatalf("a dry run claimed to have removed %v", res[0].Removed)
	}
}

// A command deletes its own data by its own rules. Naming its unit's paths
// would be inventing a record of something this code did not do.
func TestCommandUnitRecordsNothingAsRemoved(t *testing.T) {
	res := (&Runner{Apply: true}).Run([]*unit.Unit{
		{ID: "c", Kind: unit.KindCmd, Command: "true", SizePaths: []string{"/tmp"}},
	})

	if len(res[0].Removed) != 0 {
		t.Fatalf("invented a removal record: %v", res[0].Removed)
	}
}
