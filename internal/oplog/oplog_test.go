package oplog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	l := &Log{Path: path}

	when := time.Date(2026, 9, 2, 15, 4, 5, 0, time.UTC)
	if err := l.Append(Entry{At: when, UnitID: "npm-cacache", Freed: 4096, Applied: true}); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(Entry{At: when, UnitID: "pip-cache", Freed: 100, Applied: false}); err != nil {
		t.Fatal(err)
	}

	got, err := l.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d entries, want 2", len(got))
	}
	if got[0].UnitID != "npm-cacache" || got[0].Freed != 4096 || !got[0].Applied {
		t.Errorf("entry round-tripped wrong: %+v", got[0])
	}
}

func TestReadLimitsToMostRecent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	l := &Log{Path: path}
	for i := 0; i < 5; i++ {
		if err := l.Append(Entry{At: time.Now(), UnitID: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := l.Read(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d, want 2", len(got))
	}
	if got[1].UnitID != "e" {
		t.Errorf("last entry = %q, want the most recent 'e'", got[1].UnitID)
	}
}

func TestReadMissingLogIsEmptyNotAnError(t *testing.T) {
	l := &Log{Path: filepath.Join(t.TempDir(), "absent.log")}
	got, err := l.Read(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want 0", len(got))
	}
}

func TestAppendCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deep", "operations.log")
	l := &Log{Path: path}
	if err := l.Append(Entry{At: time.Now(), UnitID: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log not created: %v", err)
	}
}

func TestCorruptLineIsSkippedNotFatal(t *testing.T) {
	// A truncated write from a killed run must not make history unreadable.
	path := filepath.Join(t.TempDir(), "operations.log")
	if err := os.WriteFile(path, []byte("{not json\n{\"unit_id\":\"ok\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (&Log{Path: path}).Read(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].UnitID != "ok" {
		t.Errorf("got %+v, want just the readable entry", got)
	}
}

func TestDisabledLogWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	l := &Log{Path: path, Disabled: true}
	if err := l.Append(Entry{At: time.Now(), UnitID: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("disabled log still wrote a file")
	}
}

func TestEntryIsNDJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.log")
	l := &Log{Path: path}
	l.Append(Entry{At: time.Now(), UnitID: "a"})
	l.Append(Entry{At: time.Now(), UnitID: "b"})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want one JSON object per line", len(lines))
	}
}
