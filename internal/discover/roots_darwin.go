//go:build darwin

package discover

import "path/filepath"

// CacheRoot is ~/Library/Caches, the macOS analogue of the XDG cache home and
// the single largest thing worth sweeping on the platform. The same
// "regenerable by location" argument applies: Apple documents it as the place
// for data an application can recreate.
func CacheRoot(home string) string { return filepath.Join(home, "Library", "Caches") }

// NestedRoots is ~/Library/Application Support.
//
// This holds real application state, not caches -- it is the analogue of a
// Chromium profile directory. It is swept only because the nested rule matches
// by *name* against a strict list, so "Slack/Cache" is claimed while the
// databases and preferences beside it are not. Pointing the by-location rule
// here instead would delete everything every application owns.
func NestedRoots(home string) []string {
	return []string{filepath.Join(home, "Library", "Application Support")}
}
