// Package fsutil measures disk usage and resolves mounts.
//
// Sizes are apparent file sizes summed over a tree, matching what the bash
// version reported via du. Nothing here mutates the filesystem.
package fsutil

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

var sizeRe = regexp.MustCompile(`^([0-9]+)([KMGT]?)B?$`)

// ParseSize turns "12G" into a byte count. Units are binary (1K = 1024).
//
// Leading zeros are decimal: "08G" is eight gigabytes. The bash implementation
// passed these digits to shell arithmetic, which treated them as octal and
// failed outright on 08 and 09.
func ParseSize(s string) (int64, error) {
	m := sizeRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil {
		return 0, fmt.Errorf("bad size %q: want digits with an optional K/M/G/T suffix", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad size %q: %w", s, err)
	}
	mult := int64(1)
	switch m[2] {
	case "K":
		mult = 1 << 10
	case "M":
		mult = 1 << 20
	case "G":
		mult = 1 << 30
	case "T":
		mult = 1 << 40
	}
	return n * mult, nil
}

// Human renders a byte count with binary units, one decimal place above KiB.
func Human(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// PathBytes sums the apparent size of everything under path.
//
// A missing path is not an error: units routinely list paths for tools that are
// not installed on this machine, and those simply contribute nothing.
// Symlinks are counted as links, never followed, so a link cannot drag the
// measurement out of the tree being examined.
func PathBytes(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}

	var total int64
	err = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable subtrees are skipped rather than failing the probe:
			// a root-owned directory inside a user cache is common and must
			// not abort the whole run.
			return nil //nolint:nilerr
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr
		}
		total += fi.Size()
		return nil
	})
	if err != nil {
		return total, err
	}
	return total, nil
}

// AvailBytes reports free space on the filesystem holding path, measured as
// space available to an unprivileged user.
func AvailBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// MountOf returns the mount point of the filesystem holding path.
//
// How that is answered differs by platform, and getting it wrong is not
// cosmetic: this is what --auto and --free aim at, so a wrong answer means a
// run with no target.
func MountOf(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "/"
	}
	// Walk up to the first existing ancestor: a unit may name a path that is
	// already gone, and its mount still matters for planning.
	for {
		if _, err := os.Stat(abs); err == nil {
			break
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "/"
		}
		abs = parent
	}

	return mountPointOf(abs)
}

func deviceOf(path string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Dev), nil
}
