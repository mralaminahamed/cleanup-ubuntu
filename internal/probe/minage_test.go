package probe

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func aged(t *testing.T, path string, size int, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

// A unit that only takes old entries must only report old entries. Counting the
// whole directory would promise space that the run will then decline to free.
func TestMinAgeUnitMeasuresOnlyWhatItWouldTake(t *testing.T) {
	dir := t.TempDir()
	aged(t, filepath.Join(dir, "old"), 4096, 30*24*time.Hour)
	aged(t, filepath.Join(dir, "fresh"), 8192, time.Hour)

	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "crash", Kind: unit.KindPaths, Paths: []string{dir},
		MinAge: 7 * 24 * time.Hour})
	All(r, 1)

	u, _ := r.Get("crash")
	if u.Bytes != 4096 {
		t.Fatalf("bytes %d, want only the old entry", u.Bytes)
	}
}

func TestUnitWithoutMinAgeStillMeasuresTheWholePath(t *testing.T) {
	dir := t.TempDir()
	aged(t, filepath.Join(dir, "old"), 4096, 30*24*time.Hour)
	aged(t, filepath.Join(dir, "fresh"), 8192, time.Hour)

	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "cache", Kind: unit.KindPaths, Paths: []string{dir}})
	All(r, 1)

	u, _ := r.Get("cache")
	if u.Bytes != 12288 {
		t.Fatalf("bytes %d, want the whole directory", u.Bytes)
	}
}

// Some targets need root to delete but not to measure. /var/crash is
// world-readable, so a command unit that shells out to sudo can still report an
// honest number -- and with an age bound, the number has to exclude the fresh
// entries the command will leave behind.
func TestMinAgeAppliesToSizePathsToo(t *testing.T) {
	dir := t.TempDir()
	aged(t, filepath.Join(dir, "old"), 4096, 30*24*time.Hour)
	aged(t, filepath.Join(dir, "fresh"), 8192, time.Hour)

	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "crash", Kind: unit.KindCmd, Command: "true",
		SizePaths: []string{dir}, MinAge: 7 * 24 * time.Hour})
	All(r, 1)

	u, _ := r.Get("crash")
	if u.Bytes != 4096 {
		t.Fatalf("bytes %d, want only the old entry", u.Bytes)
	}
}
