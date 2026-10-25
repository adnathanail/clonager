// Package discover finds git repos on disk and reads what's needed to add
// them to the config.
package discover

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adnathanail/clonager/internal/cli"
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
// any other remotes, and whether it's in a GitButler workspace, with its
// target branch and push remote where they differ from `but setup`'s.
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

	repo.GitButler = InGitButler(path)
	if repo.GitButler {
		// If they can't be read (e.g. but isn't installed), the repo is still
		// added, and config tidy fills them in later.
		repo.GitButlerTarget, repo.GitButlerPush, _ = GitButlerSettings(path)
	}
	return repo, nil
}

// GitButlerSettings reads the target branch (e.g. upstream/main) and push
// remote of the GitButler workspace at path. Each is "" where it's what `but
// setup` picks: origin's default branch (refs/remotes/origin/HEAD, which a
// fresh clone has), and origin.
func GitButlerSettings(path string) (target, push string, err error) {
	var t struct {
		Branch string `json:"branch"`
	}
	if err := butJSON(path, &t, "config", "target", "--json"); err != nil {
		return "", "", err
	}
	var p struct {
		PushRemote string `json:"push_remote"`
	}
	if err := butJSON(path, &p, "config", "push-remote", "--json"); err != nil {
		return "", "", err
	}
	if def, _ := git(path, "symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD"); t.Branch != def {
		target = t.Branch
	}
	if p.PushRemote != "origin" {
		push = p.PushRemote
	}
	return target, push, nil
}

// butJSON runs but in dir and decodes its JSON output into v.
func butJSON(dir string, v any, args ...string) error {
	if !cli.Installed("but") {
		return errors.New("but is not installed")
	}
	out, err := cli.But(dir, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("but %s %s: unexpected output: %w", args[0], args[1], err)
	}
	return nil
}

// HasOrigin reports whether the repo at path has an origin remote, which
// Describe needs. It's cheaper than Describe, which also asks GitButler
// about repos in a workspace.
func HasOrigin(path string) bool {
	url, _ := git(path, "config", "--get", "remote.origin.url")
	return url != ""
}

// InGitButler reports whether the repo at path is in a GitButler workspace:
// HEAD is gitbutler/workspace, or the older gitbutler/integration. It's
// false when path isn't a repo.
func InGitButler(path string) bool {
	head, _ := git(path, "symbolic-ref", "--short", "-q", "HEAD")
	return head == "gitbutler/workspace" || head == "gitbutler/integration"
}

func git(dir string, args ...string) (string, error) {
	return cli.Git(dir, args...)
}
