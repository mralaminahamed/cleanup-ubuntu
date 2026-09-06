package system

import (
	"strings"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func joined(v []string) string { return strings.Join(v, " ") }

// The kernel in use must never be a candidate, whatever apt would decide on its
// own. This is the whole reason kernels get their own unit instead of leaning
// on autoremove.
func TestRemovableNeverIncludesTheRunningKernel(t *testing.T) {
	installed := []string{
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}

	got := removable(installed, "6.8.0-48-generic")

	if joined(got.Packages) == "" {
		t.Fatal("nothing selected")
	}
	for _, p := range got.Packages {
		if strings.Contains(p, "6.8.0-48") {
			t.Fatalf("running kernel selected for removal: %v", got.Packages)
		}
	}
}

// Removing everything but the running kernel leaves nothing to boot into when
// the current one later fails to. One spare is the difference between a bad
// upgrade and an unbootable machine.
func TestRemovableKeepsAFallbackBesidesTheRunningKernel(t *testing.T) {
	installed := []string{
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}

	got := removable(installed, "6.8.0-52-generic")

	if joined(got.Packages) != "linux-image-6.8.0-45-generic" {
		t.Fatalf("packages %v, want only the oldest", got.Packages)
	}
}

// Between an upgrade and the reboot that takes it up, the newest kernel is not
// the running one. It is still the fallback worth keeping.
func TestRemovableKeepsTheNewestWhenItIsNotRunning(t *testing.T) {
	installed := []string{
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}

	got := removable(installed, "6.8.0-45-generic")

	if joined(got.Packages) != "linux-image-6.8.0-48-generic" {
		t.Fatalf("packages %v, want only the middle one", got.Packages)
	}
}

func TestRemovableSelectsNothingWithOnlyTwoKernels(t *testing.T) {
	installed := []string{"linux-image-6.8.0-48-generic", "linux-image-6.8.0-52-generic"}

	if got := removable(installed, "6.8.0-52-generic"); len(got.Packages) != 0 {
		t.Fatalf("packages %v, want none", got.Packages)
	}
}

func TestRemovableSelectsNothingWithOneKernel(t *testing.T) {
	if got := removable([]string{"linux-image-6.8.0-52-generic"}, "6.8.0-52-generic"); len(got.Packages) != 0 {
		t.Fatalf("packages %v, want none", got.Packages)
	}
}

// Versions are numbers, not strings: 6.8.0-100 is newer than 6.8.0-99, and
// sorting these as text would pick the wrong kernel to keep.
func TestRemovableComparesVersionsNumerically(t *testing.T) {
	installed := []string{
		"linux-image-6.8.0-99-generic",
		"linux-image-6.8.0-100-generic",
		"linux-image-6.8.0-9-generic",
	}

	got := removable(installed, "6.8.0-100-generic")

	if joined(got.Packages) != "linux-image-6.8.0-9-generic" {
		t.Fatalf("packages %v, want only 6.8.0-9", got.Packages)
	}
}

// A kernel is more than its image: modules and headers for the same version are
// the bulk of the space and are useless without it.
func TestRemovableTakesModulesAndHeadersOfTheSameVersion(t *testing.T) {
	installed := []string{
		"linux-image-6.8.0-45-generic",
		"linux-modules-6.8.0-45-generic",
		"linux-modules-extra-6.8.0-45-generic",
		"linux-headers-6.8.0-45-generic",
		"linux-headers-6.8.0-45",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}

	got := removable(installed, "6.8.0-52-generic")

	want := "linux-headers-6.8.0-45 linux-headers-6.8.0-45-generic " +
		"linux-image-6.8.0-45-generic linux-modules-6.8.0-45-generic " +
		"linux-modules-extra-6.8.0-45-generic"
	if joined(got.Packages) != want {
		t.Fatalf("packages %v", got.Packages)
	}
}

// Headers and modules belonging to a kernel that is being kept must survive it.
func TestRemovableLeavesTheKeptKernelsPartsAlone(t *testing.T) {
	installed := []string{
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-52-generic",
		"linux-modules-6.8.0-52-generic",
		"linux-headers-6.8.0-52-generic",
	}

	got := removable(installed, "6.8.0-52-generic")

	for _, p := range got.Packages {
		if strings.Contains(p, "6.8.0-52") {
			t.Fatalf("part of a kept kernel selected: %v", got.Packages)
		}
	}
}

// Anything that is not a versioned kernel package -- the meta-packages that
// track the latest kernel, above all -- must be left where it is. Removing
// linux-image-generic is how a machine stops receiving kernel updates.
func TestRemovableIgnoresUnversionedKernelPackages(t *testing.T) {
	installed := []string{
		"linux-image-generic",
		"linux-headers-generic",
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}

	got := removable(installed, "6.8.0-52-generic")

	for _, p := range got.Packages {
		if p == "linux-image-generic" || p == "linux-headers-generic" {
			t.Fatalf("meta-package selected: %v", got.Packages)
		}
	}
}

// The measurement has to come from somewhere a command unit does not own, so
// each removable version contributes the files it actually put on disk.
func TestRemovableReportsThePathsOfWhatItWouldRemove(t *testing.T) {
	installed := []string{
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}

	got := removable(installed, "6.8.0-52-generic")

	want := map[string]bool{
		"/boot/vmlinuz-6.8.0-45-generic":    true,
		"/boot/initrd.img-6.8.0-45-generic": true,
		"/lib/modules/6.8.0-45-generic":     true,
	}
	for _, p := range got.Paths {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("paths %v do not cover %v", got.Paths, want)
	}
}

func kernelEnv(installed []string, running string) Env {
	return Env{
		Has:            func(bin string) bool { return bin == "apt-get" },
		KernelPackages: func() []string { return installed },
		RunningKernel:  func() string { return running },
	}
}

func TestKernelUnitIsRegisteredWhenThereIsSomethingToRemove(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, kernelEnv([]string{
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}, "6.8.0-52-generic"))

	u, ok := r.Get("system-kernels")
	if !ok {
		t.Fatal("system-kernels not registered")
	}
	if u.Flag != "--kernels" {
		t.Errorf("flag %q, want --kernels", u.Flag)
	}
	if !u.NeedsRoot {
		t.Error("purging packages needs root")
	}
	if !u.Reversible {
		t.Error("the packages are in the archive, so this is reversible")
	}
}

// The dry run has to name the packages. A byte count alone is not something
// anyone can sensibly consent to for a kernel removal.
func TestKernelUnitNamesThePackagesItWouldRemove(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, kernelEnv([]string{
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}, "6.8.0-52-generic"))

	u, _ := r.Get("system-kernels")
	if len(u.Detail) == 0 {
		t.Fatal("no detail")
	}
	if !strings.Contains(strings.Join(u.Detail, " "), "linux-image-6.8.0-45-generic") {
		t.Errorf("detail %v does not name the package", u.Detail)
	}
}

// The command must spell the packages out. A glob would be expanded by the
// shell at run time against whatever is installed then, which is not what the
// dry run showed anyone.
func TestKernelUnitCommandContainsNoGlob(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, kernelEnv([]string{
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}, "6.8.0-52-generic"))

	u, _ := r.Get("system-kernels")
	if strings.ContainsAny(u.Command, "*?") {
		t.Fatalf("command globs: %q", u.Command)
	}
	if !strings.Contains(u.Command, "linux-image-6.8.0-45-generic") {
		t.Errorf("command does not name the package: %q", u.Command)
	}
	if strings.Contains(u.Command, "6.8.0-52") {
		t.Fatalf("command names the running kernel: %q", u.Command)
	}
}

// Measurement comes from the files the removed kernels own, so the report shows
// a real number rather than the 0B a command unit would otherwise carry.
func TestKernelUnitIsMeasuredThroughTheFilesItFrees(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, kernelEnv([]string{
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}, "6.8.0-52-generic"))

	u, _ := r.Get("system-kernels")
	if len(u.SizePaths) == 0 {
		t.Fatal("no size paths")
	}
	if len(u.Paths) != 0 {
		t.Fatalf("paths %v: apt removes these, nothing here may delete them", u.Paths)
	}
}

func TestKernelUnitIsAbsentWithNothingRemovable(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, kernelEnv([]string{"linux-image-6.8.0-52-generic"}, "6.8.0-52-generic"))

	if _, ok := r.Get("system-kernels"); ok {
		t.Error("registered with nothing to remove")
	}
}

func TestKernelUnitIsAbsentWithoutApt(t *testing.T) {
	r := unit.NewRegistry()
	env := kernelEnv([]string{
		"linux-image-6.8.0-45-generic",
		"linux-image-6.8.0-48-generic",
		"linux-image-6.8.0-52-generic",
	}, "6.8.0-52-generic")
	env.Has = func(string) bool { return false }
	Add(r, env)

	if _, ok := r.Get("system-kernels"); ok {
		t.Error("registered on a machine without apt")
	}
}
