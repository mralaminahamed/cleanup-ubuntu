package system

import (
	"strings"
	"testing"
	"time"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func crashEnv(dirs ...string) Env {
	return Env{
		Has:       func(string) bool { return false },
		CrashDirs: dirs,
	}
}

// Crash artifacts have no regeneration cost at all: nothing re-downloads or
// reindexes, the space is simply free. That is what tier 0 means.
func TestCrashUnitIsTierZero(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, crashEnv(t.TempDir()))

	u, ok := r.Get("system-crash")
	if !ok {
		t.Fatal("system-crash not registered")
	}
	if u.Tier != unit.TierNative {
		t.Errorf("tier %v, want TierNative", u.Tier)
	}
	if u.Flag != "--system" {
		t.Errorf("flag %q, want --system", u.Flag)
	}
	if !u.NeedsRoot {
		t.Error("these are root-owned")
	}
}

// The age bound is the whole reason this is safe to mark reversible. A dump
// written this morning belongs to a crash someone may be looking at right now;
// one from last month does not.
func TestCrashUnitOnlyTakesOldEntries(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, crashEnv(t.TempDir()))

	u, _ := r.Get("system-crash")
	if u.MinAge <= 0 {
		t.Fatal("no age bound: a dump from this morning would be taken")
	}
	if u.MinAge < 24*time.Hour {
		t.Errorf("age bound %v is too short to protect an active investigation", u.MinAge)
	}
}

// The command has to agree with the measurement. A find that took a different
// set than Targets measured would report space it does not free, or free space
// it never reported.
func TestCrashUnitCommandMatchesItsAgeBound(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, crashEnv(t.TempDir()))

	u, _ := r.Get("system-crash")
	mins := int(u.MinAge.Minutes())
	if !strings.Contains(u.Command, "-mmin +"+itoa(mins)) {
		t.Errorf("command %q does not use the unit's own bound of %d minutes", u.Command, mins)
	}
	// mindepth keeps the directory itself, which is what receives new reports.
	if !strings.Contains(u.Command, "-mindepth 1") {
		t.Errorf("command could remove the directory itself: %q", u.Command)
	}
}

func TestCrashUnitMeasuresWithoutOwningPaths(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, crashEnv(t.TempDir()))

	u, _ := r.Get("system-crash")
	if len(u.SizePaths) == 0 {
		t.Error("nothing to measure")
	}
	if len(u.Paths) != 0 {
		t.Errorf("paths %v: root owns these, so the shell removes them", u.Paths)
	}
}

func TestCrashUnitIsAbsentWhenNoDirectoryExists(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, crashEnv("/nonexistent/crash"))

	if _, ok := r.Get("system-crash"); ok {
		t.Error("registered with nothing on the machine")
	}
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
