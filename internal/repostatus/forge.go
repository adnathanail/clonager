package repostatus

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/adnathanail/clonager/internal/cli"
)

// githubRepo matches the owner/name in GitHub clone URLs: git@github.com:o/r.git,
// https://github.com/o/r and ssh://git@github.com/o/r.git.
var githubRepo = regexp.MustCompile(`^(?:git@github\.com:|(?:https|ssh)://(?:[^@/]+@)?github\.com/)([^/]+/[^/]+?)(?:\.git)?/?$`)

type mergedPR struct {
	Number      int    `json:"number"`
	HeadRefName string `json:"headRefName"`
	HeadRefOid  string `json:"headRefOid"`
}

// checkForge asks GitHub, via gh, about what the local checks couldn't
// settle:
//
//   - Local branches: merged if a merged PR's head is (or contains) the local
//     tip. A merged PR whose head doesn't contain the tip of an otherwise
//     flagged branch means there may be local work that never made it in.
//   - origin's branches: whether each still exists on GitHub (if not, the
//     local ref is stale), and whether it's merged at the commit GitHub has.
func (s *Status) checkForge(g git) {
	var repos []string // every GitHub remote, origin first
	seen := map[string]bool{}
	for _, name := range sortedKeys(s.RemoteURLs) {
		if repo := githubName(s.RemoteURLs[name]); repo != "" && !seen[repo] {
			seen[repo] = true
			repos = append(repos, repo)
		}
	}
	if len(repos) == 0 {
		return // not on GitHub
	}
	if !cli.Installed("gh") {
		s.ForgeErr = errors.New("gh is not installed")
		return
	}

	// The API calls are independent, so run them at once: the merged PR list
	// can take seconds for a busy repo.
	origin := githubName(s.OriginURL)
	var (
		wg      sync.WaitGroup
		info    *GitHubRepo
		live    map[string]liveBranch
		prLists = make([][]mergedPR, len(repos))
		errs    = make([]error, len(repos)+2)
	)
	if origin != "" {
		wg.Add(2)
		go func() { defer wg.Done(); info, errs[0] = repoInfo(origin) }()
		go func() { defer wg.Done(); live, errs[1] = listBranches(origin) }()
	}
	for i, repo := range repos {
		wg.Add(1)
		go func() { defer wg.Done(); prLists[i], errs[i+2] = listMergedPRs(repo) }()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		s.ForgeErr = firstError(errs)
		return
	}

	// origin's live branches, to spot stale refs and branches pushed to since
	// the last fetch.
	liveTip := map[string]string{} // Ref -> commit on GitHub
	if origin != "" {
		s.GitHub = info
		for i := range s.RemoteBranches {
			rb := &s.RemoteBranches[i]
			switch b, ok := live[rb.Name]; {
			case !ok:
				rb.Stale = true
			case b.protected:
				rb.Merged = "" // long-lived (main, develop, release branches); never suggest deleting
			default:
				liveTip[rb.Ref] = b.sha
			}
		}
	}

	var pendingLocal []*Branch
	for i := range s.Branches {
		if b := &s.Branches[i]; b.Name != s.defaultBranchName() && b.Merged == "" {
			pendingLocal = append(pendingLocal, b)
		}
	}
	var pendingRemote []*RemoteBranch
	for i := range s.RemoteBranches {
		rb := &s.RemoteBranches[i]
		if tip, ok := liveTip[rb.Ref]; ok && rb.Ref != s.DefaultBranch && (rb.Merged == "" || tip != rb.Tip) {
			pendingRemote = append(pendingRemote, rb)
		}
	}
	byBranch := map[string][]mergedPR{}
	for _, prs := range prLists {
		for _, pr := range prs {
			byBranch[pr.HeadRefName] = append(byBranch[pr.HeadRefName], pr)
		}
	}
	// mergedPRFor finds a merged PR whose head is or contains tip.
	mergedPRFor := func(name, tip string) (pr int, found bool) {
		for _, p := range byBranch[name] {
			if p.HeadRefOid == tip || g.isAncestor(tip, p.HeadRefOid) {
				return p.Number, true
			}
		}
		return 0, false
	}

	for _, b := range pendingLocal {
		if pr, ok := mergedPRFor(b.Name, b.Tip); ok {
			b.Merged, b.PR = MergedPR, pr
		} else if prs := byBranch[b.Name]; len(prs) > 0 && b.flagged() {
			// Only a warning for branches that are flagged anyway: a
			// long-lived branch that's up to date with its remote (develop,
			// say) will have had PRs merged from older commits.
			b.PR, b.PRDiffers = prs[0].Number, true // gh lists newest first
		}
	}

	for _, rb := range pendingRemote {
		tip := liveTip[rb.Ref]
		if pr, ok := mergedPRFor(rb.Name, tip); ok {
			rb.Merged, rb.PR = MergedPR, pr
			continue
		}
		prs := byBranch[rb.Name]
		if rb.Merged != "" || len(prs) > 0 {
			// Merged at some point, but GitHub's copy has moved on since.
			rb.Moved = true
			if len(prs) > 0 {
				rb.PR = prs[0].Number
			}
		}
	}
}

func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func githubName(url string) string {
	if m := githubRepo.FindStringSubmatch(url); m != nil {
		return m[1]
	}
	return ""
}

func repoInfo(repo string) (*GitHubRepo, error) {
	out, err := gh("api", "repos/"+repo, "--jq",
		"{autoDelete: .delete_branch_on_merge, push: .permissions.push, admin: .permissions.admin}")
	if err != nil {
		return nil, err
	}
	var v struct {
		AutoDelete *bool `json:"autoDelete"`
		Push       bool  `json:"push"`
		Admin      bool  `json:"admin"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("gh api repos/%s: unexpected output: %w", repo, err)
	}
	return &GitHubRepo{Name: repo, AutoDelete: v.AutoDelete, CanPush: v.Push, Admin: v.Admin}, nil
}

type liveBranch struct {
	sha       string
	protected bool
}

func listBranches(repo string) (map[string]liveBranch, error) {
	out, err := gh("api", "--paginate", "repos/"+repo+"/branches?per_page=100", "--jq",
		`.[] | [.name, .commit.sha, (.protected | tostring)] | @tsv`)
	if err != nil {
		return nil, err
	}
	branches := map[string]liveBranch{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f := strings.Split(line, "\t"); len(f) == 3 {
			branches[f[0]] = liveBranch{sha: f[1], protected: f[2] == "true"}
		}
	}
	return branches, nil
}

// gh runs a read-only GitHub CLI command via internal/cli.
func gh(args ...string) ([]byte, error) {
	return cli.GH(args...)
}

func listMergedPRs(repo string) ([]mergedPR, error) {
	out, err := gh("pr", "list", "--repo", repo, "--state", "merged",
		"--limit", "1000", "--json", "number,headRefName,headRefOid")
	if err != nil {
		return nil, err
	}
	var prs []mergedPR
	if err := json.Unmarshal(out, &prs); err != nil {
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
