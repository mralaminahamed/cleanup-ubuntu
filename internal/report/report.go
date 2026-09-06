// Package report renders what was found and what happened.
package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/mralaminahamed/reclaim/internal/discover"
	"github.com/mralaminahamed/reclaim/internal/fsutil"
	"github.com/mralaminahamed/reclaim/internal/installers"
	"github.com/mralaminahamed/reclaim/internal/unit"
)

// Failure is a unit that was attempted and did not succeed, with the reason in
// terms the user can act on.
type Failure struct {
	Unit   *unit.Unit
	Reason string
}

// Summary is everything one run wants to tell the user.
type Summary struct {
	Selected []*unit.Unit
	Failed   []Failure
	Locked   []*unit.Unit
	Withheld []*unit.Unit
	// OptIn holds units that were not attempted only because their flag was
	// not given. They cost nothing to report and are the user's to claim.
	OptIn []*unit.Unit
	Heavy []discover.Heavy
	// Installers are stale downloads. Advisory only: nothing here is ever
	// selected, planned or deleted.
	Installers   []installers.Installer
	TotalBytes   int64
	DryRun       bool
	StoppedEarly bool
}

type jsonUnit struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Tier     int      `json:"tier"`
	Bytes    int64    `json:"bytes"`
	Mount    string   `json:"mount,omitempty"`
	Flag     string   `json:"flag,omitempty"`
	LockedBy string   `json:"locked_by,omitempty"`
	PID      int      `json:"pid,omitempty"`
	Detail   []string `json:"detail,omitempty"`
}

// JSON writes a machine-readable summary.
func JSON(w io.Writer, s Summary) error {
	out := map[string]any{
		"dry_run":           s.DryRun,
		"reclaimable_bytes": s.TotalBytes,
		"reclaimable_human": fsutil.Human(s.TotalBytes),
		"stopped_early":     s.StoppedEarly,
		"units":             toJSON(s.Selected),
		"locked":            toJSON(s.Locked),
		"withheld":          toJSON(s.Withheld),
		"opt_in":            toJSON(s.OptIn),
	}
	failed := make([]map[string]any, 0, len(s.Failed))
	for _, f := range s.Failed {
		failed = append(failed, map[string]any{
			"id": f.Unit.ID, "label": f.Unit.Label, "reason": f.Reason})
	}
	out["failed"] = failed

	heavy := make([]map[string]any, 0, len(s.Heavy))
	for _, h := range s.Heavy {
		heavy = append(heavy, map[string]any{"path": h.Path, "bytes": h.Bytes})
	}
	out["heavyweights"] = heavy

	inst := make([]map[string]any, 0, len(s.Installers))
	for _, i := range s.Installers {
		inst = append(inst, map[string]any{"path": i.Path, "bytes": i.Bytes,
			"age_days": int(i.Age.Hours() / 24), "redundant": i.Redundant,
			"reason": i.Reason})
	}
	out["installers"] = inst

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func toJSON(us []*unit.Unit) []jsonUnit {
	out := make([]jsonUnit, 0, len(us))
	for _, u := range us {
		out = append(out, jsonUnit{u.ID, u.Label, int(u.Tier), u.Bytes, u.Mount, u.Flag, u.LockedBy, u.PID, u.Detail})
	}
	return out
}

// Text writes the human-readable report.
func Text(w io.Writer, s Summary) {
	if s.DryRun {
		fmt.Fprintln(w, "DRY-RUN — nothing was deleted. Re-run with --apply to clean.")
	}

	if len(s.Selected) > 0 {
		fmt.Fprintln(w, "\n== Reclaimable ==")
		for _, u := range s.Selected {
			fmt.Fprintf(w, "  • %-38s %10s\n", u.Label, fsutil.Human(u.Bytes))
			// A size is enough to consent to deleting a cache, and not enough
			// to consent to removing named packages.
			for _, d := range u.Detail {
				fmt.Fprintf(w, "      %s\n", d)
			}
		}
	}

	if len(s.Failed) > 0 {
		fmt.Fprintln(w, "\n== Failed ==")
		for _, f := range s.Failed {
			fmt.Fprintf(w, "  • %-38s %s\n", f.Unit.Label, f.Reason)
		}
	}

	if len(s.Locked) > 0 {
		fmt.Fprintln(w, "\n== Locked by running apps ==")
		for _, u := range s.Locked {
			fmt.Fprintf(w, "  • %-38s %10s  held by %s (pid %d)\n",
				u.Label, fsutil.Human(u.Bytes), u.LockedBy, u.PID)
			fmt.Fprintf(w, "      quit it, then re-run\n")
		}
	}

	if len(s.Withheld) > 0 {
		fmt.Fprintln(w, "\n== Withheld (may destroy information) ==")
		for _, u := range s.Withheld {
			line := fmt.Sprintf("  • %-38s %10s", u.Label, fsutil.Human(u.Bytes))
			if u.Flag != "" {
				line += fmt.Sprintf("  include with: %s --allow-lossy", u.Flag)
			} else {
				line += "  include with: --allow-lossy"
			}
			fmt.Fprintln(w, line)
		}
	}

	if len(s.OptIn) > 0 {
		fmt.Fprintln(w, "\n== Available with an opt-in flag ==")
		for _, u := range s.OptIn {
			// An irreversible unit is gated twice. Naming only its own flag
			// would send the user to a command that still does nothing.
			how := u.Flag
			if !u.Reversible {
				how += " --allow-lossy"
			}
			fmt.Fprintf(w, "  • %-38s %10s  include with: %s\n",
				u.Label, fsutil.Human(u.Bytes), how)
		}
	}

	if len(s.Heavy) > 0 {
		fmt.Fprintln(w, "\n== Large directories (advisory, never deleted) ==")
		for _, h := range s.Heavy {
			fmt.Fprintf(w, "  • %-38s %10s\n", h.Path, fsutil.Human(h.Bytes))
		}
	}

	if len(s.Installers) > 0 {
		fmt.Fprintln(w, "\n== Stale installers (advisory, never deleted) ==")
		for _, i := range s.Installers {
			line := fmt.Sprintf("  • %-46s %10s  %dd", i.Path, fsutil.Human(i.Bytes),
				int(i.Age.Hours()/24))
			if i.Redundant {
				line += "  " + i.Reason
			}
			fmt.Fprintln(w, line)
		}
	}

	verb := "would free"
	if !s.DryRun {
		verb = "freed"
	}
	fmt.Fprintf(w, "\n%s %s\n", verb, fsutil.Human(s.TotalBytes))
	if s.StoppedEarly {
		fmt.Fprintln(w, "stopped early: free-space target met")
	}
}
