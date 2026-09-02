package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mralaminahamed/cleanup-ubuntu/internal/unit"
)

func TestBuildRegistersOnlyExistingPaths(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, ".npm/_cacache"))
	mkdir(t, filepath.Join(home, ".gradle/caches"))

	r := Build(Env{Home: home, Has: func(string) bool { return false }})

	if _, ok := r.Get("npm-cacache"); !ok {
		t.Error("npm-cacache not registered though the path exists")
	}
	if _, ok := r.Get("gradle-caches"); !ok {
		t.Error("gradle-caches not registered though the path exists")
	}
	if _, ok := r.Get("pip-cache"); ok {
		t.Error("pip-cache registered though its path does not exist")
	}
}

func TestNativeUnitsRequireTheToolOnPath(t *testing.T) {
	home := t.TempDir()
	r := Build(Env{Home: home, Has: func(bin string) bool { return bin == "npm" }})

	u, ok := r.Get("npm-native")
	if !ok {
		t.Fatal("npm-native missing though npm is installed")
	}
	if u.Kind != unit.KindCmd {
		t.Error("npm-native should be a command unit")
	}
	if _, ok := r.Get("pnpm-native"); ok {
		t.Error("pnpm-native registered though pnpm is not installed")
	}
}

func TestGradleAndMavenAreColdReloadAndOptIn(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, ".gradle/caches"))
	mkdir(t, filepath.Join(home, ".m2/repository"))
	r := Build(Env{Home: home, Has: func(string) bool { return false }})

	g, _ := r.Get("gradle-caches")
	if g.Tier != unit.TierColdReload {
		t.Errorf("gradle tier = %v, want TierColdReload", g.Tier)
	}
	if g.Flag != "--gradle" {
		t.Errorf("gradle flag = %q, want --gradle", g.Flag)
	}
	if !g.Reversible {
		t.Error("gradle caches should be reversible: the next build re-downloads")
	}
	m, _ := r.Get("maven-repo")
	if m.Flag != "--maven" {
		t.Errorf("maven flag = %q, want --maven", m.Flag)
	}
}

func TestLossyUnitsAreMarkedIrreversible(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, ".config/Claude/vm_bundles"))
	r := Build(Env{Home: home, Has: func(string) bool { return false }})

	u, ok := r.Get("claude-vm")
	if !ok {
		t.Fatal("claude-vm missing")
	}
	if u.Reversible {
		t.Error("claude-vm must be marked irreversible")
	}
}

func TestNoUnitTargetsAProtectedDirectory(t *testing.T) {
	// A catalog entry that names $HOME or a system directory would be caught by
	// the runner's backstop, but it should never get that far.
	home := t.TempDir()
	for _, d := range []string{".npm/_cacache", ".cache/pip", ".gradle/caches", ".m2/repository"} {
		mkdir(t, filepath.Join(home, d))
	}
	r := Build(Env{Home: home, Has: func(string) bool { return true }})

	bad := map[string]bool{"/": true, "/home": true, "/usr": true, "/etc": true, home: true}
	for _, u := range r.All() {
		for _, p := range u.Paths {
			if bad[filepath.Clean(p)] {
				t.Errorf("unit %q targets protected path %q", u.ID, p)
			}
			if !strings.HasPrefix(p, "/") {
				t.Errorf("unit %q has a relative path %q", u.ID, p)
			}
		}
	}
}

func TestEveryUnitHasIDLabelAndTier(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{".npm/_cacache", ".cache/pip", ".gradle/caches"} {
		mkdir(t, filepath.Join(home, d))
	}
	r := Build(Env{Home: home, Has: func(string) bool { return true }})
	if len(r.All()) == 0 {
		t.Fatal("catalog built nothing")
	}
	for _, u := range r.All() {
		if u.ID == "" || u.Label == "" {
			t.Errorf("unit %+v missing id or label", u)
		}
		if u.Kind == unit.KindPaths && len(u.Paths) == 0 {
			t.Errorf("path unit %q has no paths", u.ID)
		}
		if u.Kind == unit.KindCmd && u.Command == "" {
			t.Errorf("cmd unit %q has no command", u.ID)
		}
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}
