package cmd

import (
	"fmt"
	"net/url"
	"runtime"
	"slices"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/cli"
	"github.com/adnathanail/clonager/internal/config"
	"github.com/adnathanail/clonager/internal/repostatus"
)

var tidyFlags struct {
	dryRun bool
}

var tidyCmd = &cobra.Command{
	Use:   "tidy",
	Short: "Switch the config's URLs to SSH, and remove airlifted branches that exist here",
	Long: `Tidy up the config:

  - HTTPS URLs on GitHub, GitLab, Bitbucket and Codeberg are switched to SSH,
    once git ls-remote shows the repo can be read over SSH. Repos where it
    can't (e.g. not found, or permission denied) get ssh: false, so they
    aren't tried again; remove it to retry. Network failures (no
    connection, timeouts) are only reported. Existing clones keep their old
    URLs: clonager clone prints the git remote set-url commands for them
    (commented out, to check first).
  - Branches recorded by clonager config airlift that now exist on this
    laptop are removed from the config (a repo with nothing else set goes
    back to just its URL). The rest stay listed until they're created, which
    clonager clone prints the commands for.

Don't run it on the laptop branches were airlifted from until its clones are
deleted: the branches still exist there, so it would remove them.

It checks over the network, and never changes a clone itself.`,
	Args: cobra.NoArgs,
	RunE: runTidy,
}

func init() {
	tidyCmd.Flags().BoolVarP(&tidyFlags.dryRun, "dry-run", "n", false, "show what would change without changing the config")
	configCmd.AddCommand(tidyCmd)
}

// sshHosts are the forges whose HTTPS URLs have a known SSH equivalent.
var sshHosts = []string{"github.com", "gitlab.com", "bitbucket.org", "codeberg.org"}

// sshURL returns the SSH URL for an HTTPS URL on one of the sshHosts, e.g.
// git@github.com:me/vip-proj.git for https://github.com/me/vip-proj, or
// false for any other URL.
func sshURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if !slices.Contains(sshHosts, host) {
		return "", false
	}
	path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || slices.Contains(parts, "") {
		return "", false
	}
	return "git@" + host + ":" + path + ".git", true
}

// urlChange is a remote whose URL in the config can be switched to SSH.
type urlChange struct {
	repo     config.Repo
	remote   string
	from, to string
	err      error // why it can't be (the SSH URL couldn't be read)
}

// sshChanges lists the remotes in repos with an SSH equivalent (see sshURL),
// other than in repos marked ssh: false.
func sshChanges(repos []config.Repo) []urlChange {
	var out []urlChange
	for _, r := range repos {
		if r.NoSSH {
			continue
		}
		remotes := append([]config.Remote{{Name: "origin", URL: r.URL}}, r.Remotes...)
		for _, rem := range remotes {
			if to, ok := sshURL(rem.URL); ok {
				out = append(out, urlChange{repo: r, remote: rem.Name, from: rem.URL, to: to})
			}
		}
	}
	return out
}

// checkSSH sets each change's err to why its SSH URL can't be read, by
// canRead, checking each URL once and several at a time.
func checkSSH(changes []urlChange, canRead func(string) error) {
	var urls []string
	for _, c := range changes {
		if !slices.Contains(urls, c.to) {
			urls = append(urls, c.to)
		}
	}
	errs := map[string]error{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(4, runtime.NumCPU()))
	for _, u := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			err := canRead(u)
			mu.Lock()
			errs[u] = err
			mu.Unlock()
		}()
	}
	wg.Wait()
	for i := range changes {
		changes[i].err = errs[changes[i].to]
	}
}

// networkErrors are signs that git ls-remote failed for want of a
// connection, rather than because the repo can't be read over SSH.
var networkErrors = []string{
	"could not resolve hostname",
	"timed out",
	"network is unreachable",
	"no route to host",
	"connection refused",
	"connection reset",
}

// isNetworkError reports whether err is one of the networkErrors, which
// shouldn't mark a repo ssh: false, as it may work next time.
func isNetworkError(err error) bool {
	msg := strings.ToLower(err.Error())
	return slices.ContainsFunc(networkErrors, func(e string) bool { return strings.Contains(msg, e) })
}

// tidyNeeds is what clonager config tidy may have to do, as far as can be
// told offline (see needsTidy).
type tidyNeeds struct {
	urls    int // HTTPS URLs that may switch to SSH
	landed  int // airlifted branches that exist here
	waiting int // airlifted branches that don't yet
}

func (n tidyNeeds) any() bool { return n.urls+n.landed+n.waiting > 0 }

// needsTidy works out, without the network, what tidy may have to do for
// repos: statuses (by path) are what's here, for their airlifted branches.
func needsTidy(repos []config.Repo, statuses map[string]*repostatus.Status) tidyNeeds {
	n := tidyNeeds{urls: len(sshChanges(repos))}
	var airlifted []*repostatus.Status
	for _, r := range repos {
		if len(r.Branches) == 0 {
			continue
		}
		s := repostatus.Status{Missing: true} // e.g. only in the config's source so far
		if found := statuses[r.Path]; found != nil {
			s = *found
		}
		s.Repo = r // the config's, which may be its source's
		airlifted = append(airlifted, &s)
	}
	for _, l := range landBranches(airlifted) {
		n.landed += len(l.landed)
		n.waiting += len(l.waiting)
	}
	return n
}

// landing is what tidy does with a repo's airlifted branches.
type landing struct {
	repo    config.Repo
	landed  []config.Branch // exist here, so they come out of the config
	waiting []config.Branch // don't exist here yet
	err     error           // the repo couldn't be checked
}

// landBranches works out, for each repo with airlifted branches, which of
// them exist here.
func landBranches(statuses []*repostatus.Status) []landing {
	var out []landing
	for _, s := range statuses {
		l := landing{repo: s.Repo}
		switch {
		case s.Missing:
			l.waiting = s.Repo.Branches
		case s.Err != nil:
			l.err = s.Err
		default:
			for _, b := range s.Repo.Branches {
				if slices.ContainsFunc(s.Branches, func(h repostatus.Branch) bool { return h.Name == b.Name }) {
					l.landed = append(l.landed, b)
				} else {
					l.waiting = append(l.waiting, b)
				}
			}
		}
		out = append(out, l)
	}
	return out
}

func runTidy(cmd *cobra.Command, args []string) error {
	if err := loadOpenIn(); err != nil {
		return err
	}
	cfg, fromSource, err := writableConfig("tidy", tidyFlags.dryRun)
	if err != nil {
		return err
	}

	changes := sshChanges(cfg.Repos)
	checkSSH(changes, cli.CanRead)
	var airlifted []config.Repo
	for _, r := range cfg.Repos {
		if len(r.Branches) > 0 {
			airlifted = append(airlifted, r)
		}
	}
	landings := landBranches(inspectAll(airlifted, repostatus.Options{}))

	// Edit and print as we go: each section is a heading and rows of a path
	// and details, with the paths lined up across sections.
	type row struct{ path, icon, detail string }
	type section struct {
		heading string
		rows    []row
	}
	var switched, marked, unchecked, landed, waiting, failed section
	code := exitOK
	changed := false
	for _, c := range changes {
		if c.err != nil && isNetworkError(c.err) {
			unchecked.rows = append(unchecked.rows, row{c.repo.Path, styleError.Render("✗"),
				styleBranch.Render(c.remote) + "  " + styleDim.Render(c.err.Error())})
			code = max(code, exitErrors)
			continue
		}
		if c.err != nil {
			if err := cfg.SetNoSSH(c.repo.Path); err != nil {
				return err
			}
			changed = true
			marked.rows = append(marked.rows, row{c.repo.Path, styleWarn.Render("!"),
				styleBranch.Render(c.remote) + "  " + styleDim.Render(c.err.Error())})
			continue
		}
		if err := cfg.SetURL(c.repo.Path, c.remote, c.to); err != nil {
			return err
		}
		changed = true
		switched.rows = append(switched.rows, row{c.repo.Path, styleOK.Render("→"),
			styleBranch.Render(c.remote) + "  " + c.to})
	}
	names := func(branches []config.Branch) string {
		out := make([]string, len(branches))
		for i, b := range branches {
			out[i] = styleBranch.Render(b.Name)
		}
		return strings.Join(out, ", ")
	}
	for _, l := range landings {
		switch {
		case l.err != nil:
			failed.rows = append(failed.rows, row{l.repo.Path, styleError.Render("✗"), styleDim.Render(l.err.Error())})
			code = max(code, exitErrors)
			continue
		case len(l.landed) > 0:
			if err := cfg.SetBranches(l.repo.Path, l.waiting); err != nil {
				return err
			}
			changed = true
			landed.rows = append(landed.rows, row{l.repo.Path, styleOK.Render("-"), names(l.landed)})
		}
		if len(l.waiting) > 0 {
			waiting.rows = append(waiting.rows, row{l.repo.Path, styleWarn.Render("●"), names(l.waiting)})
			code = max(code, exitAttention)
		}
	}

	switched.heading = "Switched to SSH:"
	marked.heading = "Can't be read over SSH, so marked ssh: false:"
	unchecked.heading = "Couldn't check over SSH:"
	landed.heading = "Removed from the config, as they exist here:"
	waiting.heading = "Airlifted branches not created here yet (clonager clone prints the commands):"
	failed.heading = "Couldn't check airlifted branches:"
	if tidyFlags.dryRun {
		switched.heading = "Would switch to SSH:"
		marked.heading = "Can't be read over SSH, so would mark ssh: false:"
		landed.heading = "Would remove from the config, as they exist here:"
	}

	sections := []section{switched, marked, unchecked, landed, waiting, failed}
	pathW := 0
	for _, s := range sections {
		for _, r := range s.rows {
			pathW = max(pathW, len(config.TildePath(r.path)))
		}
	}
	for _, s := range sections {
		if len(s.rows) == 0 {
			continue
		}
		lipgloss.Println(styleHeading.Render(s.heading))
		for _, r := range s.rows {
			path := folderLink(r.path, config.TildePath(r.path))
			lipgloss.Println(fmt.Sprintf("  %s %s  %s", r.icon, padRight(path, pathW), r.detail))
		}
		lipgloss.Println()
	}

	switch {
	case !changed && code == exitOK:
		lipgloss.Println(styleDim.Render("Nothing to tidy."))
	case !changed:
		lipgloss.Println(styleDim.Render("The config is unchanged."))
	case tidyFlags.dryRun:
		lipgloss.Println(styleDim.Render("Not changing the config (--dry-run)."))
	default:
		if err := cfg.Save(); err != nil {
			return err
		}
		lipgloss.Println("Updated " + config.TildePath(cfg.Path) + ".")
		if fromSource {
			lipgloss.Println(styleDim.Render("Rebuild (e.g. darwin-rebuild switch) to apply it."))
		}
		if len(switched.rows) > 0 {
			lipgloss.Println(styleDim.Render("clonager clone prints the git remote set-url commands for clones still using the old URLs."))
		}
	}
	return codeFor(code)
}
