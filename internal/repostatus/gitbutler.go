package repostatus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adnathanail/clonager/internal/cli"
)

// The subset of `but status --json` that clonager uses.
type butStatus struct {
	Stacks []struct {
		Branches []struct {
			Name         string `json:"name"`
			BranchStatus string `json:"branchStatus"`
			Commits      []struct {
				Conflicted *bool `json:"conflicted"`
			} `json:"commits"`
		} `json:"branches"`
	} `json:"stacks"`
}

// SetupTarget is the target branch `but setup` picks for the repo at path:
// origin's HEAD (refs/remotes/origin/HEAD, which a fresh clone has), e.g.
// origin/main. It's "" where that isn't set, as a clone made some other way
// may not have it. Where it's "", a target is always recorded, as a fresh
// clone's origin/HEAD can't be known offline.
func SetupTarget(path string) string {
	ref, _ := git{path}.run("symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD")
	return ref
}

// GitButlerSettings reads the target branch (e.g. upstream/main) and push
// remote of the GitButler workspace at path. Each is "" where it's what `but
// setup` picks: SetupTarget, and origin.
func GitButlerSettings(path string) (target, push string, err error) {
	target, push, _, err = gitButlerSettings(path)
	return target, push, err
}

// gitButlerSettings is GitButlerSettings, also returning the SetupTarget it
// compared the target with.
func gitButlerSettings(path string) (target, push, setupTarget string, err error) {
	var t struct {
		Branch string `json:"branch"`
	}
	if err := butJSON(path, &t, "config", "target", "--json"); err != nil {
		return "", "", "", err
	}
	var p struct {
		PushRemote string `json:"push_remote"`
	}
	if err := butJSON(path, &p, "config", "push-remote", "--json"); err != nil {
		return "", "", "", err
	}
	setupTarget = SetupTarget(path)
	if t.Branch != setupTarget {
		target = t.Branch
	}
	if p.PushRemote != "origin" {
		push = p.PushRemote
	}
	return target, push, setupTarget, nil
}

// ReadGitButlerSettings reads the target branch and push remote of a repo in
// a GitButler workspace (GitButler.Target and Push), as
// Options.GitButlerSettings does during Inspect, for reading them later.
func (s *Status) ReadGitButlerSettings() {
	if s.GitButler.Mode != GitButlerActive {
		return
	}
	var err error
	s.GitButler.Target, s.GitButler.Push, s.GitButler.SetupTarget, err = gitButlerSettings(s.Repo.Path)
	s.GitButler.Settings = err == nil
}

func (s *Status) readGitButler(gitDir string) {
	onWorkspace := s.Head == "gitbutler/workspace" || s.Head == "gitbutler/integration"
	_, err := os.Stat(filepath.Join(gitDir, "gitbutler"))
	hasData := err == nil

	switch {
	case onWorkspace:
		s.GitButler.Mode = GitButlerActive
	case hasData:
		s.GitButler.Mode = GitButlerInactive
		return
	default:
		return
	}

	var bs butStatus
	if err := butJSON(s.Repo.Path, &bs, "status", "--json"); err != nil {
		s.GitButler.Err = err
		return
	}
	for _, stack := range bs.Stacks {
		for _, b := range stack.Branches {
			s.GitButler.Branches = append(s.GitButler.Branches, GitButlerBranch{Name: b.Name, Status: b.BranchStatus})
			for _, c := range b.Commits {
				if c.Conflicted != nil && *c.Conflicted {
					s.GitButler.Conflicted++
				}
			}
		}
	}
}

// butJSON runs but in dir and decodes its JSON output into v. args end
// with --json.
func butJSON(dir string, v any, args ...string) error {
	if !cli.Installed("but") {
		return errors.New("but is not installed")
	}
	out, err := cli.But(dir, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("but %s: unexpected output: %w", strings.Join(args[:len(args)-1], " "), err)
	}
	return nil
}
