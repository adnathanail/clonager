package cmd

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var pruneFlags struct {
	forge bool
	tags  []string
}

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Print git commands to delete merged branches",
	Long: `Print the git commands to delete local branches that are already merged
into their repo's default branch. Nothing is deleted: review the output and run
the commands yourself (or pipe them to sh).

Commands use git branch -D, since rebase- and squash-merged branches aren't in
the default branch's history and git branch -d would refuse them.

Branches whose merged PR doesn't contain the local tip (found with --forge) are
printed commented out, as they may hold work that never made it in. The
checked-out branch and branches applied in a GitButler workspace are skipped.

With --forge, it also prints:
  - git remote prune for refs to branches already deleted on GitHub
  - git push origin --delete for merged branches still on GitHub, commented
    out as it changes the repo for everyone (only for repos you can push to,
    and never for protected branches)
  - gh repo edit --delete-branch-on-merge, commented out, for repos you admin
    that don't delete merged branches automatically`,
	Args: cobra.NoArgs,
	RunE: runPrune,
}

func init() {
	pruneCmd.Flags().BoolVarP(&pruneFlags.forge, "forge", "f", false, "also check GitHub (via gh) for merged PRs")
	pruneCmd.Flags().StringSliceVarP(&pruneFlags.tags, "tag", "t", nil, "only repos with this tag (repeatable)")
	rootCmd.AddCommand(pruneCmd)
}

func runPrune(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	repos := filterByTags(cfg.Repos, pruneFlags.tags)
	statuses := inspectAll(repos, repostatus.Options{Forge: pruneFlags.forge})

	comment := func(s string) string { return styleDim.Render("# " + s) }
	deletes, review, stale, onGitHub := 0, 0, 0, 0
	first := true
	for _, s := range statuses {
		if s.Missing || s.Err != nil {
			continue
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
				review++
			default:
				lines = append(lines, prefix+shellQuote(b.Name)+"  "+comment(how))
				deletes++
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
			stale += len(bs)
		}

		// Merged branches still on GitHub. Only with --forge, which knows
		// they're really there, aren't protected, and that you can push.
		// Always commented out: deleting them affects everyone.
		if gh := s.GitHub; gh != nil {
			bs := s.MergedRemote()
			switch {
			case len(bs) > 0 && !gh.CanPush:
				lines = append(lines, comment(fmt.Sprintf("skipped %s merged on GitHub: no push access to %s",
					plural(len(bs), "branch", "branches"), gh.Name)))
			case len(bs) > 0:
				for _, b := range bs {
					lines = append(lines, comment("git -C "+shellPath(s.Repo.Path)+" push origin --delete "+
						shellQuote(b.Name)+"  # "+describeMerge(b.Merged, b.PR, s.DefaultBranch)))
				}
				onGitHub += len(bs)
			}
			if gh.Admin && gh.AutoDelete != nil && !*gh.AutoDelete {
				lines = append(lines, comment("gh repo edit "+gh.Name+" --delete-branch-on-merge  # delete branches when their PRs merge"))
			}
		}

		if len(lines) == 0 {
			continue
		}
		if !first {
			fmt.Println()
		}
		first = false
		fmt.Println(styleHeading.Render("# " + config.TildePath(s.Repo.Path)))
		fmt.Println(strings.Join(lines, "\n"))
	}

	if !first {
		fmt.Println()
	}
	var summary []string
	if deletes > 0 {
		summary = append(summary, plural(deletes, "merged local branch", "merged local branches"))
	}
	if stale > 0 {
		summary = append(summary, plural(stale, "stale remote ref", "stale remote refs"))
	}
	if review > 0 {
		summary = append(summary, fmt.Sprintf("%d local commented out to review", review))
	}
	if onGitHub > 0 {
		summary = append(summary, fmt.Sprintf("%d on GitHub commented out", onGitHub))
	}
	if len(summary) == 0 {
		summary = append(summary, "Nothing to prune")
	}
	line := strings.Join(summary, ", ")
	if !pruneFlags.forge {
		line += " (--forge also checks GitHub for merged PRs, stale refs and merged branches there)"
	}
	fmt.Println(comment(line))
	return nil
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
