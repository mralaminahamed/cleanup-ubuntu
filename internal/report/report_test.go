package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func units() []*unit.Unit {
	return []*unit.Unit{
		{ID: "npm", Label: "npm cache", Tier: unit.TierPkgCache, Reversible: true, Bytes: 4096},
		{ID: "gradle", Label: "gradle caches", Tier: unit.TierColdReload, Reversible: true,
			Bytes: 3 << 30, Flag: "--gradle"},
	}
}

func TestJSONIsValidAndCarriesTotals(t *testing.T) {
	var buf bytes.Buffer
	s := Summary{Selected: units(), DryRun: true, TotalBytes: 4096 + (3 << 30)}
	if err := JSON(&buf, s); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got["dry_run"] != true {
		t.Error("dry_run not reported as true")
	}
	if _, ok := got["reclaimable_bytes"]; !ok {
		t.Error("missing reclaimable_bytes")
	}
	if _, ok := got["units"]; !ok {
		t.Error("missing units")
	}
}

func TestJSONWithNoUnitsStillParses(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, Summary{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("empty run produced invalid JSON: %v", err)
	}
}

func TestTextListsUnitsWithHumanSizes(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, Summary{Selected: units(), DryRun: true, TotalBytes: 4096 + (3 << 30)})
	out := buf.String()

	if !strings.Contains(out, "gradle caches") {
		t.Error("unit label missing from text output")
	}
	if !strings.Contains(out, "GiB") {
		t.Error("sizes are not human readable")
	}
}

func TestTextSaysDryRunLoudly(t *testing.T) {
	// The single most important line: a user must never think something was
	// deleted when it was not, or the reverse.
	var buf bytes.Buffer
	Text(&buf, Summary{Selected: units(), DryRun: true})
	if !strings.Contains(strings.ToLower(buf.String()), "dry") {
		t.Error("dry run not announced in text output")
	}

	buf.Reset()
	Text(&buf, Summary{Selected: units(), DryRun: false, TotalBytes: 4096})
	if strings.Contains(strings.ToLower(buf.String()), "dry-run") {
		t.Error("an applied run claimed to be a dry run")
	}
}

func TestTextNamesLockedAppsAndTheirPIDs(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, Summary{Locked: []*unit.Unit{
		{ID: "jb", Label: "JetBrains caches", LockedBy: "jetbrains-ide", PID: 4242, Bytes: 1 << 20},
	}})
	out := buf.String()
	if !strings.Contains(out, "jetbrains-ide") || !strings.Contains(out, "4242") {
		t.Errorf("locked section must name the app and pid, got:\n%s", out)
	}
}

func TestFailedUnitsAreNotReportedAsReclaimed(t *testing.T) {
	// A unit that errored appearing under "Reclaimable" tells the user space
	// was freed when none was. It has its own section, with the reason.
	var buf bytes.Buffer
	Text(&buf, Summary{
		Failed: []Failure{{
			Unit:   &unit.Unit{ID: "system-apt", Label: "apt cache clean"},
			Reason: "needs root: could not acquire root via sudo",
		}},
	})
	out := buf.String()
	if !strings.Contains(out, "Failed") {
		t.Errorf("no failed section:\n%s", out)
	}
	if !strings.Contains(out, "sudo") {
		t.Errorf("failure reason not shown:\n%s", out)
	}
	if strings.Contains(out, "Reclaimable") {
		t.Errorf("failed unit rendered under Reclaimable:\n%s", out)
	}
}

func TestJSONCarriesFailures(t *testing.T) {
	var buf bytes.Buffer
	err := JSON(&buf, Summary{Failed: []Failure{{
		Unit: &unit.Unit{ID: "system-apt", Label: "apt cache clean"}, Reason: "needs root",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	f, ok := got["failed"].([]any)
	if !ok || len(f) != 1 {
		t.Fatalf("failed not reported in JSON: %v", got["failed"])
	}
}

func TestTextSuggestsTheFlagForWithheldUnits(t *testing.T) {
	// Telling the user what was skipped is useless without telling them how to
	// include it.
	var buf bytes.Buffer
	Text(&buf, Summary{Withheld: []*unit.Unit{
		{ID: "vm", Label: "Claude VM bundles", Flag: "--claude-vm", Bytes: 2 << 30},
	}})
	if !strings.Contains(buf.String(), "--claude-vm") {
		t.Error("withheld section does not name the flag that would include it")
	}
}
