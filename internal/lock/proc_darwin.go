//go:build darwin

package lock

import (
	"os"
	"os/exec"
)

// Running reads the process table with ps.
//
// macOS has no /proc. The alternative to ps is sysctl with KERN_PROC_ALL, which
// means decoding kinfo_proc out of raw bytes -- a struct whose layout is not
// part of any stable interface and has changed between releases. Getting that
// wrong would not fail loudly; it would return plausible nonsense, and this is
// the table that decides whether a running application's cache is safe to
// delete. ps is a documented interface, present on every Mac, and the cost is
// one fork per run.
func Running() []Process {
	// -A is every process; -o with a trailing = suppresses the header, which
	// would otherwise parse as a process called "COMMAND".
	out, err := exec.Command("ps", "-Ao", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	return parsePS(string(out), os.Getpid(), os.Getppid())
}
