package cmd

import (
	"slices"
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
