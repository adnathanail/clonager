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
- `cmd/` — Cobra commands (`root.go`, `status.go`, `discover.go`) and Lip Gloss
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
- **`status` is read-only and never fetches.** Remote state is as of the last
  fetch. Don't add commands to it that write refs or objects (e.g. `git
  merge-tree --write-tree`, `git fetch`).
- **The config is edited as a `yaml.Node` tree** so comments and ordering
  survive. After any edit, `reparse()` re-validates through `Parse`, so an edit
  can't produce a config `Load` would reject. New keys go in alphabetically
  (case-insensitive) among siblings; new top-level keys are appended.
- **Config format:** top-level keys are absolute or `~` paths; a string value is
  a repo URL; a mapping with `url` is a repo with options (`remotes`,
  `gitbutler`, `tags`); any other mapping is a folder. Those option names are
  reserved and can't be folder names. Child keys can't contain `/`.
- **Keep git process counts per repo roughly constant**, not per branch. Repos
  can have dozens of stale branches; `checkMerged` batches with `rev-list
  --stdin` and one `log -p | patch-id` per side for this reason. `status` across
  ~25 repos should stay under a second.
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
  gives the intersection of the ranges, not the union. For the union, use the
  common ancestor from `git merge-base --octopus`.
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
