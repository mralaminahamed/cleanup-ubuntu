// Package installers reports downloaded installer files that have gone stale.
//
// This package deletes nothing, and that is the design rather than a stage it
// is passing through. Everything else this tool touches lives in a cache
// directory, where the containing directory is itself the argument that the
// contents are disposable. ~/Downloads is the opposite: a user directory
// holding user files, where a download may be the only copy of something.
//
// So the answer here is a report, and the decision stays with the person who
// knows what they downloaded. The one exception is a claim that can be proven
// rather than guessed -- a .deb whose package is already installed at exactly
// that version is a second copy of something the system holds -- and even that
// is reported rather than acted on.
package installers

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// suffixes are the shapes an installer takes: formats whose only purpose is to
// install something.
//
// Archives are deliberately absent. A first version included .zip and .tar.*,
// and on a real ~/Downloads the largest things it reported were "ML, AI and DL
// books.zip" and a Figma UI kit -- data somebody downloaded and kept, listed
// under a heading claiming they were installers. An archive is a container, not
// an intent, and there is nothing in a .zip that says which it is.
var suffixes = []string{
	".deb", ".rpm", ".appimage", ".iso", ".img", ".run", ".snap", ".flatpakref",
	".msi", ".pkg", ".dmg",
}

// Installer is one stale download.
type Installer struct {
	Path  string
	Bytes int64
	Age   time.Duration
	// Redundant is true only when the file has been proven to duplicate
	// something already installed. A guess never sets it.
	Redundant bool
	Reason    string
}

// Env supplies the package checks. Both are injected: they shell out, and a
// test must be able to describe a machine it is not running on.
type Env struct {
	// DebInfo reads a .deb's own package name and version from its control
	// file. Nil disables the redundancy check.
	DebInfo func(path string) (name, version string, err error)
	// Installed returns the installed version of a package, or "" if it is not
	// installed.
	Installed func(pkg string) string
}

// DefaultEnv returns an Env backed by dpkg, where it exists.
func DefaultEnv() Env {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		return Env{}
	}
	return Env{
		DebInfo: func(path string) (string, string, error) {
			out, err := exec.Command("dpkg-deb", "-f", path, "Package", "Version").Output()
			if err != nil {
				return "", "", err
			}
			var name, version string
			for _, line := range strings.Split(string(out), "\n") {
				k, v, ok := strings.Cut(line, ": ")
				if !ok {
					continue
				}
				switch k {
				case "Package":
					name = v
				case "Version":
					version = v
				}
			}
			return name, version, nil
		},
		Installed: func(pkg string) string {
			out, err := exec.Command("dpkg-query", "-W", "-f", "${Version}", pkg).Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		},
	}
}

// Find reports installer files in dir older than age, largest first.
func Find(env Env, dir string, age time.Duration) []Installer {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-age)

	var out []Installer
	for _, e := range entries {
		if e.IsDir() || !isInstaller(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		i := Installer{
			Path:  filepath.Join(dir, e.Name()),
			Bytes: info.Size(),
			Age:   time.Since(info.ModTime()),
		}
		markRedundant(env, &i)
		out = append(out, i)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Bytes > out[b].Bytes })
	return out
}

// markRedundant sets Redundant only on proof: this .deb declares a package and
// version, and that exact version is what dpkg has installed.
func markRedundant(env Env, i *Installer) {
	if env.DebInfo == nil || env.Installed == nil {
		return
	}
	if !strings.EqualFold(filepath.Ext(i.Path), ".deb") {
		return
	}
	name, version, err := env.DebInfo(i.Path)
	if err != nil || name == "" || version == "" {
		return
	}
	if env.Installed(name) != version {
		return
	}
	i.Redundant = true
	i.Reason = name + " " + version + " is already installed"
}

func isInstaller(name string) bool {
	lower := strings.ToLower(name)
	for _, s := range suffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}
