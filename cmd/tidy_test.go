package cmd

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

func TestSSHURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/acmeltd/vip-proj":        "git@github.com:acmeltd/vip-proj.git",
		"https://github.com/acmeltd/vip-proj.git":    "git@github.com:acmeltd/vip-proj.git",
		"https://github.com/acmeltd/vip-proj/":       "git@github.com:acmeltd/vip-proj.git",
		"https://me@GitHub.com/acmeltd/vip-proj.git": "git@github.com:acmeltd/vip-proj.git",
		"https://gitlab.com/acmeltd/team/vip-proj":   "git@gitlab.com:acmeltd/team/vip-proj.git",
		"https://me@bitbucket.org/acmeltd/vip-proj":  "git@bitbucket.org:acmeltd/vip-proj.git",
		"https://codeberg.org/me/dotfiles.git":       "git@codeberg.org:me/dotfiles.git",
		"git@github.com:acmeltd/vip-proj.git":        "", // already SSH
		"ssh://git@github.com/acmeltd/vip-proj.git":  "",
		"http://github.com/acmeltd/vip-proj":         "",
		"https://git.acme.example/acmeltd/vip-proj":  "", // unknown host
		"https://github.com:8443/acmeltd/vip-proj":   "",
		"https://github.com/acmeltd":                 "",
		"https://github.com//vip-proj":               "",
		"https://github.com/acmeltd/vip-proj?x=1":    "",
		"/srv/git/vip-proj.git":                      "",
	}
	for in, want := range cases {
		got, ok := sshURL(in)
		if got != want || ok != (want != "") {
			t.Errorf("sshURL(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestSSHChanges(t *testing.T) {
	repos := []config.Repo{
		{Path: "/w/a", URL: "https://github.com/me/a", Remotes: []config.Remote{
			{Name: "upstream", URL: "https://github.com/acmeltd/a.git"},
			{Name: "mirror", URL: "https://git.acme.example/a"},
		}},
		{Path: "/w/b", URL: "git@github.com:me/b.git"},
		{Path: "/w/c", URL: "https://github.com/acmeltd/a"}, // same SSH URL as a's upstream
	}
	changes := sshChanges(repos)
	var mu sync.Mutex
	var calls []string
	checkSSH(changes, func(u string) error {
		mu.Lock()
		calls = append(calls, u)
		mu.Unlock()
		if u == "git@github.com:me/a.git" {
			return errors.New("Permission denied (publickey)")
		}
		return nil
	})

	type got struct{ path, remote, to, err string }
	var gots []got
	for _, c := range changes {
		e := ""
		if c.err != nil {
			e = c.err.Error()
		}
		gots = append(gots, got{c.repo.Path, c.remote, c.to, e})
	}
	want := []got{
		{"/w/a", "origin", "git@github.com:me/a.git", "Permission denied (publickey)"},
		{"/w/a", "upstream", "git@github.com:acmeltd/a.git", ""},
		{"/w/c", "origin", "git@github.com:acmeltd/a.git", ""},
	}
	if !slices.Equal(gots, want) {
		t.Errorf("got %+v, want %+v", gots, want)
	}
	if len(calls) != 2 {
		t.Errorf("checked %d URLs, want each of the 2 once: %q", len(calls), calls)
	}
}

func TestLandBranches(t *testing.T) {
	feat, fix := config.Branch{Name: "feat", From: "origin/feat"}, config.Branch{Name: "fix", From: "upstream/fix"}
	repo := func(path string) config.Repo {
		return config.Repo{Path: path, Branches: []config.Branch{feat, fix}}
	}
	statuses := []*repostatus.Status{
		{Repo: repo("/w/both"), Branches: []repostatus.Branch{{Name: "main"}, {Name: "feat"}, {Name: "fix"}}},
		{Repo: repo("/w/some"), Branches: []repostatus.Branch{{Name: "main"}, {Name: "fix"}}},
		{Repo: repo("/w/missing"), Missing: true},
		{Repo: repo("/w/broken"), Err: errors.New("not a git repository")},
	}
	got := landBranches(statuses)
	check := func(i int, landed, waiting []config.Branch, err bool) {
		t.Helper()
		l := got[i]
		if !slices.Equal(l.landed, landed) || !slices.Equal(l.waiting, waiting) || (l.err != nil) != err {
			t.Errorf("%s: got landed %v, waiting %v, err %v", l.repo.Path, l.landed, l.waiting, l.err)
		}
	}
	check(0, []config.Branch{feat, fix}, nil, false)
	check(1, []config.Branch{fix}, []config.Branch{feat}, false)
	check(2, nil, []config.Branch{feat, fix}, false)
	check(3, nil, nil, true)
}

func TestSSHChangesSkipsNoSSH(t *testing.T) {
	repos := []config.Repo{{Path: "/w/a", URL: "https://github.com/me/a", NoSSH: true}}
	if got := sshChanges(repos); len(got) != 0 {
		t.Errorf("got %+v, want none for ssh: false", got)
	}
}

func TestIsNetworkError(t *testing.T) {
	network := []string{
		"git ls-remote: ssh: Could not resolve hostname github.com: nodename nor servname provided, or not known",
		"git ls-remote: ssh: connect to host github.com port 22: Operation timed out",
		"git ls-remote: ssh: connect to host github.com port 22: Network is unreachable",
		"git ls-remote: ssh: connect to host github.com port 22: Connection refused",
	}
	for _, msg := range network {
		if !isNetworkError(errors.New(msg)) {
			t.Errorf("%q: not a network error", msg)
		}
	}
	definite := []string{
		"git ls-remote: ERROR: Repository not found.",
		"git ls-remote: git@github.com: Permission denied (publickey).",
		"git ls-remote: Host key verification failed.",
	}
	for _, msg := range definite {
		if isNetworkError(errors.New(msg)) {
			t.Errorf("%q: is a network error", msg)
		}
	}
}

func TestNeedsTidy(t *testing.T) {
	feat, fix := config.Branch{Name: "feat", From: "origin/feat"}, config.Branch{Name: "fix", From: "origin/fix"}
	repos := []config.Repo{
		{Path: "/w/https", URL: "https://github.com/me/https", Remotes: []config.Remote{{Name: "upstream", URL: "https://github.com/acmeltd/https"}}},
		{Path: "/w/marked", URL: "https://github.com/me/marked", NoSSH: true},
		{Path: "/w/elsewhere", URL: "https://git.acme.example/me/elsewhere"},
		{Path: "/w/airlifted", URL: "git@github.com:me/airlifted.git", Branches: []config.Branch{feat, fix}},
		{Path: "/w/new", URL: "git@github.com:me/new.git", Branches: []config.Branch{feat}}, // no status: only in the source
		{Path: "/w/switched", URL: "git@github.com:me/switched.git"},
	}
	statuses := map[string]*repostatus.Status{
		"/w/airlifted": {Repo: config.Repo{Path: "/w/airlifted"}, Branches: []repostatus.Branch{{Name: "main"}, {Name: "feat"}},
			RemoteURLs: map[string]string{"origin": "git@github.com:me/airlifted.git"}},
		"/w/switched": {Repo: config.Repo{Path: "/w/switched"}, RemoteURLs: map[string]string{"origin": "https://github.com/me/switched"}},
	}
	got := needsTidy(repos, statuses)
	if want := (tidyNeeds{urls: 2, clones: 1, landed: 1, waiting: 2}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if needsTidy(repos[1:3], nil).any() {
		t.Error("nothing to tidy for ssh: false or unknown hosts")
	}
}

func TestSetURLCommands(t *testing.T) {
	repos := []config.Repo{
		{Path: "/w/vip-proj", URL: "git@github.com:acmeltd/vip-proj.git"},
		{Path: "/w/it's here", URL: "git@github.com:me/here.git", Remotes: []config.Remote{{Name: "up stream", URL: "git@github.com:me/x.git"}}},
		{Path: "/w/done", URL: "git@github.com:me/done.git"},       // clone already switched
		{Path: "/w/missing", URL: "git@github.com:me/missing.git"}, // not cloned
		{Path: "/w/other", URL: "git@github.com:me/other.git"},     // clone has some other URL: clone's to point out
		{Path: "/w/https", URL: "https://github.com/me/https"},     // still HTTPS in the config
	}
	clones := map[string]string{
		"/w/vip-proj origin":     "https://github.com/acmeltd/vip-proj",
		"/w/it's here origin":    "git@github.com:me/here.git",
		"/w/it's here up stream": "https://github.com/me/x.git",
		"/w/done origin":         "git@github.com:me/done.git",
		"/w/other origin":        "https://github.com/someone/other",
		"/w/https origin":        "https://github.com/me/https",
	}
	got := setURLCommands(repos, func(path, remote string) string { return clones[path+" "+remote] })
	want := []string{
		"git -C /w/vip-proj remote set-url origin git@github.com:acmeltd/vip-proj.git",
		`git -C '/w/it'\''s here' remote set-url 'up stream' git@github.com:me/x.git`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
