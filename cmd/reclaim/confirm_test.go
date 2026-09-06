package main

import (
	"strings"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// The prompt is the last gate before deletion, so it has to show what the
// report showed. Approving "1 location (700MiB)" is not approving the removal
// of four named packages.
func TestConfirmMessageNamesUnitDetail(t *testing.T) {
	msg := confirmMessage([]*unit.Unit{{
		ID: "system-kernels", Label: "old kernels", Bytes: 700 << 20,
		Detail: []string{"linux-image-6.8.0-45-generic"},
	}})

	if !strings.Contains(msg, "linux-image-6.8.0-45-generic") {
		t.Errorf("prompt hides what it is about to remove:\n%s", msg)
	}
}

func TestConfirmMessageCountsAndSizes(t *testing.T) {
	msg := confirmMessage([]*unit.Unit{
		{ID: "a", Label: "a", Bytes: 512},
		{ID: "b", Label: "b", Bytes: 512},
	})

	if !strings.Contains(msg, "2") || !strings.Contains(msg, "1.0KiB") {
		t.Errorf("prompt = %q", msg)
	}
}

// "1 cache locations" is both ungrammatical and wrong: a unit may be a package
// removal or a command, not only a directory.
func TestConfirmMessageReadsCorrectlyForOneUnit(t *testing.T) {
	msg := confirmMessage([]*unit.Unit{{ID: "a", Label: "a", Bytes: 512}})

	if strings.Contains(msg, "locations") {
		t.Errorf("plural used for a single unit: %q", msg)
	}
	if strings.Contains(msg, "cache") {
		t.Errorf("not everything selected is a cache: %q", msg)
	}
}

func TestConfirmMessagePluralisesForSeveralUnits(t *testing.T) {
	msg := confirmMessage([]*unit.Unit{
		{ID: "a", Label: "a", Bytes: 1},
		{ID: "b", Label: "b", Bytes: 1},
	})

	if !strings.Contains(msg, "units") {
		t.Errorf("singular used for several units: %q", msg)
	}
}
