//go:build darwin

package fsutil

import "syscall"

// mountPointOf asks the filesystem directly.
//
// The device-number walk that works on Linux does not work here. macOS splits
// the boot disk into a read-only system volume and a writable data volume, and
// presents the data volume through firmlinks -- /Users is really
// /System/Volumes/Data/Users -- with both halves reporting the same device
// number, deliberately, so the split is invisible. Walking up from a home
// directory therefore never sees the device change and answers "/", which is
// the sealed read-only volume: not where the files are, and not somewhere
// anything can be reclaimed.
//
// statfs knows the real answer, so ask it rather than infer.
func mountPointOf(abs string) string {
	var st syscall.Statfs_t
	if err := syscall.Statfs(abs, &st); err != nil {
		return "/"
	}
	if p := cstring(st.Mntonname[:]); p != "" {
		return p
	}
	return "/"
}
