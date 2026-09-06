package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPressureBands(t *testing.T) {
	cases := []struct {
		pct  int
		want Pressure
	}{
		{10, PressureLow}, {70, PressureLow},
		{75, PressureModerate}, {84, PressureModerate},
		{85, PressureHigh}, {94, PressureHigh},
		{95, PressureCritical}, {100, PressureCritical},
	}
	for _, c := range cases {
		if got := PressureOf(c.pct); got != c.want {
			t.Errorf("PressureOf(%d) = %v, want %v", c.pct, got, c.want)
		}
	}
}

func TestMountsReportsRealFilesystems(t *testing.T) {
	ms, err := Mounts()
	if err != nil {
		t.Skip("no mount table here")
	}
	if len(ms) == 0 {
		t.Fatal("no mounts reported")
	}
	var sawRoot bool
	for _, m := range ms {
		if m.Path == "/" {
			sawRoot = true
		}
		if m.Total <= 0 {
			t.Errorf("mount %q has non-positive total %d", m.Path, m.Total)
		}
		if m.UsedPct < 0 || m.UsedPct > 100 {
			t.Errorf("mount %q has impossible usage %d%%", m.Path, m.UsedPct)
		}
	}
	if !sawRoot {
		t.Error("root filesystem missing from Mounts()")
	}
}

func TestMountsExcludesPseudoFilesystems(t *testing.T) {
	// tmpfs and loop devices are not real reclaimable space; reporting them
	// would send the planner chasing filesystems it cannot help.
	ms, err := Mounts()
	if err != nil {
		t.Skip("no mount table here")
	}
	for _, m := range ms {
		switch m.Device {
		case "tmpfs", "devtmpfs", "efivarfs", "none", "proc", "sysfs":
			t.Errorf("pseudo filesystem %q (%s) reported", m.Path, m.Device)
		}
	}
}

func TestWorstMountIsTheMostPressured(t *testing.T) {
	ms := []Mount{
		{Path: "/", UsedPct: 40},
		{Path: "/home", UsedPct: 97},
		{Path: "/data", UsedPct: 80},
	}
	if got := Worst(ms); got.Path != "/home" {
		t.Errorf("Worst() = %q, want /home", got.Path)
	}
}

func TestWorstOfNothingIsZeroValue(t *testing.T) {
	if got := Worst(nil); got.Path != "" {
		t.Errorf("Worst(nil) = %+v, want zero value", got)
	}
}

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
