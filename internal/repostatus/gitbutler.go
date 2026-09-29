package repostatus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
