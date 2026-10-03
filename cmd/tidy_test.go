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
