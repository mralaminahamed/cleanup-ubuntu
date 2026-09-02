package fsutil

import "testing"

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
