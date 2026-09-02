// Package scan finds reclaimable space inside project directories.
//
// Unlike a cache, a dependency tree is not freely regenerable: reinstalling
// needs the network and a lockfile that may no longer resolve to the same
// versions. Everything here is therefore lossy and opt-in.
package scan

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// depDirs are dependency trees, paired with the manifest that proves the
// directory really is a project of that kind.
var depDirs = []struct{ dir, manifest string }{
	{"node_modules", "package.json"},
	{"vendor", "composer.json"},
}

// IdleProjects registers dependency trees belonging to projects whose sources
// have not been touched for idleDays.
//
// Judging the project's own activity, rather than the timestamp on the
// dependency directory, is what distinguishes a dormant site from one that was
// merely installed a while ago and is still in daily use.
func IdleProjects(r *unit.Registry, root string, idleDays int) {
	if idleDays <= 0 || root == "" {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Duration(idleDays) * 24 * time.Hour)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		proj := filepath.Join(root, e.Name())
		newest := newestSource(proj)
		if newest.IsZero() || newest.After(cutoff) {
			continue
		}
		idle := int(time.Since(newest).Hours() / 24)

		for _, d := range depDirs {
			dep := filepath.Join(proj, d.dir)
			if !isDir(dep) {
				continue
			}
			// The manifest is what proves this is a project. A bare
			// node_modules with nothing beside it may be something else.
			if _, err := os.Stat(filepath.Join(proj, d.manifest)); err != nil {
				continue
			}
			r.Add(&unit.Unit{
				ID:         "idle-" + d.dir + "-" + strings.ReplaceAll(e.Name(), " ", "-"),
				Tier:       unit.TierLossy,
				Reversible: false,
				Label:      e.Name() + "/" + d.dir + " (" + itoa(idle) + "d idle)",
				Kind:       unit.KindPaths,
				Paths:      []string{dep},
				Flag:       "--sites-idle",
			})
		}
	}
}

// newestSource returns the newest modification time among a project's own
// sources, ignoring VCS metadata and the dependency trees themselves.
func newestSource(proj string) time.Time {
	var newest time.Time
	filepath.WalkDir(proj, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	return newest
}

func isDir(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
