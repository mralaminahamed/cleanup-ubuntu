package fsutil

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
