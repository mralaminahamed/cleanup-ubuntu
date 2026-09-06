//go:build darwin

package fsutil

import "testing"

// A real macOS run listed seven filesystems, six of which were APFS plumbing
// sharing one container -- so the same 320GB appeared seven times as though it
// were seven disks, and --auto could have picked Preboot as the most pressured
// and then found nothing there.
func TestSystemVolumesAreNotReported(t *testing.T) {
	for _, p := range []string{
		"/System/Volumes/VM",
		"/System/Volumes/Preboot",
		"/System/Volumes/Update",
		"/System/Volumes/xarts",
		"/System/Volumes/iSCPreboot",
		"/System/Volumes/Hardware",
	} {
		if !isSystemVolume(p) {
			t.Errorf("%q would be reported as a reclaimable filesystem", p)
		}
	}
}

// Data is where everything a user owns lives. Excluding it would leave the tool
// with no target at all.
func TestTheDataVolumeIsReported(t *testing.T) {
	if isSystemVolume("/System/Volumes/Data") {
		t.Fatal("the data volume was excluded")
	}
}

func TestOrdinaryMountsAreUnaffected(t *testing.T) {
	for _, p := range []string{"/", "/Volumes/Backup", "/private/tmp"} {
		if isSystemVolume(p) {
			t.Errorf("%q was excluded", p)
		}
	}
}

// The reported mounts must include the one holding home, and must not be a
// list of the same container repeated.
func TestMountsAreDistinctFilesystems(t *testing.T) {
	ms, err := Mounts()
	if err != nil {
		t.Fatalf("Mounts: %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("no mounts reported")
	}
	for _, m := range ms {
		if isSystemVolume(m.Path) {
			t.Errorf("APFS plumbing reported: %q", m.Path)
		}
	}
}
