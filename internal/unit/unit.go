// Package unit models a single reclaimable thing on disk and the registry that
// holds them.
//
// The bash original carried ten parallel associative arrays keyed by unit id.
// Every new field meant editing the registrar, the reset helper and the JSON
// emitter in lockstep, and a typo in any one of them surfaced as an unbound
// variable at runtime. One struct removes that whole class of bug.
package unit

// Tier orders units from "costs nothing" to "may be irreplaceable". The planner
// walks tiers in ascending order and refuses to cross a ceiling, so the numbers
// are load-bearing: they are the escalation ladder, not labels.
type Tier int

const (
	// TierNative runs a package manager's own cache-clean command. Safer than
	// any rm we could write, because the tool knows its own layout.
	TierNative Tier = iota
	// TierPkgCache is package-manager cache leftovers: re-downloaded on demand.
	TierPkgCache
	// TierArtifact is a bigger regenerable artefact, such as browser binaries.
	TierArtifact
	// TierColdReload costs a reindex or a full re-download on next use.
	TierColdReload
	// TierLossy destroys information that cannot be regenerated from a network.
	TierLossy
	// TierIrreplaceable may destroy the only copy. Never reached by escalation.
	TierIrreplaceable
)

// Kind says how a unit reclaims space.
type Kind int

const (
	// KindPaths deletes the listed paths.
	KindPaths Kind = iota
	// KindCmd shells out to a tool's own cleanup command.
	KindCmd
)

// Unit is one reclaimable thing: a set of paths or a command, plus the metadata
// the planner needs to decide whether and when to run it.
type Unit struct {
	ID    string
	Tier  Tier
	Label string
	Kind  Kind

	// Reversible is false when running this unit destroys information that
	// cannot be re-fetched. The planner refuses these unless explicitly allowed.
	Reversible bool

	// Paths is used when Kind is KindPaths.
	Paths []string
	// Command is used when Kind is KindCmd.
	Command string

	// Flag is the opt-in flag that lets this unit exceed the tier ceiling.
	Flag string
	// MountHint resolves a mount for units that own no path of their own.
	MountHint string

	// Filled in by the probe phase.
	Bytes int64
	Mount string
	// LockedBy names the running application that makes this unit unsafe to
	// run right now. Empty means free to run.
	LockedBy string
}

// Registry holds units in stable registration order.
type Registry struct {
	order []*Unit
	byID  map[string]*Unit
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]*Unit)}
}

// Add registers u. A duplicate ID is ignored so the first registration wins:
// hardcoded units are registered before the generic scanners run, and must not
// be downgraded by a later, less specific claim on the same id.
func (r *Registry) Add(u *Unit) {
	if u == nil || u.ID == "" {
		return
	}
	if _, exists := r.byID[u.ID]; exists {
		return
	}
	r.byID[u.ID] = u
	r.order = append(r.order, u)
}

// All returns the units in registration order.
func (r *Registry) All() []*Unit { return r.order }

// Get returns the unit with the given id.
func (r *Registry) Get(id string) (*Unit, bool) {
	u, ok := r.byID[id]
	return u, ok
}

// Claimed reports whether some registered unit already owns this exact path.
// The generic cache scanners consult it so they never re-claim a directory a
// hardcoded unit already covers, which would double-count reclaimable bytes.
func (r *Registry) Claimed(path string) bool {
	if path == "" {
		return false
	}
	for _, u := range r.order {
		if u.Kind != KindPaths {
			continue
		}
		for _, p := range u.Paths {
			if p == path {
				return true
			}
		}
	}
	return false
}
