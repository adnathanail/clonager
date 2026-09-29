package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdd(t *testing.T) {
	home, _ := os.UserHomeDir()
	docs := filepath.Join(home, "Documents")
	cfg, err := Parse("test.yaml", []byte(`# My repos
~/Documents:
  Projects:
    # the tool itself
    clonager: git@github.com:me/clonager.git
    zzz: git@github.com:me/zzz.git
`))
	if err != nil {
		t.Fatal(err)
	}

	adds := []struct {
		repo Repo
		root string
	}{
		{Repo{Path: filepath.Join(docs, "Projects/qzfr"), URL: "git@github.com:me/qzfr.git"}, docs},
		{Repo{Path: filepath.Join(docs, "Uni/pyzx"), URL: "git@github.com:me/pyzx.git", GitButler: true,
			Remotes: []Remote{{"upstream", "git@github.com:zxcalc/pyzx.git"}}}, docs},
		{Repo{Path: filepath.Join(home, ".config/nix-darwin"), URL: "git@github.com:me/nix.git"}, filepath.Join(home, ".config/nix-darwin")},
		{Repo{Path: filepath.Join(home, "src/a/tool"), URL: "git@github.com:me/tool.git"}, filepath.Join(home, "src")},
	}
	for _, a := range adds {
		if err := cfg.Add(a.repo, a.root); err != nil {
			t.Fatalf("Add(%s): %v", a.repo.Path, err)
		}
	}

	got, err := cfg.encode()
	if err != nil {
		t.Fatal(err)
	}
	want := `# My repos
~/Documents:
  Projects:
    # the tool itself
    clonager: git@github.com:me/clonager.git
    qzfr: git@github.com:me/qzfr.git
    zzz: git@github.com:me/zzz.git
  Uni:
    pyzx:
      url: git@github.com:me/pyzx.git
      remotes:
        upstream: git@github.com:zxcalc/pyzx.git
      gitbutler: true
~/.config/nix-darwin: git@github.com:me/nix.git
~/src:
  a:
    tool: git@github.com:me/tool.git
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if len(cfg.Repos) != 6 {
		t.Errorf("got %d repos after adding, want 6", len(cfg.Repos))
	}
}

func TestAddErrors(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg, err := Parse("test.yaml", []byte("~/x:\n  repo: u\n"))
	if err != nil {
		t.Fatal(err)
	}
	x := filepath.Join(home, "x")
	cases := map[string]struct {
		path, want string
	}{
		"inside repo":  {filepath.Join(x, "repo/sub"), "inside a configured repo"},
		"duplicate":    {filepath.Join(x, "repo"), "already in the config"},
		"is folder":    {x, "configured as a folder"},
		"reserved":     {filepath.Join(x, "tags"), "reserved"},
		"outside root": {filepath.Join(home, "y/z"), "is not inside"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := cfg.Add(Repo{Path: c.path, URL: "u"}, filepath.Join(home, "elsewhere"))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want error containing %q", err, c.want)
			}
		})
	}
}

func TestAddToEmpty(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg := New("test.yaml")
	root := filepath.Join(home, "Documents")
	if err := cfg.Add(Repo{Path: filepath.Join(root, "a/b"), URL: "u"}, root); err != nil {
		t.Fatal(err)
	}
	got, _ := cfg.encode()
	if want := "~/Documents:\n  a:\n    b: u\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAddKeepsFolderOptionsFirst(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg, err := Parse("test.yaml", []byte("~/Work:\n  mine: false\n  zed: u\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Add(Repo{Path: filepath.Join(home, "Work/alpha"), URL: "v"}, filepath.Join(home, "Work")); err != nil {
		t.Fatal(err)
	}
	got, _ := cfg.encode()
	if want := "~/Work:\n  mine: false\n  alpha: v\n  zed: u\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
	for _, r := range cfg.Repos {
		if !r.NotMine {
			t.Errorf("%s should inherit mine: false", r.Name())
		}
	}
}
