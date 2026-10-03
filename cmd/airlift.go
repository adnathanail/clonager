package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var airliftFlags struct {
	land bool
}

var airliftCmd = &cobra.Command{
	Use:   "airlift",
	Short: "Record this laptop's branches in the config, to recreate them on another",
	Long: `Record each repo's local branches in the config, so clonager clone can
recreate them on another laptop.

Only a tidy laptop can be airlifted: first it checks prune --forge has nothing
to do, and that status (with --forge) has nothing needing attention, so every
branch is on a remote and nothing is left uncommitted or unpushed. If either
finds something, it's shown and the config isn't changed.

Each repo's branches list is replaced with its current local branches, other
than the default branch, which clone checks out anyway. Repos with none have
their list removed.

On the other laptop, clonager clone prints a git branch command for each
listed branch that doesn't exist there yet. Once they're created, airlift
--land removes the lists from the config.

Like discover, it edits the config's source when the Home Manager module
installs it with configSource set.`,
	Args: cobra.NoArgs,
	RunE: runAirlift,
}

func init() {
	airliftCmd.Flags().BoolVar(&airliftFlags.land, "land", false, "remove the branch lists from the config, once the branches exist on this laptop")
	rootCmd.AddCommand(airliftCmd)
}

func runAirlift(cmd *cobra.Command, args []string) error {
	cfg, fromSource, err := editableConfig()
	if err != nil {
		return err
	}
	if err := cfg.Writable(); errors.Is(err, config.ErrReadOnly) {
		return fmt.Errorf("%w. If Home Manager installs it, set programs.clonager.configSource "+
			"(to the file in your checkout, or commands to decrypt and encrypt it), "+
			"and airlift will edit that instead", err)
	}
	if airliftFlags.land {
		return runLand(cfg, fromSource)
	}
	if err := checkForgeAvailable(true); err != nil {
		return err
	}

	statuses := inspectAll(cfg.Repos, repostatus.Options{Forge: true})
	code := exitOK
	blocked := func(c int, heading string) {
		if code != exitOK {
			lipgloss.Println()
		}
		code = max(code, c)
		lipgloss.Println(styleHeading.Render(heading))
	}

	if script, n := pruneScript(statuses, true); n.exitCode() != exitOK {
		blocked(n.exitCode(), "clonager prune --forge has things to do:")
		lipgloss.Print(script)
	}
	reports := make([]report, len(statuses))
	for i, s := range statuses {
		reports[i] = buildReport(s)
	}
	if c := statusCode(reports); c != exitOK {
		blocked(c, "Repos need attention:")
		printReports(reports)
	}
	var unknown []string
	for _, s := range statuses {
		for _, b := range airliftBranches(s) {
			if !s.Repo.HasRemote(b.Remote()) {
				unknown = append(unknown, fmt.Sprintf("%s: %s tracks %s, from a remote not in the config",
					config.TildePath(s.Repo.Path), b.Name, b.From))
			}
		}
	}
	if len(unknown) > 0 {
		blocked(exitAttention, "Branches that clone couldn't recreate:")
		for _, u := range unknown {
			lipgloss.Println("  " + styleWarn.Render("●") + " " + u)
		}
	}
	dirs, _, err := settingsDirs()
	if err != nil {
		return err
	}
	untracked, err := findUntracked(dirs, cfg, nil)
	if err != nil {
		return err
	}
	if len(untracked) > 0 {
		blocked(exitAttention, "Repos not in the config (clonager discover adds them):")
		for _, r := range untracked {
			lipgloss.Println("  " + styleWarn.Render("?") + " " + folderLink(r.path, config.TildePath(r.path)))
		}
	}
	if code != exitOK {
		lipgloss.Println()
		lipgloss.Println(styleWarn.Render("Not airlifting until these are dealt with; the config is unchanged."))
		return codeFor(code)
	}

	total, repos := 0, 0
	for _, s := range statuses {
		branches := airliftBranches(s)
		if err := cfg.SetBranches(s.Repo.Path, branches); err != nil {
			return err
		}
		if len(branches) == 0 {
			continue
		}
		total += len(branches)
		repos++
		names := make([]string, len(branches))
		for i, b := range branches {
			names[i] = styleBranch.Render(b.Name)
		}
		lipgloss.Println(folderLink(s.Repo.Path, config.TildePath(s.Repo.Path)) + "  " + strings.Join(names, ", "))
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if total > 0 {
		lipgloss.Println()
	}
	lipgloss.Println(fmt.Sprintf("Recorded %s in %s in %s.", plural(total, "branch", "branches"),
		plural(repos, "repo", "repos"), config.TildePath(cfg.Path)))
	if fromSource {
		lipgloss.Println(styleDim.Render("Rebuild (e.g. darwin-rebuild switch) to apply it."))
	}
	if total > 0 {
		lipgloss.Println(styleDim.Render("On the other laptop, clonager clone prints the commands to create them; " +
			"once they're there, clonager airlift --land removes them from the config."))
	}
	return nil
}

// airliftBranches are the branches to record for a repo: its local branches
// other than the default branch (which clone checks out anyway) and merged
// ones (which prune would delete, but for being checked out or applied).
func airliftBranches(s *repostatus.Status) []config.Branch {
	var out []config.Branch
	for _, b := range s.Branches {
		if b.Remote == "" || b.Remote == s.DefaultBranch || b.Merged != "" {
			continue
		}
		out = append(out, config.Branch{Name: b.Name, From: b.Remote})
	}
	return out
}

// runLand removes the branch lists from the config, once every listed branch
// exists on this laptop.
func runLand(cfg *config.Config, fromSource bool) error {
	var listed []config.Repo
	for _, r := range cfg.Repos {
		if len(r.Branches) > 0 {
			listed = append(listed, r)
		}
	}
	if len(listed) == 0 {
		lipgloss.Println(styleDim.Render("No branches in the config to land."))
		return nil
	}

	var missing []string
	for _, s := range inspectAll(listed, repostatus.Options{}) {
		var here []string
		for _, b := range s.Branches {
			here = append(here, b.Name)
		}
		for _, b := range s.Repo.Branches {
			if !slices.Contains(here, b.Name) {
				missing = append(missing, config.TildePath(s.Repo.Path)+": "+b.Name)
			}
		}
	}
	if len(missing) > 0 {
		lipgloss.Println(styleHeading.Render("Not created on this laptop yet:"))
		for _, m := range missing {
			lipgloss.Println("  " + styleWarn.Render("●") + " " + m)
		}
		lipgloss.Println()
		lipgloss.Println(styleWarn.Render("Not landing until they are (see clonager clone); the config is unchanged."))
		return codeFor(exitAttention)
	}

	for _, r := range listed {
		if err := cfg.SetBranches(r.Path, nil); err != nil {
			return err
		}
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	lipgloss.Println(fmt.Sprintf("Removed the branch lists of %s from %s.", plural(len(listed), "repo", "repos"), config.TildePath(cfg.Path)))
	if fromSource {
		lipgloss.Println(styleDim.Render("Rebuild (e.g. darwin-rebuild switch) to apply it."))
	}
	return nil
}
