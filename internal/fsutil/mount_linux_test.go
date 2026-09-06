//go:build linux

package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A bind-mounted file is an entry in /proc/mounts with a real filesystem type,
// so nothing in the pseudo-filesystem filter catches it. In a container that
// puts /etc/hosts and /etc/resolv.conf in "reclaim status" as though they were
// disks, each reporting the size of the filesystem underneath them.
//
// Found by running the tool inside a Fedora container while checking which
// distributions it supports.
func TestMountsExcludesBindMountedFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "hosts")
	if err := os.WriteFile(file, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	table := "/dev/sda1 " + dir + " ext4 rw 0 0\n" +
		"/dev/sda1 " + file + " ext4 rw 0 0\n"

	got, err := parseMounts(strings.NewReader(table))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range got {
		if m.Path == file {
			t.Fatalf("a bind-mounted file was reported as a filesystem: %+v", m)
		}
	}
	if len(got) != 1 || got[0].Path != dir {
		t.Fatalf("the real mount was dropped too: %+v", got)
	}
}
