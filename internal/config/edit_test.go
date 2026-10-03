package config

import (
	"errors"
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

func TestSaveThroughSymlink(t *testing.T) {
	home, _ := os.UserHomeDir()
	dir := t.TempDir()
	real, link := filepath.Join(dir, "dotfiles.yaml"), filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(real, []byte("~/x:\n  a: u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Add(Repo{Path: filepath.Join(home, "x/b"), URL: "v"}, filepath.Join(home, "x")); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("config is no longer a symlink (%v)", err)
	}
	if data, _ := os.ReadFile(real); !strings.Contains(string(data), "b: v") {
		t.Errorf("link target not updated:\n%s", data)
	}
}

func TestReadOnly(t *testing.T) {
	dir := t.TempDir()
	ro := filepath.Join(dir, "store.yaml")
	if err := os.WriteFile(ro, []byte("~/x:\n  a: u\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(ro, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{ro, link} {
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.Save(); !errors.Is(err, ErrReadOnly) {
			t.Errorf("%s: Save got %v, want ErrReadOnly", filepath.Base(path), err)
		}
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("read-only symlink was replaced (%v)", err)
	}
	if err := New(filepath.Join(dir, "new", "config.yaml")).Writable(); err != nil {
		t.Errorf("a config that doesn't exist yet should be writable: %v", err)
	}
}

func TestSetBranches(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg, err := Parse("test.yaml", []byte(`~/x:
  plain: git@github.com:me/plain.git # a comment
  fork:
    url: git@github.com:me/fork.git
    remotes:
      upstream: git@github.com:acmeltd/fork.git
    branches: [stale]
  old:
    url: git@github.com:me/old.git
    branches: [gone]
`))
	if err != nil {
		t.Fatal(err)
	}
	x := filepath.Join(home, "x")
	set := func(name string, branches ...Branch) {
		t.Helper()
		if err := cfg.SetBranches(filepath.Join(x, name), branches); err != nil {
			t.Fatalf("SetBranches(%s): %v", name, err)
		}
	}
	set("plain", Branch{"feat", "origin/feat"})
	set("fork", Branch{"fix", "origin/fix"}, Branch{"theirs", "upstream/main"})
	set("old")

	got, err := cfg.encode()
	if err != nil {
		t.Fatal(err)
	}
	want := `~/x:
  plain:
    url: git@github.com:me/plain.git # a comment
    branches:
      - feat
  fork:
    url: git@github.com:me/fork.git
    remotes:
      upstream: git@github.com:acmeltd/fork.git
    branches:
      - fix
      - theirs: upstream/main
  old: git@github.com:me/old.git
`
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if b := cfg.Repos[0].Branches; len(b) != 1 || b[0] != (Branch{"feat", "origin/feat"}) {
		t.Errorf("plain's branches after reparse: %+v", b)
	}
	if b := cfg.Repos[1].Branches; len(b) != 2 || b[1] != (Branch{"theirs", "upstream/main"}) {
		t.Errorf("fork's branches after reparse: %+v", b)
	}

	if err := cfg.SetBranches(filepath.Join(x, "missing"), nil); err == nil {
		t.Error("SetBranches on a repo not in the config should fail")
	}
	if err := cfg.SetBranches(filepath.Join(x, "plain"), []Branch{{"feat", "nowhere/feat"}}); err == nil {
		t.Error("SetBranches from an unknown remote should fail")
	}
}
