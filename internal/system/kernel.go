package system

import (
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// kernelPkg matches a versioned kernel package and captures its version.
//
// The version is the whole tail: "6.8.0-45-generic". Unversioned names like
// linux-image-generic do not match, and must not -- those are the meta-packages
// that track the latest kernel, and removing one is how a machine quietly stops
// receiving kernel updates.
var kernelPkg = regexp.MustCompile(
	`^linux-(?:image|headers|modules|modules-extra|image-unsigned)-(\d[\w.+-]*)$`)

// Removal is the set of kernel packages that can go, with the files they own.
type Removal struct {
	// Versions are the kernel versions being removed, for the report.
	Versions []string
	// Packages are the exact package names to purge. Exact, never a glob: a
	// glob is evaluated by the shell against whatever happens to be installed
	// at the time, which is not what anyone reviewed in the dry run.
	Packages []string
	// Paths are what those versions put on disk, used to measure the unit.
	// Nothing deletes these; apt does the removing.
	Paths []string
}

// removable selects the kernel packages that are safe to purge, given the
// packages installed and the running kernel version.
//
// Two kernels always survive. The running one is never a candidate -- that is
// the whole reason this exists rather than leaning on apt's autoremove, which
// decides for itself. The newest is kept as well, because removing everything
// but the kernel currently booted leaves nothing to fall back to when that one
// later fails to boot. Between an upgrade and the reboot that takes it up those
// are two different kernels, and both are worth keeping.
func removable(installed []string, running string) Removal {
	versions := map[string]bool{}
	byVersion := map[string][]string{}
	for _, p := range installed {
		m := kernelPkg.FindStringSubmatch(p)
		if m == nil {
			continue
		}
		v := m[1]
		// "linux-headers-6.8.0-45" belongs to "6.8.0-45-generic": the flavour
		// is missing from the headers package for the shared part.
		versions[v] = true
		byVersion[v] = append(byVersion[v], p)
	}

	all := make([]string, 0, len(versions))
	for v := range versions {
		all = append(all, v)
	}
	sort.Slice(all, func(i, j int) bool { return newer(all[i], all[j]) })

	keep := map[string]bool{}
	for _, v := range all {
		if sameKernel(v, running) {
			keep[v] = true
		}
	}
	// The newest, whether or not it is the one running.
	if len(all) > 0 {
		keep[all[0]] = true
		// If the newest is also the running one, the fallback is the next.
		if sameKernel(all[0], running) && len(all) > 1 {
			keep[all[1]] = true
		}
	}

	var out Removal
	for _, v := range all {
		if keep[v] || keepsPartOf(v, keep) {
			continue
		}
		out.Versions = append(out.Versions, v)
		out.Packages = append(out.Packages, byVersion[v]...)
		out.Paths = append(out.Paths,
			"/boot/vmlinuz-"+v,
			"/boot/initrd.img-"+v,
			"/boot/System.map-"+v,
			"/boot/config-"+v,
			"/lib/modules/"+v,
			"/usr/src/linux-headers-"+v,
		)
	}
	sort.Strings(out.Packages)
	return out
}

// sameKernel reports whether a package version belongs to a kernel release.
// "6.8.0-45" is the headers half of "6.8.0-45-generic".
func sameKernel(v, release string) bool {
	return v == release || strings.HasPrefix(release, v+"-")
}

// keepsPartOf reports whether v is the shared half of a version being kept.
func keepsPartOf(v string, keep map[string]bool) bool {
	for k := range keep {
		if sameKernel(v, k) {
			return true
		}
	}
	return false
}

// newer compares two kernel versions by their numeric fields. Comparing them as
// text puts 6.8.0-9 above 6.8.0-100 and keeps the wrong kernel.
func newer(a, b string) bool {
	na, nb := nums(a), nums(b)
	for i := 0; i < len(na) && i < len(nb); i++ {
		if na[i] != nb[i] {
			return na[i] > nb[i]
		}
	}
	if len(na) != len(nb) {
		return len(na) > len(nb)
	}
	return a > b
}

var digits = regexp.MustCompile(`\d+`)

func nums(s string) []int {
	fields := digits.FindAllString(s, -1)
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// installedKernels lists the kernel packages dpkg knows about.
func installedKernels() []string {
	out, err := exec.Command("dpkg-query", "-W", "-f", "${Package} ${Status}\n",
		"linux-image-*", "linux-headers-*", "linux-modules-*").Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	var pkgs []string
	for _, line := range strings.Split(string(out), "\n") {
		name, status, ok := strings.Cut(line, " ")
		// dpkg-query lists packages it merely knows of. Only those actually
		// unpacked own files worth removing.
		if !ok || !strings.HasSuffix(status, " installed") {
			continue
		}
		pkgs = append(pkgs, name)
	}
	return pkgs
}

// runningKernel is the release the machine booted.
func runningKernel() string {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		// An empty running kernel would make every installed kernel look
		// removable. Returning a value nothing can match keeps the whole set.
		return "\x00unknown"
	}
	return strings.TrimSpace(string(out))
}
