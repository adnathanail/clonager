package repostatus

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adnathanail/clonager/internal/config"
)

// TestCheckMerged builds a repo with a bare "remote" and one
// branch per way of being merged (or not), then checks each is classified.
func TestCheckMerged(t *testing.T) {
	dir := t.TempDir()
	remote, work := filepath.Join(dir, "remote.git"), filepath.Join(dir, "work")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(file, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		run(work, "add", file)
		run(work, "commit", "-q", "-m", file+": "+content)
	}

	run(dir, "init", "-q", "--bare", "-b", "main", remote)
	run(dir, "clone", "-q", remote, work)
	run(work, "checkout", "-q", "-b", "main")
	commit("base", "1")
	run(work, "push", "-q", "-u", "origin", "main")

	// ancestor: fast-forward merged
	run(work, "checkout", "-q", "-b", "ff")
	commit("ff", "1")
	run(work, "checkout", "-q", "main")
	run(work, "merge", "-q", "--ff-only", "ff")

	// rebased: two commits, cherry-picked onto main after it moved on
	run(work, "checkout", "-q", "-b", "rebased", "main")
	commit("r1", "1")
	commit("r2", "1")
	run(work, "checkout", "-q", "main")
	commit("other", "1")
	run(work, "cherry-pick", "rebased~1", "rebased")

	// squashed: two commits, landed as one
	run(work, "checkout", "-q", "-b", "squashed", "main~3")
	commit("s1", "1")
	commit("s2", "1")
	run(work, "checkout", "-q", "main")
	run(work, "merge", "-q", "--squash", "squashed")
	run(work, "commit", "-q", "-m", "squash")

	// partly: only one of its two commits landed
	run(work, "checkout", "-q", "-b", "partly", "main")
	commit("p1", "1")
	commit("p2", "1")
	run(work, "checkout", "-q", "main")
	run(work, "cherry-pick", "partly~1")

	// pushed: up to date with its remote branch, which outlived a rebase merge
	run(work, "checkout", "-q", "-b", "pushed", "main")
	commit("pu", "1")
	run(work, "push", "-q", "-u", "origin", "pushed")
	run(work, "checkout", "-q", "main")
	commit("other2", "1")
	run(work, "cherry-pick", "pushed")

	// pushedOpen: up to date with its remote, not merged
	run(work, "checkout", "-q", "-b", "pushedOpen", "main")
	commit("po", "1")
	run(work, "push", "-q", "-u", "origin", "pushedOpen")

	// unmerged
	run(work, "checkout", "-q", "-b", "unmerged", "main")
	commit("u", "1")
	run(work, "checkout", "-q", "main")
	run(work, "push", "-q", "origin", "main")
	run(work, "remote", "set-head", "origin", "main")

	s := Inspect(config.Repo{Path: work, URL: remote}, Options{})
	if s.Err != nil {
		t.Fatal(s.Err)
	}
	if s.DefaultBranch != "origin/main" {
		t.Errorf("default branch %q, want origin/main", s.DefaultBranch)
	}
	want := map[string]string{
		"main":       "",
		"ff":         MergedAncestor,
		"rebased":    MergedRebased,
		"squashed":   MergedSquashed,
		"partly":     "",
		"pushed":     MergedRebased,
		"pushedOpen": "",
		"unmerged":   "",
	}
	for _, b := range s.Branches {
		w, ok := want[b.Name]
		if !ok {
			t.Errorf("unexpected branch %s", b.Name)
			continue
		}
		if b.Merged != w {
			t.Errorf("%s: merged %q, want %q", b.Name, b.Merged, w)
		}
		delete(want, b.Name)
	}
	for name := range want {
		t.Errorf("branch %s missing", name)
	}

	var localOnly []string
	for _, b := range s.LocalOnly() {
		localOnly = append(localOnly, b.Name)
	}
	if got := strings.Join(localOnly, ","); got != "partly,unmerged" {
		t.Errorf("local-only %q, want partly,unmerged", got)
	}
}

func TestGithubRepo(t *testing.T) {
	cases := map[string]string{
		"git@github.com:owner/repo.git":         "owner/repo",
		"git@github.com:owner/repo":             "owner/repo",
		"https://github.com/owner/repo.git":     "owner/repo",
		"https://github.com/owner/repo/":        "owner/repo",
		"ssh://git@github.com/owner/repo.git":   "owner/repo",
		"https://user@github.com/owner/re.po":   "owner/re.po",
		"git@gitlab.com:owner/repo.git":         "",
		"https://github.com.evil.com/owner/rep": "",
	}
	for url, want := range cases {
		got := ""
		if m := githubRepo.FindStringSubmatch(url); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", url, got, want)
		}
	}
}
