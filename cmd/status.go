package cmd

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var statusFlags struct {
	verbose  bool
	problems bool
	tags     []string
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the state of every configured repo",
	Long: `Show the state of every configured repo: uncommitted changes, stashes,
branches that aren't on a remote, branches with unpushed commits, and
GitButler workspace state.

Remote state is as of each repo's last fetch; status never fetches.`,
	Args: cobra.NoArgs,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVarP(&statusFlags.verbose, "verbose", "v", false, "list the branches behind each count")
	statusCmd.Flags().BoolVarP(&statusFlags.problems, "problems", "p", false, "only show repos that need attention")
	statusCmd.Flags().StringSliceVarP(&statusFlags.tags, "tag", "t", nil, "only show repos with this tag (repeatable)")
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Repos) == 0 {
		fmt.Println(styleDim.Render("No repos configured in " + config.TildePath(cfg.Path)))
		return nil
	}
	repos := filterByTags(cfg.Repos, statusFlags.tags)
	if len(repos) == 0 {
		fmt.Println(styleDim.Render("No repos tagged " + strings.Join(statusFlags.tags, " or ")))
		return nil
	}

	statuses := inspectAll(repos)
	reports := make([]report, len(statuses))
	for i, s := range statuses {
		reports[i] = buildReport(s)
	}
	printReports(reports)
	return nil
}

func filterByTags(repos []config.Repo, tags []string) []config.Repo {
	if len(tags) == 0 {
		return repos
	}
	var out []config.Repo
	for _, r := range repos {
		for _, t := range tags {
			if slices.Contains(r.Tags, t) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// inspectAll inspects repos in parallel, returning results in config order.
func inspectAll(repos []config.Repo) []*repostatus.Status {
	out := make([]*repostatus.Status, len(repos))
	sem := make(chan struct{}, max(4, runtime.NumCPU()))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = repostatus.Inspect(r)
		}()
	}
	wg.Wait()
	return out
}

type severity int

const (
	sevInfo severity = iota // shown, but doesn't make the repo need attention
	sevWarn
	sevError
)

type issue struct {
	sev     severity
	text    string
	details []string // shown with --verbose
}

type report struct {
	status *repostatus.Status
	head   string // rendered branch column
	issues []issue
}

func (r report) severity() severity {
	sev := sevInfo
	for _, i := range r.issues {
		sev = max(sev, i.sev)
	}
	return sev
}

func buildReport(s *repostatus.Status) report {
	r := report{status: s}
	add := func(sev severity, text string, details ...string) {
		r.issues = append(r.issues, issue{sev, text, details})
	}

	switch {
	case s.Missing:
		add(sevError, "not cloned")
		return r
	case s.Err != nil:
		add(sevError, s.Err.Error())
		return r
	}

	switch {
	case s.GitButler.Mode == repostatus.GitButlerActive:
		r.head = styleGB.Render("gitbutler")
	case s.Head == "":
		r.head = styleWarn.Render("detached")
		add(sevWarn, "detached HEAD")
	default:
		r.head = styleBranch.Render(s.Head)
	}

	// Remotes
	switch {
	case s.OriginURL == "":
		add(sevError, "no origin remote")
	case s.OriginURL != s.Repo.URL:
		add(sevError, "origin differs from config", "config: "+s.Repo.URL, "origin: "+s.OriginURL)
	}
	for _, rem := range s.Repo.Remotes {
		switch got, ok := s.RemoteURLs[rem.Name]; {
		case !ok:
			add(sevWarn, fmt.Sprintf("remote %s missing", rem.Name))
		case got != rem.URL:
			add(sevWarn, fmt.Sprintf("remote %s differs from config", rem.Name), "config: "+rem.URL, rem.Name+": "+got)
		}
	}

	// Working tree
	if s.Changed > 0 {
		add(sevWarn, plural(s.Changed, "uncommitted change", "uncommitted changes"))
	}
	if s.Stashes > 0 {
		add(sevWarn, plural(s.Stashes, "stash", "stashes"))
	}

	// Branches
	if bs := s.LocalOnly(); len(bs) > 0 {
		add(sevWarn, plural(len(bs), "local-only branch", "local-only branches"), branchNames(bs, nil)...)
	}
	if bs := s.Unpushed(); len(bs) > 0 {
		commits := 0
		for _, b := range bs {
			commits += b.Ahead
		}
		text := fmt.Sprintf("%s unpushed on %s", plural(commits, "commit", "commits"), plural(len(bs), "branch", "branches"))
		add(sevWarn, text, branchNames(bs, func(b repostatus.Branch) string {
			return fmt.Sprintf("+%d vs %s", b.Ahead, b.Remote)
		})...)
	}
	if bs := s.GoneBranches(); len(bs) > 0 {
		add(sevWarn, plural(len(bs), "branch", "branches")+" deleted on remote", branchNames(bs, nil)...)
	}
	if bs := s.Behind(); len(bs) > 0 {
		add(sevInfo, plural(len(bs), "branch", "branches")+" behind", branchNames(bs, func(b repostatus.Branch) string {
			return fmt.Sprintf("-%d vs %s", b.Behind, b.Remote)
		})...)
	}

	// GitButler
	gb := s.GitButler
	switch gb.Mode {
	case repostatus.GitButlerActive:
		switch {
		case gb.Err != nil:
			add(sevError, gb.Err.Error())
		default:
			var force []repostatus.GitButlerBranch
			for _, b := range gb.Branches {
				if b.Status == "unpushedCommitsRequiringForce" {
					force = append(force, b)
				}
			}
			if gb.Conflicted > 0 {
				add(sevError, plural(gb.Conflicted, "conflicted commit", "conflicted commits"))
			}
			if len(force) > 0 {
				names := make([]string, len(force))
				for i, b := range force {
					names[i] = b.Name
				}
				add(sevWarn, plural(len(force), "branch needs", "branches need")+" force push", names...)
			}
			if len(gb.Branches) > 0 {
				names := make([]string, len(gb.Branches))
				for i, b := range gb.Branches {
					names[i] = b.Name + styleDim.Render(" ("+b.Status+")")
				}
				add(sevInfo, plural(len(gb.Branches), "branch", "branches")+" applied", names...)
			}
		}
		if !s.Repo.GitButler {
			add(sevInfo, "gitbutler not set in config")
		}
	default:
		if s.Repo.GitButler {
			add(sevWarn, "not in gitbutler workspace")
		}
	}
	return r
}

func branchNames(bs []repostatus.Branch, extra func(repostatus.Branch) string) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Name
		if extra != nil {
			out[i] += styleDim.Render(" (" + extra(b) + ")")
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func printReports(reports []report) {
	var nameW, headW int
	for _, r := range reports {
		nameW = max(nameW, len(r.status.Repo.Name()))
		headW = max(headW, lipgloss.Width(r.head))
	}

	counts := map[severity]int{}
	group := ""
	for _, r := range reports {
		sev := r.severity()
		counts[sev]++
		if statusFlags.problems && sev == sevInfo {
			continue
		}

		if dir := config.TildePath(filepath.Dir(r.status.Repo.Path)); dir != group {
			if group != "" {
				fmt.Println()
			}
			fmt.Println(styleHeading.Render(dir))
			group = dir
		}

		var icon string
		switch sev {
		case sevError:
			icon = styleError.Render("✗")
		case sevWarn:
			icon = styleWarn.Render("●")
		default:
			icon = styleOK.Render("✓")
		}

		var parts []string
		for _, i := range r.issues {
			parts = append(parts, severityStyle(i.sev).Render(i.text))
		}
		line := fmt.Sprintf("  %s %-*s  %s", icon, nameW, r.status.Repo.Name(), padRight(r.head, headW))
		if len(parts) > 0 {
			line += "  " + strings.Join(parts, styleDim.Render(" · "))
		}
		fmt.Println(strings.TrimRight(line, " "))

		if statusFlags.verbose {
			indent := strings.Repeat(" ", 4+nameW+2)
			for _, i := range r.issues {
				if len(i.details) > 0 {
					fmt.Println(indent + severityStyle(i.sev).Render(i.text+":") + " " + strings.Join(i.details, ", "))
				}
			}
		}
	}

	fmt.Println()
	summary := []string{plural(len(reports), "repo", "repos")}
	if n := counts[sevInfo]; n > 0 {
		summary = append(summary, styleOK.Render(fmt.Sprintf("%d ok", n)))
	}
	if n := counts[sevWarn]; n > 0 {
		summary = append(summary, styleWarn.Render(fmt.Sprintf("%d need attention", n)))
	}
	if n := counts[sevError]; n > 0 {
		summary = append(summary, styleError.Render(fmt.Sprintf("%d with errors", n)))
	}
	fmt.Println(strings.Join(summary, styleDim.Render(" · ")))
}

func severityStyle(s severity) lipgloss.Style {
	switch s {
	case sevError:
		return styleError
	case sevWarn:
		return styleWarn
	default:
		return styleDim
	}
}

// padRight pads a possibly styled string to a visible width.
func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}
