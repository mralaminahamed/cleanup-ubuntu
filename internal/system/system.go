// Package system covers reclaimable space outside the user's home: the package
// manager cache, the journal, and superseded snap revisions.
//
// Every unit here needs root and touches shared state, so all of them are
// opt-in behind --system. None runs merely because the tier ceiling allows it.
package system

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// Env describes the machine.
type Env struct {
	// Has reports whether a binary is on PATH.
	Has func(bin string) bool
	// JournalKeep bounds the journal vacuum. Empty means keep 200M.
	JournalKeep string
	// KernelPackages lists the installed kernel packages, and RunningKernel
	// names the release in use. Both are injected so the selection can be
	// tested against kernel sets this machine does not have -- and the
	// selection is the part that must not be got wrong.
	KernelPackages func() []string
	RunningKernel  func() string
	// CrashDirs are the directories crash artifacts land in. Injected so the
	// unit can be tested against a fixture rather than the real /var.
	CrashDirs []string
}

// DefaultEnv returns an Env for this machine.
func DefaultEnv() Env {
	return Env{
		Has: func(bin string) bool {
			_, err := exec.LookPath(bin)
			return err == nil
		},
		KernelPackages: installedKernels,
		RunningKernel:  runningKernel,
		CrashDirs:      []string{"/var/crash", "/var/lib/systemd/coredump"},
	}
}

// Add registers the system units that apply here.
func Add(r *unit.Registry, env Env) {
	if env.Has == nil {
		return
	}
	add := func(id, label, command string, tier unit.Tier) {
		r.Add(&unit.Unit{ID: id, Tier: tier, Reversible: true, Label: label,
			Kind: unit.KindCmd, Command: command, Flag: "--system", MountHint: "/",
			NeedsRoot: true})
	}

	addCached := func(id, label, command string, paths ...string) {
		r.Add(&unit.Unit{ID: id, Tier: unit.TierPkgCache, Reversible: true,
			Label: label, Kind: unit.KindCmd, Command: command, Flag: "--system",
			MountHint: "/", NeedsRoot: true, SizePaths: paths})
	}

	// Every distribution keeps a package cache and every one of them refills it
	// from the network, so they are all tier 1 and all reversible. Only the
	// command and the directory differ.
	//
	// Each is a cache clean and nothing more. An autoremove decides for itself
	// what is orphaned, and that set is not something this tool can preview
	// honestly.
	switch {
	case env.Has("apt-get"):
		addCached("system-apt", "apt cache clean", "sudo apt-get clean",
			"/var/cache/apt/archives")
	case env.Has("dnf"):
		// dnf5 moved the cache; naming both costs nothing and PathBytes
		// reports 0 for the one that is not there.
		addCached("system-dnf", "dnf cache clean", "sudo dnf clean all",
			"/var/cache/dnf", "/var/cache/libdnf5")
	case env.Has("yum"):
		// Only when dnf is absent: on a modern Fedora "yum" is a shim for dnf,
		// and registering both would clean one cache twice and count it twice.
		addCached("system-yum", "yum cache clean", "sudo yum clean all",
			"/var/cache/yum")
	case env.Has("pacman"):
		// -Sc, never -Scc. -Sc drops packages that are no longer installed;
		// -Scc drops the cached copy of what *is* installed, which is what a
		// downgrade needs and the only reason to keep that cache at all.
		addCached("system-pacman", "pacman cache clean",
			"sudo pacman -Sc --noconfirm", "/var/cache/pacman/pkg")
	case env.Has("zypper"):
		addCached("system-zypper", "zypper cache clean", "sudo zypper clean --all",
			"/var/cache/zypp")
	case env.Has("apk"):
		addCached("system-apk", "apk cache clean", "sudo apk cache clean",
			"/var/cache/apk")
	}
	if env.Has("journalctl") {
		keep := env.JournalKeep
		if keep == "" {
			keep = "200M"
		}
		// Bounded on purpose: an unbounded vacuum would drop the entire journal
		// and with it the logs needed to explain a recent failure.
		add("system-journal", "journal vacuum", "sudo journalctl --vacuum-size="+keep, unit.TierPkgCache)
	}
	// Kernels get a unit of their own rather than a blanket autoremove. apt
	// decides for itself what is orphaned, and that set is not something this
	// tool can preview honestly; a kernel removal is exactly where a preview
	// matters most.
	if env.Has("apt-get") && env.KernelPackages != nil && env.RunningKernel != nil {
		if rm := removable(env.KernelPackages(), env.RunningKernel()); len(rm.Packages) > 0 {
			r.Add(&unit.Unit{
				ID: "system-kernels", Tier: unit.TierPkgCache, Reversible: true,
				Label: "superseded kernels", Kind: unit.KindCmd, Flag: "--kernels",
				MountHint: "/boot", NeedsRoot: true,
				Command:   "sudo apt-get -y purge " + strings.Join(rm.Packages, " "),
				Detail:    rm.Packages,
				SizePaths: rm.Paths,
			})
		}
	}
	// Crash artifacts. Post-mortem data with no regeneration cost: nothing
	// re-downloads or reindexes, the space is simply free.
	//
	// The age bound is what makes marking these reversible defensible. A dump
	// from this morning belongs to a crash someone may be reading right now,
	// and taking it would destroy the only copy of that failure. One from last
	// month is a record the system itself is configured to expire. Without the
	// bound this unit would have to be lossy, and behind --allow-lossy it would
	// never run.
	if dirs := present(env.CrashDirs); len(dirs) > 0 {
		mins := int(crashAge.Minutes())
		r.Add(&unit.Unit{
			ID: "system-crash", Tier: unit.TierNative, Reversible: true,
			Label: "crash dumps and reports", Kind: unit.KindCmd, Flag: "--system",
			MountHint: "/var", NeedsRoot: true, MinAge: crashAge,
			SizePaths: dirs,
			// mindepth keeps the directories themselves: they are what
			// systemd-coredump and apport write into.
			Command: "sudo find " + strings.Join(dirs, " ") +
				" -mindepth 1 -maxdepth 1 -mmin +" + strconv.Itoa(mins) +
				" -exec rm -rf {} +",
		})
	}
	if env.Has("snap") {
		// "|| exit 1" matters: a while loop is the last stage of this pipeline
		// and exits 0 even when every removal inside it failed, which made a
		// failed snap cleanup completely invisible.
		add("system-snaps", "old snap revisions",
			`snap list --all | awk '/disabled/{print $1, $3}' | `+
				`while read -r sn rev; do sudo snap remove "$sn" --revision="$rev" || exit 1; done`,
			unit.TierPkgCache)
	}
}

// crashAge is how long a crash artifact is left alone. Long enough that an
// investigation in progress is never disturbed, short enough to be worth
// running.
const crashAge = 7 * 24 * time.Hour

// present filters a list of directories down to those on this machine.
func present(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			out = append(out, d)
		}
	}
	return out
}
