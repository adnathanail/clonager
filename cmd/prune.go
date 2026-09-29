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
checked-out branch and branches applied in a GitButler workspace are skipped.`,
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
	deletes, review := 0, 0
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

	if deletes == 0 && review == 0 {
		fmt.Println(comment("Nothing to prune"))
		return nil
	}
	fmt.Println()
	summary := plural(deletes, "merged branch", "merged branches")
	if review > 0 {
		summary += fmt.Sprintf(", plus %d commented out to review", review)
	}
	if !pruneFlags.forge {
		summary += " (--forge also checks GitHub for merged PRs)"
	}
	fmt.Println(comment(summary))
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
