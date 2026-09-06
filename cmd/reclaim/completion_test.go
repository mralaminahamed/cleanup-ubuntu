package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestCompletionPrintsAScriptForEachShell(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		out, code := run(t, t.TempDir(), "completion", sh)
		if code != 0 {
			t.Errorf("completion %s exited %d:\n%s", sh, code, out)
		}
		if !strings.Contains(out, "reclaim") {
			t.Errorf("completion %s produced nothing usable:\n%s", sh, out)
		}
	}
}

func TestCompletionRejectsAnUnknownShell(t *testing.T) {
	out, code := run(t, t.TempDir(), "completion", "csh")

	if code == 0 {
		t.Fatalf("an unknown shell was accepted:\n%s", out)
	}
	if !strings.Contains(out, "bash") {
		t.Errorf("the error does not say what is supported:\n%s", out)
	}
}

func TestCompletionWithoutAShellIsAnError(t *testing.T) {
	if _, code := run(t, t.TempDir(), "completion"); code == 0 {
		t.Fatal("completion with no argument succeeded")
	}
}

// Unit ids are the arguments nobody can be expected to remember, and they
// change with what is installed. The scripts ask the binary rather than
// carrying a list that would go stale.
func TestUnitsListsIdsOnePerLine(t *testing.T) {
	home := fixtureHome(t)
	out, code := run(t, home, "__units")

	if code != 0 {
		t.Fatalf("__units exited %d:\n%s", code, out)
	}
	ids := strings.Fields(out)
	if len(ids) == 0 {
		t.Fatal("no unit ids")
	}
	for _, want := range []string{"pip-cache", "npm-cacache"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, out)
		}
	}
	if strings.Contains(out, " ") && !strings.Contains(out, "\n") {
		t.Error("ids are not one per line")
	}
}

// The hidden command is a completion helper, not part of the interface.
func TestUnitsIsNotAdvertised(t *testing.T) {
	out, _ := run(t, t.TempDir(), "--help")

	if strings.Contains(out, "__units") {
		t.Errorf("a helper is listed as a command:\n%s", out)
	}
	if !strings.Contains(out, "completion") {
		t.Errorf("completion is not listed as a command:\n%s", out)
	}
}

// The scripts are embedded, so a flag added to main.go stops being completable
// without anything failing. This is the thing that notices.
func TestEveryCleanFlagAppearsInEveryCompletionScript(t *testing.T) {
	flagLine := regexp.MustCompile(`(?m)^\s+-([a-z0-9-]+)`)

	var flags []string
	// Every subcommand that has flags, not only clean: analyze grew three and
	// nothing would have noticed.
	for _, sub := range []string{"clean", "analyze"} {
		help, _ := run(t, t.TempDir(), sub, "--help")
		for _, m := range flagLine.FindAllStringSubmatch(help, -1) {
			// Short flags are spelled differently again in each shell and are
			// not worth a third rule.
			if len(m[1]) > 1 {
				flags = append(flags, m[1])
			}
		}
	}
	if len(flags) < 10 {
		t.Fatalf("only found %d flags across --help output, parsing is wrong: %v", len(flags), flags)
	}

	// Each shell spells a long option its own way. fish declares "-l apply",
	// bash and zsh carry the literal "--apply", and asserting one syntax
	// everywhere would only be testing that they all look like bash.
	offers := map[string]func(string) string{
		"bash": func(f string) string { return "--" + f },
		"zsh":  func(f string) string { return "--" + f },
		"fish": func(f string) string { return "-l " + f },
	}
	for sh, spelling := range offers {
		script, _ := run(t, t.TempDir(), "completion", sh)
		for _, f := range flags {
			if !strings.Contains(script, spelling(f)) {
				t.Errorf("%s completion does not offer --%s", sh, f)
			}
		}
	}
}
