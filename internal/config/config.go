// Package config loads the clonager config file.
//
// The file is a YAML tree that mirrors the filesystem. Top-level keys are
// absolute paths (~ allowed). Below them, a string value is a repo (the string
// is its clone URL), a mapping containing "url" is a repo with options, and any
// other mapping is a folder containing more folders or repos.
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
}

type Remote struct {
	Name string
	URL  string
}

type Repo struct {
	Path      string // absolute path on disk
	URL       string // clone URL for origin
	Remotes   []Remote
	GitButler bool
	Tags      []string
	Line      int // line in the config file, for error messages
}

// Name is the repo's directory name.
func (r Repo) Name() string { return filepath.Base(r.Path) }

type Config struct {
	Path  string
	Repos []Repo
	doc   *yaml.Node // kept so later commands can edit the file in place
}

// DefaultPath returns where the config lives unless overridden:
// $CLONAGER_CONFIG, then $XDG_CONFIG_HOME/clonager/config.yaml, then
// ~/.config/clonager/config.yaml. (os.UserConfigDir is deliberately not used,
// as on macOS it points at ~/Library/Application Support.)
func DefaultPath() (string, error) {
	if p := os.Getenv("CLONAGER_CONFIG"); p != "" {
		return ExpandHome(p)
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "clonager", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "clonager", "config.yaml"), nil
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
		p.node(filepath.Clean(dir), val)
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
func (p *parser) node(path string, n *yaml.Node) {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" || n.Value == "" {
			p.errorf(n, "%s: missing url", TildePath(path))
			return
		}
		p.repos = append(p.repos, Repo{Path: path, URL: n.Value, Line: n.Line})
	case yaml.MappingNode:
		if mappingHas(n, "url") {
			p.repo(path, n)
		} else {
			p.folder(path, n)
		}
	default:
		p.errorf(n, "%s: expected a url, repo options or a folder", TildePath(path))
	}
}

func (p *parser) folder(path string, n *yaml.Node) {
	for i := 0; i < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		name := key.Value
		switch {
		case repoKeys[name]:
			p.errorf(key, "%s: %q is only valid in a repo (did you forget url?)", TildePath(path), name)
		case name == "" || name == "." || name == ".." || strings.Contains(name, "/"):
			p.errorf(key, "%s: %q is not a valid folder or repo name", TildePath(path), name)
		default:
			p.node(filepath.Join(path, name), val)
		}
	}
}

func (p *parser) repo(path string, n *yaml.Node) {
	r := Repo{Path: path, Line: n.Line}
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
	p.repos = append(p.repos, r)
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
