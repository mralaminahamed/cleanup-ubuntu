package lock

import (
	"regexp"
	"testing"

	"github.com/mralaminahamed/cleanup-ubuntu/internal/unit"
)

func TestApplyLocksUnitUnderRootOfRunningApp(t *testing.T) {
	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "jb", Kind: unit.KindPaths, Paths: []string{"/home/u/.cache/JetBrains"}})
	r.Add(&unit.Unit{ID: "npm", Kind: unit.KindPaths, Paths: []string{"/home/u/.npm/_cacache"}})

	rules := []Rule{{
		Name:    "jetbrains-ide",
		Pattern: regexp.MustCompile(`/idea|pycharm`),
		Roots:   []string{"/home/u/.cache/JetBrains"},
	}}
	Apply(r, rules, []Process{{PID: 4242, Cmd: "/opt/idea/bin/idea.sh"}})

	jb, _ := r.Get("jb")
	if jb.LockedBy == "" {
		t.Error("unit under a running IDE's root was not locked")
	}
	if jb.PID != 4242 {
		t.Errorf("PID = %d, want 4242", jb.PID)
	}
	npm, _ := r.Get("npm")
	if npm.LockedBy != "" {
		t.Errorf("unrelated unit locked by %q", npm.LockedBy)
	}
}

func TestApplyLeavesEverythingFreeWhenNothingRuns(t *testing.T) {
	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "jb", Kind: unit.KindPaths, Paths: []string{"/home/u/.cache/JetBrains"}})
	rules := []Rule{{
		Name:    "jetbrains-ide",
		Pattern: regexp.MustCompile(`/idea`),
		Roots:   []string{"/home/u/.cache/JetBrains"},
	}}
	Apply(r, rules, []Process{{PID: 1, Cmd: "/usr/bin/bash"}})

	jb, _ := r.Get("jb")
	if jb.LockedBy != "" {
		t.Errorf("locked by %q with no matching process", jb.LockedBy)
	}
}

func TestApplyMatchesNestedPaths(t *testing.T) {
	// A unit deeper than the lock root must still be locked, or a running IDE
	// would keep its top-level cache while a subdirectory got deleted.
	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "deep", Kind: unit.KindPaths,
		Paths: []string{"/home/u/.gradle/caches/modules-2"}})
	rules := []Rule{{
		Name:    "gradle",
		Pattern: regexp.MustCompile(`GradleDaemon`),
		Roots:   []string{"/home/u/.gradle"},
	}}
	Apply(r, rules, []Process{{PID: 7, Cmd: "java -cp x GradleDaemon 8.5"}})

	deep, _ := r.Get("deep")
	if deep.LockedBy != "gradle" {
		t.Errorf("LockedBy = %q, want gradle", deep.LockedBy)
	}
}

func TestApplyDoesNotLockSiblingPrefix(t *testing.T) {
	// "/home/u/.gradle-backup" must not be treated as inside "/home/u/.gradle".
	r := unit.NewRegistry()
	r.Add(&unit.Unit{ID: "sibling", Kind: unit.KindPaths,
		Paths: []string{"/home/u/.gradle-backup"}})
	rules := []Rule{{
		Name:    "gradle",
		Pattern: regexp.MustCompile(`GradleDaemon`),
		Roots:   []string{"/home/u/.gradle"},
	}}
	Apply(r, rules, []Process{{PID: 7, Cmd: "GradleDaemon"}})

	s, _ := r.Get("sibling")
	if s.LockedBy != "" {
		t.Errorf("sibling path locked by %q", s.LockedBy)
	}
}

func TestDefaultRulesCoverKnownApps(t *testing.T) {
	rules := DefaultRules("/home/u")
	need := []string{"jetbrains-ide", "android-studio", "chrome", "firefox", "gradle", "vscode"}
	have := map[string]bool{}
	for _, r := range rules {
		have[r.Name] = true
	}
	for _, n := range need {
		if !have[n] {
			t.Errorf("DefaultRules missing a rule for %q", n)
		}
	}
}

func TestGradleRuleMatchesDaemonCommandLine(t *testing.T) {
	var gradle *Rule
	for i, r := range DefaultRules("/home/u") {
		if r.Name == "gradle" {
			gradle = &DefaultRules("/home/u")[i]
		}
	}
	if gradle == nil {
		t.Fatal("no gradle rule")
	}
	if !gradle.Pattern.MatchString("java -cp /home/u/.gradle/wrapper/dists GradleDaemon 8.5") {
		t.Error("gradle rule does not match a real GradleDaemon command line")
	}
}

func TestEveryDefaultRuleHasRoots(t *testing.T) {
	// A rule with no roots can never lock anything, so it is silently useless.
	for _, r := range DefaultRules("/home/u") {
		if len(r.Roots) == 0 {
			t.Errorf("rule %q has no roots", r.Name)
		}
		if r.Pattern == nil {
			t.Errorf("rule %q has no pattern", r.Name)
		}
	}
}
