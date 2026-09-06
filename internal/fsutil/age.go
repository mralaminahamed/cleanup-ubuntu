package fsutil

import (
	"os"
	"path/filepath"
	"time"
)

// EntriesOlderThan returns the entries directly inside dir that were last
// modified longer ago than age.
//
// Some targets are a directory of records rather than one disposable thing.
// A crash dump written this morning is the one being investigated; the one from
// March is not, and deleting the whole directory cannot tell them apart. The
// directory itself is never returned -- removing /var/crash rather than its
// contents would break the thing that writes into it.
func EntriesOlderThan(dir string, age time.Duration) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-age)

	var out []string
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			// Something removed it between the listing and the stat. Not ours
			// to report, and certainly not ours to delete.
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}
