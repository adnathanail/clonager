// Package repostatus inspects a local clone by shelling out to git (and to
// GitButler's `but`, when the repo is in a GitButler workspace).
package repostatus

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/adnathanail/clonager/internal/config"
)

type Branch struct {
	Name   string
	Tip    string // commit hash
	Remote string // remote-tracking ref it's compared against, empty if local-only
	Ahead  int
	Behind int
	Gone   bool // upstream is configured but was deleted on the remote

	Merged    string // how it was found merged into the default branch (Merged* consts), or empty
	PR        int    // merged PR for this branch, when checked with the forge
	PRDiffers bool   // a PR for this branch was merged, but without the local tip
}

// RemoteBranch is one of origin's branches, as of the last fetch.
type RemoteBranch struct {
	Name   string // branch name on the remote, e.g. fix-login
	Ref    string // remote-tracking ref, e.g. origin/fix-login
	Tip    string
	Merged string // how it was merged into the default branch (Merged* consts), or empty
	PR     int    // merged PR for this branch, when checked with the forge

	// Only known when checked with the forge:
	Stale bool // deleted on the forge; only the local remote-tracking ref is left
	Moved bool // on the forge, but with commits beyond what was merged (e.g. a bot reusing its branch)
}

type GitButlerMode int

const (
	GitButlerNone     GitButlerMode = iota // never set up
	GitButlerActive                        // HEAD is the gitbutler/workspace branch
	GitButlerInactive                      // has GitButler data but isn't in the workspace
)

type GitButlerBranch struct {
	Name   string
	Status string // but's branchStatus, e.g. completelyUnpushed, nothingToPush
}

type GitButler struct {
	Mode       GitButlerMode
	Branches   []GitButlerBranch // applied branches, across all stacks
	Conflicted int               // commits GitButler marks as conflicted
	Err        error             // `but status` failed; the rest is unknown
}

type Status struct {
	Repo config.Repo

	Missing bool  // path doesn't exist
	Err     error // exists but couldn't be inspected (e.g. not a git repo)

	Head       string // current branch, empty when detached
	Changed    int    // uncommitted changes, including untracked files
	Stashes    int
	Branches   []Branch // excludes GitButler's own gitbutler/* branches
	OriginURL  string
	RemoteURLs map[string]string
	GitButler  GitButler

	RemoteBranches []RemoteBranch // origin's branches, other than HEAD

	DefaultBranch string      // e.g. origin/main; what merged checks compare against
	GitHub        *GitHubRepo // origin on GitHub, when checked with the forge
	ForgeErr      error       // the forge check was requested but failed
}

// GitHubRepo is what the forge check learns about origin on GitHub.
type GitHubRepo struct {
	Name       string // owner/repo
	AutoDelete *bool  // delete head branches when PRs merge; nil if not visible (needs admin)
	CanPush    bool
	Admin      bool
}

type Options struct {
	Forge bool // ask the forge (GitHub, via gh) about merged PRs
}

// flagged is true for branches that would need attention if unmerged:
// local-only, deleted on the remote, or with unpushed commits.
func (b Branch) flagged() bool { return b.Remote == "" || b.Gone || b.Ahead > 0 }

// unresolved is true for branches not accounted for by a merge.
func (b Branch) unresolved() bool { return b.Merged == "" && !b.PRDiffers }

// LocalOnly lists branches with no counterpart on any remote.
func (s *Status) LocalOnly() []Branch {
	return filterBranches(s.Branches, func(b Branch) bool { return b.unresolved() && b.Remote == "" && !b.Gone })
}

// GoneBranches lists branches whose upstream was deleted on the remote.
func (s *Status) GoneBranches() []Branch {
	return filterBranches(s.Branches, func(b Branch) bool { return b.unresolved() && b.Gone })
}

// Unpushed lists branches with commits their remote counterpart doesn't have.
func (s *Status) Unpushed() []Branch {
	return filterBranches(s.Branches, func(b Branch) bool { return b.unresolved() && b.Ahead > 0 })
}

// MergedBranches lists branches already in the default branch, so safe to
// delete.
func (s *Status) MergedBranches() []Branch {
	return filterBranches(s.Branches, func(b Branch) bool { return b.Merged != "" })
}

// DiffersFromPR lists branches whose PR was merged without the local tip.
func (s *Status) DiffersFromPR() []Branch {
	return filterBranches(s.Branches, func(b Branch) bool { return b.PRDiffers })
}

// MergedRemote lists origin's branches that are merged into the default
// branch and, as far as is known, still on the remote. Without the forge
// check this is as of the last fetch, so may include stale refs.
func (s *Status) MergedRemote() []RemoteBranch {
	var out []RemoteBranch
	for _, rb := range s.RemoteBranches {
		if rb.Merged != "" && !rb.Stale && !rb.Moved {
			out = append(out, rb)
		}
	}
	return out
}

// StaleRemote lists remote-tracking refs for branches deleted on the forge.
func (s *Status) StaleRemote() []RemoteBranch {
	var out []RemoteBranch
	for _, rb := range s.RemoteBranches {
		if rb.Stale {
			out = append(out, rb)
		}
	}
	return out
}

// Behind lists branches their remote counterpart has moved past (as of the
// last fetch).
func (s *Status) Behind() []Branch {
	return filterBranches(s.Branches, func(b Branch) bool { return b.Behind > 0 })
}

func filterBranches(bs []Branch, keep func(Branch) bool) []Branch {
	var out []Branch
	for _, b := range bs {
		if keep(b) {
			out = append(out, b)
		}
	}
	return out
}

func Inspect(repo config.Repo, opts Options) *Status {
	s := &Status{Repo: repo}
	if _, err := os.Stat(repo.Path); errors.Is(err, os.ErrNotExist) {
		s.Missing = true
		return s
	}
	if err := s.inspect(opts); err != nil {
		s.Err = err
	}
	return s
}

func (s *Status) inspect(opts Options) error {
	g := git{dir: s.Repo.Path}

	out, err := g.run("rev-parse", "--show-toplevel", "--absolute-git-dir")
	if err != nil {
		return errors.New("not a git repo")
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return fmt.Errorf("unexpected rev-parse output %q", out)
	}
	top, gitDir := lines[0], lines[1]
	if !samePath(top, s.Repo.Path) {
		return fmt.Errorf("inside the repo %s, not a repo itself", config.TildePath(top))
	}

	if err := s.readWorkingTree(g); err != nil {
		return err
	}
	if err := s.readBranches(g); err != nil {
		return err
	}
	if err := s.readRemotes(g); err != nil {
		return err
	}
	if out, err := g.run("stash", "list"); err == nil && out != "" {
		s.Stashes = strings.Count(out, "\n") + 1
	}
	s.readGitButler(gitDir)
	s.checkMerged(g)
	if opts.Forge {
		s.checkForge(g)
	}
	return nil
}

func (s *Status) readWorkingTree(g git) error {
	out, err := g.run("status", "--porcelain=v2", "--branch")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, "# branch.head "):
			if head := strings.TrimPrefix(line, "# branch.head "); head != "(detached)" {
				s.Head = head
			}
		case strings.HasPrefix(line, "#"):
		default:
			s.Changed++
		}
	}
	return nil
}

func (s *Status) readBranches(g git) error {
	remoteRefs, err := g.run("for-each-ref", "--format=%(refname)%00%(objectname)", "refs/remotes")
	if err != nil {
		return err
	}
	var remoteNames []string
	onRemote := map[string]bool{}
	for _, line := range strings.Split(remoteRefs, "\n") {
		full, tip, ok := strings.Cut(line, "\x00")
		if !ok || strings.HasSuffix(full, "/HEAD") {
			continue
		}
		ref := strings.TrimPrefix(full, "refs/remotes/")
		onRemote[ref] = true
		if name, ok := strings.CutPrefix(ref, "origin/"); ok {
			s.RemoteBranches = append(s.RemoteBranches, RemoteBranch{Name: name, Ref: ref, Tip: tip})
		}
	}
	if out, err := g.run("remote"); err == nil && out != "" {
		remoteNames = strings.Split(out, "\n")
	}

	out, err := g.run("for-each-ref",
		"--format=%(refname:short)%00%(objectname)%00%(upstream:short)%00%(upstream:track,nobracket)", "refs/heads")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\x00", 4)
		if len(f) != 4 {
			continue
		}
		b := Branch{Name: f[0], Tip: f[1]}
		if strings.HasPrefix(b.Name, "gitbutler/") {
			continue // GitButler's own bookkeeping branches
		}
		upstream, track := f[2], f[3]
		switch {
		case track == "gone":
			b.Gone = true
		case upstream != "":
			b.Remote = upstream
			b.Ahead, b.Behind = parseTrack(track)
		default:
			// No upstream configured. GitButler pushes without setting one, so
			// fall back to a same-named branch on a remote (origin first).
			for _, r := range preferOrigin(remoteNames) {
				if ref := r + "/" + b.Name; onRemote[ref] {
					b.Remote = ref
					break
				}
			}
			if b.Remote != "" {
				counts, err := g.run("rev-list", "--left-right", "--count", b.Name+"..."+b.Remote)
				if err != nil {
					return err
				}
				if f := strings.Fields(counts); len(f) == 2 {
					b.Ahead, _ = strconv.Atoi(f[0])
					b.Behind, _ = strconv.Atoi(f[1])
				}
			}
		}
		s.Branches = append(s.Branches, b)
	}
	return nil
}

// parseTrack reads git's "ahead 2, behind 1" tracking summary.
func parseTrack(track string) (ahead, behind int) {
	for _, part := range strings.Split(track, ", ") {
		if n, ok := strings.CutPrefix(part, "ahead "); ok {
			ahead, _ = strconv.Atoi(n)
		} else if n, ok := strings.CutPrefix(part, "behind "); ok {
			behind, _ = strconv.Atoi(n)
		}
	}
	return ahead, behind
}

func preferOrigin(remotes []string) []string {
	out := make([]string, 0, len(remotes))
	for _, r := range remotes {
		if r == "origin" {
			out = append([]string{r}, out...)
		} else {
			out = append(out, r)
		}
	}
	return out
}

func (s *Status) readRemotes(g git) error {
	out, err := g.run("config", "--get-regexp", `^remote\..*\.url$`)
	if err != nil && out != "" {
		return err // exit status 1 with no output just means no remotes
	}
	s.RemoteURLs = map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		key, url, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")
		s.RemoteURLs[name] = url
	}
	s.OriginURL = s.RemoteURLs["origin"]
	return nil
}

func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

type git struct{ dir string }

// run runs a git command in the repo and returns its trimmed stdout.
func (g git) run(args ...string) (string, error) {
	return g.runStdin("", args...)
}

// runStdin is run with the given stdin.
func (g git) runStdin(stdin string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", g.dir}, args...)...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	// Don't let status checks take locks that would block the user's own git
	// commands, or prompt for anything.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := strings.TrimRight(stdout.String(), "\n")
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("git %s: %s", args[0], msg)
		}
		return out, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}
