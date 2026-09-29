package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSettings writes a file under $XDG_CONFIG_HOME/clonager.
func writeSettings(t *testing.T, xdg, name, contents string) {
	t.Helper()
	path := filepath.Join(xdg, "clonager", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSource(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		name, file, contents string
		want                 *Source
		err                  string
	}{
		{name: "none"},
		{name: "path", file: "source.json", contents: `{"path": "/Users/me/nix/clonager.yaml"}`,
			want: &Source{Path: "/Users/me/nix/clonager.yaml"}},
		{name: "path with ~", file: "source.json", contents: `{"path": "~/nix/clonager.yaml"}`,
			want: &Source{Path: filepath.Join(home, "nix/clonager.yaml")}},
		{name: "commands", file: "source.json", contents: `{"decrypt": "age -d x.age", "encrypt": "age -e -o x.age"}`,
			want: &Source{Decrypt: "age -d x.age", Encrypt: "age -e -o x.age"}},
		{name: "legacy plain path", file: "source", contents: "~/nix/clonager.yaml\n",
			want: &Source{Path: filepath.Join(home, "nix/clonager.yaml")}},
		{name: "legacy empty", file: "source", contents: "  \n"},
		{name: "both", file: "source.json", contents: `{"path": "/x", "decrypt": "a", "encrypt": "b"}`, err: "not both"},
		{name: "decrypt only", file: "source.json", contents: `{"decrypt": "a"}`, err: "go together"},
		{name: "relative", file: "source.json", contents: `{"path": "nix/clonager.yaml"}`, err: "absolute"},
		{name: "empty", file: "source.json", contents: `{}`, err: "no path or commands"},
		{name: "not json", file: "source.json", contents: `/x`, err: "source.json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			xdg := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", xdg)
			if c.file != "" {
				writeSettings(t, xdg, c.file, c.contents)
			}
			got, err := ReadSource()
			switch {
			case c.err != "":
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Errorf("got %+v, %v; want error containing %q", got, err, c.err)
				}
			case err != nil:
				t.Errorf("unexpected error %v", err)
			case (got == nil) != (c.want == nil) || (got != nil && *got != *c.want):
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestSourceCommands edits a config through decrypt and encrypt commands,
// standing in for e.g. agenix: here "encryption" is reversing the lines, and
// every run of the encrypt command is counted. The decrypted config is
// indented differently from how clonager writes YAML, which mustn't count as
// a change.
func TestSourceCommands(t *testing.T) {
	home, _ := os.UserHomeDir()
	dir := t.TempDir()
	secret := filepath.Join(dir, "config.enc")
	count := filepath.Join(dir, "encrypts")
	if err := os.WriteFile(secret, []byte("    a: u\n  ~/x:\n# repos\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &Source{
		Decrypt: "tail -r " + secret + " 2>/dev/null || tac " + secret,
		Encrypt: "(tail -r 2>/dev/null || tac) > " + secret + "; echo >> " + count,
	}

	cfg, err := src.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].URL != "u" {
		t.Fatalf("decrypted config: got %+v", cfg.Repos)
	}
	if err := cfg.Writable(); err != nil {
		t.Errorf("a config with an encrypt command should be writable: %v", err)
	}

	// Unchanged: nothing is re-encrypted.
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(count); err == nil {
		t.Errorf("an unchanged config was re-encrypted")
	}

	if err := cfg.Add(Repo{Path: filepath.Join(home, "x/b"), URL: "v"}, filepath.Join(home, "x")); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil { // and a second save changes nothing more
		t.Fatal(err)
	}
	if n, _ := os.ReadFile(count); strings.Count(string(n), "\n") != 1 {
		t.Errorf("encrypt ran %d times, want 1", strings.Count(string(n), "\n"))
	}

	back, err := src.Read()
	if err != nil {
		t.Fatal(err)
	}
	if want := "# repos\n~/x:\n  a: u\n  b: v\n"; string(back) != want {
		t.Errorf("stored config:\n%s\nwant:\n%s", back, want)
	}
}

func TestSourceCommandFails(t *testing.T) {
	src := &Source{Decrypt: "echo 'no identity found' >&2; exit 1", Encrypt: "cat"}
	if _, err := src.Load(); err == nil || !strings.Contains(err.Error(), "no identity found") {
		t.Errorf("got %v, want the decrypt command's error", err)
	}
}

func TestSourcePath(t *testing.T) {
	dir := t.TempDir()
	src := &Source{Path: filepath.Join(dir, "clonager.yaml")}
	cfg, err := src.Load()
	if err != nil || len(cfg.Repos) != 0 {
		t.Fatalf("a source file that doesn't exist yet should load empty: %+v, %v", cfg, err)
	}
	if err := os.WriteFile(src.Path, []byte("~/x:\n  a: u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, err = src.Load(); err != nil || len(cfg.Repos) != 1 {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}
