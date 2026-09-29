package cmd

import (
	"regexp"
	"runtime/debug"
)

// stampedVersion is set by the Nix flake (-ldflags -X): Nix builds from a
// copy of the source without .git, so Go has no version to record itself.
var stampedVersion string

// version is what clonager reports: the release tag (e.g. v0.2.0) for an
// install of a tagged commit, the commit hash for an install of any other
// commit, and "dev" for a build from a local checkout.
var version = resolveVersion()

func resolveVersion() string {
	if stampedVersion != "" {
		return stampedVersion
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return versionFrom(info)
}

// pseudoVersion matches the end of a Go pseudo-version, which `go install`
// records for an untagged commit (v0.1.1-0.20260929135032-b1fab10197fb),
// capturing the commit hash.
var pseudoVersion = regexp.MustCompile(`[-.]\d{14}-([0-9a-f]{12})$`)

// versionFrom works out the version from what Go recorded at build time.
// A build inside a git checkout records the commit it was built from
// (vcs.revision); that's a local build, so "dev". `go install ...@<version>`
// records the version asked for instead: a tag, or for an untagged commit a
// pseudo-version ending in its hash.
func versionFrom(info *debug.BuildInfo) string {
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return "dev"
		}
	}
	v := info.Main.Version
	if m := pseudoVersion.FindStringSubmatch(v); m != nil {
		return m[1][:7]
	}
	if v != "" && v != "(devel)" {
		return v
	}
	return "dev"
}
