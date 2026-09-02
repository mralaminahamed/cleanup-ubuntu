// Package lock refuses to clean caches belonging to applications that are
// currently running.
//
// Deleting a live IDE's cache corrupts the session it is in the middle of, so
// this is a safety gate, not an optimisation. Processes are passed in rather
// than read directly, which keeps the matching logic testable without spawning
// real applications.
package lock

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// Process is one running process as seen in the process table.
type Process struct {
	PID int
	Cmd string
}

// Rule ties a process signature to the directories that process owns.
type Rule struct {
	Name    string
	Pattern *regexp.Regexp
	Roots   []string
}

// Apply marks every unit whose paths sit under the roots of a currently running
// application. Marked units are skipped by the planner and reported so the user
// can quit the app and re-run.
func Apply(r *unit.Registry, rules []Rule, procs []Process) {
	for _, rule := range rules {
		proc, running := firstMatch(rule, procs)
		if !running {
			continue
		}
		for _, u := range r.All() {
			if u.LockedBy != "" || u.Kind != unit.KindPaths {
				continue
			}
			if anyPathUnder(u.Paths, rule.Roots) {
				u.LockedBy = rule.Name
				u.PID = proc.PID
			}
		}
	}
}

func firstMatch(rule Rule, procs []Process) (Process, bool) {
	if rule.Pattern == nil {
		return Process{}, false
	}
	for _, p := range procs {
		if rule.Pattern.MatchString(p.Cmd) {
			return p, true
		}
	}
	return Process{}, false
}

// anyPathUnder reports whether any path is the root itself or sits inside it.
// The separator check stops "/x/.gradle-backup" from counting as inside
// "/x/.gradle".
func anyPathUnder(paths, roots []string) bool {
	for _, p := range paths {
		cp := filepath.Clean(p)
		for _, root := range roots {
			cr := filepath.Clean(root)
			if cp == cr || strings.HasPrefix(cp, cr+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// DefaultRules returns the built-in lock table for a given home directory.
func DefaultRules(home string) []Rule {
	j := func(rel ...string) []string {
		out := make([]string, 0, len(rel))
		for _, x := range rel {
			out = append(out, filepath.Join(home, x))
		}
		return out
	}
	jetbrainsRoots := j(".cache/JetBrains", ".config/JetBrains", ".local/share/JetBrains")

	return []Rule{
		// Android Studio drives Gradle, so its lock has to cover the gradle home
		// as well as the IDE's own caches.
		{"android-studio", regexp.MustCompile(`android-studio|/studio\.sh|/studio64|bin/studio( |$)`),
			append(append([]string{}, jetbrainsRoots...), filepath.Join(home, ".gradle"))},
		{"jetbrains-ide", regexp.MustCompile(`jetbrains|/idea|pycharm|phpstorm|webstorm|goland|clion|rider|rubymine|datagrip`),
			jetbrainsRoots},
		{"gradle", regexp.MustCompile(`GradleDaemon|/gradlew( |$)|KotlinCompileDaemon`),
			j(".gradle")},
		{"zed", regexp.MustCompile(`/zed( |$)|/zed-editor`), j(".local/share/zed", ".cache/zed")},
		{"vscode", regexp.MustCompile(`/code( |$)|/code-insiders|/cursor( |$)`),
			j(".config/Code", ".config/Cursor", ".cache/Code")},
		{"chrome", regexp.MustCompile(`/chrome( |$)|/chromium( |$)`),
			j(".cache/google-chrome", ".config/google-chrome", ".cache/Google")},
		{"brave", regexp.MustCompile(`/brave( |$)|brave-browser`),
			j(".cache/BraveSoftware", ".config/BraveSoftware")},
		{"firefox", regexp.MustCompile(`/firefox( |$)`), j(".cache/mozilla", ".mozilla")},
		{"slack", regexp.MustCompile(`/slack( |$)`), j(".config/Slack")},
		{"discord", regexp.MustCompile(`/discord( |$)|/Discord( |$)`), j(".config/discord")},
		{"postman", regexp.MustCompile(`/postman( |$)|/Postman( |$)`), j(".config/Postman")},
		{"figma", regexp.MustCompile(`figma-linux|/figma( |$)`), j(".config/figma-linux")},
	}
}
