package unitfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// DefaultFiles are the locations a unit file is read from, user first.
//
// The system directory is a directory rather than a file so a package can drop
// one in without editing anything an administrator owns.
func DefaultFiles(home string) []string {
	files := []string{filepath.Join(home, ".config", "reclaim", "units.json")}
	sys, _ := filepath.Glob("/etc/reclaim/units.d/*.json")
	sort.Strings(sys)
	return append(files, sys...)
}

// Add registers the units defined in files, skipping any whose tool is not
// installed or whose paths are not on disk.
//
// Registration happens after the catalog, so unit.Registry's first-wins rule
// means a file can add to the shipped set but never restate it. That is
// deliberate: a shipped unit's tier and reversibility were decided with
// knowledge of the target, and a file that could overwrite them would be a way
// to smuggle a deletion past the model.
//
// A missing file is the normal case and is not an error. A file that exists and
// does not parse is: the user wrote something that is not being honoured, and
// cleaning with a unit set nobody authored is worse than not cleaning.
func Add(r *unit.Registry, home string, files []string, has func(string) bool) error {
	for _, f := range files {
		data, err := os.ReadFile(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading %s: %w", f, err)
		}
		us, err := Parse(data, home, has)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		for _, u := range us {
			if !onDisk(u) {
				continue
			}
			r.Add(u)
		}
	}
	return nil
}

// onDisk reports whether a path unit has anything to act on. The catalog
// applies the same rule: a unit for a directory that is not there is noise in
// every report.
func onDisk(u *unit.Unit) bool {
	if u.Kind != unit.KindPaths {
		return true
	}
	for _, p := range u.Paths {
		if _, err := os.Lstat(p); err == nil {
			return true
		}
	}
	return false
}
