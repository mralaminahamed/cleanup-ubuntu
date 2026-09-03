package main

import "testing"

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		name    string
		ldflags string
		fromVCS string
		want    string
	}{
		// A binary from "go install pkg@v0.1.0" has no ldflags but does carry
		// the module version in its build info. Reporting "dev" there is wrong.
		{"installed release", "dev", "v0.1.0", "v0.1.0"},
		// An explicit -ldflags build wins: it is what a release pipeline sets.
		{"ldflags wins", "v0.2.0", "v0.1.0", "v0.2.0"},
		// go build in a working tree reports "(devel)", which is not useful.
		{"devel falls back", "dev", "(devel)", "dev"},
		{"no build info", "dev", "", "dev"},
	}
	for _, c := range cases {
		if got := resolveVersion(c.ldflags, c.fromVCS); got != c.want {
			t.Errorf("%s: resolveVersion(%q, %q) = %q, want %q",
				c.name, c.ldflags, c.fromVCS, got, c.want)
		}
	}
}
