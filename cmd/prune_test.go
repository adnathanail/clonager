package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"main":               "main",
		"zak/tra-1-fix":      "zak/tra-1-fix",
		"feat#2":             "'feat#2'",
		"it's":               `'it'\''s'`,
		"a;rm -rf":           "'a;rm -rf'",
		"$(whoami)":          "'$(whoami)'",
		"release/v1.2+build": "release/v1.2+build",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestShellPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		filepath.Join(home, "Documents/x"):   "~/Documents/x",
		filepath.Join(home, "My Projects/x"): "~/'My Projects/x'",
		"/opt/src/tool":                      "/opt/src/tool",
		"/opt/my src":                        "'/opt/my src'",
	}
	for in, want := range cases {
		if got := shellPath(in); got != want {
			t.Errorf("shellPath(%q) = %s, want %s", in, got, want)
		}
	}
}

// localOnlyCommand is the only kind of line prune may print uncommented: a git
// command that touches nothing but the local clone.
var localOnlyCommand = regexp.MustCompile(`^git -C \S+ (branch -D \S+|remote prune origin)(  # .*)?$`)

func TestPruneOutput(t *testing.T) {
	no := false
	github := func(push, admin bool) *repostatus.GitHubRepo {
		return &repostatus.GitHubRepo{Name: "me/repo", CanPush: push, Admin: admin, AutoDelete: &no}
	}
	remote := []repostatus.RemoteBranch{
		{Name: "gone", Ref: "origin/gone", Stale: true},
		{Name: "done", Ref: "origin/done", Merged: repostatus.MergedRebased},
	}
	repo := func(name string) config.Repo { return config.Repo{Path: "/work/" + name} }

	statuses := []*repostatus.Status{
		{ // everything prune can suggest
			Repo:          repo("mine"),
			DefaultBranch: "origin/main",
			Head:          "current",
			Branches: []repostatus.Branch{
				{Name: "feat", Merged: repostatus.MergedRebased},
				{Name: "old", PR: 7, PRDiffers: true},
				{Name: "current", Merged: repostatus.MergedAncestor},
				{Name: "applied", Merged: repostatus.MergedPR, PR: 8},
				{Name: "open"},
			},
			GitButler:      repostatus.GitButler{Branches: []repostatus.GitButlerBranch{{Name: "applied"}}},
			RemoteBranches: remote,
			GitHub:         github(true, true),
		},
		{ // marked mine: false
			Repo:           config.Repo{Path: "/work/theirs", NotMine: true},
			DefaultBranch:  "origin/main",
			RemoteBranches: remote,
			GitHub:         github(true, true),
		},
		{ // no push access
			Repo:           repo("readonly"),
			DefaultBranch:  "origin/main",
			RemoteBranches: remote,
			GitHub:         github(false, false),
		},
		{Repo: repo("missing"), Missing: true},
		{Repo: repo("broken"), Err: errors.New("not a git repo")},
		{ // GitButler unreadable: applied branches unknown, so no deletions
			Repo:          repo("gb-broken"),
			DefaultBranch: "origin/main",
			Branches:      []repostatus.Branch{{Name: "maybe-applied", Merged: repostatus.MergedRebased}},
			GitButler:     repostatus.GitButler{Mode: repostatus.GitButlerActive, Err: errors.New("but status: boom")},
		},
		{ // GitHub failed: local results still stand
			Repo:          repo("gh-broken"),
			DefaultBranch: "origin/main",
			Branches:      []repostatus.Branch{{Name: "local-merged", Merged: repostatus.MergedAncestor}},
			ForgeErr:      errors.New("gh api: rate limited"),
		},
	}

	// As printed when piped, which strips styles.
	script, n := pruneScript(statuses, true)
	out := ansi.Strip(script)

	// Only local-only commands run; everything else is a comment.
	for _, line := range strings.Split(out, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") && !localOnlyCommand.MatchString(line) {
			t.Errorf("runnable line isn't a local-only git command: %q", line)
		}
	}
	syntax := exec.Command("sh", "-n")
	syntax.Stdin = strings.NewReader(out)
	if msg, err := syntax.CombinedOutput(); err != nil {
		t.Errorf("output isn't valid shell: %v\n%s", err, msg)
	}

	blocks := pruneBlocks(out)
	mustContain := map[string][]string{
		"/work/mine": {
			"git -C /work/mine branch -D feat  # rebased into origin/main",
			"# git -C /work/mine branch -D old  # PR #7 was merged",
			"# skipped current: checked out",
			"# skipped applied: applied in GitButler",
			"git -C /work/mine remote prune origin  # 1 ref deleted on GitHub: gone",
			"# git -C /work/mine push origin --delete done  # rebased into origin/main",
			"# gh repo edit me/repo --delete-branch-on-merge",
		},
		"/work/theirs":    {"git -C /work/theirs remote prune origin"},
		"/work/readonly":  {"git -C /work/readonly remote prune origin"},
		"/work/missing":   {"# couldn't check: not cloned"},
		"/work/broken":    {"# couldn't check: not a git repo"},
		"/work/gb-broken": {"# couldn't check: couldn't read GitButler's state"},
		"/work/gh-broken": {"git -C /work/gh-broken branch -D local-merged", "# couldn't check GitHub: gh api: rate limited"},
	}
	for repo, wants := range mustContain {
		for _, want := range wants {
			if !strings.Contains(blocks[repo], want) {
				t.Errorf("%s: missing %q in:\n%s", repo, want, blocks[repo])
			}
		}
	}
	mustNotContain := map[string][]string{
		"/work/mine":      {"branch -D open", "branch -D current", "branch -D applied"},
		"/work/theirs":    {"push origin", "gh repo edit"},
		"/work/readonly":  {"push origin", "gh repo edit", "skipped"},
		"/work/gb-broken": {"branch -D"},
	}
	for repo, unwanted := range mustNotContain {
		for _, u := range unwanted {
			if strings.Contains(blocks[repo], u) {
				t.Errorf("%s: shouldn't contain %q:\n%s", repo, u, blocks[repo])
			}
		}
	}

	summary := lastLine(out)
	if !strings.Contains(summary, "4 checks failed") || strings.Contains(summary, "Nothing to prune") {
		t.Errorf("summary %q should report 4 failed checks", summary)
	}
	if got := n.exitCode(); got != exitErrors {
		t.Errorf("exit code %d, want %d", got, exitErrors)
	}
}

func TestPruneExitCode(t *testing.T) {
	merged := []repostatus.Branch{{Name: "feat", Merged: repostatus.MergedAncestor}}
	for _, tc := range []struct {
		name   string
		status repostatus.Status
		want   int
	}{
		{"tidy", repostatus.Status{}, exitOK},
		{"merged branch", repostatus.Status{Branches: merged}, exitAttention},
		{"only on GitHub", repostatus.Status{
			GitHub: &repostatus.GitHubRepo{Name: "me/repo", CanPush: true, Admin: true, AutoDelete: new(bool)},
		}, exitAttention},
		{"not cloned", repostatus.Status{Missing: true}, exitErrors},
		{"GitHub failed", repostatus.Status{Branches: merged, ForgeErr: errors.New("rate limited")}, exitErrors},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.status
			s.Repo = config.Repo{Path: "/work/repo"}
			s.DefaultBranch = "origin/main"
			if _, n := pruneScript([]*repostatus.Status{&s}, true); n.exitCode() != tc.want {
				t.Errorf("exit code %d, want %d", n.exitCode(), tc.want)
			}
		})
	}
}

func TestPruneNothing(t *testing.T) {
	script, n := pruneScript([]*repostatus.Status{{
		Repo:          config.Repo{Path: "/work/tidy"},
		DefaultBranch: "origin/main",
		Branches:      []repostatus.Branch{{Name: "main"}, {Name: "open"}},
		GitHub:        &repostatus.GitHubRepo{Name: "me/tidy", CanPush: true, Admin: true},
	}}, true)
	if n.exitCode() != exitOK {
		t.Errorf("exit code %d, want %d", n.exitCode(), exitOK)
	}
	out := ansi.Strip(script)
	if got := strings.TrimSpace(out); got != "# Nothing to prune" {
		t.Errorf("got %q, want # Nothing to prune", got)
	}
}

// pruneBlocks splits prune output into each repo's lines, keyed by path.
func pruneBlocks(out string) map[string]string {
	blocks := map[string]string{}
	for _, block := range strings.Split(out, "\n\n") {
		header, body, _ := strings.Cut(block, "\n")
		blocks[strings.TrimPrefix(header, "# ")] = body
	}
	return blocks
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
