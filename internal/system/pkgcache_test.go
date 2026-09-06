package system

import (
	"strings"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func onlyHas(bins ...string) func(string) bool {
	set := map[string]bool{}
	for _, b := range bins {
		set[b] = true
	}
	return func(b string) bool { return set[b] }
}

// Every distribution keeps a package cache, and every one of them refills it
// from the network. Covering only apt meant a Fedora or Arch machine got
// nothing from --system but a journal vacuum.
func TestPackageCacheUnitForEachManager(t *testing.T) {
	cases := []struct{ bin, id string }{
		{"apt-get", "system-apt"},
		{"dnf", "system-dnf"},
		{"yum", "system-yum"},
		{"pacman", "system-pacman"},
		{"zypper", "system-zypper"},
		{"apk", "system-apk"},
	}
	for _, c := range cases {
		t.Run(c.bin, func(t *testing.T) {
			r := unit.NewRegistry()
			Add(r, Env{Has: onlyHas(c.bin)})

			u, ok := r.Get(c.id)
			if !ok {
				t.Fatalf("%s not registered with %s present", c.id, c.bin)
			}
			if u.Flag != "--system" || !u.NeedsRoot {
				t.Errorf("flag %q needsRoot %v", u.Flag, u.NeedsRoot)
			}
			if !u.Reversible {
				t.Error("a package cache refills from the network, so this is reversible")
			}
			if !strings.Contains(u.Command, c.bin) {
				t.Errorf("command %q does not use %s", u.Command, c.bin)
			}
		})
	}
}

// A cache clean frees exactly what is sitting in the cache directory, so the
// dry run can report a real number instead of the 0B a command unit carries by
// default.
func TestPackageCacheUnitsAreMeasured(t *testing.T) {
	for _, bin := range []string{"apt-get", "dnf", "pacman", "zypper", "apk"} {
		r := unit.NewRegistry()
		Add(r, Env{Has: onlyHas(bin)})
		for _, u := range r.All() {
			if strings.HasPrefix(u.ID, "system-") && u.ID != "system-crash" {
				if len(u.SizePaths) == 0 {
					t.Errorf("%s reports no measurable size", u.ID)
				}
			}
		}
	}
}

// dnf replaced yum. Registering both on a machine that has dnf with a yum
// shim would clean the same cache twice and double-count it.
func TestYumIsNotRegisteredAlongsideDnf(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, Env{Has: onlyHas("dnf", "yum")})

	if _, ok := r.Get("system-yum"); ok {
		t.Error("yum registered on a machine that has dnf")
	}
	if _, ok := r.Get("system-dnf"); !ok {
		t.Error("dnf not registered")
	}
}

// pacman -Sc removes packages no longer installed. Removing the cached copy of
// what *is* installed (-Scc) breaks a downgrade, which is the one thing that
// cache is for.
func TestPacmanKeepsInstalledPackagesCached(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, Env{Has: onlyHas("pacman")})

	u, _ := r.Get("system-pacman")
	if strings.Contains(u.Command, "-Scc") {
		t.Fatalf("command would remove the cache a downgrade needs: %q", u.Command)
	}
}

// Kernels stay apt-only on purpose, and the reason is not that the others are
// too hard. dnf enforces installonly_limit itself, and Arch ships one "linux"
// package that is replaced rather than accumulated, so there is nothing to
// collect. Registering a kernel unit there would invent work.
func TestNoKernelUnitOnNonAptSystems(t *testing.T) {
	for _, bin := range []string{"dnf", "pacman", "zypper", "apk"} {
		r := unit.NewRegistry()
		Add(r, Env{
			Has:            onlyHas(bin),
			KernelPackages: func() []string { return []string{"kernel-core-6.8.0", "kernel-core-6.9.0", "kernel-core-7.0.0"} },
			RunningKernel:  func() string { return "7.0.0" },
		})
		if _, ok := r.Get("system-kernels"); ok {
			t.Errorf("kernel unit registered on a %s system", bin)
		}
	}
}
