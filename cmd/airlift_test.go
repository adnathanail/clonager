package cmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

func TestAirliftBranches(t *testing.T) {
	s := &repostatus.Status{
		DefaultBranch: "origin/main",
		Branches: []repostatus.Branch{
			{Name: "main", Remote: "origin/main"},
			{Name: "trunk", Remote: "origin/main"}, // just another name for the default branch
			{Name: "feat", Remote: "origin/feat", Behind: 2},
			{Name: "theirs", Remote: "upstream/theirs"},
			{Name: "done", Remote: "origin/done", Merged: repostatus.MergedAncestor},
			{Name: "local"},
		},
	}
	got := airliftBranches(s)
	want := []config.Branch{{Name: "feat", From: "origin/feat"}, {Name: "theirs", From: "upstream/theirs"}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestAskGitHub(t *testing.T) {
	for answer, want := range map[string]bool{
		"y\n":   true,
		"Yes\n": true,
		" y \n": true,
		"\n":    false, // the default
		"n\n":   false,
		"yep\n": false,
		"":      false, // stdin closed
	} {
		var out strings.Builder
		if got := askGitHub(strings.NewReader(answer), &out); got != want {
			t.Errorf("answer %q: got %v, want %v", answer, got, want)
		}
		if !strings.Contains(out.String(), "Check GitHub? [y/N]") {
			t.Errorf("answer %q: question not asked, got %q", answer, out.String())
		}
	}
}
