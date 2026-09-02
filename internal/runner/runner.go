// Package runner executes a plan.
//
// Dry run is the default everywhere: a Runner with Apply false measures and
// reports but never removes a byte or executes a command.
package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mralaminahamed/cleanup-ubuntu/internal/fsutil"
	"github.com/mralaminahamed/cleanup-ubuntu/internal/unit"
)

// Result is the outcome of one unit.
type Result struct {
	Unit  *unit.Unit
	Freed int64
	Err   error
}

// Runner executes units in order.
type Runner struct {
	// Apply switches from reporting to actually deleting.
	Apply bool

	// TargetBytes, when non-zero, stops the run as soon as TargetPath has that
	// much free space.
	TargetBytes int64
	TargetPath  string

	// Avail reports free space; injectable so the early-stop logic is testable
	// without filling a real disk.
	Avail func(string) (int64, error)
	// Exec runs a command unit; injectable for the same reason.
	Exec func(string) error

	// StoppedEarly records that the target was met before the plan ran out.
	StoppedEarly bool
}

// Run executes units in the order given, re-checking free space after each one
// so it can stop as soon as the target is met.
//
// A failing unit is recorded and the run continues: one broken cache directory
// must not strand the rest of the cleanup.
func (r *Runner) Run(units []*unit.Unit) []Result {
	var out []Result
	for _, u := range units {
		freed, err := r.runOne(u)
		out = append(out, Result{Unit: u, Freed: freed, Err: err})

		if r.TargetBytes > 0 && r.targetMet() {
			r.StoppedEarly = true
			break
		}
	}
	return out
}

func (r *Runner) targetMet() bool {
	avail := r.Avail
	if avail == nil {
		avail = fsutil.AvailBytes
	}
	path := r.TargetPath
	if path == "" {
		path = "/"
	}
	n, err := avail(path)
	if err != nil {
		return false
	}
	return n >= r.TargetBytes
}

func (r *Runner) runOne(u *unit.Unit) (int64, error) {
	if u.Kind == unit.KindCmd {
		if !r.Apply {
			return u.Bytes, nil
		}
		run := r.Exec
		if run == nil {
			run = shellRun
		}
		if err := run(u.Command); err != nil {
			return 0, err
		}
		return u.Bytes, nil
	}

	var freed int64
	for _, p := range u.Paths {
		if p == "" {
			continue
		}
		if err := checkSafe(p); err != nil {
			return freed, err
		}
		n, _ := fsutil.PathBytes(p)
		if !r.Apply {
			freed += n
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return freed, err
		}
		freed += n
	}
	return freed, nil
}

// protected are paths that no unit may ever delete, whatever it claims. This is
// a backstop against a malformed unit definition, not a substitute for getting
// unit paths right.
var protected = map[string]bool{
	"/": true, "/home": true, "/usr": true, "/etc": true, "/var": true,
	"/bin": true, "/sbin": true, "/lib": true, "/boot": true, "/opt": true,
	"/root": true, "/srv": true, "/proc": true, "/sys": true, "/dev": true,
}

// checkSafe refuses obviously catastrophic targets: the filesystem root, a
// top-level system directory, or the user's home itself.
func checkSafe(p string) error {
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("refusing to delete a relative path %q", p)
	}
	if protected[clean] {
		return fmt.Errorf("refusing to delete protected path %q", clean)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if clean == filepath.Clean(home) {
			return fmt.Errorf("refusing to delete the home directory %q", clean)
		}
	}
	// Depth two keeps "/home/user" and "/var/lib" safe while allowing the
	// caches that live below them.
	if strings.Count(clean, string(filepath.Separator)) < 2 {
		return fmt.Errorf("refusing to delete top-level path %q", clean)
	}
	return nil
}

func shellRun(cmd string) error {
	c := exec.Command("sh", "-c", cmd)
	c.Stdout = nil
	c.Stderr = nil
	return c.Run()
}
