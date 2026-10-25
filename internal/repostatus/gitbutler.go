package repostatus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adnathanail/clonager/internal/cli"
	"github.com/adnathanail/clonager/internal/discover"
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

// ReadGitButlerSettings reads the target branch and push remote of a repo in
// a GitButler workspace (GitButler.Target and Push). It's separate from
// Inspect as the two `but` calls add noticeably to it, and they're only for
// status's note of what config tidy would record. tidy reports it itself if
// they can't be read.
func (s *Status) ReadGitButlerSettings() {
	if s.GitButler.Mode != GitButlerActive {
		return
	}
	var err error
	s.GitButler.Target, s.GitButler.Push, err = discover.GitButlerSettings(s.Repo.Path)
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

	if !cli.Installed("but") {
		s.GitButler.Err = errors.New("but is not installed")
		return
	}
	out, err := cli.But(s.Repo.Path, "status", "--json")
	if err != nil {
		s.GitButler.Err = err
		return
	}
	var bs butStatus
	if err := json.Unmarshal(out, &bs); err != nil {
		s.GitButler.Err = fmt.Errorf("but status: unexpected output: %w", err)
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
