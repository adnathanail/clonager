// Package config loads the clonager config file.
//
// The file is a YAML tree that mirrors the filesystem. Top-level keys are
// absolute paths (~ allowed). Below them, a string value is a repo (the string
// is its clone URL), a mapping containing "url" is a repo with options, and any
// other mapping is a folder containing more folders or repos. Folders can set
// options their repos inherit (just "mine" for now).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Keys with special meaning inside a repo mapping. They can't be used as
// folder names.
var repoKeys = map[string]bool{
	"url":       true,
	"remotes":   true,
	"gitbutler": true,
	"tags":      true,
	"mine":      true,
	"branches":  true,
}

// Repo options a folder can set for everything inside it.
var folderKeys = map[string]bool{
	"mine": true,
}

// inherited holds the options passed down from folders.
type inherited struct {
	notMine bool
}

type Remote struct {
	Name string
	URL  string
}

// Branch is a local branch to recreate on another laptop (see airlift),
// starting from a remote-tracking ref.
type Branch struct {
	Name string
	From string // remote-tracking ref, e.g. origin/fix-login
}

type Repo struct {
	Path      string // absolute path on disk
	URL       string // clone URL for origin
	Remotes   []Remote
	GitButler bool
	Tags      []string
	Branches  []Branch // recorded by airlift, until tidy finds them here
	// NotMine is set (by mine: false, on the repo or a folder above it) for
	// repos whose remote isn't the user's to change, so clonager never
	// suggests deleting branches there or changing its settings.
	NotMine bool
	Line    int // line in the config file, for error messages
}

// Name is the repo's directory name.
func (r Repo) Name() string { return filepath.Base(r.Path) }

type Config struct {
	Path  string // the file, or for a config from a Source's commands, a description
	Repos []Repo
	doc   *yaml.Node // kept so later commands can edit the file in place

	source *Source // set for a config read with a Source's decrypt command
	loaded []byte  // its encoding when loaded, to tell whether it changed
}

// DefaultPath returns where the config lives unless overridden:
// $CLONAGER_CONFIG, then $XDG_CONFIG_HOME/clonager/config.yaml, then
// ~/.config/clonager/config.yaml. (os.UserConfigDir is deliberately not used,
// as on macOS it points at ~/Library/Application Support.)
func DefaultPath() (string, error) {
	if p := os.Getenv("CLONAGER_CONFIG"); p != "" {
		return ExpandHome(p)
	}
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clonager", "config.yaml"), nil
}

// configDir is $XDG_CONFIG_HOME, or ~/.config.
func configDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return x, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(path, data)
}

func Parse(path string, data []byte) (*Config, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg := &Config{Path: path, doc: &doc}
	if len(doc.Content) == 0 {
		return cfg, nil // empty file
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s:%d: top level must be a mapping of paths", path, root.Line)
	}

	p := parser{file: path}
	for i := 0; i < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		if !strings.HasPrefix(key.Value, "/") && !strings.HasPrefix(key.Value, "~") {
			p.errorf(key, "top-level key %q must be an absolute path (or start with ~)", key.Value)
			continue
		}
		dir, err := ExpandHome(key.Value)
		if err != nil {
			p.errorf(key, "%v", err)
			continue
		}
		p.node(filepath.Clean(dir), val, inherited{})
	}
	cfg.Repos = p.repos
	p.checkOverlaps()
	if len(p.errs) > 0 {
		return nil, errors.Join(p.errs...)
	}
	return cfg, nil
}

type parser struct {
	file  string
	repos []Repo
	errs  []error
}

func (p *parser) errorf(n *yaml.Node, format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf("%s:%d: %s", p.file, n.Line, fmt.Sprintf(format, args...)))
}

// node handles the value found at path: a repo URL, a repo mapping or a folder.
func (p *parser) node(path string, n *yaml.Node, in inherited) {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" || n.Value == "" {
			p.errorf(n, "%s: missing url", TildePath(path))
			return
		}
		p.repos = append(p.repos, Repo{Path: path, URL: n.Value, NotMine: in.notMine, Line: n.Line})
	case yaml.MappingNode:
		if mappingHas(n, "url") {
			p.repo(path, n, in)
		} else {
			p.folder(path, n, in)
		}
	default:
		p.errorf(n, "%s: expected a url, repo options or a folder", TildePath(path))
	}
}

func (p *parser) folder(path string, n *yaml.Node, in inherited) {
	// Folder options first, so they apply whatever order the keys are in.
	for i := 0; i < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if key.Value == "mine" {
			var mine bool
			if err := val.Decode(&mine); err != nil {
				p.errorf(val, "%s: mine: %v", TildePath(path), err)
			}
			in.notMine = !mine
		}
	}
	for i := 0; i < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		name := key.Value
		switch {
		case folderKeys[name]:
		case repoKeys[name]:
			p.errorf(key, "%s: %q is only valid in a repo (did you forget url?)", TildePath(path), name)
		case name == "" || name == "." || name == ".." || strings.Contains(name, "/"):
			p.errorf(key, "%s: %q is not a valid folder or repo name", TildePath(path), name)
		default:
			p.node(filepath.Join(path, name), val, in)
		}
	}
}

func (p *parser) repo(path string, n *yaml.Node, in inherited) {
	r := Repo{Path: path, NotMine: in.notMine, Line: n.Line}
	for i := 0; i < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		var err error
		switch key.Value {
		case "url":
			err = val.Decode(&r.URL)
		case "gitbutler":
			err = val.Decode(&r.GitButler)
		case "tags":
			err = val.Decode(&r.Tags)
		case "remotes":
			r.Remotes, err = decodeRemotes(val)
		case "branches":
			r.Branches, err = decodeBranches(val)
		case "mine":
			var mine bool
			err = val.Decode(&mine)
			r.NotMine = !mine
		default:
			p.errorf(key, "%s: unknown repo option %q (repos can't contain folders)", TildePath(path), key.Value)
			continue
		}
		if err != nil {
			p.errorf(val, "%s: %s: %v", TildePath(path), key.Value, err)
		}
	}
	if r.URL == "" {
		p.errorf(n, "%s: missing url", TildePath(path))
	}
	for _, b := range r.Branches {
		if !r.HasRemote(b.Remote()) {
			p.errorf(n, "%s: branches: %s is from %s, which isn't one of the repo's remotes", TildePath(path), b.Name, b.From)
		}
	}
	p.repos = append(p.repos, r)
}

// HasRemote reports whether the repo is configured with the named remote.
func (r Repo) HasRemote(name string) bool {
	if name == "origin" {
		return true
	}
	for _, rem := range r.Remotes {
		if rem.Name == name {
			return true
		}
	}
	return false
}

// Remote is the remote From is on, e.g. origin.
func (b Branch) Remote() string {
	remote, _, _ := strings.Cut(b.From, "/")
	return remote
}

// decodeBranches reads a list of branches: each a name, for the same-named
// branch on origin, or a name: remote/branch pair.
func decodeBranches(n *yaml.Node) ([]Branch, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, errors.New("expected a list of branch names")
	}
	var out []Branch
	for _, item := range n.Content {
		var b Branch
		switch {
		case item.Kind == yaml.ScalarNode && item.Value != "":
			b = Branch{Name: item.Value, From: "origin/" + item.Value}
		case item.Kind == yaml.MappingNode && len(item.Content) == 2:
			b = Branch{Name: item.Content[0].Value, From: item.Content[1].Value}
		default:
			return nil, fmt.Errorf("line %d: expected a branch name, or name: remote/branch", item.Line)
		}
		if b.Name == "" || !strings.Contains(b.From, "/") {
			return nil, fmt.Errorf("line %d: expected a branch name, or name: remote/branch", item.Line)
		}
		out = append(out, b)
	}
	return out, nil
}

// decodeRemotes reads a name: url mapping, keeping the file's order.
func decodeRemotes(n *yaml.Node) ([]Remote, error) {
	if n.Kind != yaml.MappingNode {
		return nil, errors.New("expected a mapping of remote name to url")
	}
	var out []Remote
	for i := 0; i < len(n.Content); i += 2 {
		name, url := n.Content[i].Value, n.Content[i+1].Value
		if name == "origin" {
			return nil, errors.New("origin is set by url, not remotes")
		}
		out = append(out, Remote{Name: name, URL: url})
	}
	return out, nil
}

// checkOverlaps catches the same repo listed twice (e.g. under two top-level
// keys) and repos nested inside other repos.
func (p *parser) checkOverlaps() {
	sorted := make([]Repo, len(p.repos))
	copy(sorted, p.repos)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for i := 1; i < len(sorted); i++ {
		prev, cur := sorted[i-1], sorted[i]
		switch {
		case prev.Path == cur.Path:
			p.errs = append(p.errs, fmt.Errorf("%s:%d: %s is also listed on line %d", p.file, cur.Line, TildePath(cur.Path), prev.Line))
		case strings.HasPrefix(cur.Path, prev.Path+string(filepath.Separator)):
			p.errs = append(p.errs, fmt.Errorf("%s:%d: %s is inside the repo %s (line %d)", p.file, cur.Line, TildePath(cur.Path), TildePath(prev.Path), prev.Line))
		}
	}
}

func mappingHas(n *yaml.Node, key string) bool {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return true
		}
	}
	return false
}

// ExpandHome replaces a leading ~ with the home directory.
func ExpandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[1:]), nil
}

// TildePath shortens a path under the home directory to start with ~.
func TildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return p
}
