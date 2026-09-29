package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"main":               "main",
		"zak/tra-1-fix":      "zak/tra-1-fix",
		"feat#2":             "'feat#2'",
		"it's":               `'it'\''s'`,
		"a;rm -rf":           "'a;rm -rf'",
		"$(whoami)":          "'$(whoami)'",
		"release/v1.2+build": "release/v1.2+build",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestShellPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		filepath.Join(home, "Documents/x"):   "~/Documents/x",
		filepath.Join(home, "My Projects/x"): "~/'My Projects/x'",
		"/opt/src/tool":                      "/opt/src/tool",
		"/opt/my src":                        "'/opt/my src'",
	}
	for in, want := range cases {
		if got := shellPath(in); got != want {
			t.Errorf("shellPath(%q) = %s, want %s", in, got, want)
		}
	}
}
