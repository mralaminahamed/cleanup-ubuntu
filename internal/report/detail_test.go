package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mralaminahamed/reclaim/internal/unit"
)

// A byte count is enough to consent to deleting a cache. It is not enough to
// consent to removing named packages, and a kernel removal that showed only a
// size would be asking the user to approve something they cannot see.
func TestTextPrintsUnitDetail(t *testing.T) {
	var b bytes.Buffer
	Text(&b, Summary{
		DryRun: true,
		Selected: []*unit.Unit{{
			ID: "system-kernels", Label: "old kernels", Bytes: 700 << 20,
			Detail: []string{"linux-image-6.8.0-45-generic", "linux-modules-6.8.0-45-generic"},
		}},
	})
	out := b.String()

	for _, want := range []string{"linux-image-6.8.0-45-generic", "linux-modules-6.8.0-45-generic"} {
		if !strings.Contains(out, want) {
			t.Errorf("report does not name %q:\n%s", want, out)
		}
	}
}

func TestJSONIncludesUnitDetail(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, Summary{Selected: []*unit.Unit{{
		ID: "system-kernels", Label: "old kernels",
		Detail: []string{"linux-image-6.8.0-45-generic"},
	}}}); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Units []struct {
			Detail []string `json:"detail"`
		} `json:"units"`
	}
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, b.String())
	}
	if len(got.Units) != 1 || len(got.Units[0].Detail) != 1 {
		t.Fatalf("detail = %+v", got.Units)
	}
}

// A unit with nothing extra to say must not grow a blank line.
func TestTextIsUnchangedWithoutDetail(t *testing.T) {
	var b bytes.Buffer
	Text(&b, Summary{Selected: []*unit.Unit{{ID: "pip", Label: "pip cache", Bytes: 100}}})

	if strings.Contains(b.String(), "\n\n  ") {
		t.Errorf("stray blank line:\n%q", b.String())
	}
}
