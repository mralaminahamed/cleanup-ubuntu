package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "reclaim-build")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	bin = filepath.Join(dir, "reclaim")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		panic(string(out))
	}
	os.Exit(m.Run())
}

// fixtureHome builds a home with a couple of known caches in it.
func fixtureHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, d := range []string{".npm/_cacache", ".cache/pip", ".cache/randomtool"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, d, "blob"), make([]byte, 4096), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func run(t *testing.T, home string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "HOME="+home, "RECLAIM_NO_OPLOG=1")
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	return string(out), code
}

func TestHelpListsSubcommands(t *testing.T) {
	out, code := run(t, t.TempDir(), "--help")
	if code != 0 {
		t.Fatalf("--help exited %d:\n%s", code, out)
	}
	for _, want := range []string{"clean", "status", "analyze", "history", "version"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help does not mention %q", want)
		}
	}
}

func TestCleanIsDryRunByDefault(t *testing.T) {
	// The whole safety posture rests on this: running with no flags must never
	// delete anything.
	home := fixtureHome(t)
	out, code := run(t, home, "clean")
	if code != 0 {
		t.Fatalf("clean exited %d:\n%s", code, out)
	}
	if !strings.Contains(strings.ToUpper(out), "DRY-RUN") {
		t.Errorf("clean did not announce a dry run:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".npm/_cacache/blob")); err != nil {
		t.Fatalf("default clean deleted files: %v", err)
	}
}

func TestCleanApplyDeletes(t *testing.T) {
	home := fixtureHome(t)
	out, code := run(t, home, "clean", "--apply", "--yes")
	if code != 0 {
		t.Fatalf("clean --apply exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".npm/_cacache")); !os.IsNotExist(err) {
		t.Errorf("cache survived --apply: %v", err)
	}
}

func TestJSONOutputParses(t *testing.T) {
	out, code := run(t, fixtureHome(t), "clean", "--json")
	if code != 0 {
		t.Fatalf("exited %d:\n%s", code, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got["dry_run"] != true {
		t.Error("json dry_run should be true without --apply")
	}
}

func TestUnknownFlagExitsNonZero(t *testing.T) {
	out, code := run(t, t.TempDir(), "clean", "--nonsense")
	if code == 0 {
		t.Errorf("unknown flag exited 0:\n%s", out)
	}
}

func TestBadFreeSizeExitsNonZero(t *testing.T) {
	out, code := run(t, t.TempDir(), "clean", "--free", "banana")
	if code == 0 {
		t.Errorf("bad --free exited 0:\n%s", out)
	}
}

func TestFreeAcceptsLeadingZero(t *testing.T) {
	// The octal bug, guarded end to end.
	_, code := run(t, fixtureHome(t), "clean", "--free", "08G")
	if code != 0 {
		t.Errorf("--free 08G exited %d, want 0", code)
	}
}

func TestStatusReportsMounts(t *testing.T) {
	out, code := run(t, t.TempDir(), "status")
	if code != 0 {
		t.Fatalf("status exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "/") {
		t.Errorf("status printed no mounts:\n%s", out)
	}
}

func TestVersionPrints(t *testing.T) {
	out, code := run(t, t.TempDir(), "version")
	if code != 0 || strings.TrimSpace(out) == "" {
		t.Errorf("version exited %d with %q", code, out)
	}
}

func TestExcludeKeepsAUnitOutOfThePlan(t *testing.T) {
	home := fixtureHome(t)
	out, _ := run(t, home, "clean", "--exclude", "npm-*", "--json")
	if strings.Contains(out, "npm-cacache") {
		t.Errorf("excluded unit still planned:\n%s", out)
	}
}

func TestOnlyRestrictsThePlan(t *testing.T) {
	home := fixtureHome(t)
	out, _ := run(t, home, "clean", "--only", "pip-cache", "--json")
	var got struct {
		Units []struct {
			ID string `json:"id"`
		} `json:"units"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	for _, u := range got.Units {
		if u.ID != "pip-cache" {
			t.Errorf("--only still planned %q", u.ID)
		}
	}
}

// planned reports the unit ids a clean run would run.
func planned(t *testing.T, out string) []string {
	t.Helper()
	var got struct {
		Units []struct {
			ID string `json:"id"`
		} `json:"units"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	ids := make([]string, 0, len(got.Units))
	for _, u := range got.Units {
		ids = append(ids, u.ID)
	}
	return ids
}

// hugeCache creates a directory under ~/.cache whose apparent size is 2GiB.
// The file is sparse, so it costs no disk: the probe sums stat sizes, which is
// exactly what the promotion pass reads.
func hugeCache(t *testing.T, home, name string) {
	t.Helper()
	dir := filepath.Join(home, ".cache", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "blob"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(2 << 30); err != nil {
		t.Fatal(err)
	}
}

// A discovered cache is claimed for its location, which says nothing about what
// refilling it costs. A very large one must not be reachable by a default run:
// deleting a model cache is reversible, but only in the sense that hours of
// download will put it back.
func TestDiscoverDoesNotPlanAHugeCacheByDefault(t *testing.T) {
	home := fixtureHome(t)
	hugeCache(t, home, "hugecache")

	out, _ := run(t, home, "clean", "--discover", "--json")

	for _, id := range planned(t, out) {
		if id == "xdg-hugecache" {
			t.Fatalf("a 2GiB discovered cache was planned by a default run:\n%s", out)
		}
	}
}

// Promotion raises the price, it does not forbid the purchase. Naming the
// group must still reach it.
func TestDiscoverPlansAHugeCacheWhenHeavyIsGiven(t *testing.T) {
	home := fixtureHome(t)
	hugeCache(t, home, "hugecache")

	out, _ := run(t, home, "clean", "--discover", "--heavy", "--json")

	for _, id := range planned(t, out) {
		if id == "xdg-hugecache" {
			return
		}
	}
	t.Fatalf("--heavy did not reach the promoted unit:\n%s", out)
}

// Opt-in means opt-in. Raising the tier ceiling authorises a tier, never a
// group, so it must not be a back door into the promoted unit.
func TestRaisingTheTierAloneDoesNotReachAHugeCache(t *testing.T) {
	home := fixtureHome(t)
	hugeCache(t, home, "hugecache")

	out, _ := run(t, home, "clean", "--discover", "--tier", "5", "--json")

	for _, id := range planned(t, out) {
		if id == "xdg-hugecache" {
			t.Fatalf("--tier reached a unit that needs --heavy:\n%s", out)
		}
	}
}

// The ordinary case must not regress: discovery still buys what it always did.
func TestDiscoverStillPlansSmallCachesByDefault(t *testing.T) {
	home := fixtureHome(t)

	out, _ := run(t, home, "clean", "--discover", "--json")

	for _, id := range planned(t, out) {
		if id == "xdg-randomtool" {
			return
		}
	}
	t.Fatalf("a small discovered cache was not planned:\n%s", out)
}

// Withholding the unit is only half the fix. Before this, a huge discovered
// cache was deleted without being asked about; a version that instead says
// nothing at all would hide the same space rather than reclaim it.
func TestDiscoverOffersTheHugeCacheItHeldBack(t *testing.T) {
	home := fixtureHome(t)
	hugeCache(t, home, "hugecache")

	out, _ := run(t, home, "clean", "--discover")

	for _, want := range []string{".cache/hugecache", "2.0GiB", "--heavy"} {
		if !strings.Contains(out, want) {
			t.Errorf("report does not mention %q:\n%s", want, out)
		}
	}
}

func writeUnitFile(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "reclaim")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "units.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUnitFileAddsAUnit(t *testing.T) {
	home := fixtureHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".cache/ccache"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeUnitFile(t, home, `{"units":[
		{"id":"ccache","label":"ccache objects","tier":1,"reversible":true,
		 "paths":[".cache/ccache"]}]}`)

	out, _ := run(t, home, "clean", "--json")

	for _, id := range planned(t, out) {
		if id == "ccache" {
			return
		}
	}
	t.Fatalf("file-defined unit not planned:\n%s", out)
}

// A file cannot register a CLI flag, so its opt-in units are named through
// --with. Without that there would be no way to gate one.
func TestUnitFileUnitIsOptInThroughWith(t *testing.T) {
	home := fixtureHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".cache/models"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeUnitFile(t, home, `{"units":[
		{"id":"models","tier":3,"reversible":true,"flag":"--models",
		 "paths":[".cache/models"]}]}`)

	out, _ := run(t, home, "clean", "--json")
	for _, id := range planned(t, out) {
		if id == "models" {
			t.Fatalf("flagged file unit ran without its flag:\n%s", out)
		}
	}

	out, _ = run(t, home, "clean", "--with", "models", "--json")
	for _, id := range planned(t, out) {
		if id == "models" {
			return
		}
	}
	t.Fatalf("--with did not reach the file unit:\n%s", out)
}

// Reversibility is absolute, and writing a unit down does not lower the gate.
func TestUnitFileLossyUnitStillNeedsAllowLossy(t *testing.T) {
	home := fixtureHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".cache/notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeUnitFile(t, home, `{"units":[
		{"id":"notes","tier":1,"reversible":false,"paths":[".cache/notes"]}]}`)

	out, _ := run(t, home, "clean", "--json")
	for _, id := range planned(t, out) {
		if id == "notes" {
			t.Fatalf("irreversible file unit planned without --allow-lossy:\n%s", out)
		}
	}
}

// A file that exists and does not parse means the user wrote something that is
// not being honoured. Running anyway would clean with a unit set nobody wrote.
func TestBrokenUnitFileStopsTheRun(t *testing.T) {
	home := fixtureHome(t)
	writeUnitFile(t, home, `{"units":[{"id":"x"`)

	out, code := run(t, home, "clean")

	if code == 0 {
		t.Fatalf("broken unit file did not stop the run:\n%s", out)
	}
	if !strings.Contains(out, "units.json") {
		t.Errorf("error does not name the file:\n%s", out)
	}
}

// The shipped definition wins. Otherwise a file could restate "trash" as
// irreversible tier 0 and smuggle a deletion past the model.
func TestUnitFileCannotRedefineAShippedUnit(t *testing.T) {
	home := fixtureHome(t)
	writeUnitFile(t, home, `{"units":[
		{"id":"pip-cache","label":"hijacked","tier":5,"reversible":false,
		 "paths":[".cache/pip"]}]}`)

	out, _ := run(t, home, "clean")

	if strings.Contains(out, "hijacked") {
		t.Errorf("a file redefined a shipped unit:\n%s", out)
	}
}

// Every opt-in group in the catalog needs a flag of its own. Reaching it only
// through --with would make it a second-class group for no reason the user can
// see.
func TestFlatpakFlagIsDefined(t *testing.T) {
	out, code := run(t, fixtureHome(t), "clean", "--flatpak")

	if code != 0 {
		t.Fatalf("--flatpak exited %d:\n%s", code, out)
	}
	if strings.Contains(out, "not defined") {
		t.Errorf("--flatpak is not a defined flag:\n%s", out)
	}
}

// Kernels are not part of --system. That flag is documented as the apt cache,
// a bounded journal vacuum and old snap revisions, and quietly growing it to
// include package removal would change what an existing command does.
func TestKernelsFlagIsSeparateFromSystem(t *testing.T) {
	out, code := run(t, fixtureHome(t), "clean", "--kernels")

	if code != 0 {
		t.Fatalf("--kernels exited %d:\n%s", code, out)
	}
	if strings.Contains(out, "not defined") {
		t.Errorf("--kernels is not a defined flag:\n%s", out)
	}
}

func TestModelsFlagIsDefined(t *testing.T) {
	out, code := run(t, fixtureHome(t), "clean", "--models")

	if code != 0 {
		t.Fatalf("--models exited %d:\n%s", code, out)
	}
	if strings.Contains(out, "not defined") {
		t.Errorf("--models is not a defined flag:\n%s", out)
	}
}

// The bug that started this: a discovered cache carries an assumed tier. Now
// that the catalog names the model caches, discovery must leave them alone --
// with their considered tier and flag, not the scanner's guess.
func TestDiscoverDoesNotSecondGuessTheModelCache(t *testing.T) {
	home := fixtureHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".cache/huggingface"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, _ := run(t, home, "clean", "--discover", "--json")

	for _, id := range planned(t, out) {
		if id == "xdg-huggingface" || id == "hf-cache" {
			t.Fatalf("model cache planned by a default run as %q:\n%s", id, out)
		}
	}
}

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "reclaim")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigExclusionAppliesWithoutTheFlag(t *testing.T) {
	home := fixtureHome(t)
	writeConfig(t, home, `{"exclude":["pip-*"]}`)

	out, _ := run(t, home, "clean", "--json")

	for _, id := range planned(t, out) {
		if id == "pip-cache" {
			t.Fatalf("config exclusion ignored:\n%s", out)
		}
	}
}

// A flag is typed at the moment of use. Whatever the file says, what is on the
// command line is what the person meant this time.
func TestFlagBeatsConfig(t *testing.T) {
	home := fixtureHome(t)
	writeConfig(t, home, `{"only":["npm-cacache"]}`)

	out, _ := run(t, home, "clean", "--only", "pip-cache", "--json")

	ids := planned(t, out)
	if len(ids) == 0 {
		t.Fatalf("nothing planned:\n%s", out)
	}
	for _, id := range ids {
		if id != "pip-cache" {
			t.Fatalf("config won over the flag: planned %q\n%s", id, out)
		}
	}
}

// The rule the file rests on, end to end: a config cannot authorise deletion.
func TestConfigCannotTurnOnApply(t *testing.T) {
	home := fixtureHome(t)
	writeConfig(t, home, `{"apply":true}`)

	out, code := run(t, home, "clean")

	if code == 0 {
		t.Fatalf("a config that authorises deletion was accepted:\n%s", out)
	}
	if !strings.Contains(out, "command line") {
		t.Errorf("the error does not say where --apply belongs:\n%s", out)
	}
}

func TestBrokenConfigStopsTheRun(t *testing.T) {
	home := fixtureHome(t)
	writeConfig(t, home, `{"workers":`)

	out, code := run(t, home, "clean")

	if code == 0 {
		t.Fatalf("broken config did not stop the run:\n%s", out)
	}
	if !strings.Contains(out, "config.json") {
		t.Errorf("the error does not name the file:\n%s", out)
	}
}

func TestConfigTierLowersTheCeiling(t *testing.T) {
	home := fixtureHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".cache/ms-playwright"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, home, `{"tier":0}`)

	out, _ := run(t, home, "clean", "--json")

	for _, id := range planned(t, out) {
		if id == "pip-cache" {
			t.Fatalf("a tier 1 unit ran under a tier 0 ceiling:\n%s", out)
		}
	}
}

// A timer that fires hourly should almost always do nothing. --below is the
// guard that makes that cheap: if the disk is not under pressure the run stops
// before it builds a catalog, let alone probes one.
func TestBelowStopsTheRunWhenThereIsPlentyFree(t *testing.T) {
	home := fixtureHome(t)

	out, code := run(t, home, "clean", "--below", "1K")

	if code != 0 {
		t.Fatalf("exited %d:\n%s", code, out)
	}
	if strings.Contains(out, "Reclaimable") {
		t.Errorf("the run went ahead despite free space above the threshold:\n%s", out)
	}
	if !strings.Contains(out, "1.0KiB") {
		t.Errorf("the output does not say what threshold was not met:\n%s", out)
	}
}

func TestBelowLetsTheRunProceedWhenSpaceIsShort(t *testing.T) {
	home := fixtureHome(t)

	out, code := run(t, home, "clean", "--below", "999999T")

	if code != 0 {
		t.Fatalf("exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Reclaimable") {
		t.Errorf("the run was skipped though free space is under the threshold:\n%s", out)
	}
}

// The guard has to hold with --apply, which is the only way it is ever used.
func TestBelowDeletesNothingWhenNotMet(t *testing.T) {
	home := fixtureHome(t)
	blob := filepath.Join(home, ".cache/pip/blob")

	out, code := run(t, home, "clean", "--below", "1K", "--apply", "--yes")

	if code != 0 {
		t.Fatalf("exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(blob); err != nil {
		t.Fatalf("a skipped run deleted something: %v", err)
	}
}

func TestBadBelowSizeExitsNonZero(t *testing.T) {
	if _, code := run(t, fixtureHome(t), "clean", "--below", "banana"); code == 0 {
		t.Fatal("a bad --below size was accepted")
	}
}

// The log answers "what did that run actually take from me". Before this it
// could only answer "which unit ran", which is not the same question after a
// --discover run.
func TestHistoryShowsWhatWasRemoved(t *testing.T) {
	home := fixtureHome(t)
	cmd := exec.Command(bin, "clean", "--only", "pip-cache", "--apply", "--yes")
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clean failed: %v\n%s", err, out)
	}

	cmd = exec.Command(bin, "history")
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("history failed: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), ".cache/pip") {
		t.Errorf("history does not say what was removed:\n%s", out)
	}
}

// A dry run deletes nothing and is not written to the log at all, so it must
// not leave a record naming paths that are still there.
func TestHistoryRecordsNothingForADryRun(t *testing.T) {
	home := fixtureHome(t)
	cmd := exec.Command(bin, "clean", "--only", "pip-cache")
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clean failed: %v\n%s", err, out)
	}

	cmd = exec.Command(bin, "history")
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, _ := cmd.CombinedOutput()

	if !strings.Contains(string(out), "no recorded runs") {
		t.Errorf("a dry run left a record:\n%s", out)
	}
}
