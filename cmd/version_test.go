package cmd

import (
	"runtime/debug"
	"testing"
)

func TestVersionFrom(t *testing.T) {
	local := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "b1fab10197fb0b767f45743b7752f220b30c25a0"},
		{Key: "vcs.modified", Value: "true"},
	}
	cases := []struct {
		name     string
		version  string
		settings []debug.BuildSetting
		want     string
	}{
		{"go install @tag", "v0.2.0", nil, "v0.2.0"},
		{"go install @prerelease tag", "v0.2.0-rc.1", nil, "v0.2.0-rc.1"},
		{"go install @commit after a tag", "v0.1.1-0.20260929135032-b1fab10197fb", nil, "b1fab10"},
		{"go install @commit, no tags yet", "v0.0.0-20260929135032-b1fab10197fb", nil, "b1fab10"},
		{"go install @commit after a prerelease", "v0.2.0-rc.1.0.20260929135032-b1fab10197fb", nil, "b1fab10"},
		{"go build in a checkout, at a tag", "v0.2.0", local, "dev"},
		{"go build in a checkout", "v0.1.1-0.20260929135032-b1fab10197fb+dirty", local, "dev"},
		{"nothing recorded", "(devel)", nil, "dev"},
	}
	for _, c := range cases {
		info := &debug.BuildInfo{Main: debug.Module{Version: c.version}, Settings: c.settings}
		if got := versionFrom(info); got != c.want {
			t.Errorf("%s: versionFrom(%q) = %q, want %q", c.name, c.version, got, c.want)
		}
	}
}
