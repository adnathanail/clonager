package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/cli"
	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/discover"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var statusFlags struct {
	verbose bool
	all     bool
	forge   bool
	tags    []string
}

// statusHelp describes what clonager shows when run without a command.
const statusHelp = `Run without a command, clonager shows the configured repos that need
attention: uncommitted changes, stashes, branches that aren't on a remote,
branches with unpushed commits, and GitButler workspace state. --all also
shows the ones that are fine.

Branches already merged into the default branch (as a merge, fast-forward,
rebase or squash) are listed separately and don't need attention, as are
origin's merged branches.

Repos in the discoverPaths (see clonager discover) that aren't in the config
are listed at the end, including ones with no origin. Not with --tag.

With --forge, GitHub is also asked (via gh):
  - about merged PRs, which catches merges git alone can't see
  - which of origin's branches still exist, to spot stale remote refs
  - whether the repo deletes merged branches automatically

Remote state is as of each repo's last fetch; clonager never fetches. The
column before each repo shows how long ago that was (e.g. 23m, 7h, 5d),
in yellow if over a month.`

func init() {
	// Local, not persistent: they don't apply to discover or prune. Taking -v
	// leaves the version as just --version.
	rootCmd.Flags().BoolVarP(&statusFlags.verbose, "verbose", "v", false, "list the branches behind each count")
	rootCmd.Flags().BoolVarP(&statusFlags.all, "all", "a", false, "also show repos that are fine")
	rootCmd.Flags().BoolVarP(&statusFlags.forge, "forge", "f", false, "also check GitHub (via gh): merged PRs, stale refs, repo settings")
	rootCmd.Flags().StringSliceVarP(&statusFlags.tags, "tag", "t", nil, "only show repos with this tag (repeatable)")
	rootCmd.Args = cobra.NoArgs
	rootCmd.RunE = runStatus
}

func runStatus(cmd *cobra.Command, args []string) error {
	if err := checkForgeAvailable(statusFlags.forge); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Repos) == 0 {
		lipgloss.Println(styleDim.Render("No repos configured in " + config.TildePath(cfg.Path)))
		return nil
	}
	repos := filterByTags(cfg.Repos, statusFlags.tags)
	if len(repos) == 0 {
		lipgloss.Println(styleDim.Render("No repos tagged " + strings.Join(statusFlags.tags, " or ")))
		return nil
	}

	statuses := inspectAll(repos, repostatus.Options{Forge: statusFlags.forge})
	reports := make([]report, len(statuses))
	for i, s := range statuses {
		reports[i] = buildReport(s)
	}
	printReports(reports)
	code := statusCode(reports)
	pending, note := unapplied(cfg.Path)
	if len(statusFlags.tags) == 0 {
		untracked, err := printUntracked(cfg, pending)
		if err != nil {
			return err
		}
		if untracked {
			code = max(code, exitAttention)
		}
	}
	if note != "" {
		lipgloss.Println(styleWarn.Render(note))
	}
	return codeFor(code)
}

// statusCode is the exit code for the reports: exitErrors if any repo has an
// error, exitAttention if any needs attention.
func statusCode(reports []report) int {
	code := exitOK
	for _, r := range reports {
		switch r.severity() {
		case sevError:
			code = max(code, exitErrors)
		case sevWarn:
			code = max(code, exitAttention)
		}
	}
	return code
}

// untrackedRepo is a repo in the discoverPaths that isn't in the config.
type untrackedRepo struct {
	path     string
	noOrigin bool // so discover can't add it
}

// printUntracked lists the repos in the discoverPaths (see settingsDirs)
// that aren't in the config, nor in its source waiting to be applied
// (pending, which may be nil), and reports whether there were any.
func printUntracked(cfg, pending *config.Config) (bool, error) {
	dirs, _, err := settingsDirs()
	if err != nil {
		return false, err
	}
	repos, err := findUntracked(dirs, cfg, pending)
	if err != nil || len(repos) == 0 {
		return false, err
	}

	lipgloss.Println()
	lipgloss.Println(styleHeading.Render("Not in the config"))
	addable := 0
	for _, r := range repos {
		line := "  " + styleWarn.Render("?") + " " + fileLink(r.path, config.TildePath(r.path))
		if r.noOrigin {
			line += "  " + styleDim.Render("no origin")
		} else {
			addable++
		}
		lipgloss.Println(line)
	}
	switch {
	case addable == len(repos):
		lipgloss.Println(styleDim.Render("Add them with clonager discover."))
	case addable > 0:
		lipgloss.Println(styleDim.Render("Add them with clonager discover, once those without an origin have one."))
	default:
		lipgloss.Println(styleDim.Render("clonager discover can add them once they have an origin."))
	}
	return true, nil
}

func findUntracked(dirs []string, cfg, pending *config.Config) ([]untrackedRepo, error) {
	var repos []untrackedRepo
	for _, dir := range dirs {
		paths, err := discover.Find(dir, defaultDepth)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if cfg.Contains(path) || (pending != nil && pending.Contains(path)) {
				continue
			}
			_, err := discover.Describe(path)
			repos = append(repos, untrackedRepo{path: path, noOrigin: errors.Is(err, discover.ErrNoOrigin)})
		}
	}
	return repos, nil
}

// checkForgeAvailable fails early if --forge was given without gh installed,
// rather than reporting it for every repo.
func checkForgeAvailable(forge bool) error {
	if forge && !cli.Installed("gh") {
		return errors.New("--forge needs the GitHub CLI (gh) on your PATH")
	}
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
func inspectAll(repos []config.Repo, opts repostatus.Options) []*repostatus.Status {
	out := make([]*repostatus.Status, len(repos))
	sem := make(chan struct{}, max(4, runtime.NumCPU()))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = repostatus.Inspect(r, opts)
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
	fetch  string // rendered last-fetch column
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
		r.head = styleGB.Render(gitButlerHead(s.GitButler))
	case s.Head == "":
		r.head = styleWarn.Render("detached")
		add(sevWarn, "detached HEAD")
	default:
		r.head = styleBranch.Render(s.Head)
	}
	if len(s.RemoteURLs) > 0 {
		age, stale := fetchAge(s.LastFetch, time.Now())
		if stale {
			r.fetch = styleWarn.Render(age)
		} else {
			r.fetch = styleDim.Render(age)
		}
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
	if bs := s.DiffersFromPR(); len(bs) > 0 {
		add(sevWarn, plural(len(bs), "branch differs from its merged PR", "branches differ from their merged PRs"), branchNames(bs, func(b repostatus.Branch) string {
			return fmt.Sprintf("PR #%d", b.PR)
		})...)
	}
	if bs := s.MergedBranches(); len(bs) > 0 {
		add(sevInfo, plural(len(bs), "branch", "branches")+" merged", branchNames(bs, func(b repostatus.Branch) string {
			return mergedHow(b, s.DefaultBranch)
		})...)
	}
	if s.ForgeErr != nil {
		add(sevError, s.ForgeErr.Error())
	}

	// origin's branches
	if bs := s.StaleRemote(); len(bs) > 0 {
		add(sevInfo, plural(len(bs), "stale remote ref", "stale remote refs"), remoteNames(bs, nil)...)
	}
	if bs := s.MergedRemote(); len(bs) > 0 && ownsRemote(s) {
		where := "on origin"
		if s.GitHub != nil {
			where = "on GitHub"
		}
		add(sevInfo, plural(len(bs), "merged branch", "merged branches")+" "+where, remoteNames(bs, func(b repostatus.RemoteBranch) string {
			return describeMerge(b.Merged, b.PR, s.DefaultBranch)
		})...)
	}
	if gh := s.GitHub; gh != nil && ownsRemote(s) && gh.AutoDelete != nil && !*gh.AutoDelete {
		add(sevInfo, "GitHub doesn't auto-delete merged branches")
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

// gitButlerLogo stands in for GitButler's bowtie-shaped logo.
const gitButlerLogo = "⧓"

// gitButlerHead is the branch column for a repo in a GitButler workspace: the
// logo and its applied branches, or just the logo if there are none (or
// they're unknown).
func gitButlerHead(gb repostatus.GitButler) string {
	if gb.Err != nil || len(gb.Branches) == 0 {
		return gitButlerLogo
	}
	names := make([]string, len(gb.Branches))
	for i, b := range gb.Branches {
		names[i] = b.Name
	}
	return gitButlerLogo + " " + strings.Join(names, ", ")
}

// staleFetch is how long since a repo's last fetch before it's highlighted.
const staleFetch = 30 * 24 * time.Hour

// fetchAge gives how long ago a repo last fetched, in short form (23m, 7h,
// 5d), and whether that's long enough ago to highlight.
func fetchAge(last, now time.Time) (age string, stale bool) {
	if last.IsZero() {
		return "never", true
	}
	d := max(0, now.Sub(last))
	switch {
	case d < time.Hour:
		age = fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		age = fmt.Sprintf("%dh", int(d.Hours()))
	default:
		age = fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return age, d >= staleFetch
}

// mergedHow describes how a merged branch got into the default branch.
func mergedHow(b repostatus.Branch, defaultBranch string) string {
	return describeMerge(b.Merged, b.PR, defaultBranch)
}

func describeMerge(merged string, pr int, defaultBranch string) string {
	switch merged {
	case repostatus.MergedAncestor:
		return "in " + defaultBranch
	case repostatus.MergedRebased:
		return "rebased into " + defaultBranch
	case repostatus.MergedSquashed:
		return "squashed into " + defaultBranch
	case repostatus.MergedPR:
		return fmt.Sprintf("merged in PR #%d", pr)
	}
	return merged
}

func remoteNames(bs []repostatus.RemoteBranch, extra func(repostatus.RemoteBranch) string) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Name
		if extra != nil {
			out[i] += styleDim.Render(" (" + extra(b) + ")")
		}
	}
	return out
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
	// Repos that are fine are only shown with --all. Size the columns to the
	// rows shown, not the hidden ones.
	counts := map[severity]int{}
	var shown []report
	for _, r := range reports {
		sev := r.severity()
		counts[sev]++
		if statusFlags.all || sev != sevInfo {
			shown = append(shown, r)
		}
	}

	var nameW, headW, fetchW int
	for _, r := range shown {
		nameW = max(nameW, len(r.status.Repo.Name()))
		headW = max(headW, lipgloss.Width(r.head))
		fetchW = max(fetchW, lipgloss.Width(r.fetch))
	}

	group := ""
	for _, r := range shown {
		sev := r.severity()

		if dir := config.TildePath(filepath.Dir(r.status.Repo.Path)); dir != group {
			if group != "" {
				lipgloss.Println()
			}
			lipgloss.Println(styleHeading.Render(dir))
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
		name := fileLink(r.status.Repo.Path, r.status.Repo.Name())
		line := fmt.Sprintf("  %s %s %s  %s", padLeft(r.fetch, fetchW), icon, padRight(name, nameW), padRight(r.head, headW))
		if len(parts) > 0 {
			line += "  " + strings.Join(parts, styleDim.Render(" · "))
		}
		lipgloss.Println(strings.TrimRight(line, " "))

		if statusFlags.verbose {
			indent := strings.Repeat(" ", 2+fetchW+3+nameW+2)
			for _, i := range r.issues {
				if len(i.details) > 0 {
					lipgloss.Println(indent + severityStyle(i.sev).Render(i.text+":") + " " + strings.Join(i.details, ", "))
				}
			}
		}
	}

	if len(shown) > 0 {
		lipgloss.Println()
	}
	summary := []string{plural(len(reports), "repo", "repos")}
	if n := counts[sevInfo]; n > 0 {
		ok := styleOK.Render(fmt.Sprintf("%d ok", n))
		if !statusFlags.all {
			ok += styleDim.Render(" (-a to show)")
		}
		summary = append(summary, ok)
	}
	if n := counts[sevWarn]; n > 0 {
		summary = append(summary, styleWarn.Render(fmt.Sprintf("%d need attention", n)))
	}
	if n := counts[sevError]; n > 0 {
		summary = append(summary, styleError.Render(fmt.Sprintf("%d with errors", n)))
	}
	lipgloss.Println(strings.Join(summary, styleDim.Render(" · ")))
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

// padLeft right-aligns a possibly styled string to a visible width.
func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(0, w-lipgloss.Width(s))) + s
}

// padRight pads a possibly styled string to a visible width.
func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}
