package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/cli"
	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var airliftCmd = &cobra.Command{
	Use:   "airlift",
	Short: "Record this laptop's branches in the config, to recreate them on another",
	Long:  helpText("airlift"),
	Args:  cobra.NoArgs,
	RunE:  runAirlift,
}

var airliftFlags struct {
	offline bool
}

func init() {
	airliftCmd.Flags().BoolVarP(&airliftFlags.offline, "offline", "o", false,
		"--offline=false checks GitHub without asking, when alwaysOffline is set")
	configCmd.AddCommand(airliftCmd)
}

func runAirlift(cmd *cobra.Command, args []string) error {
	cfg, fromSource, err := writableConfig("airlift", false)
	if err != nil {
		return err
	}
	// It has to know branches are merged on GitHub, so it can't do without.
	settings, err := config.ReadSettings()
	if err != nil {
		return err
	}
	given := cmd.Flags().Changed("offline")
	if given && airliftFlags.offline {
		return errors.New("airlift has to check GitHub, so it can't run --offline")
	}
	if !cli.Installed("gh") {
		return errors.New("airlift needs the GitHub CLI (gh) on your PATH")
	}
	if settings.AlwaysOffline && !given {
		if !term.IsTerminal(os.Stdin.Fd()) {
			return errors.New("alwaysOffline is set, and airlift has to check GitHub: " +
				"run it in a terminal to be asked, or pass --offline=false")
		}
		if !askGitHub(os.Stdin, cmd.ErrOrStderr()) {
			fmt.Fprintln(cmd.ErrOrStderr(), styleDim.Render("Not airlifting; the config is unchanged."))
			return codeFor(exitFailed)
		}
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

	if script, n := pruneScript(statuses); n.exitCode() != exitOK {
		blocked(n.exitCode(), "clonager prune has things to do:")
		lipgloss.Print(script)
	}
	reports := make([]report, len(statuses))
	for i, s := range statuses {
		reports[i] = buildReport(s)
	}
	if c := statusCode(reports); c != exitOK {
		blocked(c, "Repos need attention:")
		var w strings.Builder
		writeReports(&w, reports)
		lipgloss.Print(w.String())
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
		blocked(exitAttention, "Repos not in the config (clonager config discover adds them):")
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
			"once they're there, clonager config tidy removes them from the config."))
		lipgloss.Println(styleWarn.Render("Don't run clonager config tidy on this laptop until these clones are deleted: " +
			"it would remove them, as the branches still exist here."))
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

// askGitHub asks on out whether airlift may check GitHub despite
// alwaysOffline, and reads the answer from in: yes only for y or yes.
func askGitHub(in io.Reader, out io.Writer) bool {
	fmt.Fprint(out, "alwaysOffline is set, but airlift has to check GitHub to know every branch is merged or pushed. Check GitHub? [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}
