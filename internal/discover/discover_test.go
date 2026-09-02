package discover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mralaminahamed/cleanup-ubuntu/internal/unit"
)

func mk(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestXDGCachesClaimsEveryChild(t *testing.T) {
	// Anything directly under ~/.cache is regenerable by the XDG basedir spec,
	// so it needs no name whitelist. This is what the bash version missed: it
	// scanned at depth two only, leaving every top-level entry invisible.
	home := t.TempDir()
	cache := mk(t, home, ".cache")
	mk(t, cache, "node")
	mk(t, cache, "deno")

	r := unit.NewRegistry()
	XDGCaches(r, cache)

	if len(r.All()) != 2 {
		t.Fatalf("discovered %d units, want 2", len(r.All()))
	}
	for _, u := range r.All() {
		if u.Tier != unit.TierArtifact || !u.Reversible {
			t.Errorf("unit %q: tier %v reversible %v, want TierArtifact/true", u.ID, u.Tier, u.Reversible)
		}
	}
}

func TestXDGCachesSkipsAlreadyClaimedPaths(t *testing.T) {
	home := t.TempDir()
	cache := mk(t, home, ".cache")
	npm := mk(t, cache, "npm")
	mk(t, cache, "other")

	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "npm-cacache", Kind: unit.KindPaths, Paths: []string{npm}})
	XDGCaches(r, cache)

	if len(r.All()) != 2 {
		t.Fatalf("got %d units, want the hardcoded one plus one discovery", len(r.All()))
	}
	count := 0
	for _, u := range r.All() {
		for _, p := range u.Paths {
			if p == npm {
				count++
			}
		}
	}
	if count != 1 {
		t.Errorf("path claimed %d times, want 1", count)
	}
}

func TestXDGCachesIgnoresFiles(t *testing.T) {
	home := t.TempDir()
	cache := mk(t, home, ".cache")
	if err := os.WriteFile(filepath.Join(cache, "stray.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := unit.NewRegistry()
	XDGCaches(r, cache)
	if len(r.All()) != 0 {
		t.Errorf("discovered %d units from a file, want 0", len(r.All()))
	}
}

func TestNestedCachesMatchesCacheNamesOnly(t *testing.T) {
	home := t.TempDir()
	cfg := mk(t, home, ".config")
	mk(t, cfg, "FakeApp", "Cache")
	mk(t, cfg, "FakeApp", "Local Storage")
	mk(t, cfg, "FakeApp", "Extensions")

	r := unit.NewRegistry()
	NestedCaches(r, []string{cfg})

	var got []string
	for _, u := range r.All() {
		got = append(got, filepath.Base(u.Paths[0]))
	}
	if len(got) != 1 || got[0] != "Cache" {
		t.Fatalf("discovered %v, want only [Cache]", got)
	}
}

func TestNestedCachesNeverTouchesProtectedNames(t *testing.T) {
	// These hold real application state. Deleting them logs the user out or
	// destroys local data, so they must never match however they are cased.
	for _, name := range []string{
		"Local Storage", "local storage", "IndexedDB", "indexeddb",
		"Cookies", "cookies", "Login Data", "History", "Bookmarks",
	} {
		if IsCacheName(name) {
			t.Errorf("IsCacheName(%q) = true, want false", name)
		}
	}
}

func TestIsCacheNameIsCaseInsensitive(t *testing.T) {
	// The bash whitelist was Chromium-cased, so a lowercase "cache" never
	// matched even though most Linux apps use it.
	for _, name := range []string{"Cache", "cache", "CACHE", "GPUCache", "gpucache", "Code Cache"} {
		if !IsCacheName(name) {
			t.Errorf("IsCacheName(%q) = false, want true", name)
		}
	}
}

func TestHeavyweightsReportsLargeDirsOnly(t *testing.T) {
	root := t.TempDir()
	big := mk(t, root, "big")
	mk(t, root, "small")
	if err := os.WriteFile(filepath.Join(big, "blob"), make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}

	got := Heavyweights([]string{root}, 1000)
	if len(got) != 1 {
		t.Fatalf("got %d heavyweights, want 1: %+v", len(got), got)
	}
	if filepath.Base(got[0].Path) != "big" {
		t.Errorf("reported %q, want the big directory", got[0].Path)
	}
	if got[0].Bytes < 5000 {
		t.Errorf("Bytes = %d, want at least 5000", got[0].Bytes)
	}
}

func TestHeavyweightsRegistersNothing(t *testing.T) {
	// Heavyweights are advisory. They name directories the user might want to
	// look at; the tool must never queue them for deletion.
	root := t.TempDir()
	big := mk(t, root, "big")
	if err := os.WriteFile(filepath.Join(big, "blob"), make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	r := unit.NewRegistry()
	Heavyweights([]string{root}, 1000)
	if len(r.All()) != 0 {
		t.Error("Heavyweights registered a deletable unit")
	}
}
