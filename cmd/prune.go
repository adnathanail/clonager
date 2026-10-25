package cmd

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var pruneFlags struct {
	offline bool
	tags    []string
}

var pruneCmd = &cobra.Command{
	Use:     "prune",
	Aliases: []string{"p"},
	Short:   "Print git commands to delete merged branches",
	Long:    helpText("prune"),
	Args:    cobra.NoArgs,
	RunE:    runPrune,
}

func init() {
	pruneCmd.Flags().BoolVarP(&pruneFlags.offline, "offline", "o", false, "don't check GitHub (default from alwaysOffline in settings.json)")
	pruneCmd.Flags().StringSliceVarP(&pruneFlags.tags, "tag", "t", nil, "only repos with this tag (repeatable)")
	rootCmd.AddCommand(pruneCmd)
}

func runPrune(cmd *cobra.Command, args []string) error {
	settings, err := config.ReadSettings()
	if err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	repos := filterByTags(cfg.Repos, pruneFlags.tags)
	forge := !offline(cmd, pruneFlags.offline, settings)
	var statuses []*repostatus.Status
	if forge && liveTerminal(os.Stderr) {
		var interrupted bool
		if statuses, interrupted = inspectSpinning(repos, cmd.ErrOrStderr()); interrupted {
			fmt.Fprintln(cmd.ErrOrStderr(), styleWarn.Render("Couldn't check GitHub: interrupted."))
			return codeFor(exitErrors)
		}
	} else {
		statuses = inspectAll(repos, repostatus.Options{Forge: forge})
	}
	script, n := pruneScript(statuses)
	lipgloss.Print(script)
	return codeFor(n.exitCode())
}

// inspectSpinning inspects repos and checks GitHub for them, with a spinner
// on w meanwhile. That's stderr, so the spinner never gets into the script,
// and it's cleared before the script is printed. It reports whether Ctrl-C
// was pressed, in which case the statuses aren't returned.
func inspectSpinning(repos []config.Repo, w io.Writer) ([]*repostatus.Status, bool) {
	var statuses []*repostatus.Status
	var checked atomic.Int32
	local, finished := make(chan struct{}), make(chan struct{})
	go func() {
		statuses = inspectAll(repos, repostatus.Options{})
		close(local)
		forgeAll(statuses, &checked)
		close(finished)
	}()
	label := func() string {
		select {
		case <-local:
			return fmt.Sprintf("Checking GitHub… %d/%d", checked.Load(), len(repos))
		default:
			return "Checking repos…"
		}
	}
	width, _, err := term.GetSize(os.Stderr.Fd())
	if err != nil {
		width = 0
	}
	if interrupted, _ := spin(w, width, label, finished); interrupted {
		return nil, true
	}
	return statuses, false
}

// pruneCounts tallies what the script contains, for its summary line.
type pruneCounts struct {
	deletes   int // merged local branches to delete
	review    int // local branches commented out to check first
	stale     int // stale remote-tracking refs
	onGitHub  int // commented-out changes on GitHub
	unchecked int // repos (or their GitHub side) that couldn't be checked

	// Repos GitHub couldn't be asked about at all (see forgeUnreachable),
	// noted once at the end rather than in each repo's lines, and why.
	unreachable    int
	unreachableWhy string
}

// exitCode is exitErrors if a check failed, exitAttention if there's anything
// to prune (even if only commented out), else exitOK: "Nothing to prune".
func (n pruneCounts) exitCode() int {
	switch {
	case n.unchecked > 0:
		return exitErrors
	case n.deletes+n.review+n.stale+n.onGitHub > 0:
		return exitAttention
	}
	return exitOK
}

// pruneScript returns the prune script for the inspected repos, and what it
// contains. Every line is either a command that only touches the local clone
// or a # comment, so it's safe to pipe to sh.
func pruneScript(statuses []*repostatus.Status) (string, pruneCounts) {
	var w strings.Builder
	var n pruneCounts
	first := true
	for _, s := range statuses {
		lines := repoPruneLines(s, &n)
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
	if n.unreachable > 0 {
		w.WriteString(pruneComment(styleWarn.Render(fmt.Sprintf("Couldn't check GitHub for %s: %s.",
			plural(n.unreachable, "repo", "repos"), n.unreachableWhy))) + "\n")
	}
	var summary []string
	if n.deletes > 0 {
		summary = append(summary, plural(n.deletes, "merged local branch", "merged local branches"))
	}
	if n.stale > 0 {
		summary = append(summary, plural(n.stale, "stale remote ref", "stale remote refs"))
	}
	if n.review > 0 {
		summary = append(summary, fmt.Sprintf("%d local commented out to review", n.review))
	}
	if n.onGitHub > 0 {
		summary = append(summary, fmt.Sprintf("%d on GitHub commented out", n.onGitHub))
	}
	if n.unchecked > 0 {
		summary = append(summary, styleWarn.Render(plural(n.unchecked, "check", "checks")+" failed"))
	}
	if len(summary) == 0 {
		summary = append(summary, "Nothing to prune")
	}
	w.WriteString(pruneComment(strings.Join(summary, ", ")) + "\n")
	return w.String(), n
}

func pruneComment(s string) string { return styleDim.Render("# " + s) }

// uncheckable says why prune can't safely suggest anything for a repo, or
// returns "" if it can.
func uncheckable(s *repostatus.Status) string {
	switch {
	case s.Missing:
		return "not cloned"
	case s.Err != nil:
		return s.Err.Error()
	case s.GitButler.Err != nil:
		// Without it, branches applied in the workspace aren't known, and
		// could be suggested for deletion.
		return "couldn't read GitButler's state: " + s.GitButler.Err.Error()
	}
	return ""
}

// repoPruneLines returns one repo's lines of the script, adding to n.
func repoPruneLines(s *repostatus.Status, n *pruneCounts) []string {
	comment := pruneComment
	if reason := uncheckable(s); reason != "" {
		n.unchecked++
		return []string{comment(styleWarn.Render("couldn't check: " + reason))}
	}

	var applied []string
	for _, b := range s.GitButler.Branches {
		applied = append(applied, b.Name)
	}

	var lines []string
	prefix := "git -C " + shellPath(s.Repo.Path) + " branch -D "
	for _, b := range s.Branches {
		var how string
		switch {
		case b.Merged != "":
			how = mergedHow(b, s.DefaultBranch)
		case b.PRDiffers:
			how = fmt.Sprintf("PR #%d was merged, but not from this branch's tip; check before deleting", b.PR)
		default:
			continue
		}
		switch {
		case b.Name == s.Head:
			lines = append(lines, comment(fmt.Sprintf("skipped %s: checked out (%s)", b.Name, how)))
		case slices.Contains(applied, b.Name):
			lines = append(lines, comment(fmt.Sprintf("skipped %s: applied in GitButler, unapply it there (%s)", b.Name, how)))
		case b.PRDiffers:
			lines = append(lines, comment(prefix+shellQuote(b.Name)+"  # "+how))
			n.review++
		default:
			lines = append(lines, prefix+shellQuote(b.Name)+"  "+comment(how))
			n.deletes++
		}
	}

	// Stale remote-tracking refs: tidying them only touches this clone.
	if bs := s.StaleRemote(); len(bs) > 0 {
		var names []string
		for i, b := range bs {
			if i == 5 {
				names = append(names, fmt.Sprintf("and %d more", len(bs)-5))
				break
			}
			names = append(names, b.Name)
		}
		lines = append(lines, "git -C "+shellPath(s.Repo.Path)+" remote prune origin  "+
			comment(plural(len(bs), "ref", "refs")+" deleted on GitHub: "+strings.Join(names, ", ")))
		n.stale += len(bs)
	}

	// Merged branches still on GitHub. Only when GitHub was checked, so it's known they're
	// really there, aren't protected, and that you can push. Always commented
	// out: deleting them affects everyone.
	if gh := s.GitHub; gh != nil && ownsRemote(s) {
		for _, b := range s.MergedRemote() {
			lines = append(lines, comment("git -C "+shellPath(s.Repo.Path)+" push origin --delete "+
				shellQuote(b.Name)+"  # "+describeMerge(b.Merged, b.PR, s.DefaultBranch)))
			n.onGitHub++
		}
		if gh.Admin && gh.AutoDelete != nil && !*gh.AutoDelete {
			lines = append(lines, comment("gh repo edit "+gh.Name+" --delete-branch-on-merge  # delete branches when their PRs merge"))
			n.onGitHub++
		}
	}

	// The local results still stand, but anything only GitHub could tell us
	// is missing.
	if s.ForgeErr != nil {
		if why := forgeUnreachable(s.ForgeErr); why != "" {
			n.unreachable++
			n.unreachableWhy = why
		} else {
			lines = append(lines, comment(styleWarn.Render("couldn't check GitHub: "+s.ForgeErr.Error())))
		}
		n.unchecked++
	}
	return lines
}

// ownsRemote reports whether changes to the repo's remote (deleting branches,
// settings) are the user's to make: not marked mine: false in the config, and,
// when the forge was checked, pushable.
func ownsRemote(s *repostatus.Status) bool {
	if s.Repo.NotMine {
		return false
	}
	return s.GitHub == nil || s.GitHub.CanPush
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./@%+=:,-]+$`)

// shellQuote quotes s for a POSIX shell, leaving it bare when that's safe.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellPath quotes a path for the shell, keeping a leading ~/ unquoted so it
// still expands.
func shellPath(p string) string {
	t := config.TildePath(p)
	if rest, ok := strings.CutPrefix(t, "~/"); ok {
		return "~/" + shellQuote(rest)
	}
	return shellQuote(t)
}
