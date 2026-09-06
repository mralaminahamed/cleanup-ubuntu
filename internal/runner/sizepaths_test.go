package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// SizePaths exist so a command unit can report a real number. They are an
// answer to "how much would this free", not an instruction. Nothing here may
// treat them as a deletion list -- the kernel unit names /boot and /lib/modules
// that way, and apt is what is supposed to remove those.
func TestRunnerNeverDeletesSizePaths(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "blob")
	if err := os.WriteFile(keep, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Runner{Apply: true}
	r.Run([]*unit.Unit{{
		ID: "cmd", Kind: unit.KindCmd, Command: "true",
		SizePaths: []string{dir},
	}})

	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("a size path was deleted: %v", err)
	}
}
