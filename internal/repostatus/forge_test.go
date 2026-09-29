package repostatus

import (
	"strings"
	"testing"
)

// TestApplyForge checks how GitHub's answers change a status, with made-up
// PRs and branches in place of calls to gh.
func TestApplyForge(t *testing.T) {
	s := &Status{
		DefaultBranch: "origin/main",
		Branches: []Branch{
			{Name: "main", Tip: "m1", Remote: "origin/main"},
			{Name: "pr-exact", Tip: "a1"},                                      // local-only; PR merged at this commit
			{Name: "pr-later", Tip: "b1"},                                      // local-only; PR merged with more commits on top
			{Name: "differs", Tip: "c1"},                                       // local-only; PR merged without this commit
			{Name: "develop", Tip: "d1", Remote: "origin/develop"},             // long-lived, up to date; had PRs merged from older commits
			{Name: "rebased", Tip: "r1", Merged: MergedRebased},                // already settled locally
			{Name: "no-pr", Tip: "n1"},                                         // local-only; no PR at all
			{Name: "unpushed", Tip: "u2", Remote: "origin/unpushed", Ahead: 1}, // flagged; PR merged from before the unpushed commit
		},
		RemoteBranches: []RemoteBranch{
			{Name: "main", Ref: "origin/main", Tip: "m1"},
			{Name: "stale", Ref: "origin/stale", Tip: "s1", Merged: MergedRebased},      // deleted on GitHub
			{Name: "develop", Ref: "origin/develop", Tip: "d1", Merged: MergedAncestor}, // protected
			{Name: "merged-live", Ref: "origin/merged-live", Tip: "l1", Merged: MergedRebased},
			{Name: "bot", Ref: "origin/bot", Tip: "e2"},                                          // bot reused the branch after its PR merged
			{Name: "pushed-since", Ref: "origin/pushed-since", Tip: "f1", Merged: MergedRebased}, // GitHub has moved on to f2
			{Name: "pr-remote", Ref: "origin/pr-remote", Tip: "g1"},                              // squash-merged PR, found only via GitHub
			{Name: "open", Ref: "origin/open", Tip: "o1"},                                        // unmerged, no PR merged
		},
	}
	ancestors := map[[2]string]bool{{"b1", "b2"}: true, {"u1", "u2"}: true}
	isAncestor := func(a, b string) bool { return ancestors[[2]string{a, b}] }

	s.applyForge(forgeData{
		origin: true,
		info:   &GitHubRepo{Name: "me/repo", CanPush: true},
		live: map[string]liveBranch{
			"main":         {sha: "m1", protected: true},
			"develop":      {sha: "d1", protected: true},
			"merged-live":  {sha: "l1"},
			"bot":          {sha: "e2"},
			"pushed-since": {sha: "f2"},
			"pr-remote":    {sha: "g1"},
			"open":         {sha: "o1"},
		},
		prs: []mergedPR{
			{Number: 10, HeadRefName: "pr-exact", HeadRefOid: "a1"},
			{Number: 11, HeadRefName: "pr-later", HeadRefOid: "b2"},
			{Number: 12, HeadRefName: "differs", HeadRefOid: "c0"},
			{Number: 13, HeadRefName: "develop", HeadRefOid: "d0"},
			{Number: 14, HeadRefName: "bot", HeadRefOid: "e1"},
			{Number: 15, HeadRefName: "pr-remote", HeadRefOid: "g1"},
			{Number: 16, HeadRefName: "unpushed", HeadRefOid: "u1"},
		},
	}, isAncestor)

	if s.GitHub == nil || s.GitHub.Name != "me/repo" {
		t.Errorf("GitHub info not recorded: %+v", s.GitHub)
	}

	type local struct {
		merged    string
		pr        int
		prDiffers bool
	}
	wantLocal := map[string]local{
		"main":     {},
		"pr-exact": {MergedPR, 10, false},
		"pr-later": {MergedPR, 11, false},
		"differs":  {"", 12, true},
		"develop":  {}, // not flagged, so no differs warning
		"rebased":  {MergedRebased, 0, false},
		"no-pr":    {},
		"unpushed": {"", 16, true}, // its unpushed commit isn't in the PR
	}
	for _, b := range s.Branches {
		got := local{b.Merged, b.PR, b.PRDiffers}
		if got != wantLocal[b.Name] {
			t.Errorf("branch %s: got %+v, want %+v", b.Name, got, wantLocal[b.Name])
		}
	}

	type remote struct {
		merged       string
		pr           int
		stale, moved bool
	}
	wantRemote := map[string]remote{
		"main":         {},
		"stale":        {MergedRebased, 0, true, false},
		"develop":      {}, // protected: never counted as merged
		"merged-live":  {MergedRebased, 0, false, false},
		"bot":          {"", 14, false, true},
		"pushed-since": {MergedRebased, 0, false, true},
		"pr-remote":    {MergedPR, 15, false, false},
		"open":         {},
	}
	for _, rb := range s.RemoteBranches {
		got := remote{rb.Merged, rb.PR, rb.Stale, rb.Moved}
		if got != wantRemote[rb.Name] {
			t.Errorf("remote %s: got %+v, want %+v", rb.Ref, got, wantRemote[rb.Name])
		}
	}

	var merged, stale []string
	for _, rb := range s.MergedRemote() {
		merged = append(merged, rb.Name)
	}
	for _, rb := range s.StaleRemote() {
		stale = append(stale, rb.Name)
	}
	if got := strings.Join(merged, ","); got != "merged-live,pr-remote" {
		t.Errorf("MergedRemote = %s, want merged-live,pr-remote", got)
	}
	if got := strings.Join(stale, ","); got != "stale" {
		t.Errorf("StaleRemote = %s, want stale", got)
	}
}

// TestApplyForgeNotOnGitHub covers origin elsewhere (another GitHub remote
// only): no repo info, and no branch is marked stale.
func TestApplyForgeNotOnGitHub(t *testing.T) {
	s := &Status{
		DefaultBranch:  "origin/main",
		Branches:       []Branch{{Name: "feature", Tip: "a1"}},
		RemoteBranches: []RemoteBranch{{Name: "feature", Ref: "origin/feature", Tip: "a1"}},
	}
	s.applyForge(forgeData{prs: []mergedPR{{Number: 3, HeadRefName: "feature", HeadRefOid: "a1"}}},
		func(a, b string) bool { return false })
	if s.GitHub != nil {
		t.Errorf("GitHub info set without origin on GitHub")
	}
	if s.RemoteBranches[0].Stale {
		t.Errorf("remote branch marked stale without origin's branch list")
	}
	if b := s.Branches[0]; b.Merged != MergedPR || b.PR != 3 {
		t.Errorf("local branch: got %+v, want merged in PR #3", b)
	}
}
