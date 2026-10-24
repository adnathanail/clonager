# How merged branches are detected

Rebase and squash merges create new commits, so a merged branch isn't
necessarily in the default branch's history. For each local branch, and each of
`origin`'s branches, clonager compares it with the default branch
(`origin/HEAD`, else `origin/main` or `origin/master`):

1. **Ancestor** — the branch tip is in the default branch's history (merge
   commits, fast-forwards), or the only commits beyond it are merges.
2. **Rebased** — every commit on the branch has a patch-identical commit on the
   default branch, using `git patch-id` (as `git cherry` does). This also
   catches single-commit squash merges.
3. **Squashed** — the branch's whole diff matches one commit on the default
   branch. (Local branches only.)

A patch-id match is always required, so these never give false positives. To
keep this fast, patch-ids are only computed for likely matches: default-branch
commits with the same author and subject (ignoring a trailing `(#123)`) for
rebases, or touching the same files for squashes. So a merge is missed if the
default branch later changed the same lines, if the commit messages were
reworded, or if it's more than 10,000 commits back.

With `--forge`, branches still unexplained are checked against merged GitHub
PRs using `gh pr list`. A branch counts as merged if a merged PR's head is its
local tip or contains it. If a PR for a local-only, deleted-on-remote or
unpushed branch was merged but doesn't contain the local tip, it's flagged as
differing from its merged PR: there may be local work that never made it in.
(Branches up to date with their remote aren't flagged this way, as long-lived
branches like `develop` will have had PRs merged from older commits.) Only
GitHub remotes are checked.
