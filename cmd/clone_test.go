package cmd

import (
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

// cloneCommand is the only kind of line clone may print uncommented: a git
// command that creates a local clone or adds a remote to one.
var cloneCommand = regexp.MustCompile(`^git (clone \S+ ` + arg + `|-C ` + arg + ` remote add -f \S+ \S+)$`)

// arg is a shell word: bare, or single-quoted by shellQuote.
const arg = `(\S+|'[^']*')`

func TestCloneOutput(t *testing.T) {
	repo := func(name string) config.Repo {
		return config.Repo{Path: "/work/" + name, URL: "git@github.com:me/" + name + ".git"}
	}
	upstream := []config.Remote{{Name: "upstream", URL: "git@github.com:acmeltd/fork.git"}}
	withRemotes := func(r config.Repo) config.Repo { r.Remotes = upstream; return r }
	withGitButler := func(r config.Repo) config.Repo { r.GitButler = true; return r }
	urls := func(r config.Repo) map[string]string { return map[string]string{"origin": r.URL} }

	fork := withRemotes(repo("fork"))
	fork2 := withRemotes(repo("fork2"))
	moved := repo("moved")
	gb := withGitButler(repo("gb"))
	gbDone := withGitButler(repo("gb-done"))
	statuses := []*repostatus.Status{
		{Repo: withGitButler(fork), Missing: true},
		{Repo: config.Repo{Path: "/work/my repo", URL: "https://example.com/me/x.git"}, Missing: true},
		{Repo: repo("empty"), Err: errors.New("not a git repo")},
		{Repo: repo("broken"), Err: errors.New("not a git repo")},
		{Repo: repo("tidy"), OriginURL: repo("tidy").URL, RemoteURLs: urls(repo("tidy"))},
		{Repo: repo("no-origin"), RemoteURLs: map[string]string{}},
		{ // origin moved, upstream missing
			Repo:       withRemotes(moved),
			OriginURL:  "git@github.com:old/moved.git",
			RemoteURLs: map[string]string{"origin": "git@github.com:old/moved.git"},
		},
		{ // upstream differs
			Repo:       fork2,
			OriginURL:  fork2.URL,
			RemoteURLs: map[string]string{"origin": fork2.URL, "upstream": "git@github.com:other/fork.git"},
		},
		{Repo: gb, OriginURL: gb.URL, RemoteURLs: urls(gb)},
		{Repo: gbDone, OriginURL: gbDone.URL, RemoteURLs: urls(gbDone),
			GitButler: repostatus.GitButler{Mode: repostatus.GitButlerActive}},
	}
	isEmpty := func(p string) bool { return p == "/work/empty" }

	script, n := cloneScript(statuses, isEmpty)
	out := ansi.Strip(script)

	for _, line := range strings.Split(out, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") && !cloneCommand.MatchString(line) {
			t.Errorf("runnable line isn't a clone or remote add: %q", line)
		}
	}
	syntax := exec.Command("sh", "-n")
	syntax.Stdin = strings.NewReader(out)
	if msg, err := syntax.CombinedOutput(); err != nil {
		t.Errorf("output isn't valid shell: %v\n%s", err, msg)
	}

	blocks := pruneBlocks(out)
	mustContain := map[string][]string{
		"/work/fork": {
			"git clone git@github.com:me/fork.git /work/fork",
			"git -C /work/fork remote add -f upstream git@github.com:acmeltd/fork.git",
			"# but -C /work/fork setup  # switches to GitButler's workspace branch",
		},
		"/work/my repo":   {"git clone https://example.com/me/x.git '/work/my repo'"},
		"/work/empty":     {"git clone git@github.com:me/empty.git /work/empty"},
		"/work/broken":    {"# couldn't check: not a git repo"},
		"/work/no-origin": {"git -C /work/no-origin remote add -f origin git@github.com:me/no-origin.git"},
		"/work/moved": {
			"# git -C /work/moved remote set-url origin git@github.com:me/moved.git  # origin is git@github.com:old/moved.git",
			"git -C /work/moved remote add -f upstream git@github.com:acmeltd/fork.git",
		},
		"/work/fork2": {"# git -C /work/fork2 remote set-url upstream git@github.com:acmeltd/fork.git  # upstream is git@github.com:other/fork.git"},
		"/work/gb":    {"# but -C /work/gb setup"},
	}
	for repo, wants := range mustContain {
		for _, want := range wants {
			if !strings.Contains(blocks[repo], want) {
				t.Errorf("%s: missing %q in:\n%s", repo, want, blocks[repo])
			}
		}
	}
	for _, repo := range []string{"/work/tidy", "/work/gb-done"} {
		if b, ok := blocks[repo]; ok {
			t.Errorf("%s: should have nothing to do, got:\n%s", repo, b)
		}
	}
	if strings.Contains(blocks["/work/fork"], "remote add -f origin") {
		t.Errorf("/work/fork: git clone already adds origin:\n%s", blocks["/work/fork"])
	}

	want := "# 3 repos to clone, 3 remotes to add, 4 commented out to review, 1 check failed"
	if got := lastLine(out); got != want {
		t.Errorf("summary %q, want %q", got, want)
	}
	if got := n.exitCode(); got != exitErrors {
		t.Errorf("exit code %d, want %d", got, exitErrors)
	}
}

func TestCloneExitCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status repostatus.Status
		want   int
	}{
		{"set up", repostatus.Status{OriginURL: "u", RemoteURLs: map[string]string{"origin": "u"}}, exitOK},
		{"not cloned", repostatus.Status{Missing: true}, exitAttention},
		{"origin differs", repostatus.Status{OriginURL: "v", RemoteURLs: map[string]string{"origin": "v"}}, exitAttention},
		{"not a git repo", repostatus.Status{Err: errors.New("not a git repo")}, exitErrors},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.status
			s.Repo = config.Repo{Path: "/work/repo", URL: "u"}
			_, n := cloneScript([]*repostatus.Status{&s}, func(string) bool { return false })
			if n.exitCode() != tc.want {
				t.Errorf("exit code %d, want %d", n.exitCode(), tc.want)
			}
		})
	}
}

func TestCloneNothing(t *testing.T) {
	script, _ := cloneScript(nil, emptyDir)
	if got := strings.TrimSpace(ansi.Strip(script)); got != "# Nothing to clone" {
		t.Errorf("got %q, want # Nothing to clone", got)
	}
}
