package scan

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// shaped builds a project of one kind: a dependency directory, a manifest that
// proves what it is, and an age.
func shaped(t *testing.T, root, name, dep, manifest string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, dep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, dep, "artifact"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	m := filepath.Join(dir, manifest)
	if err := os.WriteFile(m, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(m, when, when); err != nil {
		t.Fatal(err)
	}
	return dir
}

func registered(r *unit.Registry) map[string]bool {
	out := map[string]bool{}
	for _, u := range r.All() {
		out[u.ID] = true
	}
	return out
}

func TestScanRecognisesTheCommonProjectShapes(t *testing.T) {
	cases := []struct{ name, dep, manifest, id string }{
		{"rust", "target", "Cargo.toml", "idle-target-rust"},
		{"maven", "target", "pom.xml", "idle-target-maven"},
		{"gradle", "build", "build.gradle", "idle-build-gradle"},
		{"gradle-kts", "build", "build.gradle.kts", "idle-build-gradle-kts"},
		{"python", ".venv", "pyproject.toml", "idle-.venv-python"},
		{"pip", "venv", "requirements.txt", "idle-venv-pip"},
		{"next", ".next", "package.json", "idle-.next-next"},
		{"elixir", "_build", "mix.exs", "idle-_build-elixir"},
		{"cocoa", "Pods", "Podfile", "idle-Pods-cocoa"},
		{"zig", "zig-out", "build.zig", "idle-zig-out-zig"},
		{"dart", ".dart_tool", "pubspec.yaml", "idle-.dart_tool-dart"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			shaped(t, root, c.name, c.dep, c.manifest, 90*24*time.Hour)

			r := unit.NewRegistry()
			IdleProjects(r, root, 30)

			if !registered(r)[c.id] {
				t.Fatalf("%s/%s not registered; got %v", c.dep, c.manifest, registered(r))
			}
		})
	}
}

// Some manifests are only identifiable by extension: there is no fixed name for
// a terraform config or a .NET project file.
func TestScanMatchesManifestsByPattern(t *testing.T) {
	cases := []struct{ name, dep, manifest, id string }{
		{"terraform", ".terraform", "main.tf", "idle-.terraform-terraform"},
		{"dotnet", "obj", "App.csproj", "idle-obj-dotnet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			shaped(t, root, c.name, c.dep, c.manifest, 90*24*time.Hour)

			r := unit.NewRegistry()
			IdleProjects(r, root, 30)

			if !registered(r)[c.id] {
				t.Fatalf("not registered; got %v", registered(r))
			}
		})
	}
}

// The manifest pairing is load-bearing here in a way it was not for
// node_modules. "build", "obj" and "target" are ordinary English words, and a
// directory called build with nothing to prove what built it is somebody's
// source.
func TestScanIgnoresAmbiguousDirectoriesWithoutAManifest(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "notes")
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-90 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "README.md"), when, when); err != nil {
		t.Fatal(err)
	}

	r := unit.NewRegistry()
	IdleProjects(r, root, 30)

	if len(r.All()) != 0 {
		t.Fatalf("claimed a directory with nothing to prove what it is: %v", registered(r))
	}
}

// Idleness is judged from the project's own sources. A dependency or build
// directory that a tool touched must not count as activity, or a dormant
// project looks busy and is never offered.
func TestScanJudgesIdlenessFromSourcesNotArtifacts(t *testing.T) {
	root := t.TempDir()
	dir := shaped(t, root, "rust", "target", "Cargo.toml", 90*24*time.Hour)
	// A build output written moments ago.
	if err := os.WriteFile(filepath.Join(dir, "target", "fresh"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := unit.NewRegistry()
	IdleProjects(r, root, 30)

	if !registered(r)["idle-target-rust"] {
		t.Fatal("a fresh build artifact made a dormant project look active")
	}
}

// "dist" is left out on purpose. Unlike .next or _build it carries no
// framework's meaning, and plenty of published packages commit one.
func TestScanLeavesDistAlone(t *testing.T) {
	root := t.TempDir()
	shaped(t, root, "lib", "dist", "package.json", 90*24*time.Hour)

	r := unit.NewRegistry()
	IdleProjects(r, root, 30)

	if registered(r)["idle-dist-lib"] {
		t.Fatal("claimed dist, which may well be committed source")
	}
}
