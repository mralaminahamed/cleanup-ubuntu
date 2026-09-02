package unit

import "testing"

func TestTierOrdering(t *testing.T) {
	// The whole safety model rests on tier order: cheap and regenerable first,
	// information-losing last. Guard the constants so a reorder is caught.
	if !(TierNative < TierPkgCache && TierPkgCache < TierArtifact &&
		TierArtifact < TierColdReload && TierColdReload < TierLossy &&
		TierLossy < TierIrreplaceable) {
		t.Fatalf("tier constants out of order")
	}
}

func TestAddAssignsAndFinds(t *testing.T) {
	r := NewRegistry()
	u := &Unit{ID: "npm-cache", Tier: TierPkgCache, Reversible: true,
		Label: "npm cache", Kind: KindPaths, Paths: []string{"/tmp/x"}}
	r.Add(u)

	if got := len(r.All()); got != 1 {
		t.Fatalf("len(All()) = %d, want 1", got)
	}
	got, ok := r.Get("npm-cache")
	if !ok {
		t.Fatal("Get(npm-cache) not found")
	}
	if got.Label != "npm cache" {
		t.Errorf("Label = %q, want %q", got.Label, "npm cache")
	}
}

func TestAddIsIdempotentByID(t *testing.T) {
	// register_all_units and the discovery scanners can both reach the same
	// directory; the second claim must not create a duplicate unit.
	r := NewRegistry()
	r.Add(&Unit{ID: "dup", Tier: TierPkgCache, Kind: KindPaths, Paths: []string{"/a"}})
	r.Add(&Unit{ID: "dup", Tier: TierLossy, Kind: KindPaths, Paths: []string{"/b"}})

	if got := len(r.All()); got != 1 {
		t.Fatalf("len(All()) = %d, want 1 (duplicate ID must not be added)", got)
	}
	u, _ := r.Get("dup")
	if u.Tier != TierPkgCache {
		t.Errorf("Tier = %v, want the first registration to win", u.Tier)
	}
}

func TestClaimedRejectsAlreadyOwnedPath(t *testing.T) {
	// This is what stops the generic ~/.cache sweep from re-claiming a
	// directory a hardcoded unit already owns.
	r := NewRegistry()
	r.Add(&Unit{ID: "hardcoded", Kind: KindPaths, Paths: []string{"/home/u/.cache/npm"}})

	if !r.Claimed("/home/u/.cache/npm") {
		t.Error("Claimed() = false for an owned path, want true")
	}
	if r.Claimed("/home/u/.cache/other") {
		t.Error("Claimed() = true for an unowned path, want false")
	}
}

func TestClaimedIgnoresCmdUnits(t *testing.T) {
	// A cmd unit owns no paths, so it must never make a path look claimed.
	r := NewRegistry()
	r.Add(&Unit{ID: "npm-native", Kind: KindCmd, Command: "npm cache clean --force"})
	if r.Claimed("") {
		t.Error("Claimed(\"\") = true, want false")
	}
}

func TestAllPreservesRegistrationOrder(t *testing.T) {
	// Execution order is tier-then-size, but stable registration order keeps
	// reports and JSON deterministic between runs.
	r := NewRegistry()
	for _, id := range []string{"a", "b", "c"} {
		r.Add(&Unit{ID: id, Kind: KindPaths, Paths: []string{"/" + id}})
	}
	want := []string{"a", "b", "c"}
	for i, u := range r.All() {
		if u.ID != want[i] {
			t.Fatalf("All()[%d].ID = %q, want %q", i, u.ID, want[i])
		}
	}
}
