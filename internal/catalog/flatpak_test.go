package catalog

import (
	"path/filepath"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func hasFlatpak(bin string) bool { return bin == "flatpak" }

// Orphaned runtimes are the largest thing a desktop accumulates that this tool
// did not cover: each is 1-2GiB and nothing removes them by default.
func TestFlatpakUnusedIsAnOptInCommand(t *testing.T) {
	r := Build(Env{Home: t.TempDir(), Has: hasFlatpak})

	u, ok := r.Get("flatpak-unused")
	if !ok {
		t.Fatal("flatpak-unused not registered")
	}
	if u.Kind != unit.KindCmd || u.Command == "" {
		t.Errorf("kind %v command %q", u.Kind, u.Command)
	}
	if u.Flag != "--flatpak" {
		t.Errorf("flag %q, want --flatpak", u.Flag)
	}
	if !u.Reversible {
		t.Error("reinstalling an app pulls its runtime back, so this is reversible")
	}
	if u.MountHint == "" {
		t.Error("a command unit owns no path, so it needs a mount hint")
	}
}

func TestFlatpakUnitsAreAbsentWithoutFlatpak(t *testing.T) {
	r := Build(Env{Home: t.TempDir(), Has: func(string) bool { return false }})

	for _, id := range []string{"flatpak-unused", "flatpak-app-caches"} {
		if _, ok := r.Get(id); ok {
			t.Errorf("%s registered on a machine without flatpak", id)
		}
	}
}

// ~/.var/app/<id>/cache is the sandbox's ~/.cache and is regenerable by the
// same argument. Its siblings are not: data holds the application's real state
// and config holds its settings. Globbing the app directory would take both.
func TestFlatpakAppCachesClaimCacheAndNothingBeside(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, ".var/app/com.example.App")
	mkdir(t, filepath.Join(app, "cache"))
	mkdir(t, filepath.Join(app, "data"))
	mkdir(t, filepath.Join(app, "config"))

	r := Build(Env{Home: home, Has: hasFlatpak})

	u, ok := r.Get("flatpak-app-caches")
	if !ok {
		t.Fatal("flatpak-app-caches not registered")
	}
	if len(u.Paths) != 1 || u.Paths[0] != filepath.Join(app, "cache") {
		t.Fatalf("paths %v", u.Paths)
	}
}

func TestFlatpakAppCachesCoverEveryInstalledApp(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, ".var/app/com.example.One/cache"))
	mkdir(t, filepath.Join(home, ".var/app/com.example.Two/cache"))
	mkdir(t, filepath.Join(home, ".var/app/com.example.Three/data"))

	r := Build(Env{Home: home, Has: hasFlatpak})

	u, _ := r.Get("flatpak-app-caches")
	if len(u.Paths) != 2 {
		t.Fatalf("paths %v, want the two apps that have a cache", u.Paths)
	}
}

func TestFlatpakAppCachesAbsentWhenNoAppHasOne(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, ".var/app/com.example.One/data"))

	r := Build(Env{Home: home, Has: hasFlatpak})

	if _, ok := r.Get("flatpak-app-caches"); ok {
		t.Error("registered a unit with nothing to clean")
	}
}
