package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg, err := Parse("test.yaml", []byte(`
~/Documents:
  Projects:
    clonager: git@github.com:me/clonager.git
  Uni:
    pyzx:
      url: git@github.com:me/pyzx.git
      gitbutler: true
      tags: [uni]
      remotes:
        upstream: git@github.com:zxcalc/pyzx.git
~/.config/nix-darwin: git@github.com:me/nix-darwin.git
/opt/src:
  tool: https://example.com/tool.git
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Repo{
		{Path: filepath.Join(home, "Documents/Projects/clonager"), URL: "git@github.com:me/clonager.git"},
		{Path: filepath.Join(home, "Documents/Uni/pyzx"), URL: "git@github.com:me/pyzx.git", GitButler: true,
			Tags: []string{"uni"}, Remotes: []Remote{{"upstream", "git@github.com:zxcalc/pyzx.git"}}},
		{Path: filepath.Join(home, ".config/nix-darwin"), URL: "git@github.com:me/nix-darwin.git"},
		{Path: "/opt/src/tool", URL: "https://example.com/tool.git"},
	}
	if len(cfg.Repos) != len(want) {
		t.Fatalf("got %d repos, want %d: %+v", len(cfg.Repos), len(want), cfg.Repos)
	}
	for i, w := range want {
		g := cfg.Repos[i]
		if g.Path != w.Path || g.URL != w.URL || g.GitButler != w.GitButler ||
			strings.Join(g.Tags, ",") != strings.Join(w.Tags, ",") || len(g.Remotes) != len(w.Remotes) {
			t.Errorf("repo %d: got %+v, want %+v", i, g, w)
		}
		for j := range w.Remotes {
			if g.Remotes[j] != w.Remotes[j] {
				t.Errorf("repo %d remote %d: got %+v, want %+v", i, j, g.Remotes[j], w.Remotes[j])
			}
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"relative top-level":         {"Documents:\n  a: url\n", "must be an absolute path"},
		"reserved folder key":        {"~/x:\n  tags: [a]\n", `"tags" is only valid in a repo`},
		"missing url":                {"~/x:\n  a:\n", "missing url"},
		"unknown option":             {"~/x:\n  a:\n    url: u\n    sub: v\n", `unknown repo option "sub"`},
		"slash in name":              {"~/x:\n  a/b: u\n", "not a valid folder or repo name"},
		"branches not a list":        {"~/x:\n  a:\n    url: u\n    branches: feat\n", "expected a list of branch names"},
		"branch without ref":         {"~/x:\n  a:\n    url: u\n    branches:\n      - feat: main\n", "expected a branch name, or name: remote/branch"},
		"branch from unknown remote": {"~/x:\n  a:\n    url: u\n    branches:\n      - feat: fork/feat\n", "isn't one of the repo's remotes"},
		"branches as folder":         {"~/x:\n  branches:\n    a: u\n", `"branches" is only valid in a repo`},
		"origin in remotes":          {"~/x:\n  a:\n    url: u\n    remotes: {origin: v}\n", "origin is set by url"},
		"duplicate":                  {"~/x:\n  a: u\n~/x/a: v\n", "is also listed on line"},
		"nested repo":                {"~/x: u\n~/x/a: v\n", "is inside the repo"},
		"not a mapping":              {"- a\n", "top level must be a mapping"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("test.yaml", []byte(c.yaml))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got error %v, want one containing %q", err, c.want)
			}
		})
	}
}

func TestEmpty(t *testing.T) {
	cfg, err := Parse("test.yaml", []byte("# nothing yet\n"))
	if err != nil || len(cfg.Repos) != 0 {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}

func TestMine(t *testing.T) {
	cfg, err := Parse("test.yaml", []byte(`
~/Documents:
  Work:
    mine: false
    vip-proj: git@github.com:acmeltd/vip-proj.git
    my-fork:
      url: git@github.com:me/my-fork.git
      mine: true
    Nested:
      deep: git@github.com:acmeltd/deep.git
  Projects:
    clonager: git@github.com:me/clonager.git
    upstream-thing:
      url: git@github.com:someone/thing.git
      mine: false
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"vip-proj": true, "my-fork": false, "deep": true, "clonager": false, "upstream-thing": true}
	for _, r := range cfg.Repos {
		if r.NotMine != want[r.Name()] {
			t.Errorf("%s: NotMine %v, want %v", r.Name(), r.NotMine, want[r.Name()])
		}
	}
	if len(cfg.Repos) != len(want) {
		t.Errorf("got %d repos, want %d", len(cfg.Repos), len(want))
	}

	if _, err := Parse("test.yaml", []byte("~/x:\n  mine: nope\n  a: u\n")); err == nil || !strings.Contains(err.Error(), "mine") {
		t.Errorf("bad mine value: got %v", err)
	}
}
