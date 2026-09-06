// Package config reads default flag values from a file.
//
// One rule holds the whole design together: a config file may narrow a run and
// may never widen one.
//
// The person this protects is the one who wrote the file months ago and forgot
// it exists. A flag is typed at the moment of use and read back by whoever is
// about to press enter. A file is not read back by anyone, so anything it could
// say that makes a run delete *more* is a decision taken by someone who is not
// in the room. Narrowing has the opposite failure: the worst a stale exclusion
// can do is leave space unreclaimed, which the next report will point out.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// Config holds the settings a file may carry. Every one of them can only make
// a run do less.
type Config struct {
	// Workers sizes the probe pool. Performance only.
	Workers *int `json:"workers"`
	// Tier lowers the ceiling. It cannot raise it: the default is already the
	// top of the ladder.
	Tier *int `json:"tier"`
	// Only restricts the run; Exclude drops from it.
	Only    []string `json:"only"`
	Exclude []string `json:"exclude"`
	// SitesRoot says where projects live. Inert unless --sites-idle is given
	// on the command line, which a file cannot do.
	SitesRoot string `json:"sites_root"`
	// JSON selects machine-readable output. Presentation only.
	JSON *bool `json:"json"`
}

// refused are keys that would widen a run, mapped to why they are not here.
//
// They are named explicitly rather than left to fall out as unknown fields:
// "unknown field apply" reads like a typo, and someone who writes "apply": true
// into a config has made a reasonable guess about what a config is for and
// deserves to be told the actual reason it is refused.
var refused = map[string]string{
	"apply":       "deleting is not something a file may authorise; pass --apply on the command line",
	"yes":         "skipping the confirmation prompt belongs on the command line, where it is read back before it takes effect",
	"allow_lossy": "the reversibility ceiling is absolute; pass --allow-lossy on the command line",
	"discover":    "discovery widens what a later --apply reaches, so it belongs on the command line",
	"sites_idle":  "an opt-in group means a deliberate act at the call site; pass --sites-idle on the command line",
}

// DefaultPath returns the usual location of the config file.
func DefaultPath(home string) string {
	return filepath.Join(home, ".config", "reclaim", "config.json")
}

// Load reads the config at path. A missing file is the normal case and yields
// an empty config; a file that exists and does not parse is an error, because
// running with defaults the user did not choose is worse than not running.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse reads a config, refusing anything that would widen a run.
func Parse(data []byte) (*Config, error) {
	// Checked before decoding, so a refused key is reported for what it is
	// rather than as an unknown field.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	for k := range raw {
		if why, bad := refused[strings.ToLower(k)]; bad {
			return nil, fmt.Errorf("%q cannot be set in a config file: %s", k, why)
		}
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	// Catches both a typo and an opt-in group name nobody thought to list.
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	if c.Tier != nil && (*c.Tier < int(unit.TierNative) || *c.Tier > int(unit.TierIrreplaceable)) {
		return nil, fmt.Errorf("tier %d is outside 0..%d", *c.Tier, unit.TierIrreplaceable)
	}
	return &c, nil
}
