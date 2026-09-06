package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, body string) *Config {
	t.Helper()
	c, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return c
}

func loadErr(t *testing.T, body string) string {
	t.Helper()
	if _, err := Parse([]byte(body)); err != nil {
		return err.Error()
	}
	t.Fatal("Parse accepted a key it should have refused")
	return ""
}

func TestParseReadsTheSettableKeys(t *testing.T) {
	c := load(t, `{"workers":12,"tier":3,"exclude":["go-*"],"only":["pip-cache"],
		"sites_root":"~/Projects"}`)

	if c.Workers == nil || *c.Workers != 12 {
		t.Errorf("workers %v", c.Workers)
	}
	if c.Tier == nil || *c.Tier != 3 {
		t.Errorf("tier %v", c.Tier)
	}
	if len(c.Exclude) != 1 || c.Exclude[0] != "go-*" {
		t.Errorf("exclude %v", c.Exclude)
	}
	if c.SitesRoot != "~/Projects" {
		t.Errorf("sites_root %q", c.SitesRoot)
	}
}

// The rule the whole file rests on: a config may narrow a run and may never
// widen one. Everything refused below widens.
//
// The person this protects is the one who set the file up months ago and
// forgot. A flag is written at the moment of use and read back by whoever is
// about to press enter; a file is not.
func TestParseRefusesAnythingThatAuthorisesDeletion(t *testing.T) {
	for _, key := range []string{"apply", "yes", "allow_lossy"} {
		msg := loadErr(t, `{"`+key+`":true}`)
		if !strings.Contains(msg, key) {
			t.Errorf("error for %q does not name it: %s", key, msg)
		}
		if !strings.Contains(msg, "command line") {
			t.Errorf("error for %q does not say where it belongs: %s", key, msg)
		}
	}
}

// --discover is not an authorisation on its own, but it widens what a later
// --apply reaches. Same class of surprise, same answer.
func TestParseRefusesDiscover(t *testing.T) {
	loadErr(t, `{"discover":true}`)
}

// An opt-in group means a deliberate act at the call site. A file that could
// pre-authorise one would be exactly what the flag exists to prevent.
func TestParseRefusesOptInGroups(t *testing.T) {
	for _, key := range []string{"gradle", "models", "system", "kernels", "sites_idle"} {
		loadErr(t, `{"`+key+`":true}`)
	}
}

func TestParseRefusesAnUnknownKey(t *testing.T) {
	msg := loadErr(t, `{"wokrers":4}`)
	if !strings.Contains(msg, "wokrers") {
		t.Errorf("error does not name the key: %s", msg)
	}
}

// A tier above the default would widen rather than narrow, which is the one
// thing this file may not do.
func TestParseRefusesATierOutsideTheLadder(t *testing.T) {
	loadErr(t, `{"tier":9}`)
	loadErr(t, `{"tier":-1}`)
}

func TestLoadIgnoresAMissingFile(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing file reported as an error: %v", err)
	}
	if c == nil {
		t.Fatal("no config returned")
	}
}

// A file that exists and does not parse means the user wrote something that is
// not being honoured. Running with defaults they did not choose is worse.
func TestLoadFailsOnABrokenFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"workers":`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(p); err == nil {
		t.Fatal("broken file accepted")
	}
}

func TestDefaultPathIsUnderTheUsualDirectory(t *testing.T) {
	if got := DefaultPath("/home/u"); got != "/home/u/.config/reclaim/config.json" {
		t.Errorf("path %q", got)
	}
}
