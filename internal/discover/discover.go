// Package discover finds caches the catalog has no hardcoded rule for.
//
// Two different problems, two different rules:
//
//   - Everything directly under ~/.cache is regenerable by the XDG basedir
//     spec, so it is claimed by location and needs no name matching.
//   - A cache nested inside ~/.config sits beside real application state, so
//     there it is claimed by name against a strict whitelist.
package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mralaminahamed/reclaim/internal/fsutil"
	"github.com/mralaminahamed/reclaim/internal/unit"
)

// cacheNames are directory names that hold only regenerable data.
var cacheNames = []string{
	"cache", "code cache", "gpucache", "shadercache", "cacheddata",
	"dawncache", "dawngraphitecache", "grshadercache", "crashpad",
	"cache_data", "component_crx_cache", "logs",
}

// protectedNames hold real application state: session tokens, local databases,
// bookmarks. Deleting one logs the user out or destroys data outright, so they
// are refused even when a name would otherwise look cache-like.
var protectedNames = []string{
	"local storage", "indexeddb", "session storage", "databases",
	"service worker", "local state", "preferences", "cookies",
	"login data", "web data", "history", "bookmarks",
}

// IsCacheName reports whether a directory name denotes a regenerable cache.
// Matching is case-insensitive in both directions: Chromium ships "GPUCache"
// while most Linux apps use lowercase, and a lowercase "cookies" must still be
// protected.
func IsCacheName(name string) bool {
	n := strings.ToLower(name)
	for _, p := range protectedNames {
		if n == p {
			return false
		}
	}
	for _, c := range cacheNames {
		if n == c {
			return true
		}
	}
	return false
}

// XDGCaches claims every directory directly inside the XDG cache root that no
// registered unit already owns.
func XDGCaches(r *unit.Registry, cacheRoot string) {
	for _, d := range subdirs(cacheRoot) {
		if r.Claimed(d) {
			continue
		}
		name := filepath.Base(d)
		r.Add(&unit.Unit{
			ID:         "xdg-" + strings.ReplaceAll(name, " ", "-"),
			Tier:       unit.TierArtifact,
			Reversible: true,
			Discovered: true,
			Label:      ".cache/" + name,
			Kind:       unit.KindPaths,
			Paths:      []string{d},
		})
	}
}

// NestedCaches claims name-matched cache directories one level inside each
// root, the shape used by Electron and Chromium profiles.
func NestedCaches(r *unit.Registry, roots []string) {
	for _, root := range roots {
		for _, app := range subdirs(root) {
			for _, d := range subdirs(app) {
				name := filepath.Base(d)
				if !IsCacheName(name) || r.Claimed(d) {
					continue
				}
				id := "disc-" + filepath.Base(app) + "-" + name
				r.Add(&unit.Unit{
					ID:         strings.ReplaceAll(id, " ", "-"),
					Tier:       unit.TierArtifact,
					Reversible: true,
					Discovered: true,
					Label:      filepath.Base(app) + "/" + name,
					Kind:       unit.KindPaths,
					Paths:      []string{d},
				})
			}
		}
	}
}

// Heavy is a large directory worth a human's attention.
type Heavy struct {
	Path  string
	Bytes int64
}

// Heavyweights reports directories larger than min. It is advisory only and
// deliberately registers nothing: these are places the tool has no rule for and
// must not guess about.
func Heavyweights(roots []string, min int64) []Heavy {
	var out []Heavy
	for _, root := range roots {
		for _, d := range subdirs(root) {
			n, err := fsutil.PathBytes(d)
			if err != nil || n < min {
				continue
			}
			out = append(out, Heavy{Path: d, Bytes: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

func subdirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, filepath.Join(root, e.Name()))
	}
	return out
}
