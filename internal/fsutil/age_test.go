package fsutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func touch(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

// Some targets are directories of records rather than a single disposable
// thing: a crash dump from this morning is the one being investigated, and the
// one from March is not.
func TestEntriesOlderThanSelectsOnlyOldEntries(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "old"), 30*24*time.Hour)
	touch(t, filepath.Join(dir, "fresh"), time.Hour)

	got := EntriesOlderThan(dir, 7*24*time.Hour)

	if len(got) != 1 || filepath.Base(got[0]) != "old" {
		t.Fatalf("got %v, want only the old entry", got)
	}
}

// The directory itself is not a candidate. Removing /var/crash rather than its
// contents would break the thing that writes into it.
func TestEntriesOlderThanNeverReturnsTheDirectory(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "old"), 30*24*time.Hour)
	when := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatal(err)
	}

	for _, p := range EntriesOlderThan(dir, 7*24*time.Hour) {
		if p == dir {
			t.Fatal("the directory itself was selected")
		}
	}
}

func TestEntriesOlderThanIsEmptyForAMissingDirectory(t *testing.T) {
	if got := EntriesOlderThan("/nonexistent", time.Hour); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

// A subdirectory of records is still one record. systemd writes core dumps as
// files, apport writes reports as files, but neither shape should be assumed.
func TestEntriesOlderThanIncludesDirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(sub, when, when); err != nil {
		t.Fatal(err)
	}

	if got := EntriesOlderThan(dir, 7*24*time.Hour); len(got) != 1 {
		t.Fatalf("got %v, want the subdirectory", got)
	}
}
