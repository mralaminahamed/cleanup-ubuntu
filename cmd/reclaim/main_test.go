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
