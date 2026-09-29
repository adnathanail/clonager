package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// New returns an empty config that will be written to path.
func New(path string) *Config {
	return &Config{Path: path}
}

// Contains reports whether a repo at path is already configured.
func (c *Config) Contains(path string) bool {
	for _, r := range c.Repos {
		if r.Path == path {
			return true
		}
	}
	return false
}

// Add inserts repo into the config tree. It goes under the most specific
// top-level key containing it, creating folders as needed; if no top-level
// key contains it, a new one is created for rootDir (which must contain the
// repo, or be the repo itself). The file isn't written until Save.
func (c *Config) Add(repo Repo, rootDir string) error {
	root := c.rootMapping()

	var parent *yaml.Node // mapping for the top-level key's value
	var base string
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i]
		dir, err := ExpandHome(key.Value)
		if err != nil {
			continue
		}
		dir = filepath.Clean(dir)
		if (repo.Path == dir || isUnder(repo.Path, dir)) && len(dir) > len(base) {
			base, parent = dir, root.Content[i+1]
		}
	}

	if parent == nil {
		if repo.Path != rootDir && !isUnder(repo.Path, rootDir) {
			return fmt.Errorf("%s is not inside %s", TildePath(repo.Path), TildePath(rootDir))
		}
		if repo.Path == rootDir {
			root.Content = append(root.Content, str(TildePath(rootDir)), repoNode(repo))
			return c.reparse()
		}
		parent = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, str(TildePath(rootDir)), parent)
		base = rootDir
	} else if repo.Path == base {
		return fmt.Errorf("%s is configured as a folder", TildePath(base))
	}

	rel, err := filepath.Rel(base, repo.Path)
	if err != nil {
		return err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	here := base
	for _, name := range parts[:len(parts)-1] {
		if repoKeys[name] {
			return fmt.Errorf("%s: folder name %q is reserved", TildePath(here), name)
		}
		here = filepath.Join(here, name)
		if !isFolder(parent) {
			return fmt.Errorf("%s is inside a configured repo", TildePath(repo.Path))
		}
		child := mappingGet(parent, name)
		if child == nil {
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			insertSorted(parent, name, child)
		}
		parent = child
	}
	if !isFolder(parent) {
		return fmt.Errorf("%s is inside a configured repo", TildePath(repo.Path))
	}
	name := parts[len(parts)-1]
	if repoKeys[name] {
		return fmt.Errorf("%s: repo name %q is reserved", TildePath(here), name)
	}
	if mappingGet(parent, name) != nil {
		return fmt.Errorf("%s is already in the config", TildePath(repo.Path))
	}
	insertSorted(parent, name, repoNode(repo))
	return c.reparse()
}

// Save writes the config back to its file, preserving comments.
func (c *Config) Save() error {
	data, err := c.encode()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.Path), ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if info, err := os.Stat(c.Path); err == nil {
		os.Chmod(tmp.Name(), info.Mode().Perm())
	}
	return os.Rename(tmp.Name(), c.Path)
}

func (c *Config) encode() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c.doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// reparse re-derives Repos from the edited tree, so Add can't leave the
// config in a state Load would reject.
func (c *Config) reparse() error {
	data, err := c.encode()
	if err != nil {
		return err
	}
	parsed, err := Parse(c.Path, data)
	if err != nil {
		return fmt.Errorf("edit would make the config invalid: %w", err)
	}
	c.Repos = parsed.Repos
	return nil
}

// rootMapping returns the document's top-level mapping, creating it for an
// empty file.
func (c *Config) rootMapping() *yaml.Node {
	if c.doc == nil || c.doc.Kind != yaml.DocumentNode || len(c.doc.Content) == 0 {
		c.doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	return c.doc.Content[0]
}

func repoNode(r Repo) *yaml.Node {
	if !r.GitButler && len(r.Remotes) == 0 && len(r.Tags) == 0 {
		return str(r.URL)
	}
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	n.Content = append(n.Content, str("url"), str(r.URL))
	if len(r.Remotes) > 0 {
		remotes := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, rem := range r.Remotes {
			remotes.Content = append(remotes.Content, str(rem.Name), str(rem.URL))
		}
		n.Content = append(n.Content, str("remotes"), remotes)
	}
	if r.GitButler {
		n.Content = append(n.Content, str("gitbutler"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	if len(r.Tags) > 0 {
		tags := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		for _, t := range r.Tags {
			tags.Content = append(tags.Content, str(t))
		}
		n.Content = append(n.Content, str("tags"), tags)
	}
	return n
}

func str(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func isFolder(n *yaml.Node) bool {
	return n.Kind == yaml.MappingNode && !mappingHas(n, "url")
}

func mappingGet(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// insertSorted adds key: val before the first existing key that sorts after
// it (case-insensitively), so alphabetised folders stay alphabetised.
func insertSorted(n *yaml.Node, key string, val *yaml.Node) {
	at := len(n.Content)
	for i := 0; i < len(n.Content); i += 2 {
		if strings.ToLower(n.Content[i].Value) > strings.ToLower(key) {
			at = i
			break
		}
	}
	n.Content = append(n.Content[:at], append([]*yaml.Node{str(key), val}, n.Content[at:]...)...)
}

func isUnder(path, dir string) bool {
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
