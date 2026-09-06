package catalog

import (
	"path/filepath"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// Model caches are reversible -- every file re-downloads -- and nothing like
// cheap. Tens to hundreds of gigabytes over hours, often metered. That gap
// between "comes back" and "comes back for free" is exactly what the tier
// ladder exists to express.
func TestModelCachesAreColdReloadAndOptIn(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{".cache/huggingface", ".cache/torch", ".cache/whisper",
		".lmstudio/models"} {
		mkdir(t, filepath.Join(home, d))
	}

	r := Build(Env{Home: home, Has: func(string) bool { return false }})

	for _, id := range []string{"hf-cache", "torch-hub", "whisper-models", "lmstudio-models"} {
		u, ok := r.Get(id)
		if !ok {
			t.Errorf("%s not registered", id)
			continue
		}
		if u.Tier != unit.TierColdReload {
			t.Errorf("%s: tier %v, want TierColdReload", id, u.Tier)
		}
		if u.Flag != "--models" {
			t.Errorf("%s: flag %q, want --models", id, u.Flag)
		}
		if !u.Reversible {
			t.Errorf("%s: marked lossy, but every file re-downloads", id)
		}
	}
}

// Emptying a model cache is the expensive half. Pruning detached and
// incomplete revisions is the cheap half, and it is the one that should run
// without being asked -- the same reason the catalog prefers "npm cache clean"
// to deleting the directory.
func TestHuggingfacePruneIsCheapAndUnflagged(t *testing.T) {
	r := Build(Env{Home: t.TempDir(), Has: func(bin string) bool { return bin == "hf" }})

	u, ok := r.Get("hf-prune")
	if !ok {
		t.Fatal("hf-prune not registered")
	}
	if u.Flag != "" {
		t.Errorf("flag %q: pruning dead revisions needs no permission", u.Flag)
	}
	if u.Tier != unit.TierPkgCache {
		t.Errorf("tier %v, want TierPkgCache", u.Tier)
	}
	if u.Kind != unit.KindCmd {
		t.Errorf("kind %v: the tool knows which revisions are dead, we do not", u.Kind)
	}
}

func TestHuggingfacePruneNeedsTheTool(t *testing.T) {
	r := Build(Env{Home: t.TempDir(), Has: func(string) bool { return false }})

	if _, ok := r.Get("hf-prune"); ok {
		t.Error("registered without the hf command")
	}
}

// Once the catalog names these, discovery must not claim them a second time.
// This is the other half of the fix for the flat-tier bug: a hardcoded unit
// carries a considered tier, and the generic scanner has to defer to it.
func TestDiscoveryCannotReclaimTheHuggingfaceCache(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, ".cache/huggingface")
	mkdir(t, cache)

	r := Build(Env{Home: home, Has: func(string) bool { return false }})

	if !r.Claimed(cache) {
		t.Fatal("the huggingface cache is unclaimed, so --discover would re-register it")
	}
}
