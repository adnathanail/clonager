# clonager

A Go CLI for managing the git clones on a laptop, driven by a YAML config at
`~/.config/clonager/config.yaml`. User-facing docs are in README.md; keep it in
sync when commands, flags or the config format change.

## Commands

```sh
go test ./...
go vet ./...
gofmt -l .                      # should print nothing
go build -o clonager .          # CGO_ENABLED=0 for the static release binary
./clonager status -c test-config.yaml   # test-config.yaml is gitignored, local only
```

## Layout

- `main.go` → `cmd.Execute()`
- `cmd/` — Cobra commands (`root.go`, `status.go`, `discover.go`, `prune.go`) and Lip Gloss
  styles (`style.go`). Rendering lives here.
- `internal/config/` — parsing the YAML tree into a flat `[]Repo` (`config.go`)
  and editing it in place (`edit.go`)
- `internal/repostatus/` — inspecting one clone: git state (`repostatus.go`),
  GitButler via `but status --json` (`gitbutler.go`), merged-branch detection
  (`merged.go`), GitHub PRs via `gh` (`forge.go`)
- `internal/discover/` — walking the filesystem for repos, and reading a repo's
  remotes to build its config entry

## Design decisions

- **Shell out to `git`, `but` and `gh`** rather than using libraries, so results
  match what the user sees and respect their git config. Run git with
  `GIT_OPTIONAL_LOCKS=0` and `GIT_TERMINAL_PROMPT=0` (see the `git` helper in
  `repostatus.go`).
- **`prune` only prints commands; it must never delete anything itself.** Its
  output must stay valid shell (`clonager prune | sh -n`), so everything that
  isn't a command is a `#` comment, and paths and branch names go through
  `shellQuote`/`shellPath`.
- **`status` is read-only and never fetches.** Remote state is as of the last
  fetch. Don't add commands to it that write refs or objects (e.g. `git
  merge-tree --write-tree`, `git fetch`).
- **The config is edited as a `yaml.Node` tree** so comments and ordering
  survive. After any edit, `reparse()` re-validates through `Parse`, so an edit
  can't produce a config `Load` would reject. New keys go in alphabetically
  (case-insensitive) among siblings; new top-level keys are appended.
- **Config format:** top-level keys are absolute or `~` paths; a string value is
  a repo URL; a mapping with `url` is a repo with options (`remotes`,
  `gitbutler`, `tags`, `mine`); any other mapping is a folder. Those option
  names are reserved and can't be folder names. Child keys can't contain `/`.
  Folders can set `mine` (listed in `folderKeys`) for the repos inside them;
  it's read before the folder's children, whatever the key order. In Go it's
  `Repo.NotMine`, so the zero value (e.g. from `discover`) means "mine".
- **Keep git process counts per repo roughly constant**, not per branch, and
  avoid `log -p` over long histories. Repos can have 150+ remote branches, some
  forked years ago, and large diffs (notebooks, lockfiles). `classifyMerged`
  batches with `--stdin` and only computes patch-ids for candidates found by
  cheap means (author + subject fingerprints; files touched). `status` across
  ~25 repos should stay under a second. To check it still agrees with git,
  compare against `git cherry` per branch.
- **Nothing that changes a shared remote is ever a runnable line in `prune`**
  (`git push --delete`, `gh repo edit`): always commented out. Only suggest
  deleting branches on GitHub with `--forge`, which knows they exist, aren't
  protected, and that the user can push. Never suggest remote changes for
  repos that aren't the user's (`ownsRemote`: `mine: false`, or no push
  access), and don't print "skipped" notes for them either: `prune` should say
  "Nothing to prune" once everything actionable is done.
- Colours are basic ANSI (0–7) so they follow the terminal theme. Lip Gloss
  strips them automatically when output isn't a TTY.

## Gotchas

- **GitButler doesn't set upstream tracking** on branches it pushes. A branch
  with no upstream is compared against a same-named remote branch (origin
  first) before being called local-only. GitButler's own `gitbutler/*`
  branches are ignored. A repo is "in GitButler" when HEAD is
  `gitbutler/workspace` (or the older `gitbutler/integration`); a
  `.git/gitbutler` directory alone means it was set up and later left.
- **`git log A --not B C`** excludes commits reachable from *any* of B, C, which
  gives the intersection of the ranges, not the union.
- **`git merge-base --octopus` fails if any branch has unrelated history**
  (`gh-pages` and other orphan branches). Don't depend on it for a whole repo's
  branches: one orphan would silently disable checks for all of them.
- **Remote-tracking refs go stale.** A branch deleted on GitHub stays in
  `refs/remotes/origin/` until `git fetch --prune`/`git remote prune`, and
  most clones don't prune automatically. Only `--forge` can tell stale from
  live.
- **GitHub squash merges** give the commit a new date and append ` (#123)` to
  the subject; bots (renovate, pre-commit-ci) reuse a branch name for new PRs
  after the old one merged, so a merged PR with that head name doesn't mean
  the branch's current commits were merged.
- **Don't use `os.UserConfigDir`** for the config path: on macOS it's
  `~/Library/Application Support`. `config.DefaultPath` handles this.
- `g.run` trims trailing newlines, so add one back before piping output into
  `git patch-id`.
- yaml.v3 re-encodes inline comments with a single space before `#`.

## Tests

- Config parsing and editing have table tests on YAML strings.
- Git behaviour is tested against real throwaway repos built in `t.TempDir()`
  (see `merged_test.go`), with `GIT_CONFIG_GLOBAL=/dev/null` so the user's git
  config can't affect results. Prefer this over mocking git.

## Docs and examples

Don't use real repo names, organisations or remote URLs from this machine in
README, CLAUDE.md, help text or test fixtures. Use made-up ones (`vip-proj`,
`acmeltd`, `me/…`).
