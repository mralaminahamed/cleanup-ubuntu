//go:build linux

package discover

import "path/filepath"

// CacheRoot is the directory whose children are regenerable by location. On
// Linux that is the XDG cache home.
func CacheRoot(home string) string { return filepath.Join(home, ".cache") }

// NestedRoots are directories holding per-application state, swept one level
// deeper and matched by name rather than by location -- a cache in here sits
// beside data that is not one.
func NestedRoots(home string) []string {
	return []string{
		filepath.Join(home, ".config"),
		filepath.Join(home, ".local", "share"),
	}
}
