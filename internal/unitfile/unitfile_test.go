package unitfile

import (
	"strings"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func parse(t *testing.T, home, body string) []*unit.Unit {
	t.Helper()
	us, err := Parse([]byte(body), home, always)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return us
}

func parseErr(t *testing.T, home, body string) string {
	t.Helper()
	if _, err := Parse([]byte(body), home, always); err != nil {
		return err.Error()
	}
	t.Fatal("Parse accepted a definition it should have refused")
	return ""
}

func TestParseReadsAPathUnit(t *testing.T) {
	us := parse(t, "/home/u", `{"units":[
		{"id":"ccache","label":"ccache objects","tier":1,"reversible":true,
		 "paths":[".cache/ccache"]}]}`)

	if len(us) != 1 {
		t.Fatalf("got %d units, want 1", len(us))
	}
	u := us[0]
	if u.ID != "ccache" || u.Label != "ccache objects" {
		t.Errorf("id %q label %q", u.ID, u.Label)
	}
	if u.Tier != unit.TierPkgCache || !u.Reversible {
		t.Errorf("tier %v reversible %v", u.Tier, u.Reversible)
	}
	if u.Kind != unit.KindPaths {
		t.Errorf("kind %v, want KindPaths", u.Kind)
	}
}

// A relative path is relative to the home the run is for, so the same file
// works for any user and a test can point it somewhere harmless.
func TestParseResolvesRelativePathsUnderHome(t *testing.T) {
	us := parse(t, "/home/u", `{"units":[
		{"id":"ccache","tier":1,"reversible":true,"paths":[".cache/ccache"]}]}`)

	if got := us[0].Paths[0]; got != "/home/u/.cache/ccache" {
		t.Errorf("path %q", got)
	}
}

func TestParseReadsACommandUnit(t *testing.T) {
	us := parse(t, "/home/u", `{"units":[
		{"id":"flatpak-unused","tier":1,"reversible":true,
		 "command":"flatpak uninstall --unused -y","flag":"--flatpak",
		 "mount_hint":"/var/lib/flatpak","needs_root":true}]}`)

	u := us[0]
	if u.Kind != unit.KindCmd {
		t.Fatalf("kind %v, want KindCmd", u.Kind)
	}
	if u.Command != "flatpak uninstall --unused -y" {
		t.Errorf("command %q", u.Command)
	}
	if u.Flag != "--flatpak" || u.MountHint != "/var/lib/flatpak" || !u.NeedsRoot {
		t.Errorf("flag %q mount %q root %v", u.Flag, u.MountHint, u.NeedsRoot)
	}
}

// A file-defined unit is a deliberate declaration, not a guess, so it must not
// be swept up by the promotion pass that re-rates what discovery assumed.
func TestParseDoesNotMarkUnitsDiscovered(t *testing.T) {
	us := parse(t, "/home/u", `{"units":[
		{"id":"ccache","tier":1,"reversible":true,"paths":[".cache/ccache"]}]}`)

	if us[0].Discovered {
		t.Error("file unit marked discovered")
	}
}

func TestParseRejectsAUnitWithNoTarget(t *testing.T) {
	msg := parseErr(t, "/home/u", `{"units":[{"id":"x","tier":1,"reversible":true}]}`)
	if !strings.Contains(msg, "paths") || !strings.Contains(msg, "command") {
		t.Errorf("unhelpful error: %s", msg)
	}
}

func TestParseRejectsAUnitWithBothTargets(t *testing.T) {
	parseErr(t, "/home/u", `{"units":[{"id":"x","tier":1,"reversible":true,
		"paths":[".cache/x"],"command":"true"}]}`)
}

func TestParseRejectsAMissingID(t *testing.T) {
	parseErr(t, "/home/u", `{"units":[{"tier":1,"reversible":true,"paths":[".cache/x"]}]}`)
}

// Tier and reversibility are the two claims the whole safety model rests on.
// Defaulting either would let an omission quietly make a promise the author
// never made, so both must be written down.
func TestParseRequiresTierAndReversibleToBeStated(t *testing.T) {
	if msg := parseErr(t, "/home/u",
		`{"units":[{"id":"x","reversible":true,"paths":[".cache/x"]}]}`); !strings.Contains(msg, "tier") {
		t.Errorf("error does not name the missing field: %s", msg)
	}
	if msg := parseErr(t, "/home/u",
		`{"units":[{"id":"x","tier":1,"paths":[".cache/x"]}]}`); !strings.Contains(msg, "reversible") {
		t.Errorf("error does not name the missing field: %s", msg)
	}
}

func TestParseRejectsATierOutsideTheLadder(t *testing.T) {
	parseErr(t, "/home/u", `{"units":[{"id":"x","tier":9,"reversible":true,"paths":[".cache/x"]}]}`)
}

// A typo in a field name must not be read as "this field was omitted". Silently
// ignoring "reversable" would hand the author a unit that does not match what
// they wrote.
func TestParseRejectsAnUnknownField(t *testing.T) {
	msg := parseErr(t, "/home/u", `{"units":[{"id":"x","tier":1,"reversable":true,
		"paths":[".cache/x"]}]}`)
	if !strings.Contains(msg, "reversable") {
		t.Errorf("error does not name the bad field: %s", msg)
	}
}

// A unit file is a deletion instruction written by hand, so the load is the
// right place to refuse an obviously catastrophic one. The runner's backstop
// still stands behind this; being refused twice is the point.
func TestParseRefusesCatastrophicPaths(t *testing.T) {
	for _, p := range []string{"/", "/usr", "/etc", "/home"} {
		body := `{"units":[{"id":"x","tier":1,"reversible":true,"paths":["` + p + `"]}]}`
		if msg := parseErr(t, "/home/u", body); !strings.Contains(msg, p) {
			t.Errorf("error for %q does not name it: %s", p, msg)
		}
	}
}

// The home directory is not protected by depth alone -- "/home/u" is two levels
// down like any cache -- so it has to be refused against the home the run is
// actually for.
func TestParseRefusesTheHomeDirectoryItself(t *testing.T) {
	parseErr(t, "/home/u", `{"units":[{"id":"x","tier":1,"reversible":true,"paths":["."]}]}`)
	parseErr(t, "/home/u", `{"units":[{"id":"x","tier":1,"reversible":true,"paths":["/home/u"]}]}`)
}

// Relative does not mean contained. A path that climbs out of home lands
// somewhere nobody wrote down.
func TestParseRefusesAPathThatClimbsOutOfHome(t *testing.T) {
	parseErr(t, "/home/u", `{"units":[{"id":"x","tier":1,"reversible":true,"paths":["../.."]}]}`)
}
