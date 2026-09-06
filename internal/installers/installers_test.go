package installers

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func file(t *testing.T, dir, name string, size int, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
	return p
}

func names(found []Installer) map[string]Installer {
	out := map[string]Installer{}
	for _, i := range found {
		out[filepath.Base(i.Path)] = i
	}
	return out
}

func TestFindReportsInstallerShapedFiles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"code.deb", "tool.rpm", "app.AppImage", "ubuntu.iso",
		"installer.run", "thing.snap"} {
		file(t, dir, n, 1024, 200*24*time.Hour)
	}
	file(t, dir, "notes.txt", 1024, 200*24*time.Hour)
	file(t, dir, "photo.jpg", 1024, 200*24*time.Hour)

	got := names(Find(Env{}, dir, 90*24*time.Hour))

	if len(got) != 6 {
		t.Fatalf("found %d, want 6: %v", len(got), got)
	}
	for _, n := range []string{"notes.txt", "photo.jpg"} {
		if _, ok := got[n]; ok {
			t.Errorf("%q is not an installer", n)
		}
	}
}

// An archive is a container, not an intent. On a real ~/Downloads the largest
// .zip files were a book collection and a Figma UI kit -- data somebody
// downloaded and kept. Listing those under "stale installers" would be a claim
// the file format cannot support.
func TestFindIgnoresArchives(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"books.zip", "src.tar.gz", "data.tar.xz", "stuff.tgz"} {
		file(t, dir, n, 1<<20, 300*24*time.Hour)
	}

	if got := Find(Env{}, dir, 90*24*time.Hour); len(got) != 0 {
		t.Fatalf("archives reported as installers: %v", got)
	}
}

// An installer downloaded this morning is in use. One from eight months ago is
// not, and that is the only thing separating them.
func TestFindIgnoresRecentDownloads(t *testing.T) {
	dir := t.TempDir()
	file(t, dir, "old.deb", 1024, 200*24*time.Hour)
	file(t, dir, "today.deb", 1024, 2*time.Hour)

	got := names(Find(Env{}, dir, 90*24*time.Hour))

	if _, ok := got["today.deb"]; ok {
		t.Error("a fresh download was reported as stale")
	}
	if _, ok := got["old.deb"]; !ok {
		t.Error("an eight-month-old installer was not reported")
	}
}

// The one thing that can be proven rather than guessed: this .deb is already
// installed at this version, so the file is a second copy of something the
// system holds. That is the difference between a suggestion and a fact.
func TestFindMarksADebAlreadyInstalledAtTheSameVersion(t *testing.T) {
	dir := t.TempDir()
	file(t, dir, "code.deb", 1024, 200*24*time.Hour)

	got := names(Find(Env{
		DebInfo:   func(string) (string, string, error) { return "code", "1.2.3", nil },
		Installed: func(pkg string) string { return "1.2.3" },
	}, dir, 90*24*time.Hour))

	i := got["code.deb"]
	if !i.Redundant {
		t.Fatal("an installed package's own .deb was not marked redundant")
	}
	if i.Reason == "" {
		t.Error("no reason given for a claim this strong")
	}
}

func TestFindDoesNotMarkADifferentVersionRedundant(t *testing.T) {
	dir := t.TempDir()
	file(t, dir, "code.deb", 1024, 200*24*time.Hour)

	got := names(Find(Env{
		DebInfo:   func(string) (string, string, error) { return "code", "1.3.0", nil },
		Installed: func(pkg string) string { return "1.2.3" },
	}, dir, 90*24*time.Hour))

	if got["code.deb"].Redundant {
		t.Fatal("a newer .deb than the installed version was called redundant")
	}
}

// Without dpkg there is no proof, and no proof means no claim.
func TestFindClaimsNothingWithoutDpkg(t *testing.T) {
	dir := t.TempDir()
	file(t, dir, "code.deb", 1024, 200*24*time.Hour)

	got := names(Find(Env{}, dir, 90*24*time.Hour))

	if got["code.deb"].Redundant {
		t.Fatal("claimed redundancy with nothing to check it against")
	}
}

func TestFindIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "archive.iso"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := Find(Env{}, dir, time.Hour); len(got) != 0 {
		t.Fatalf("reported a directory: %v", got)
	}
}

func TestFindOnAMissingDirectoryIsEmpty(t *testing.T) {
	if got := Find(Env{}, "/nonexistent/Downloads", time.Hour); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

// Largest first: the whole point is to show the 4GB ISO before the 2MB .deb.
func TestFindOrdersBySizeDescending(t *testing.T) {
	dir := t.TempDir()
	file(t, dir, "small.deb", 1024, 200*24*time.Hour)
	file(t, dir, "big.iso", 8192, 200*24*time.Hour)

	got := Find(Env{}, dir, 90*24*time.Hour)

	if len(got) != 2 || filepath.Base(got[0].Path) != "big.iso" {
		t.Fatalf("order %v", got)
	}
}
