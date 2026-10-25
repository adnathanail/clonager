package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var cloneCmd = &cobra.Command{
	Use:     "clone",
	Aliases: []string{"c"},
	Short:   "Print git commands to clone repos, add remotes and create branches from the config",
	Long:    helpText("clone"),
	Args:    cobra.NoArgs,
	RunE:    runClone,
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
	readGitButlerSettings(statuses)
	script, n := cloneScript(statuses, emptyDir)
	lipgloss.Print(script)
	return codeFor(n.exitCode())
}

// cloneCounts tallies what the script contains, for its summary line.
type cloneCounts struct {
	clones    int // repos to clone
	remotes   int // remotes to add
	branches  int // airlifted branches to create
	review    int // commented-out commands to check first
	unchecked int // repos that couldn't be checked
}

// exitCode is exitErrors if a check failed, exitAttention if there's anything
// to do (even if only commented out), else exitOK: "Nothing to clone".
func (n cloneCounts) exitCode() int {
	switch {
	case n.unchecked > 0:
		return exitErrors
	case n.clones+n.remotes+n.branches+n.review > 0:
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
	if n.branches > 0 {
		summary = append(summary, plural(n.branches, "branch", "branches")+" to create")
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

	// Branches airlifted from another laptop.
	var here []string
	for _, b := range s.Branches {
		here = append(here, b.Name)
	}
	for _, b := range s.Repo.Branches {
		if !slices.Contains(here, b.Name) {
			lines = append(lines, "git -C "+path+" branch "+shellQuote(b.Name)+" "+shellQuote(b.From))
			n.branches++
		}
	}

	if d := gitButlerDriftOf(s.Repo, s); d.any() {
		if d.target != "" {
			lines = append(lines, comment("but -C "+path+" config target "+shellQuote(d.target)+
				"  # the target is "+d.gotTarget+"; check which is right"))
			n.review++
		}
		if d.push != "" {
			lines = append(lines, comment("but -C "+path+" config push-remote "+shellQuote(d.push)+
				"  # pushes go to "+d.gotPush+"; check which is right"))
			n.review++
		}
	}
	if s.Repo.GitButler && (fresh || s.GitButler.Mode != repostatus.GitButlerActive) {
		lines = append(lines, comment("but -C "+path+" setup  # switches to GitButler's workspace branch"))
		n.review++
		// After the remotes, so a target on one of them exists by now.
		switch r := s.Repo; {
		case r.GitButlerTarget != "" && r.GitButlerPush != "":
			lines = append(lines, comment("but -C "+path+" config target "+shellQuote(r.GitButlerTarget)+" --push-remote "+shellQuote(r.GitButlerPush)))
		case r.GitButlerTarget != "":
			lines = append(lines, comment("but -C "+path+" config target "+shellQuote(r.GitButlerTarget)))
		case r.GitButlerPush != "":
			lines = append(lines, comment("but -C "+path+" config push-remote "+shellQuote(r.GitButlerPush)))
		}
	}
	return lines
}

// gitButlerDrift is where a GitButler workspace's target branch or push
// remote differ from the ones recorded in the config: the config's and the
// workspace's, for each that differs, or "".
type gitButlerDrift struct {
	target, gotTarget string
	push, gotPush     string
}

func (d gitButlerDrift) any() bool { return d.target != "" || d.push != "" }

// gitButlerDriftOf compares the workspace at s (once its settings are read,
// see Status.ReadGitButlerSettings) with r's config entry. It's only for
// entries with a target or push: for the rest, config tidy records the
// workspace's. Either left out, in the config or the workspace, means what
// `but setup` picks: origin's HEAD (GitButler.SetupTarget), and origin.
// Where origin's HEAD isn't set, a config entry with no target can't be
// compared, so only the push remote is.
func gitButlerDriftOf(r config.Repo, s *repostatus.Status) gitButlerDrift {
	var d gitButlerDrift
	gb := s.GitButler
	if r.GitButlerTarget == "" && r.GitButlerPush == "" || gb.Mode != repostatus.GitButlerActive || !gb.Settings {
		return d
	}
	or := func(v, def string) string {
		if v == "" {
			return def
		}
		return v
	}
	if want, got := or(r.GitButlerTarget, gb.SetupTarget), or(gb.Target, gb.SetupTarget); want != got && want != "" && got != "" {
		d.target, d.gotTarget = want, got
	}
	if want, got := or(r.GitButlerPush, "origin"), or(gb.Push, "origin"); want != got {
		d.push, d.gotPush = want, got
	}
	return d
}

// emptyDir reports whether path is a folder with nothing in it.
func emptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}
