package fsutil

import (
	"bufio"
	"io"
	"os"
	"strings"
	"syscall"
)

// Pressure classifies how full a filesystem is. The planner uses it to decide
// how hard to try without being told an explicit target.
type Pressure int

const (
	PressureLow Pressure = iota
	PressureModerate
	PressureHigh
	PressureCritical
)

func (p Pressure) String() string {
	switch p {
	case PressureCritical:
		return "critical"
	case PressureHigh:
		return "high"
	case PressureModerate:
		return "moderate"
	default:
		return "low"
	}
}

// PressureOf maps a percentage used to a band.
func PressureOf(pct int) Pressure {
	switch {
	case pct >= 95:
		return PressureCritical
	case pct >= 85:
		return PressureHigh
	case pct >= 75:
		return PressureModerate
	default:
		return PressureLow
	}
}

// Mount is one real filesystem.
type Mount struct {
	Device  string
	Path    string
	Total   int64
	Avail   int64
	UsedPct int
}

// Pressure returns this mount's band.
func (m Mount) Pressure() Pressure { return PressureOf(m.UsedPct) }

// pseudo filesystems hold no reclaimable space, so listing them would send the
// planner after filesystems it cannot help.
var pseudoFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "efivarfs": true, "none": true,
	"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true,
	"devpts": true, "securityfs": true, "pstore": true, "debugfs": true,
	"tracefs": true, "configfs": true, "fusectl": true, "squashfs": true,
	"overlay": true, "ramfs": true, "mqueue": true, "hugetlbfs": true,
	"binfmt_misc": true, "autofs": true, "nsfs": true, "bpf": true,
}

// Mounts lists the real filesystems on this machine.
func Mounts() ([]Mount, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseMounts(f)
}

// parseMounts reads mount table lines. Split out from Mounts so the filtering
// can be tested against a table this machine does not have -- a bind-mounted
// file only occurs in a container, and that is exactly where it was found.
func parseMounts(r io.Reader) ([]Mount, error) {
	f := r
	seen := map[string]bool{}
	var out []Mount
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		dev, path, fstype := fields[0], unescape(fields[1]), fields[2]
		if pseudoFS[fstype] || pseudoFS[dev] || strings.HasPrefix(dev, "/dev/loop") {
			continue
		}
		if seen[path] {
			continue
		}
		// A bind-mounted file has a real filesystem type, so nothing above
		// catches it, and statfs happily reports the size of the filesystem
		// underneath it. In a container that puts /etc/hosts and
		// /etc/resolv.conf in the report as though each were a disk.
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			continue
		}
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil {
			continue
		}
		total := int64(st.Blocks) * int64(st.Bsize)
		if total <= 0 {
			continue
		}
		avail := int64(st.Bavail) * int64(st.Bsize)
		used := total - int64(st.Bfree)*int64(st.Bsize)

		seen[path] = true
		out = append(out, Mount{
			Device: dev, Path: path, Total: total, Avail: avail,
			UsedPct: int(used * 100 / total),
		})
	}
	return out, sc.Err()
}

// Worst returns the most pressured mount, or the zero value for none.
func Worst(ms []Mount) Mount {
	var worst Mount
	for _, m := range ms {
		if m.UsedPct > worst.UsedPct {
			worst = m
		}
	}
	return worst
}

// unescape decodes the octal escapes /proc/mounts uses for spaces and tabs in
// mount points.
func unescape(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}
