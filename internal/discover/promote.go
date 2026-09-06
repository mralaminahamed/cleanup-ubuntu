package discover

import "github.com/mralaminahamed/reclaim/internal/unit"

// HeavyThreshold is the size at which a discovered cache stops being cheap.
const HeavyThreshold = 1 << 30 // 1GiB

// HeavyFlag opts in to discovered caches above HeavyThreshold.
const HeavyFlag = "--heavy"

// PromoteHeavy re-rates discovered units larger than threshold.
//
// Discovery claims a directory for where it sits, which is a sound argument
// that the contents are regenerable and no argument at all about what
// regenerating them costs. So every discovered unit lands on one tier, and a
// model cache that takes hours to re-download is ranked with a font cache that
// rebuilds in a second.
//
// Size is the only evidence available here, and it is enough: past a point, the
// cost of refilling stops being negligible whatever the directory turns out to
// hold. Two things change together, because they answer different questions.
// The flag is what actually gates the unit — the tier ceiling defaults to the
// top, so an unflagged unit runs whatever its tier. The tier is what the report
// shows and what orders the plan, and leaving it at the cheap end would keep
// telling the user something untrue.
func PromoteHeavy(r *unit.Registry, threshold int64) {
	for _, u := range r.All() {
		if !u.Discovered || u.Bytes < threshold {
			continue
		}
		if u.Tier < unit.TierColdReload {
			u.Tier = unit.TierColdReload
		}
		if u.Flag == "" {
			u.Flag = HeavyFlag
		}
	}
}
