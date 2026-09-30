package config

import (
	"cmp"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSettings writes $XDG_CONFIG_HOME/clonager/settings.json.
func writeSettings(t *testing.T, xdg, contents string) {
	t.Helper()
	path := filepath.Join(xdg, "clonager", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSettings(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		name, contents string
		want           []string
		openIn         string // "" for DefaultOpenIn
		err            string
	}{
		{name: "none"},
		{name: "empty", contents: `{}`},
		{name: "paths", contents: `{"discoverPaths": ["~/Documents", "/src"]}`,
			want: []string{filepath.Join(home, "Documents"), "/src"}},
		{name: "relative", contents: `{"discoverPaths": ["Documents"]}`, err: "absolute"},
		{name: "not json", contents: `~/Documents`, err: "settings.json"},
		{name: "open in", contents: `{"openIn": "cursor"}`, openIn: "cursor"},
		{name: "open in unknown", contents: `{"openIn": "emacs"}`, err: "openIn"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			xdg := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", xdg)
			if c.contents != "" {
				writeSettings(t, xdg, c.contents)
			}
			got, err := ReadSettings()
			switch {
			case c.err != "":
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Errorf("got %+v, %v; want error containing %q", got, err, c.err)
				}
			case err != nil:
				t.Errorf("unexpected error %v", err)
			case strings.Join(got.DiscoverPaths, "\n") != strings.Join(c.want, "\n"):
				t.Errorf("got %q, want %q", got.DiscoverPaths, c.want)
			case got.OpenIn != cmp.Or(c.openIn, DefaultOpenIn):
				t.Errorf("got openIn %q, want %q", got.OpenIn, cmp.Or(c.openIn, DefaultOpenIn))
			}
		})
	}
}
