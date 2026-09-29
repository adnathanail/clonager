# clonager

A Go CLI for managing the git clones on a laptop, driven by a YAML config at
`~/.config/clonager/config.yaml`. User-facing docs are in README.md; keep it in
sync when commands, flags or the config format change.

## Commands

```sh
go test ./...
go vet ./...
gofmt -l .                      # should print nothing
nix run nixpkgs#golangci-lint -- run ./...   # as CI does (.github/workflows/ci.yml)
go build -o clonager .          # CGO_ENABLED=0 for the static release binary
./clonager status -c test-config.yaml   # test-config.yaml is gitignored, local only
nix build                       # the flake package; runs the tests too
```

Versions: `cmd/version.go` reports the tag for `go install`s of a tag, the
commit hash for other installs, and "dev" for local builds (detected by Go
having recorded `vcs.revision`). Nix builds have no git metadata, so the flake
passes the version in with `-ldflags -X cmd.stampedVersion`: the contents of
`VERSION` if present, else the commit. `VERSION` only ever exists on release
commits made by `.github/workflows/release.yml`, which are reachable from
their tag but not from main. Never add `VERSION` to main.

When `go.mod`/`go.sum` change, `vendorHash` in `flake.nix` must be updated
(set it to `pkgs.lib.fakeHash`, `nix build`, copy the "got:" hash). Nix copies
dependencies into `vendor/` inside the source tree when building, so anything
that walks the tree (like `TestOnlyPackageRunsPrograms`) must skip it.

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
- `internal/cli/` — the only place external programs run (`git`, `gh`, `but`),
  with the allowlist of read-only subcommands

## Design decisions

- **clonager only ever reads.** It never changes a repo, remote, setting or
  file other than its own config (which `discover` edits); where something
  should change, it prints the command for the user to run (`prune`).
- **All external programs run through `internal/cli`** (`cli.Git`,
  `cli.GitStdin`, `cli.GH`, `cli.But`, `cli.Installed`), which checks each call
  against the `allowed` list of read-only subcommands and refuses anything
  else before running it. Need a new command? Add the subcommand there, with a
  `check` for any arguments that would make it write (`git config` without
  `--get`, `git stash` other than `list`, `gh api` with a method or fields),
  and a case in `TestCheck`. Nothing else may import `os/exec`:
  `TestOnlyPackageRunsPrograms` enforces this (tests are exempt, as they build
  throwaway repos). `internal/cli` also sets `GIT_OPTIONAL_LOCKS=0` (so `git
  status` doesn't refresh the index) and disables prompts.
- **Shell out to `git`, `but` and `gh`** rather than using libraries, so results
  match what the user sees and respect their git config.
- **`prune` only prints commands; it must never delete anything itself.** Its
  output must stay valid shell (`clonager prune | sh -n`), so everything that
  isn't a command is a `#` comment, and paths and branch names go through
  `shellQuote`/`shellPath`.
- **`status` is read-only and never fetches.** Remote state is as of the last
  fetch. Don't add commands to it that write refs or objects (e.g. `git
  merge-tree --write-tree`, `git fetch`).
- **Where the config lives:** `status`/`prune` read the installed config
  (`config.DefaultPath`). With the Home Manager module (`homeModules.default`
  in `flake.nix`), that's read-only (a copy in the Nix store, or a secret
  agenix decrypts), and `~/.config/clonager/source.json` records the
  editable source (`config.ReadSource`; the older plain-text `source` file is
  still read): either a file in the user's checkout, or the user's own
  decrypt/encrypt commands. Anything that changes the config must go through
  `editableConfig` and `Config.Save`, never write files itself, so it works
  for every kind of source. `Save` only runs `encrypt` when the config
  changed (re-encrypting changes the ciphertext anyway), writes through
  symlinks, and refuses read-only configs (`Writable`): renaming over a Home
  Manager link would otherwise silently replace it. `status` notes when the
  source differs from the installed copy.
- **clonager's own options** (as opposed to repos) go in
  `~/.config/clonager/settings.json` (`config.ReadSettings`), which the Home
  Manager module writes from its options (e.g. `discoverPaths`), not in the
  YAML config.
- **`cli.ConfigHook` is the one exception to the allowlist:** it runs the
  user's decrypt/encrypt commands with `sh -c`. Use it only for those.
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
  "Nothing to prune" once everything actionable is done — and never while a
  check failed (`uncheckable`, `ForgeErr`: report the reason instead).
  `TestPruneOutput` checks every uncommented line is a local-only git command.
- Colours are basic ANSI (0–7) so they follow the terminal theme. Lip Gloss v2's
  `Render` always emits escape codes, so print with `lipgloss.Println` (or
  through a `colorprofile` writer, as `root.go` does for Cobra and stderr),
  never `fmt.Print*`: that strips them when output isn't a TTY or `NO_COLOR`
  is set. Tests that check rendered output `ansi.Strip` it first.

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
