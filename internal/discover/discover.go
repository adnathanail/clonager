// Package discover finds git repos on disk and reads what's needed to add
// them to the config.
package discover

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adnathanail/clonager/internal/config"
)

// Directories never worth descending into: huge, and never home to clones
// you'd want managed.
var skipDirs = map[string]bool{
	"node_modules": true,
	".venv":        true,
	"venv":         true,
	".cache":       true,
	".Trash":       true,
	"Library":      true,
	".lake":        true, // Lean/Rocq package checkouts
	"_opam":        true,
}

// Find returns the git repos under root, down to maxDepth levels below it
// (root itself is depth 0). It doesn't look inside repos, so submodules and
// clones nested in other clones aren't returned.
func Find(root string, maxDepth int) ([]string, error) {
	var repos []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return fs.SkipDir // unreadable; carry on
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipDirs[d.Name()] {
			return fs.SkipDir
		}
		if isRepo(path) {
			repos = append(repos, path)
			return fs.SkipDir
		}
		if depth(root, path) >= maxDepth {
			return fs.SkipDir
		}
		return nil
	})
	sort.Slice(repos, func(i, j int) bool { return strings.ToLower(repos[i]) < strings.ToLower(repos[j]) })
	return repos, err
}

func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git")) // a directory, or a file for worktrees
	return err == nil
}

func depth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

var ErrNoOrigin = errors.New("no origin remote, so there's nothing to clone it from")

// Describe builds the config entry for the repo at path: origin as its URL,
// any other remotes, and whether it's in a GitButler workspace.
func Describe(path string) (config.Repo, error) {
	repo := config.Repo{Path: path}

	out, err := git(path, "config", "--get-regexp", `^remote\..*\.url$`)
	if err != nil && out != "" {
		return repo, err
	}
	for _, line := range strings.Split(out, "\n") {
		key, url, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")
		if name == "origin" {
			repo.URL = url
		} else {
			repo.Remotes = append(repo.Remotes, config.Remote{Name: name, URL: url})
		}
	}
	if repo.URL == "" {
		return repo, ErrNoOrigin
	}
	sort.Slice(repo.Remotes, func(i, j int) bool { return repo.Remotes[i].Name < repo.Remotes[j].Name })

	head, _ := git(path, "symbolic-ref", "--short", "-q", "HEAD")
	repo.GitButler = head == "gitbutler/workspace" || head == "gitbutler/integration"
	return repo, nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\n"), err
}
