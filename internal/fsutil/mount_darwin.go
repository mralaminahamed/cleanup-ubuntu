//go:build darwin

// Mount enumeration on macOS, from getfsstat(2).
//
// No /proc to read, and no need for one: the kernel returns the whole table in
// a single call, already carrying the block counts that /proc/mounts needs a
// separate statfs for. Nothing here is cgo, so the darwin build stays as
// dependency-free as the Linux one.

package fsutil

import (
	"os"
	"strings"
	"syscall"
)

// mntNoWait asks for cached figures rather than making every filesystem update
// its statistics first. A network mount that has gone away must not hang a
// disk report.
//
// Spelled out because package syscall exports neither of these on darwin. Both
// are stable ABI, from sys/mount.h.
const (
	mntNoWait   = 2
	mntReadOnly = 0x1
)

// darwinPseudoFS are filesystem types that hold no reclaimable space.
//
// The list is shaped differently from the Linux one. "devfs" and "autofs" are
// the obvious analogues, and the important addition is the read-only system
// volume: on Apple Silicon "/" is a sealed, signed snapshot, and reporting it
// as a place to reclaim space would be wrong in a way the user cannot act on.
var darwinPseudoFS = map[string]bool{
	"devfs": true, "autofs": true, "map auto_home": true, "kernfs": true,
	"fdesc": true, "procfs": true, "volfs": true, "nullfs": true,
}

// Mounts lists the filesystems that could hold reclaimable space.
func Mounts() ([]Mount, error) {
	n, err := syscall.Getfsstat(nil, mntNoWait)
	if err != nil {
		return nil, err
	}
	buf := make([]syscall.Statfs_t, n)
	if _, err := syscall.Getfsstat(buf, mntNoWait); err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []Mount
	for _, fs := range buf {
		path := cstring(fs.Mntonname[:])
		fstype := cstring(fs.Fstypename[:])
		if path == "" || darwinPseudoFS[fstype] || seen[path] {
			continue
		}
		// A read-only volume cannot be reclaimed from, whatever it holds. On
		// Apple Silicon that covers the sealed system volume.
		if fs.Flags&mntReadOnly != 0 {
			continue
		}
		// The rest of /System/Volumes is APFS plumbing -- VM, Preboot, Update,
		// xarts, iSCPreboot, Hardware. They share one container, so each
		// reports the whole disk's size, and a real run lists the same 320GB
		// seven times as though they were seven disks. Worse, --auto would
		// happily pick Preboot as the most pressured filesystem and then find
		// nothing there to reclaim.
		//
		// Data is the exception and the only one that matters: it is where
		// everything a user owns lives.
		if isSystemVolume(path) {
			continue
		}
		// The same rule the Linux reader applies: an entry whose mount point is
		// not a directory is not a filesystem.
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			continue
		}

		total := int64(fs.Bsize) * int64(fs.Blocks)
		if total <= 0 {
			continue
		}
		avail := int64(fs.Bsize) * int64(fs.Bavail)
		used := total - int64(fs.Bsize)*int64(fs.Bfree)

		seen[path] = true
		out = append(out, Mount{
			Device:  cstring(fs.Mntfromname[:]),
			Path:    path,
			Total:   total,
			Avail:   avail,
			UsedPct: int(used * 100 / total),
		})
	}
	return out, nil
}

// cstring reads a NUL-terminated C string out of a fixed-width field.
func cstring(b []int8) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
}

// isSystemVolume reports whether path is APFS plumbing rather than a volume
// anything can be reclaimed from.
func isSystemVolume(path string) bool {
	const prefix = "/System/Volumes/"
	return strings.HasPrefix(path, prefix) && path != "/System/Volumes/Data"
}
