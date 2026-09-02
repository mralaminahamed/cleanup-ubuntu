package plan

import (
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func reg(us ...*unit.Unit) *unit.Registry {
	r := unit.NewRegistry()
	for _, u := range us {
		r.Add(u)
	}
	return r
}

func ids(us []*unit.Unit) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.ID
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSelectOrdersByTierThenSizeDescending(t *testing.T) {
	// Cheapest tier first, and inside a tier the big wins land first so a
	// target is met with the fewest deletions.
	r := reg(
		&unit.Unit{ID: "small1", Tier: unit.TierPkgCache, Reversible: true, Bytes: 10},
		&unit.Unit{ID: "big1", Tier: unit.TierPkgCache, Reversible: true, Bytes: 900},
		&unit.Unit{ID: "t0", Tier: unit.TierNative, Reversible: true, Bytes: 1},
		&unit.Unit{ID: "big3", Tier: unit.TierColdReload, Reversible: true, Bytes: 5000},
	)
	sel, _ := Select(r, Options{TierCap: unit.TierIrreplaceable})
	eq(t, ids(sel), []string{"t0", "big1", "small1", "big3"})
}

func TestSelectSkipsLockedUnits(t *testing.T) {
	r := reg(
		&unit.Unit{ID: "free", Tier: unit.TierPkgCache, Reversible: true, Bytes: 5},
		&unit.Unit{ID: "busy", Tier: unit.TierPkgCache, Reversible: true, Bytes: 500, LockedBy: "chrome"},
	)
	sel, _ := Select(r, Options{TierCap: unit.TierIrreplaceable})
	eq(t, ids(sel), []string{"free"})
}

func TestSelectWithholdsLossyUnlessAllowed(t *testing.T) {
	r := reg(
		&unit.Unit{ID: "safe", Tier: unit.TierPkgCache, Reversible: true, Bytes: 5},
		&unit.Unit{ID: "lossy", Tier: unit.TierLossy, Reversible: false, Bytes: 900},
	)
	sel, withheld := Select(r, Options{TierCap: unit.TierIrreplaceable})
	eq(t, ids(sel), []string{"safe"})
	eq(t, ids(withheld), []string{"lossy"})

	sel, withheld = Select(r, Options{TierCap: unit.TierIrreplaceable, AllowLossy: true})
	eq(t, ids(sel), []string{"safe", "lossy"})
	if len(withheld) != 0 {
		t.Errorf("withheld = %v, want empty when lossy is allowed", ids(withheld))
	}
}

func TestSelectRespectsTierCeiling(t *testing.T) {
	r := reg(
		&unit.Unit{ID: "cheap", Tier: unit.TierPkgCache, Reversible: true, Bytes: 5},
		&unit.Unit{ID: "expensive", Tier: unit.TierColdReload, Reversible: true, Bytes: 900},
	)
	sel, _ := Select(r, Options{TierCap: unit.TierPkgCache})
	eq(t, ids(sel), []string{"cheap"})
}

func TestFlagLetsAUnitCrossTheCeiling(t *testing.T) {
	// --gradle is how a user says "yes, I accept the cold re-download".
	r := reg(
		&unit.Unit{ID: "gradle", Tier: unit.TierColdReload, Reversible: true, Bytes: 900, Flag: "--gradle"},
	)
	sel, _ := Select(r, Options{TierCap: unit.TierPkgCache})
	if len(sel) != 0 {
		t.Fatalf("selected %v without the flag", ids(sel))
	}
	sel, _ = Select(r, Options{TierCap: unit.TierPkgCache, Forced: map[string]bool{"--gradle": true}})
	eq(t, ids(sel), []string{"gradle"})
}

func TestFlagCannotCrossTheLossyCeiling(t *testing.T) {
	// A legacy opt-in flag raises the tier ceiling but must never make an
	// information-destroying unit run. Only AllowLossy does that.
	r := reg(&unit.Unit{ID: "history", Tier: unit.TierLossy, Reversible: false,
		Bytes: 900, Flag: "--claude-history"})
	sel, withheld := Select(r, Options{
		TierCap: unit.TierIrreplaceable,
		Forced:  map[string]bool{"--claude-history": true},
	})
	if len(sel) != 0 {
		t.Fatalf("lossy unit selected via flag alone: %v", ids(sel))
	}
	eq(t, ids(withheld), []string{"history"})
}

func TestSelectFiltersByTargetMount(t *testing.T) {
	// Cleaning /home does not help when / is the full filesystem.
	r := reg(
		&unit.Unit{ID: "onroot", Tier: unit.TierPkgCache, Reversible: true, Bytes: 5, Mount: "/"},
		&unit.Unit{ID: "onhome", Tier: unit.TierPkgCache, Reversible: true, Bytes: 900, Mount: "/home"},
	)
	sel, _ := Select(r, Options{TierCap: unit.TierIrreplaceable, TargetMount: "/"})
	eq(t, ids(sel), []string{"onroot"})
}

func TestSelectExcludesByID(t *testing.T) {
	r := reg(
		&unit.Unit{ID: "keep", Tier: unit.TierPkgCache, Reversible: true, Bytes: 5},
		&unit.Unit{ID: "drop", Tier: unit.TierPkgCache, Reversible: true, Bytes: 900},
	)
	sel, _ := Select(r, Options{TierCap: unit.TierIrreplaceable, Exclude: []string{"drop"}})
	eq(t, ids(sel), []string{"keep"})
}

func TestSelectExcludeAcceptsGlob(t *testing.T) {
	r := reg(
		&unit.Unit{ID: "xdg-node", Tier: unit.TierArtifact, Reversible: true, Bytes: 5},
		&unit.Unit{ID: "xdg-deno", Tier: unit.TierArtifact, Reversible: true, Bytes: 6},
		&unit.Unit{ID: "npm-cache", Tier: unit.TierArtifact, Reversible: true, Bytes: 7},
	)
	sel, _ := Select(r, Options{TierCap: unit.TierIrreplaceable, Exclude: []string{"xdg-*"}})
	eq(t, ids(sel), []string{"npm-cache"})
}

func TestSelectOnlyKeepsListedUnits(t *testing.T) {
	r := reg(
		&unit.Unit{ID: "a", Tier: unit.TierPkgCache, Reversible: true, Bytes: 5},
		&unit.Unit{ID: "b", Tier: unit.TierPkgCache, Reversible: true, Bytes: 900},
	)
	sel, _ := Select(r, Options{TierCap: unit.TierIrreplaceable, Only: []string{"b"}})
	eq(t, ids(sel), []string{"b"})
}
