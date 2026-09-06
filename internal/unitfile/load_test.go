package unitfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func always(string) bool { return true }

func TestAddRegistersUnitsFromAFile(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cache/ccache"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := write(t, home, "units.json", `{"units":[
		{"id":"ccache","tier":1,"reversible":true,"paths":[".cache/ccache"]}]}`)

	r := unit.NewRegistry()
	if err := Add(r, home, []string{f}, always); err != nil {
		t.Fatal(err)
	}

	if _, ok := r.Get("ccache"); !ok {
		t.Fatal("unit not registered")
	}
}

// Having no unit file is the normal case, not a problem to report.
func TestAddIgnoresAMissingFile(t *testing.T) {
	r := unit.NewRegistry()
	if err := Add(r, t.TempDir(), []string{"/nonexistent/units.json"}, always); err != nil {
		t.Fatalf("missing file reported as an error: %v", err)
	}
}

// A file that exists but does not parse is different: the user wrote something
// and it is not being honoured. Carrying on with a half-loaded config would
// mean cleaning with a unit set nobody authored.
func TestAddFailsOnABrokenFile(t *testing.T) {
	home := t.TempDir()
	f := write(t, home, "units.json", `{"units":[{"id":"x"`)

	err := Add(unit.NewRegistry(), home, []string{f}, always)
	if err == nil {
		t.Fatal("broken file accepted")
	}
	if !strings.Contains(err.Error(), f) {
		t.Errorf("error does not name the file: %v", err)
	}
}

// Same rule the catalog uses: a unit for a tool this machine does not have is
// not a unit, it is noise in every report.
func TestAddSkipsUnitsWhoseToolIsAbsent(t *testing.T) {
	home := t.TempDir()
	f := write(t, home, "units.json", `{"units":[
		{"id":"flatpak-unused","tier":1,"reversible":true,
		 "command":"flatpak uninstall --unused -y","requires":"flatpak"}]}`)

	r := unit.NewRegistry()
	if err := Add(r, home, []string{f}, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}

	if _, ok := r.Get("flatpak-unused"); ok {
		t.Fatal("registered a unit for a tool that is not installed")
	}
}

func TestAddSkipsPathUnitsWithNothingOnDisk(t *testing.T) {
	home := t.TempDir()
	f := write(t, home, "units.json", `{"units":[
		{"id":"ghost","tier":1,"reversible":true,"paths":[".cache/not-here"]}]}`)

	r := unit.NewRegistry()
	if err := Add(r, home, []string{f}, always); err != nil {
		t.Fatal(err)
	}

	if _, ok := r.Get("ghost"); ok {
		t.Fatal("registered a unit whose paths do not exist")
	}
}

// A shipped unit's tier and reversibility were decided deliberately. A file
// must not be able to quietly restate them -- redefining "trash" as tier 0 and
// irreversible would be a way to smuggle a deletion past the model.
func TestAddCannotRedefineAShippedUnit(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cache/evil"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := write(t, home, "units.json", `{"units":[
		{"id":"trash","tier":5,"reversible":false,"paths":[".cache/evil"]}]}`)

	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "trash", Tier: unit.TierNative, Reversible: true,
		Kind: unit.KindPaths, Paths: []string{filepath.Join(home, ".local/share/Trash")}})
	if err := Add(r, home, []string{f}, always); err != nil {
		t.Fatal(err)
	}

	u, _ := r.Get("trash")
	if u.Tier != unit.TierNative || !u.Reversible {
		t.Fatalf("shipped unit was redefined: tier %v reversible %v", u.Tier, u.Reversible)
	}
}

func TestDefaultFilesLooksInTheUsualPlaces(t *testing.T) {
	got := DefaultFiles("/home/u")
	if len(got) == 0 {
		t.Fatal("no default locations")
	}
	if got[0] != "/home/u/.config/reclaim/units.json" {
		t.Errorf("first location is %q", got[0])
	}
}
