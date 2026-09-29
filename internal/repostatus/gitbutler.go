package repostatus

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	if _, err := exec.LookPath("but"); err != nil {
		s.GitButler.Err = errors.New("but is not installed")
		return
	}
	cmd := exec.Command("but", "status", "--json")
	cmd.Dir = s.Repo.Path
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		s.GitButler.Err = fmt.Errorf("but status: %s", firstLine(msg))
		return
	}
	var bs butStatus
	if err := json.Unmarshal(stdout.Bytes(), &bs); err != nil {
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

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
