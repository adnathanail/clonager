package repostatus

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// githubRepo matches the owner/name in GitHub clone URLs: git@github.com:o/r.git,
// https://github.com/o/r and ssh://git@github.com/o/r.git.
var githubRepo = regexp.MustCompile(`^(?:git@github\.com:|(?:https|ssh)://(?:[^@/]+@)?github\.com/)([^/]+/[^/]+?)(?:\.git)?/?$`)

type mergedPR struct {
	Number      int    `json:"number"`
	HeadRefName string `json:"headRefName"`
	HeadRefOid  string `json:"headRefOid"`
}

// checkForge asks GitHub, via gh, about branches the local checks couldn't
// explain. A branch is merged if a merged PR's head is (or contains) the
// local tip; a merged PR whose head doesn't contain the tip means there's
// local work that never made it in.
func (s *Status) checkForge(g git) {
	var pending []*Branch
	for i := range s.Branches {
		b := &s.Branches[i]
		if b.Name != s.defaultBranchName() && b.Merged == "" && (b.Remote == "" || b.Gone || b.Ahead > 0) {
			pending = append(pending, b)
		}
	}
	if len(pending) == 0 {
		return
	}

	var repos []string
	seen := map[string]bool{}
	for _, name := range sortedKeys(s.RemoteURLs) {
		if m := githubRepo.FindStringSubmatch(s.RemoteURLs[name]); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			repos = append(repos, m[1])
		}
	}
	if len(repos) == 0 {
		return // not on GitHub
	}
	if _, err := exec.LookPath("gh"); err != nil {
		s.ForgeErr = errors.New("gh is not installed")
		return
	}

	byBranch := map[string][]mergedPR{}
	for _, repo := range repos {
		prs, err := listMergedPRs(repo)
		if err != nil {
			s.ForgeErr = err
			return
		}
		for _, pr := range prs {
			byBranch[pr.HeadRefName] = append(byBranch[pr.HeadRefName], pr)
		}
	}

	for _, b := range pending {
		prs := byBranch[b.Name]
		if len(prs) == 0 {
			continue
		}
		for _, pr := range prs {
			if pr.HeadRefOid == b.Tip || g.isAncestor(b.Tip, pr.HeadRefOid) {
				b.Merged, b.PR = MergedPR, pr.Number
				break
			}
		}
		if b.Merged == "" {
			b.PR, b.PRDiffers = prs[0].Number, true // gh lists newest first
		}
	}
}

func listMergedPRs(repo string) ([]mergedPR, error) {
	cmd := exec.Command("gh", "pr", "list", "--repo", repo, "--state", "merged",
		"--limit", "1000", "--json", "number,headRefName,headRefOid")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := firstLine(strings.TrimSpace(stderr.String()))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh pr list %s: %s", repo, msg)
	}
	var prs []mergedPR
	if err := json.Unmarshal(stdout.Bytes(), &prs); err != nil {
		return nil, fmt.Errorf("gh pr list %s: unexpected output: %w", repo, err)
	}
	return prs, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// origin first, then alphabetical
	sort.Slice(keys, func(i, j int) bool {
		if (keys[i] == "origin") != (keys[j] == "origin") {
			return keys[i] == "origin"
		}
		return keys[i] < keys[j]
	})
	return keys
}
