package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/cli"
	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/discover"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var statusFlags struct {
	verbose bool
	all     bool
	offline bool
	tags    []string
}

func init() {
	// Local, not persistent: they don't apply to discover or prune. Taking -v
	// leaves the version as just --version.
	rootCmd.Flags().BoolVarP(&statusFlags.verbose, "verbose", "v", false, "list the branches behind each count")
	rootCmd.Flags().BoolVarP(&statusFlags.all, "all", "a", false, "also show repos that are fine")
	rootCmd.Flags().BoolVarP(&statusFlags.offline, "offline", "o", false, "don't check GitHub (default from alwaysOffline in settings.json)")
	rootCmd.Flags().StringSliceVarP(&statusFlags.tags, "tag", "t", nil, "only show repos with this tag (repeatable)")
	rootCmd.Args = cobra.NoArgs
	rootCmd.RunE = runStatus
}

func runStatus(cmd *cobra.Command, args []string) error {
	settings, err := config.ReadSettings()
	if err != nil {
		return err
	}
	openIn = settings.OpenIn
	offline := settings.AlwaysOffline
	if cmd.Flags().Changed("offline") {
		offline = statusFlags.offline
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

	v := &statusView{cfg: cfg, statuses: inspectAll(repos, repostatus.Options{})}
	v.pending, v.note = unapplied(cfg.Path)
	if len(statusFlags.tags) == 0 {
		dirs, _, err := settingsDirs()
		if err != nil {
			return err
		}
		if v.untracked, err = findUntracked(dirs, cfg, v.pending); err != nil {
			return err
		}
		v.showUntracked = true
	}

	switch {
	case offline:
	case !cli.Installed("gh"):
		v.forgeWarning = "Couldn't check GitHub: gh isn't installed."
	case liveTerminal():
		return showLive(v)
	default:
		forgeAll(v.statuses, nil)
		v.foldForgeErrors()
	}
	out, code := v.render()
	lipgloss.Print(out)
	return codeFor(code)
}

// statusView is everything status shows, so it can be drawn again once
// GitHub has been checked.
type statusView struct {
	cfg           *config.Config
	pending       *config.Config // the config's source, if it has changes not applied yet
	note          string         // saying so
	statuses      []*repostatus.Status
	showUntracked bool // and what config tidy has to do (not with --tag)
	untracked     []untrackedRepo
	forgeWarning  string // why GitHub couldn't be checked
}

// render draws the view, and gives the exit code for it.
func (v *statusView) render() (string, int) {
	var w strings.Builder
	reports := make([]report, len(v.statuses))
	for i, s := range v.statuses {
		reports[i] = buildReport(s)
	}
	writeReports(&w, reports)
	code := statusCode(reports)
	if v.showUntracked {
		if writeUntracked(&w, v.untracked) {
			code = max(code, exitAttention)
		}
		if writeTidy(&w, v.cfg, v.pending, v.statuses) {
			code = max(code, exitAttention)
		}
	}
	if v.note != "" {
		fmt.Fprintln(&w, styleWarn.Render(v.note))
	}
	if v.forgeWarning != "" {
		fmt.Fprintln(&w, styleWarn.Render(v.forgeWarning)+styleDim.Render(" (--offline skips checking it)"))
		code = max(code, exitErrors)
	}
	return w.String(), code
}

// foldForgeErrors moves forge errors that aren't about a particular repo
// (no connection, gh not logged in) out of the repos and into one warning.
func (v *statusView) foldForgeErrors() {
	var reason string
	n := 0
	for _, s := range v.statuses {
		if s.ForgeErr == nil {
			continue
		}
		if r := forgeUnreachable(s.ForgeErr); r != "" {
			reason = r
			n++
			s.ForgeErr = nil
		}
	}
	if n > 0 {
		v.forgeWarning = fmt.Sprintf("Couldn't check GitHub for %s: %s.", plural(n, "repo", "repos"), reason)
	}
}

// forgeUnreachable says why GitHub couldn't be asked at all, if that's what
// err is, or "" for an error about the repo itself.
func forgeUnreachable(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "gh auth login"), strings.Contains(msg, "http 401"):
		return "gh isn't logged in (run gh auth login)"
	case isNetworkError(err):
		return "no connection"
	}
	return ""
}

// liveTerminal reports whether stdout is a terminal that status can draw
// a spinner in, and redraw.
func liveTerminal() bool {
	return term.IsTerminal(os.Stdout.Fd()) && os.Getenv("TERM") != "dumb"
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// showLive prints the view straight away, then checks GitHub with a spinner
// below it, and redraws it with what GitHub says: in place if all of it is
// still on screen, otherwise again below. Ctrl-C stops waiting, leaving the
// view as it was.
func showLive(v *statusView) error {
	out, code := v.render()
	lipgloss.Print(out)
	width, height, err := term.GetSize(os.Stdout.Fd())
	if err != nil {
		width, height = 0, 0
	}
	rows := 0
	if width > 0 {
		rows = outputRows(out, width)
	}
	inPlace := width > 0 && rows+1 <= height // +1 for the spinner

	// A resize can rewrap what's on screen, so the rows no longer add up.
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	defer signal.Stop(resized)
	defer signal.Stop(interrupted)

	var checked atomic.Int32
	finished := make(chan struct{})
	go func() {
		forgeAll(v.statuses, &checked)
		close(finished)
	}()

	lipgloss.Print(ansi.HideCursor)
	defer lipgloss.Print(ansi.ShowCursor)
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	clearLine := "\r" + ansi.EraseEntireLine
	for frame, waiting := 0, true; waiting; frame++ {
		spinner := styleDim.Render(fmt.Sprintf("%s Checking GitHub… %d/%d",
			spinnerFrames[frame%len(spinnerFrames)], checked.Load(), len(v.statuses)))
		if width > 0 {
			spinner = ansi.Truncate(spinner, width-1, "")
		}
		lipgloss.Print(clearLine + spinner)
		select {
		case <-tick.C:
		case <-resized:
			inPlace = false
		case <-interrupted:
			lipgloss.Print(clearLine)
			return codeFor(code)
		case <-finished:
			waiting = false
		}
	}

	v.foldForgeErrors()
	out, code = v.render()
	if inPlace {
		lipgloss.Print(clearLine + ansi.CursorUp(rows) + ansi.EraseScreenBelow + out)
	} else {
		lipgloss.Print(clearLine + "\n\n" + out)
	}
	return codeFor(code)
}

// outputRows is how many rows out takes up in a terminal width columns wide,
// with long lines wrapping.
func outputRows(out string, width int) int {
	n := 0
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		n += max(1, (ansi.StringWidth(line)+width-1)/width)
	}
	return n
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

// writeUntracked lists the repos in the discoverPaths that aren't in the
// config (see findUntracked), and reports whether there were any.
func writeUntracked(w *strings.Builder, repos []untrackedRepo) bool {
	if len(repos) == 0 {
		return false
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, styleHeading.Render("Not in the config"))
	addable := 0
	for _, r := range repos {
		line := "  " + styleWarn.Render("?") + " " + folderLink(r.path, config.TildePath(r.path))
		if r.noOrigin {
			line += "  " + styleDim.Render("no origin")
		} else {
			addable++
		}
		fmt.Fprintln(w, line)
	}
	switch {
	case addable == len(repos):
		fmt.Fprintln(w, styleDim.Render("Add them with clonager config discover."))
	case addable > 0:
		fmt.Fprintln(w, styleDim.Render("Add them with clonager config discover, once those without an origin have one."))
	default:
		fmt.Fprintln(w, styleDim.Render("clonager config discover can add them once they have an origin."))
	}
	return true
}

// writeTidy shows what clonager config tidy may have to do (see needsTidy)
// for the config, or its source waiting to be applied (pending, which may be
// nil), and reports whether there's anything.
func writeTidy(w *strings.Builder, cfg, pending *config.Config, statuses []*repostatus.Status) bool {
	repos := cfg.Repos
	if pending != nil {
		repos = pending.Repos
	}
	byPath := map[string]*repostatus.Status{}
	for _, s := range statuses {
		byPath[s.Repo.Path] = s
	}
	n := needsTidy(repos, byPath)
	if !n.any() {
		return false
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, styleHeading.Render("Config to tidy"))
	line := func(count int, one, many, detail string) {
		if count > 0 {
			fmt.Fprintln(w, "  "+styleWarn.Render("●")+" "+plural(count, one, many)+detail)
		}
	}
	line(n.urls, "HTTPS URL", "HTTPS URLs", " to try switching to SSH")
	line(n.clones, "clone remote", "clone remotes", " to switch to SSH, as in the config")
	line(n.gitbutler, "repo", "repos", " in a GitButler workspace, to mark gitbutler: true")
	line(n.gbSettings, "GitButler workspace", "GitButler workspaces", " with a target branch or push remote to record in the config")
	line(n.gbDrift, "GitButler workspace", "GitButler workspaces", " with a target branch or push remote other than the config's "+styleDim.Render("(see clonager clone)"))
	line(n.landed, "airlifted branch", "airlifted branches", " now here, to remove from the config")
	line(n.waiting, "airlifted branch", "airlifted branches", " to create here "+styleDim.Render("(see clonager clone)"))
	if n.urls+n.clones+n.gitbutler+n.gbSettings+n.landed > 0 {
		hint := "Run clonager config tidy."
		if n.landed > 0 {
			hint += " If this is the laptop the branches were airlifted from, delete its clones first."
		}
		fmt.Fprintln(w, styleDim.Render(hint))
	}
	return true
}

// findUntracked finds the repos in dirs that aren't in the config, nor in
// its source waiting to be applied (pending, which may be nil).
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
			repos = append(repos, untrackedRepo{path: path, noOrigin: !discover.HasOrigin(path)})
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

// forgeAll checks GitHub for statuses in parallel (see Status.CheckForge),
// adding one to checked, if given, as each is done.
func forgeAll(statuses []*repostatus.Status, checked *atomic.Int32) {
	sem := make(chan struct{}, max(4, runtime.NumCPU()))
	var wg sync.WaitGroup
	for _, s := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s.CheckForge()
			if checked != nil {
				checked.Add(1)
			}
		}()
	}
	wg.Wait()
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
		r.head = gitButlerHead(s.GitButler, s.Repo.Path)
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
// they're unknown). The logo links to the project in the GitButler app.
func gitButlerHead(gb repostatus.GitButler, path string) string {
	logo := gitButlerLink(path, gitButlerLogo)
	if gb.Err != nil || len(gb.Branches) == 0 {
		return logo
	}
	names := make([]string, len(gb.Branches))
	for i, b := range gb.Branches {
		names[i] = b.Name
	}
	return logo + styleGB.Render(" "+strings.Join(names, ", "))
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

func writeReports(w *strings.Builder, reports []report) {
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
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, styleHeading.Render(dir))
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
		name := folderLink(r.status.Repo.Path, r.status.Repo.Name())
		line := fmt.Sprintf("  %s %s %s  %s", padLeft(r.fetch, fetchW), icon, padRight(name, nameW), padRight(r.head, headW))
		if len(parts) > 0 {
			line += "  " + strings.Join(parts, styleDim.Render(" · "))
		}
		fmt.Fprintln(w, strings.TrimRight(line, " "))

		if statusFlags.verbose {
			indent := strings.Repeat(" ", 2+fetchW+3+nameW+2)
			for _, i := range r.issues {
				if len(i.details) > 0 {
					fmt.Fprintln(w, indent+severityStyle(i.sev).Render(i.text+":")+" "+strings.Join(i.details, ", "))
				}
			}
		}
	}

	if len(shown) > 0 {
		fmt.Fprintln(w)
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
	fmt.Fprintln(w, strings.Join(summary, styleDim.Render(" · ")))
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
