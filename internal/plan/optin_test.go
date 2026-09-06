package plan

import (
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// Select drops a unit that wants a flag without saying so. That is right for
// the plan and wrong for the report: a 4GiB cache the user could have had, and
// was never told about, is a worse outcome than being asked.
func TestOptInListsUnitsHeldBackForWantOfAFlag(t *testing.T) {
	r := reg(
		&unit.Unit{ID: "gradle", Tier: unit.TierColdReload, Reversible: true,
			Flag: "--gradle", Bytes: 4 << 30},
	)

	eq(t, ids(OptIn(r, Options{TierCap: unit.TierIrreplaceable})), []string{"gradle"})
}

func TestOptInIgnoresUnitsThatWouldRunAnyway(t *testing.T) {
	r := reg(&unit.Unit{ID: "pip", Tier: unit.TierPkgCache, Reversible: true, Bytes: 10})

	if got := ids(OptIn(r, Options{TierCap: unit.TierIrreplaceable})); len(got) != 0 {
		t.Fatalf("got %v, want nothing", got)
	}
}

// The two lists answer one question between them, so a unit must appear in
// exactly one of them. If this ever fails, the conditions have drifted apart.
func TestOptInAndSelectAreDisjoint(t *testing.T) {
	r := reg(
		&unit.Unit{ID: "gradle", Tier: unit.TierColdReload, Reversible: true,
			Flag: "--gradle", Bytes: 4 << 30},
		&unit.Unit{ID: "pip", Tier: unit.TierPkgCache, Reversible: true, Bytes: 10},
	)
	o := Options{TierCap: unit.TierIrreplaceable}

	sel, _ := Select(r, o)
	eq(t, ids(sel), []string{"pip"})
	eq(t, ids(OptIn(r, o)), []string{"gradle"})

	// Naming the flag moves it across, and leaves nothing behind.
	o.Forced = map[string]bool{"--gradle": true}
	sel, _ = Select(r, o)
	eq(t, ids(sel), []string{"pip", "gradle"})
	if got := ids(OptIn(r, o)); len(got) != 0 {
		t.Fatalf("still held back after forcing: %v", got)
	}
}

// A run the user narrowed should not be told about units they excluded from
// it. Offering what was just filtered out is noise, not information.
func TestOptInRespectsOnlyAndExclude(t *testing.T) {
	r := reg(
		&unit.Unit{ID: "gradle", Tier: unit.TierColdReload, Reversible: true,
			Flag: "--gradle", Bytes: 4 << 30},
		&unit.Unit{ID: "maven", Tier: unit.TierColdReload, Reversible: true,
			Flag: "--maven", Bytes: 1 << 30},
	)

	eq(t, ids(OptIn(r, Options{Exclude: []string{"gradle"}})), []string{"maven"})
	eq(t, ids(OptIn(r, Options{Only: []string{"gradle"}})), []string{"gradle"})
}

// A locked unit is already reported, with the pid to blame. Listing it a
// second time under a different heading would just be telling the user to pass
// a flag that will not help.
func TestOptInSkipsLockedUnits(t *testing.T) {
	r := reg(
		&unit.Unit{ID: "jetbrains", Tier: unit.TierColdReload, Reversible: true,
			Flag: "--jetbrains", Bytes: 6 << 30, LockedBy: "idea", PID: 42},
	)

	if got := ids(OptIn(r, Options{TierCap: unit.TierIrreplaceable})); len(got) != 0 {
		t.Fatalf("got %v, want nothing", got)
	}
}

// Nothing is worth mentioning if there is nothing to gain by it.
func TestOptInSkipsEmptyUnits(t *testing.T) {
	r := reg(&unit.Unit{ID: "gradle", Tier: unit.TierColdReload, Reversible: true,
		Flag: "--gradle", Bytes: 0})

	if got := ids(OptIn(r, Options{TierCap: unit.TierIrreplaceable})); len(got) != 0 {
		t.Fatalf("got %v, want nothing", got)
	}
}
