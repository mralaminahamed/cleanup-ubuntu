package plan

import (
	"sort"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// OptIn returns the units Select passed over only because their flag was not
// given, largest first.
//
// Select is silent about these on purpose — an opt-in unit that runs without
// being named would defeat the point of the flag. But silence in the *report*
// is a different thing: a discovered cache has no documentation naming it, so
// if the run says nothing the user never learns the space is there. This is
// what the report needs to offer them the trade.
func OptIn(r *unit.Registry, o Options) []*unit.Unit {
	var out []*unit.Unit
	for _, u := range r.All() {
		if u.Flag == "" || o.Forced[u.Flag] {
			continue
		}
		if !matchesAny(u.ID, o.Only, true) || matchesAny(u.ID, o.Exclude, false) {
			continue
		}
		// Locked units are already reported with the pid to blame, and a flag
		// would not free them. Empty ones buy nothing.
		if u.LockedBy != "" || u.Bytes == 0 {
			continue
		}
		out = append(out, u)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}
