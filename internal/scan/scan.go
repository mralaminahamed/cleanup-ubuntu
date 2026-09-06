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

// depDirs are dependency and build trees, each paired with the manifests that
// prove the directory really is a project of that kind.
//
// The pairing carries far more weight here than it did for node_modules.
// "build", "target", "obj" and "bin" are ordinary English words, and a
// directory of that name with nothing beside it to explain what produced it is
// somebody's source. No entry may be added without a manifest.
//
// A manifest may be a filepath.Match pattern: there is no fixed name for a
// terraform config or a .NET project file.
//
// "dist" is deliberately absent. Unlike ".next" or "_build" it carries no
// framework's meaning, and plenty of published packages commit one.
var depDirs = []struct {
	dir       string
	manifests []string
}{
	{"node_modules", []string{"package.json"}},
	{"vendor", []string{"composer.json"}},
	{"target", []string{"Cargo.toml", "pom.xml"}},
	{"build", []string{"build.gradle", "build.gradle.kts", "pubspec.yaml"}},
	{".venv", []string{"pyproject.toml", "requirements.txt", "setup.py"}},
	{"venv", []string{"pyproject.toml", "requirements.txt", "setup.py"}},
	{".next", []string{"package.json"}},
	{".nuxt", []string{"package.json"}},
	{".turbo", []string{"package.json"}},
	{"_build", []string{"mix.exs"}},
	{"deps", []string{"mix.exs"}},
	{".terraform", []string{"*.tf"}},
	{"Pods", []string{"Podfile"}},
	{"obj", []string{"*.csproj", "*.fsproj", "*.sln"}},
	{"bin", []string{"*.csproj", "*.fsproj", "*.sln"}},
	{"zig-cache", []string{"build.zig"}},
	{".zig-cache", []string{"build.zig"}},
	{"zig-out", []string{"build.zig"}},
	{".dart_tool", []string{"pubspec.yaml"}},
}

// artifactDirs is every directory name in depDirs, for the idleness walk to
// skip. Built from the one table so the two cannot drift: a build output
// counted as a source makes a dormant project look busy, and it is then never
// offered.
var artifactDirs = func() map[string]bool {
	m := map[string]bool{".git": true}
	for _, d := range depDirs {
		m[d.dir] = true
	}
	return m
}()

// hasManifest reports whether any of the manifests is present in proj. A name
// containing a glob metacharacter is matched against the directory listing.
func hasManifest(proj string, manifests []string) bool {
	for _, m := range manifests {
		if !strings.ContainsAny(m, "*?[") {
			if _, err := os.Stat(filepath.Join(proj, m)); err == nil {
				return true
			}
			continue
		}
		entries, err := os.ReadDir(proj)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if ok, _ := filepath.Match(m, e.Name()); ok {
				return true
			}
		}
	}
	return false
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
			if !hasManifest(proj, d.manifests) {
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
			if artifactDirs[d.Name()] {
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
