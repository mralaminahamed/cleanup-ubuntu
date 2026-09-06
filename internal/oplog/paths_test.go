package oplog

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func logAt(t *testing.T) *Log {
	t.Helper()
	return &Log{Path: filepath.Join(t.TempDir(), "operations.log")}
}

func TestEntryKeepsThePathsItWasGiven(t *testing.T) {
	l := logAt(t)
	if err := l.Append(Entry{At: time.Now(), UnitID: "u", Applied: true,
		Paths: []string{"/home/u/.cache/pip", "/home/u/.npm/_cacache"}}); err != nil {
		t.Fatal(err)
	}

	got, err := l.Read(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Paths) != 2 {
		t.Fatalf("read back %+v", got)
	}
	if got[0].PathCount != 2 {
		t.Errorf("count %d, want 2", got[0].PathCount)
	}
}

// A --discover run can touch hundreds of directories, and the log is a line per
// unit that has to stay readable and bounded. The count stays true even when
// the list is trimmed, so the record never understates what happened.
func TestManyPathsAreTrimmedButStillCounted(t *testing.T) {
	l := logAt(t)
	var many []string
	for i := 0; i < 500; i++ {
		many = append(many, "/home/u/.cache/x"+strings.Repeat("y", i%7))
	}

	if err := l.Append(Entry{At: time.Now(), UnitID: "u", Applied: true, Paths: many}); err != nil {
		t.Fatal(err)
	}

	got, _ := l.Read(1)
	if len(got[0].Paths) >= len(many) {
		t.Fatalf("stored %d paths unbounded", len(got[0].Paths))
	}
	if got[0].PathCount != 500 {
		t.Fatalf("count %d, want the true total of 500", got[0].PathCount)
	}
}

func TestEntryWithoutPathsStaysCompact(t *testing.T) {
	l := logAt(t)
	if err := l.Append(Entry{At: time.Now(), UnitID: "c", Applied: true}); err != nil {
		t.Fatal(err)
	}

	got, _ := l.Read(1)
	if got[0].PathCount != 0 || len(got[0].Paths) != 0 {
		t.Fatalf("invented paths: %+v", got[0])
	}
}
