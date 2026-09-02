// Package plan decides which units run, in what order.
//
// Selection is the safety gate: it enforces the tier ceiling, refuses
// information-destroying units unless explicitly allowed, skips anything a
// running application holds, and can target a single filesystem.
package plan

import (
	"path/filepath"
	"sort"

	"github.com/mralaminahamed/cleanup-ubuntu/internal/unit"
)

// Options controls selection.
type Options struct {
	// TierCap is the highest tier that runs without an explicit opt-in flag.
	TierCap unit.Tier
	// AllowLossy permits units that destroy information. This is the only way
	// past that ceiling; an opt-in flag is not enough.
	AllowLossy bool
	// Forced holds opt-in flags the user passed, letting matching units exceed
	// TierCap.
	Forced map[string]bool
	// TargetMount, when set, restricts cleaning to one filesystem.
	TargetMount string
	// Only, when non-empty, restricts the run to these unit ids or globs.
	Only []string
	// Exclude drops matching unit ids or globs.
	Exclude []string
}

// Select returns the units to run in execution order, plus the lossy units that
// were withheld so the report can tell the user what it did not touch.
//
// Order is tier ascending, then bytes descending: the cheapest class of cleanup
// runs first, and inside a class the biggest win lands first so a free-space
// target is met with the fewest deletions.
func Select(r *unit.Registry, o Options) (selected, withheldLossy []*unit.Unit) {
	for _, u := range r.All() {
		forced := u.Flag != "" && o.Forced[u.Flag]

		if !matchesAny(u.ID, o.Only, true) {
			continue
		}
		if matchesAny(u.ID, o.Exclude, false) {
			continue
		}
		// The reversibility ceiling is absolute. An opt-in flag raises the tier
		// ceiling only; it can never authorise destroying information.
		if !u.Reversible && !o.AllowLossy {
			withheldLossy = append(withheldLossy, u)
			continue
		}
		if u.Tier > o.TierCap && !forced {
			continue
		}
		if u.LockedBy != "" {
			continue
		}
		if o.TargetMount != "" && u.Mount != "" && u.Mount != o.TargetMount && !forced {
			continue
		}
		selected = append(selected, u)
	}

	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].Tier != selected[j].Tier {
			return selected[i].Tier < selected[j].Tier
		}
		return selected[i].Bytes > selected[j].Bytes
	})
	return selected, withheldLossy
}

// matchesAny reports whether id matches one of the patterns. Patterns are unit
// ids or globs. An empty pattern list means "no filter", returning empty.
func matchesAny(id string, patterns []string, emptyMeans bool) bool {
	if len(patterns) == 0 {
		return emptyMeans
	}
	for _, p := range patterns {
		if p == id {
			return true
		}
		if ok, err := filepath.Match(p, id); err == nil && ok {
			return true
		}
	}
	return false
}
