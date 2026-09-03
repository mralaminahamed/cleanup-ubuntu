package system

import (
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

func TestUnitsRequireTheTool(t *testing.T) {
	r := unit.NewRegistry()
	Add(r, Env{Has: func(bin string) bool { return bin == "apt-get" }})

	if _, ok := r.Get("system-apt"); !ok {
		t.Error("apt unit missing though apt-get is installed")
	}
	if _, ok := r.Get("system-snaps"); ok {
		t.Error("snap unit registered though snap is absent")
	}
}

func TestAllSystemUnitsAreOptIn(t *testing.T) {
	// These need root and touch shared system state, so none may run just
	// because the tier ceiling allows it.
	r := unit.NewRegistry()
	Add(r, Env{Has: func(string) bool { return true }})
	if len(r.All()) == 0 {
		t.Fatal("no system units registered")
	}
	for _, u := range r.All() {
		if u.Flag != "--system" {
			t.Errorf("unit %q has flag %q, want --system", u.ID, u.Flag)
		}
		if u.Kind != unit.KindCmd {
			t.Errorf("unit %q should be a command unit", u.ID)
		}
		if u.MountHint != "/" {
			t.Errorf("unit %q hints mount %q, want /", u.ID, u.MountHint)
		}
	}
}

func TestSystemUnitsDeclareThatTheyNeedRoot(t *testing.T) {
	// Without this the runner cannot ask for a password up front, and each unit
	// fails separately with an unexplained non-zero exit.
	r := unit.NewRegistry()
	Add(r, Env{Has: func(string) bool { return true }})
	for _, u := range r.All() {
		if !u.NeedsRoot {
			t.Errorf("unit %q does not declare NeedsRoot", u.ID)
		}
	}
}

func TestSnapRemovalPropagatesFailure(t *testing.T) {
	// A while loop at the end of a pipeline exits 0 even when every command
	// inside it failed, so snap removal used to fail completely silently.
	r := unit.NewRegistry()
	Add(r, Env{Has: func(string) bool { return true }})
	u, ok := r.Get("system-snaps")
	if !ok {
		t.Fatal("snap unit missing")
	}
	if !contains(u.Command, "|| exit") {
		t.Errorf("snap command swallows failures: %q", u.Command)
	}
}

func TestJournalVacuumIsBounded(t *testing.T) {
	// An unbounded vacuum would delete the entire journal. It must keep a
	// window of history.
	r := unit.NewRegistry()
	Add(r, Env{Has: func(string) bool { return true }, JournalKeep: "200M"})
	u, ok := r.Get("system-journal")
	if !ok {
		t.Fatal("journal unit missing")
	}
	if u.Command == "" || !contains(u.Command, "200M") {
		t.Errorf("journal command %q does not honour the keep size", u.Command)
	}
}

func TestAptUnitDoesNotAutoremoveWithoutConsent(t *testing.T) {
	// autoremove can pull out kernels and packages the user still wants, so the
	// default must be the cache clean only.
	r := unit.NewRegistry()
	Add(r, Env{Has: func(string) bool { return true }})
	u, _ := r.Get("system-apt")
	if contains(u.Command, "autoremove") {
		t.Errorf("default apt command runs autoremove: %q", u.Command)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
