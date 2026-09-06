package catalog

import (
	"path/filepath"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// The macOS units are registered from the same table on every platform: the
// paths do not exist on Linux, so nothing registers there, and the tier and
// reversibility decisions are written down once rather than per platform.
//
// That is what lets these be tested from a Linux machine at all -- the fixture
// home supplies the paths, and the decisions under test are the ones that
// matter and are platform-independent.
func TestXcodeDerivedDataIsReclaimable(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, "Library/Developer/Xcode/DerivedData"))

	r := Build(Env{Home: home, Has: func(string) bool { return false }})

	u, ok := r.Get("xcode-derived-data")
	if !ok {
		t.Fatal("xcode-derived-data not registered")
	}
	if !u.Reversible {
		t.Error("derived data is rebuilt by a build; it is not lost")
	}
	if u.Tier != unit.TierColdReload {
		t.Errorf("tier %v: a full rebuild is a cold reload, not a free tier", u.Tier)
	}
}

// Archives are the built, signed artifacts of past releases. Xcode cannot
// recreate one from anything left on disk, and people keep them precisely
// because they cannot.
func TestXcodeArchivesAreLossy(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, "Library/Developer/Xcode/Archives"))

	r := Build(Env{Home: home, Has: func(string) bool { return false }})

	u, ok := r.Get("xcode-archives")
	if !ok {
		t.Fatal("xcode-archives not registered")
	}
	if u.Reversible {
		t.Fatal("an archive is the only copy of a shipped build")
	}
	if u.Tier != unit.TierIrreplaceable {
		t.Errorf("tier %v, want TierIrreplaceable", u.Tier)
	}
}

func TestMacDeveloperCachesAreRegistered(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{
		"Library/Developer/Xcode/iOS DeviceSupport",
		"Library/Developer/CoreSimulator/Caches",
		"Library/Caches/Homebrew",
		"Library/Caches/CocoaPods",
		"Library/Caches/org.swift.swiftpm",
		"Library/Caches/com.apple.dt.Xcode",
	} {
		mkdir(t, filepath.Join(home, d))
	}

	r := Build(Env{Home: home, Has: func(string) bool { return false }})

	for _, id := range []string{"ios-device-support", "coresimulator-caches",
		"homebrew-cache", "cocoapods-cache", "swiftpm-cache", "xcode-cache"} {
		if _, ok := r.Get(id); !ok {
			t.Errorf("%s not registered", id)
		}
	}
}

// ~/Library/Application Support is where macOS applications keep their real
// state -- the analogue of a Chromium profile, not of ~/.cache. Nothing in the
// catalog may claim it.
func TestApplicationSupportIsNeverClaimed(t *testing.T) {
	home := t.TempDir()
	appSupport := filepath.Join(home, "Library/Application Support")
	mkdir(t, filepath.Join(appSupport, "Slack"))

	r := Build(Env{Home: home, Has: func(string) bool { return false }})

	for _, u := range r.All() {
		for _, p := range u.Paths {
			if p == appSupport {
				t.Fatalf("unit %q claims Application Support itself", u.ID)
			}
		}
	}
}

// A simulator runtime is a multi-gigabyte download, and the devices directory
// holds installed apps and their data rather than a cache.
func TestSimulatorDevicesAreOptInAndLossy(t *testing.T) {
	home := t.TempDir()
	mkdir(t, filepath.Join(home, "Library/Developer/CoreSimulator/Devices"))

	r := Build(Env{Home: home, Has: func(string) bool { return false }})

	u, ok := r.Get("coresimulator-devices")
	if !ok {
		t.Fatal("coresimulator-devices not registered")
	}
	if u.Flag == "" {
		t.Error("wiping simulators should require asking for it")
	}
	if u.Reversible {
		t.Error("a simulator holds installed apps and their data")
	}
}

// brew knows its own layout: which downloads are stale, which old versions are
// still linked. Same reason the catalog prefers "npm cache clean" to deleting
// the directory.
func TestBrewCleanupIsANativeCommand(t *testing.T) {
	r := Build(Env{Home: t.TempDir(), Has: func(bin string) bool { return bin == "brew" }})

	u, ok := r.Get("brew-native")
	if !ok {
		t.Fatal("brew-native not registered")
	}
	if u.Kind != unit.KindCmd {
		t.Errorf("kind %v, want KindCmd", u.Kind)
	}
	if u.Flag != "" {
		t.Errorf("flag %q: a cache clean needs no permission", u.Flag)
	}
}

func TestBrewIsAbsentWithoutHomebrew(t *testing.T) {
	r := Build(Env{Home: t.TempDir(), Has: func(string) bool { return false }})
	if _, ok := r.Get("brew-native"); ok {
		t.Error("registered without brew installed")
	}
}
