// Package unitfile loads unit definitions from files.
//
// The compiled-in catalog covers what this tool ships with an opinion about.
// Everything else — a cache for a tool nobody here uses, a site-specific
// scratch directory, a package manager we have not got to — needed a code
// change and a release. A unit is already a declaration; this lets one be
// written down instead of compiled in.
//
// The format is JSON because it is parsed by the standard library. A
// hand-rolled parser would be unaudited code reading a file whose whole purpose
// is to name things for deletion, which is the wrong place to save a
// dependency that was never going to be added anyway.
package unitfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/mralaminahamed/reclaim/internal/runner"
	"github.com/mralaminahamed/reclaim/internal/unit"
)

// file is the on-disk shape.
type file struct {
	Units []fileUnit `json:"units"`
}

// fileUnit mirrors unit.Unit, with the two load-bearing claims as pointers so
// an omission can be told from a zero value.
type fileUnit struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Tier       *int     `json:"tier"`
	Reversible *bool    `json:"reversible"`
	Paths      []string `json:"paths"`
	Command    string   `json:"command"`
	Flag       string   `json:"flag"`
	MountHint  string   `json:"mount_hint"`
	NeedsRoot  bool     `json:"needs_root"`
	Requires   string   `json:"requires"`
}

// Parse reads unit definitions, resolving relative paths under home.
//
// has reports whether a binary is on PATH. A definition naming a tool this
// machine does not have is dropped rather than refused: the same file is meant
// to be usable on machines with different things installed.
func Parse(data []byte, home string, has func(string) bool) ([]*unit.Unit, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	// A typo must not read as an omission: "reversable" silently ignored would
	// hand the author a unit that does not match what they wrote.
	dec.DisallowUnknownFields()

	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}

	out := make([]*unit.Unit, 0, len(f.Units))
	for i, fu := range f.Units {
		u, err := fu.toUnit(home)
		if err != nil {
			return nil, fmt.Errorf("unit %d: %w", i, err)
		}
		if fu.Requires != "" && has != nil && !has(fu.Requires) {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

func (fu fileUnit) toUnit(home string) (*unit.Unit, error) {
	if fu.ID == "" {
		return nil, fmt.Errorf(`"id" is required`)
	}
	if fu.Tier == nil {
		return nil, fmt.Errorf(`%s: "tier" is required`, fu.ID)
	}
	if fu.Reversible == nil {
		return nil, fmt.Errorf(`%s: "reversible" is required`, fu.ID)
	}
	if *fu.Tier < int(unit.TierNative) || *fu.Tier > int(unit.TierIrreplaceable) {
		return nil, fmt.Errorf("%s: tier %d is outside 0..%d",
			fu.ID, *fu.Tier, unit.TierIrreplaceable)
	}

	hasPaths, hasCmd := len(fu.Paths) > 0, fu.Command != ""
	switch {
	case hasPaths && hasCmd:
		return nil, fmt.Errorf(`%s: give "paths" or "command", not both`, fu.ID)
	case !hasPaths && !hasCmd:
		return nil, fmt.Errorf(`%s: needs "paths" or "command"`, fu.ID)
	}

	u := &unit.Unit{
		ID:         fu.ID,
		Label:      fu.Label,
		Tier:       unit.Tier(*fu.Tier),
		Reversible: *fu.Reversible,
		Flag:       fu.Flag,
		MountHint:  fu.MountHint,
		NeedsRoot:  fu.NeedsRoot,
	}
	if u.Label == "" {
		u.Label = fu.ID
	}

	if hasCmd {
		u.Kind = unit.KindCmd
		u.Command = fu.Command
		return u, nil
	}

	u.Kind = unit.KindPaths
	for _, p := range fu.Paths {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(home, abs)
		}
		abs = filepath.Clean(abs)
		// The runner refuses these too. Refusing them here as well means a bad
		// definition never reaches a plan, so it cannot be reported as
		// reclaimable space that a run will then decline to touch.
		if err := runner.CheckSafe(abs, home); err != nil {
			return nil, fmt.Errorf("%s: %w", fu.ID, err)
		}
		u.Paths = append(u.Paths, abs)
	}
	return u, nil
}
