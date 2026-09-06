package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// A unit held back for want of a flag is invisible in the plan by design. The
// report is where the user finds out it exists, so it has to name both the size
// and the exact flag that would buy it.
func TestTextOffersOptInUnitsWithTheirFlag(t *testing.T) {
	var b bytes.Buffer
	Text(&b, Summary{
		DryRun: true,
		OptIn: []*unit.Unit{
			{ID: "xdg-huggingface", Label: ".cache/huggingface", Bytes: 96 << 30, Flag: "--heavy"},
		},
	})
	out := b.String()

	for _, want := range []string{".cache/huggingface", "96.0GiB", "--heavy"} {
		if !strings.Contains(out, want) {
			t.Errorf("report does not mention %q:\n%s", want, out)
		}
	}
}

// The offer must read as an offer. A user scanning the output should not
// mistake it for something that was deleted.
func TestTextSeparatesOptInFromReclaimable(t *testing.T) {
	var b bytes.Buffer
	Text(&b, Summary{
		Selected: []*unit.Unit{{ID: "pip", Label: "pip cache", Bytes: 100}},
		OptIn:    []*unit.Unit{{ID: "gradle", Label: "gradle caches", Bytes: 4 << 30, Flag: "--gradle"}},
	})
	out := b.String()

	recl := strings.Index(out, "== Reclaimable ==")
	offer := strings.Index(out, "gradle caches")
	if recl == -1 || offer == -1 {
		t.Fatalf("missing sections:\n%s", out)
	}
	if strings.Contains(out[recl:offer], "gradle caches") {
		t.Errorf("opt-in unit listed under Reclaimable:\n%s", out)
	}
}

func TestJSONIncludesOptInUnits(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, Summary{
		OptIn: []*unit.Unit{{ID: "gradle", Label: "gradle caches", Bytes: 4 << 30, Flag: "--gradle"}},
	}); err != nil {
		t.Fatal(err)
	}

	var got struct {
		OptIn []struct {
			ID   string `json:"id"`
			Flag string `json:"flag"`
		} `json:"opt_in"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, b.String())
	}
	if len(got.OptIn) != 1 || got.OptIn[0].ID != "gradle" || got.OptIn[0].Flag != "--gradle" {
		t.Fatalf("opt_in = %+v", got.OptIn)
	}
}

// An opt-in unit was never attempted, so it is not part of what the run freed
// and must not be added to the total.
func TestOptInUnitsAreNotCountedAsFreed(t *testing.T) {
	var b bytes.Buffer
	Text(&b, Summary{
		TotalBytes: 100,
		OptIn:      []*unit.Unit{{ID: "gradle", Label: "gradle caches", Bytes: 4 << 30, Flag: "--gradle"}},
	})

	if !strings.Contains(b.String(), "freed 100B") {
		t.Errorf("total is not the freed bytes:\n%s", b.String())
	}
}

// An irreversible unit needs two things, not one: its own flag and the lossy
// gate. Printing only the flag would send the user to a command that still
// does nothing, with no clue why.
func TestTextOffersLossyOptInUnitsWithBothFlags(t *testing.T) {
	var b bytes.Buffer
	Text(&b, Summary{
		OptIn: []*unit.Unit{
			{ID: "claude-history", Label: "Claude session transcripts",
				Bytes: 1 << 30, Flag: "--claude-history", Reversible: false},
		},
	})

	if !strings.Contains(b.String(), "--claude-history --allow-lossy") {
		t.Errorf("offer omits the lossy gate:\n%s", b.String())
	}
}
