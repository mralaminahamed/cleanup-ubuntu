package main

import "runtime/debug"

// resolveVersion decides what "reclaim version" reports.
//
// A release pipeline sets ldflags and that wins. A binary from
// "go install pkg@v0.1.0" carries no ldflags but does record the module version
// in its build info, and reporting "dev" for a tagged install would be wrong.
// A plain "go build" in a working tree records "(devel)", which says nothing.
func resolveVersion(ldflags, fromBuildInfo string) string {
	if ldflags != "" && ldflags != "dev" {
		return ldflags
	}
	if fromBuildInfo != "" && fromBuildInfo != "(devel)" {
		return fromBuildInfo
	}
	return ldflags
}

// moduleVersion reads the version the toolchain embedded at build time.
func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return bi.Main.Version
}
