//go:build linux

package fsutil

import "path/filepath"

// mountPointOf walks up until the device number changes.
//
// Each mount is a distinct device on Linux, so the first ancestor with a
// different one is the mount point.
func mountPointOf(abs string) string {
	dev, err := deviceOf(abs)
	if err != nil {
		return "/"
	}
	for abs != "/" {
		parent := filepath.Dir(abs)
		pdev, err := deviceOf(parent)
		if err != nil || pdev != dev {
			return abs
		}
		abs = parent
	}
	return "/"
}
