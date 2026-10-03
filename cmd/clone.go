package cmd

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var cloneCmd = &cobra.Command{
	Use:     "clone",
	Aliases: []string{"c"},
	Short:   "Print git commands to clone repos and add remotes from the config",
	Long: `Print the git commands to set up the repos in the config: git clone for
repos that aren't cloned yet, and git remote add -f for remotes that are
missing (on new clones and existing ones). Nothing is run: review the output
and run the commands yourself (or pipe them to sh).

Commented out, to check first:
  - git remote set-url for remotes whose URL differs from the config, as the
    config may be the one that's out of date
  - but setup for repos marked gitbutler: true that aren't in a GitButler
    workspace, as it switches the repo to GitButler's workspace branch

Paths that exist but aren't a git repo (other than empty folders, which git
can clone into) are listed with the reason, and clone only says "Nothing to
clone" once there's nothing left to do and every check succeeded.`,
	Args: cobra.NoArgs,
	RunE: runClone,
}

func init() {
	rootCmd.AddCommand(cloneCmd)
}

func runClone(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	statuses := inspectAll(cfg.Repos, repostatus.Options{})
	script, n := cloneScript(statuses, emptyDir)
	lipgloss.Print(script)
	return codeFor(n.exitCode())
}

// cloneCounts tallies what the script contains, for its summary line.
type cloneCounts struct {
	clones    int // repos to clone
	remotes   int // remotes to add
	review    int // commented-out commands to check first
	unchecked int // repos that couldn't be checked
}

// exitCode is exitErrors if a check failed, exitAttention if there's anything
// to do (even if only commented out), else exitOK: "Nothing to clone".
func (n cloneCounts) exitCode() int {
	switch {
	case n.unchecked > 0:
		return exitErrors
	case n.clones+n.remotes+n.review > 0:
		return exitAttention
	}
	return exitOK
}

// cloneScript returns the clone script for the inspected repos, and what it
// contains. Every line is either a command that only creates or adds to a
// local clone, or a # comment, so it's safe to pipe to sh. isEmpty reports
// whether a path is an empty folder, which git can clone into.
func cloneScript(statuses []*repostatus.Status, isEmpty func(string) bool) (string, cloneCounts) {
	var w strings.Builder
	var n cloneCounts
	first := true
	for _, s := range statuses {
		lines := repoCloneLines(s, isEmpty, &n)
		if len(lines) == 0 {
			continue
		}
		if !first {
			w.WriteString("\n")
		}
		first = false
		w.WriteString(styleHeading.Render("# "+config.TildePath(s.Repo.Path)) + "\n")
		w.WriteString(strings.Join(lines, "\n") + "\n")
	}

	if !first {
		w.WriteString("\n")
	}
	var summary []string
	if n.clones > 0 {
		summary = append(summary, plural(n.clones, "repo", "repos")+" to clone")
	}
	if n.remotes > 0 {
		summary = append(summary, plural(n.remotes, "remote", "remotes")+" to add")
	}
	if n.review > 0 {
		summary = append(summary, fmt.Sprintf("%d commented out to review", n.review))
	}
	if n.unchecked > 0 {
		summary = append(summary, styleWarn.Render(plural(n.unchecked, "check", "checks")+" failed"))
	}
	if len(summary) == 0 {
		summary = append(summary, "Nothing to clone")
	}
	w.WriteString(pruneComment(strings.Join(summary, ", ")) + "\n")
	return w.String(), n
}

// repoCloneLines returns one repo's lines of the script, adding to n.
func repoCloneLines(s *repostatus.Status, isEmpty func(string) bool, n *cloneCounts) []string {
	comment := pruneComment
	path := shellPath(s.Repo.Path)
	fresh := s.Missing || (s.Err != nil && isEmpty(s.Repo.Path))
	if s.Err != nil && !fresh {
		n.unchecked++
		return []string{comment(styleWarn.Render("couldn't check: " + s.Err.Error()))}
	}

	var lines []string
	if fresh {
		lines = append(lines, "git clone "+shellQuote(s.Repo.URL)+" "+path)
		n.clones++
	}
	addRemote := func(name, url string) {
		lines = append(lines, "git -C "+path+" remote add -f "+shellQuote(name)+" "+shellQuote(url))
		n.remotes++
	}
	setURL := func(name, url, got string) {
		lines = append(lines, comment("git -C "+path+" remote set-url "+shellQuote(name)+" "+shellQuote(url)+
			"  # "+name+" is "+got+"; check which is right"))
		n.review++
	}

	if !fresh {
		switch {
		case s.OriginURL == "":
			addRemote("origin", s.Repo.URL)
		case s.OriginURL != s.Repo.URL:
			setURL("origin", s.Repo.URL, s.OriginURL)
		}
	}
	for _, rem := range s.Repo.Remotes {
		switch got, ok := s.RemoteURLs[rem.Name]; {
		case fresh || !ok:
			addRemote(rem.Name, rem.URL)
		case got != rem.URL:
			setURL(rem.Name, rem.URL, got)
		}
	}

	if s.Repo.GitButler && (fresh || s.GitButler.Mode != repostatus.GitButlerActive) {
		lines = append(lines, comment("but -C "+path+" setup  # switches to GitButler's workspace branch"))
		n.review++
	}
	return lines
}

// emptyDir reports whether path is a folder with nothing in it.
func emptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}
