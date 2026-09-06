// Package system covers reclaimable space outside the user's home: the package
// manager cache, the journal, and superseded snap revisions.
//
// Every unit here needs root and touches shared state, so all of them are
// opt-in behind --system. None runs merely because the tier ceiling allows it.
package system

import (
	"os/exec"
	"strings"

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

	if env.Has("apt-get") {
		// Cache clean only. autoremove can pull out kernels and packages the
		// user still wants, so it is never the default.
		add("system-apt", "apt cache clean", "sudo apt-get clean", unit.TierNative)
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
