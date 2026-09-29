package repostatus

import "strings"

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
// already in the default branch, using only local git data. That includes
// pushed, up-to-date branches: a merged PR's branch often outlives the merge
// on the remote.
//
// Rebase and squash merges leave no trace in the history, so they're found by
// patch-id: a branch is rebased in if every one of its commits has a
// patch-identical commit on the default branch, and squashed in if its whole
// diff matches one. Work is batched across branches so a repo with many
// branches costs a handful of git processes rather than several per branch.
func (s *Status) checkMerged(g git) {
	s.DefaultBranch = g.defaultBranch()
	if s.DefaultBranch == "" {
		return
	}
	base, baseName := s.DefaultBranch, s.defaultBranchName()

	var cands []*Branch
	for i := range s.Branches {
		b := &s.Branches[i]
		if b.Name != baseName {
			cands = append(cands, b)
		}
	}
	if len(cands) == 0 {
		return
	}
	tips := make([]string, len(cands))
	for i, b := range cands {
		tips[i] = b.Tip
	}

	// Every commit on a candidate branch but not on base, with its parents.
	out, err := g.runStdin(strings.Join(append(tips, "--not", base), "\n"), "rev-list", "--parents", "--stdin")
	if err != nil {
		return
	}
	parents := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			parents[f[0]] = f[1:]
		}
	}

	// Each branch's own commits (excluding merges), by walking back from its
	// tip until reaching commits that are on base.
	own := make([][]string, len(cands))
	for i, b := range cands {
		if _, ok := parents[b.Tip]; !ok {
			b.Merged = MergedAncestor // the tip is on base
			continue
		}
		seen := map[string]bool{}
		stack := []string{b.Tip}
		for len(stack) > 0 {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			ps, ok := parents[c]
			if !ok || seen[c] {
				continue
			}
			seen[c] = true
			if len(ps) < 2 {
				own[i] = append(own[i], c)
			}
			stack = append(stack, ps...)
		}
	}
	if len(parents) == 0 {
		return
	}

	// Patch-ids for those commits, and for base since the common ancestor of
	// every branch (which covers each branch's fork point).
	branchIDs, err := g.patchIDMap(strings.Join(append(tips, "--not", base), "\n"))
	if err != nil {
		return
	}
	since, err := g.run(append([]string{"merge-base", "--octopus", base}, tips...)...)
	if err != nil {
		return
	}
	baseIDs, err := g.patchIDMap(base + "\n--not\n" + since)
	if err != nil {
		return
	}
	onBase := map[string]bool{}
	for _, id := range baseIDs {
		onBase[id] = true
	}

	for i, b := range cands {
		if b.Merged != "" || len(own[i]) == 0 {
			continue
		}
		var ids []string
		for _, c := range own[i] {
			if id := branchIDs[c]; id != "" { // empty commits have no patch-id
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 && allIn(ids, onBase) {
			b.Merged = MergedRebased
		} else if len(own[i]) > 1 && g.squashMerged(b.Tip, base, onBase) {
			b.Merged = MergedSquashed
		}
	}
}

// squashMerged reports whether the branch's whole diff since it forked from
// base matches a patch on base.
func (g git) squashMerged(tip, base string, onBase map[string]bool) bool {
	mb, err := g.run("merge-base", tip, base)
	if err != nil {
		return false
	}
	ids, err := g.patchIDsOf("diff", "--no-color", "--no-ext-diff", mb, tip)
	return err == nil && len(ids) == 1 && onBase[ids[0]]
}

// patchIDMap returns commit -> patch-id for the non-merge commits in revs
// (newline-separated, as for --stdin).
func (g git) patchIDMap(revs string) (map[string]string, error) {
	out, err := g.runStdin(revs, "log", "-p", "--no-merges", "--no-color", "--no-ext-diff", "--stdin")
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

func allIn(ids []string, set map[string]bool) bool {
	for _, id := range ids {
		if !set[id] {
			return false
		}
	}
	return true
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
