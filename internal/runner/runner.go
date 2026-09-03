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

	"github.com/mralaminahamed/reclaim/internal/fsutil"
	"github.com/mralaminahamed/reclaim/internal/unit"
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
	// RootCheck acquires elevated privileges, once per run, before any unit
	// that needs them. Injectable so the skip path is testable without sudo.
	RootCheck func() error

	// StoppedEarly records that the target was met before the plan ran out.
	StoppedEarly bool

	// rootOnce caches the elevation attempt: three system units must not mean
	// three password prompts.
	rootChecked bool
	rootErr     error
}

// ensureRoot acquires elevation on first use and reuses the outcome after.
func (r *Runner) ensureRoot() error {
	if r.rootChecked {
		return r.rootErr
	}
	r.rootChecked = true
	check := r.RootCheck
	if check == nil {
		check = sudoValidate
	}
	r.rootErr = check()
	return r.rootErr
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
		// Ask for elevation before running, so a missing password is reported
		// as exactly that rather than as an unexplained non-zero exit.
		if u.NeedsRoot {
			if err := r.ensureRoot(); err != nil {
				return 0, fmt.Errorf("needs root: %w", err)
			}
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

// shellRun executes a unit's command, folding its output into any error.
// "exit status 1" on its own tells the user nothing they can act on.
func shellRun(cmd string) error {
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err == nil {
		return nil
	}
	if msg := firstLine(string(out)); msg != "" {
		return fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

// sudoValidate refreshes the sudo timestamp, prompting on the terminal if a
// password is needed. Stdin and stderr are connected precisely so that prompt
// can be seen and answered.
func sudoValidate() error {
	c := exec.Command("sudo", "-v")
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("could not acquire root via sudo: %w", err)
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
