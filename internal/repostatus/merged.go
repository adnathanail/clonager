package repostatus

import (
	"fmt"
	"regexp"
	"strings"
)

// How a branch was found to be merged into the default branch.
const (
	MergedAncestor = "ancestor" // the branch tip is in the default branch's history
	MergedRebased  = "rebased"  // every commit has a patch-identical copy (rebase merge, cherry-pick)
	MergedSquashed = "squashed" // the branch's whole diff matches one commit (squash merge)
	MergedPR       = "pr"       // the forge has a merged PR whose head contains the branch tip
)

// defaultBranch finds the remote-tracking ref branches get merged into:
// origin/HEAD, falling back to the usual names.
func (g git) defaultBranch() string {
	if ref, err := g.run("symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return ref
	}
	for _, ref := range []string{"origin/main", "origin/master", "main", "master"} {
		if _, err := g.run("rev-parse", "--verify", "-q", ref+"^{commit}"); err == nil {
			return ref
		}
	}
	return ""
}

// defaultBranchName is the local name of the default branch, e.g. main for
// origin/main.
func (s *Status) defaultBranchName() string {
	_, name, found := strings.Cut(s.DefaultBranch, "/")
	if !found {
		return s.DefaultBranch
	}
	return name
}

// checkMerged sets Merged on every branch other than the default one that's
// already in the default branch, using only local git data: local branches
// (including pushed, up-to-date ones, as a merged PR's branch can outlive the
// merge) and origin's remote-tracking branches.
func (s *Status) checkMerged(g git) {
	s.DefaultBranch = g.defaultBranch()
	if s.DefaultBranch == "" {
		return
	}
	var targets []mergeTarget
	for i := range s.Branches {
		if b := &s.Branches[i]; b.Name != s.defaultBranchName() {
			targets = append(targets, mergeTarget{b.Tip, &b.Merged, true})
		}
	}
	// Remote branches skip the squash check: busy remotes have many old
	// branches, and diffing each against its fork point is slow.
	for i := range s.RemoteBranches {
		if rb := &s.RemoteBranches[i]; rb.Ref != s.DefaultBranch {
			targets = append(targets, mergeTarget{rb.Tip, &rb.Merged, false})
		}
	}
	g.classifyMerged(s.DefaultBranch, targets)
}

// A commit to check, and where to record how it was merged.
type mergeTarget struct {
	tip    string
	merged *string
	squash bool // also check for a squash merge
}

// maxBaseCommits caps how far back on the default branch merges are looked
// for. Branches merged before the cap are reported unmerged instead.
const maxBaseCommits = 10000

// fingerprintFormat identifies a commit by what survives a rebase,
// cherry-pick or single-commit squash merge: author and subject. (GitHub's
// squash merges get a new date and a " (#123)" suffix; see parseFingerprints.)
const fingerprintFormat = "--format=%H%x00%ae%x00%s"

var prSuffix = regexp.MustCompile(` \(#\d+\)$`)

// classifyMerged records, for each target, whether and how its tip is merged
// into base.
//
// Rebase and squash merges leave no trace in the history, so they're found by
// patch-id: a branch is rebased in if every one of its commits has a
// patch-identical commit on base, and squashed in if its whole diff matches
// one. Computing patch-ids means diffing, which is slow over a long history,
// so candidates are found cheaply first: for rebases, base commits with the
// same author, date and subject; for squashes, base commits touching the same
// files. Work is batched across branches so a repo with many branches costs a
// handful of git processes rather than several per branch.
func (g git) classifyMerged(base string, targets []mergeTarget) {
	var tips []string
	seenTip := map[string]bool{}
	squash := map[string]bool{}
	for _, t := range targets {
		if !seenTip[t.tip] {
			seenTip[t.tip] = true
			tips = append(tips, t.tip)
		}
		squash[t.tip] = squash[t.tip] || t.squash
	}
	if len(tips) == 0 {
		return
	}
	result := map[string]string{}
	defer func() {
		for _, t := range targets {
			*t.merged = result[t.tip]
		}
	}()
	notBase := strings.Join(append(tips, "--not", base), "\n")

	// Every commit on a target but not on base, with its parents.
	out, err := g.runStdin(notBase, "rev-list", "--parents", "--stdin")
	if err != nil {
		return
	}
	parents := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			parents[f[0]] = f[1:]
		}
	}

	// Each tip's own commits (excluding merges), by walking back from it
	// until reaching commits that are on base. A tip with none is on base.
	own := map[string][]string{}
	for _, tip := range tips {
		if _, ok := parents[tip]; !ok {
			result[tip] = MergedAncestor
			continue
		}
		seen := map[string]bool{}
		stack := []string{tip}
		for len(stack) > 0 {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			ps, ok := parents[c]
			if !ok || seen[c] {
				continue
			}
			seen[c] = true
			if len(ps) < 2 {
				own[tip] = append(own[tip], c)
			}
			stack = append(stack, ps...)
		}
		if len(own[tip]) == 0 {
			// Only merge commits beyond base (e.g. base merged back in, or a
			// GitButler workspace commit): everything else is on base.
			result[tip] = MergedAncestor
		}
	}
	if len(parents) == 0 {
		return
	}

	// Recent base commits, by fingerprint. (Not bounded by the tips' common
	// ancestor: a branch with unrelated history, like gh-pages, has none.)
	baseByFP := map[string][]string{}
	out, err = g.run("log", "--no-merges", fmt.Sprintf("--max-count=%d", maxBaseCommits), fingerprintFormat, base)
	if err != nil {
		return
	}
	for commit, fp := range parseFingerprints(out) {
		baseByFP[fp] = append(baseByFP[fp], commit)
	}
	out, err = g.runStdin(notBase, "log", "--no-merges", fingerprintFormat, "--stdin")
	if err != nil {
		return
	}
	ownFP := parseFingerprints(out)

	// Rebase candidates: tips whose every commit has a fingerprint match.
	var candidates []string
	var toDiff []string
	for _, tip := range tips {
		if result[tip] != "" || len(own[tip]) == 0 {
			continue
		}
		matched := true
		for _, c := range own[tip] {
			if len(baseByFP[ownFP[c]]) == 0 {
				matched = false
				break
			}
		}
		if matched {
			candidates = append(candidates, tip)
			for _, c := range own[tip] {
				toDiff = append(toDiff, c)
				toDiff = append(toDiff, baseByFP[ownFP[c]]...)
			}
		}
	}
	if len(toDiff) > 0 {
		if ids, err := g.commitPatchIDs(toDiff); err == nil {
			for _, tip := range candidates {
				if rebasedOnto(own[tip], ownFP, baseByFP, ids) {
					result[tip] = MergedRebased
				}
			}
		}
	}

	for _, tip := range tips {
		if result[tip] == "" && squash[tip] && len(own[tip]) > 1 && g.squashMerged(tip, base) {
			result[tip] = MergedSquashed
		}
	}
}

// rebasedOnto reports whether each commit has a fingerprint match on base
// with the same patch-id. Empty commits have no patch-id, so for those the
// fingerprint match alone counts, provided some commit has a patch.
func rebasedOnto(commits []string, fps map[string]string, baseByFP map[string][]string, ids map[string]string) bool {
	anyPatch := false
	for _, c := range commits {
		id := ids[c]
		if id == "" {
			continue
		}
		anyPatch = true
		found := false
		for _, b := range baseByFP[fps[c]] {
			if ids[b] == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return anyPatch
}

func parseFingerprints(log string) map[string]string {
	fps := map[string]string{}
	for _, line := range strings.Split(log, "\n") {
		if commit, fp, ok := strings.Cut(line, "\x00"); ok {
			fps[commit] = prSuffix.ReplaceAllString(fp, "")
		}
	}
	return fps
}

// commitPatchIDs returns commit -> patch-id for the given commits.
func (g git) commitPatchIDs(commits []string) (map[string]string, error) {
	return g.patchIDMap(strings.Join(commits, "\n"), "--no-walk")
}

// squashMerged reports whether the branch's whole diff since it forked from
// base matches a commit on base, since the fork, touching the same files.
func (g git) squashMerged(tip, base string) bool {
	mb, err := g.run("merge-base", tip, base)
	if err != nil {
		return false
	}
	files, err := g.run("diff", "--name-only", "--no-renames", mb, tip)
	if err != nil || files == "" {
		return false
	}
	args := []string{"log", "--no-merges", "--format=%H", fmt.Sprintf("--max-count=%d", maxBaseCommits), mb + ".." + base, "--"}
	cands, err := g.run(append(args, strings.Split(files, "\n")...)...)
	if err != nil || cands == "" {
		return false
	}
	ids, err := g.patchIDsOf("diff", "--no-color", "--no-ext-diff", mb, tip)
	if err != nil || len(ids) != 1 {
		return false
	}
	candIDs, err := g.commitPatchIDs(strings.Split(cands, "\n"))
	if err != nil {
		return false
	}
	for _, id := range candIDs {
		if id == ids[0] {
			return true
		}
	}
	return false
}

// patchIDMap returns commit -> patch-id for the non-merge commits in revs
// (newline-separated, as for --stdin).
func (g git) patchIDMap(revs string, extra ...string) (map[string]string, error) {
	args := append([]string{"log", "-p", "--no-merges", "--no-color", "--no-ext-diff"}, extra...)
	out, err := g.runStdin(revs, append(args, "--stdin")...)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if out == "" {
		return m, nil
	}
	lines, err := g.patchIDLines([]byte(out + "\n"))
	if err != nil {
		return nil, err
	}
	for _, f := range lines {
		m[f[1]] = f[0]
	}
	return m, nil
}

// patchIDsOf runs a git command producing patches and returns their
// patch-ids.
func (g git) patchIDsOf(args ...string) ([]string, error) {
	out, err := g.run(args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return g.patchIDs([]byte(out + "\n"))
}

// patchIDs runs git patch-id over a diff or log, returning one id per patch.
func (g git) patchIDs(patch []byte) ([]string, error) {
	lines, err := g.patchIDLines(patch)
	ids := make([]string, len(lines))
	for i, f := range lines {
		ids[i] = f[0]
	}
	return ids, err
}

// patchIDLines runs git patch-id, returning [patch-id, commit] pairs.
func (g git) patchIDLines(patch []byte) ([][2]string, error) {
	out, err := g.runStdin(string(patch), "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	var pairs [][2]string
	for _, line := range strings.Split(out, "\n") {
		if id, commit, ok := strings.Cut(line, " "); ok {
			pairs = append(pairs, [2]string{id, commit})
		}
	}
	return pairs, nil
}

// isAncestor reports whether commit a is in b's history. False if b isn't
// available locally.
func (g git) isAncestor(a, b string) bool {
	_, err := g.run("merge-base", "--is-ancestor", a, b)
	return err == nil
}
