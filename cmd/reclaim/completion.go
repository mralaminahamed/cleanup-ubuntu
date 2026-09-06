package main

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/mralaminahamed/reclaim/internal/catalog"
	"github.com/mralaminahamed/reclaim/internal/unitfile"
)

// The scripts are embedded rather than generated. There is no cobra here to
// inherit a generator from, and hand-written scripts can say things a generator
// would not -- the unit id completions call back into the binary, because which
// units exist depends on what is installed and a baked-in list would be wrong
// on the first machine that differs.
//
// The cost of hand-writing them is drift: a flag added to main.go silently
// stops being completable. A test walks "clean --help" and fails if any flag is
// missing from any of the three.
var (
	//go:embed completions/reclaim.bash
	bashCompletion string
	//go:embed completions/reclaim.zsh
	zshCompletion string
	//go:embed completions/reclaim.fish
	fishCompletion string
)

func cmdCompletion(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reclaim completion <bash|zsh|fish>")
		return 2
	}
	switch args[0] {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	default:
		fmt.Fprintf(os.Stderr, "unknown shell %q: expected bash, zsh or fish\n", args[0])
		return 2
	}
	return 0
}

// cmdUnits prints the unit ids this machine has, one per line, for the
// completion scripts to offer after --only and --exclude.
//
// Deliberately not in the usage text: it is a helper for the scripts, not part
// of the interface. It builds the catalog but never probes, so it stays fast
// enough to run on a keystroke.
func cmdUnits() int {
	home, _ := os.UserHomeDir()
	env := catalog.DefaultEnv(home)
	reg := catalog.Build(env)
	// A file-defined unit is as completable as a shipped one. A broken file is
	// not worth failing a completion over, so the error is dropped here.
	_ = unitfile.Add(reg, home, unitfile.DefaultFiles(home), env.Has)

	for _, u := range reg.All() {
		fmt.Println(u.ID)
	}
	return 0
}
