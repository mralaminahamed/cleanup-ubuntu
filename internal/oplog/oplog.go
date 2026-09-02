// Package oplog records what the tool actually deleted.
//
// A tool that removes files should be able to answer "what did you take?"
// afterwards. Entries are newline-delimited JSON so the file stays appendable,
// greppable, and readable even if a run is killed mid-write.
package oplog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Entry is one unit's outcome in one run.
type Entry struct {
	At      time.Time `json:"at"`
	UnitID  string    `json:"unit_id"`
	Label   string    `json:"label,omitempty"`
	Freed   int64     `json:"freed_bytes"`
	Applied bool      `json:"applied"`
	Err     string    `json:"error,omitempty"`
}

// Log is an append-only operations log.
type Log struct {
	Path string
	// Disabled turns logging off entirely.
	Disabled bool
}

// DefaultPath returns the usual location for the log under a home directory.
func DefaultPath(home string) string {
	return filepath.Join(home, ".local", "share", "reclaim", "operations.log")
}

// Append writes one entry, creating the parent directory if needed.
func (l *Log) Append(e Entry) error {
	if l.Disabled || l.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// Read returns up to limit of the most recent entries, oldest first.
//
// A log that does not exist yet is not an error, and an unparseable line is
// skipped rather than failing the read: a run killed mid-write must not make
// the whole history unreadable.
func (l *Log) Read(limit int) ([]Entry, error) {
	f, err := os.Open(l.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		all = append(all, e)
	}
	if err := sc.Err(); err != nil {
		return all, err
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}
