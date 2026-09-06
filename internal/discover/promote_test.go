package discover

import (
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

const gb = 1 << 30

// A discovered unit's tier is a guess. XDGCaches claims a directory for where
// it sits, not for what it costs to refill, so every one of them lands on the
// same tier. Once the size is known that guess has to be revisited: a 96GB
// model cache and a 2MB font cache are both regenerable, and only one of them
// is cheap.
func TestPromoteHeavyRaisesLargeDiscoveredUnits(t *testing.T) {
	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "xdg-huggingface", Tier: unit.TierArtifact, Reversible: true,
		Kind: unit.KindPaths, Discovered: true, Bytes: 96 * gb})

	PromoteHeavy(r, HeavyThreshold)

	u, _ := r.Get("xdg-huggingface")
	if u.Tier != unit.TierColdReload {
		t.Fatalf("tier %v, want TierColdReload", u.Tier)
	}
}

// The tier ceiling defaults to the top, so tier alone gates nothing in an
// ordinary run — a flag is what makes a unit opt-in. A promoted unit has to
// carry one or the promotion is decoration.
func TestPromoteHeavyMakesTheUnitOptIn(t *testing.T) {
	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "xdg-huggingface", Tier: unit.TierArtifact, Reversible: true,
		Kind: unit.KindPaths, Discovered: true, Bytes: 96 * gb})

	PromoteHeavy(r, HeavyThreshold)

	u, _ := r.Get("xdg-huggingface")
	if u.Flag != HeavyFlag {
		t.Fatalf("flag %q, want %q", u.Flag, HeavyFlag)
	}
}

func TestPromoteHeavyLeavesSmallUnitsUnflagged(t *testing.T) {
	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "xdg-fontconfig", Tier: unit.TierArtifact, Reversible: true,
		Kind: unit.KindPaths, Discovered: true, Bytes: 2 << 20})

	PromoteHeavy(r, HeavyThreshold)

	u, _ := r.Get("xdg-fontconfig")
	if u.Flag != "" {
		t.Fatalf("flag %q, want none", u.Flag)
	}
}

// Promotion must not cost anything for the ordinary case. Most of what
// discovery finds is small, and it should keep running in a default clean.
func TestPromoteHeavyLeavesSmallDiscoveredUnitsAlone(t *testing.T) {
	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "xdg-fontconfig", Tier: unit.TierArtifact, Reversible: true,
		Kind: unit.KindPaths, Discovered: true, Bytes: 2 << 20})

	PromoteHeavy(r, HeavyThreshold)

	u, _ := r.Get("xdg-fontconfig")
	if u.Tier != unit.TierArtifact {
		t.Fatalf("tier %v, want TierArtifact", u.Tier)
	}
}

// A catalog tier is a decision someone made with knowledge of the target.
// go-modcache is deliberately tier 1 at any size, and a size heuristic must not
// second-guess that.
func TestPromoteHeavyIgnoresCatalogUnits(t *testing.T) {
	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "go-modcache", Tier: unit.TierPkgCache, Reversible: true,
		Kind: unit.KindPaths, Bytes: 40 * gb})

	PromoteHeavy(r, HeavyThreshold)

	u, _ := r.Get("go-modcache")
	if u.Tier != unit.TierPkgCache {
		t.Fatalf("tier %v, want TierPkgCache", u.Tier)
	}
}

// Promotion raises. A pass that can also lower a tier would be a way to make
// something look safer than it is, which is the opposite of the point.
func TestPromoteHeavyNeverLowersATier(t *testing.T) {
	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "xdg-something", Tier: unit.TierLossy, Reversible: true,
		Kind: unit.KindPaths, Discovered: true, Bytes: 96 * gb})

	PromoteHeavy(r, HeavyThreshold)

	u, _ := r.Get("xdg-something")
	if u.Tier != unit.TierLossy {
		t.Fatalf("tier %v, want TierLossy left alone", u.Tier)
	}
}

func TestXDGCachesMarksUnitsDiscovered(t *testing.T) {
	home := t.TempDir()
	cache := mk(t, home, ".cache")
	mk(t, cache, "huggingface")

	r := unit.NewRegistry()
	XDGCaches(r, cache)

	for _, u := range r.All() {
		if !u.Discovered {
			t.Errorf("unit %q not marked discovered", u.ID)
		}
	}
}

func TestNestedCachesMarksUnitsDiscovered(t *testing.T) {
	root := t.TempDir()
	app := mk(t, root, "Slack")
	mk(t, app, "Cache")

	r := unit.NewRegistry()
	NestedCaches(r, []string{root})

	if len(r.All()) == 0 {
		t.Fatal("nothing discovered")
	}
	for _, u := range r.All() {
		if !u.Discovered {
			t.Errorf("unit %q not marked discovered", u.ID)
		}
	}
}
