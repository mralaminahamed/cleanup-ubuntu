package discover

import (
	"path/filepath"
	"runtime"
	"testing"
)

// Discovery hardcoded the XDG tree. On macOS almost none of that exists, so the
// sweep would walk an empty directory and find nothing, on the platform where
// ~/Library/Caches is the single biggest thing worth sweeping.
func TestCacheRootIsThePlatformsOwn(t *testing.T) {
	got := CacheRoot("/home/u")

	want := "/home/u/.cache"
	if runtime.GOOS == "darwin" {
		want = "/home/u/Library/Caches"
	}
	if got != want {
		t.Fatalf("cache root %q, want %q", got, want)
	}
}

// The nested sweep matches by name inside an application's own directory,
// which is what makes it safe to point at a directory holding real state:
// ~/Library/Application Support/Slack/Cache is a cache, and its siblings are
// not touched.
func TestNestedRootsCoverThePlatformsApplicationState(t *testing.T) {
	got := NestedRoots("/home/u")

	if len(got) == 0 {
		t.Fatal("no roots")
	}
	want := filepath.Join("/home/u", ".config")
	if runtime.GOOS == "darwin" {
		want = filepath.Join("/home/u", "Library", "Application Support")
	}
	for _, r := range got {
		if r == want {
			return
		}
	}
	t.Fatalf("roots %v do not include %q", got, want)
}
