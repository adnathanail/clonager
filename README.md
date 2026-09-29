# clonager

Keep track of the git clones on your laptop. One config file lists every repo
and where it lives, so you can check them all at a glance and (soon) recreate
them on a fresh machine.

```
$ clonager status
~/Documents/Projects
  ● you-cool-blog    gitbutler  1 stash · 2 local-only branches · 1 branch applied
  ✓ asdf             main

~/Documents/Work
  ✓ vip-proj         main
  ● work-stuff       main       5 branches deleted on remote · 26 branches merged · 1 branch behind

4 repos · 2 ok · 2 need attention
```

## Install

### With Nix

clonager is a flake. Try it with:

```sh
nix run github:adnathanail/clonager -- status          # latest commit on main
nix run github:adnathanail/clonager/v0.2.0 -- status   # a release
```

Or install it, and its config, with the Home Manager module (standalone, or
within nix-darwin):

```nix
# flake.nix
inputs.clonager.url = "github:adnathanail/clonager";

# a Home Manager module (with inputs passed through)
{ config, inputs, ... }: {
  imports = [ inputs.clonager.homeModules.default ];

  programs.clonager = {
    enable = true;
    # Installed as ~/.config/clonager/config.yaml, via the Nix store.
    configFile = ./clonager.yaml;
    # The same file in your checkout, for `clonager discover` to edit.
    configSource = "${config.home.homeDirectory}/.config/nix-darwin/clonager.yaml";
  };
}
```

The installed config is a read-only copy, so it only changes when you rebuild.
With `configSource` set, `clonager discover` adds new repos to that file
instead, so they show up as a diff in your config repo; rebuild to apply them.
Until you do, `clonager status` reminds you there are changes not applied yet.
Without `configSource`, `discover` refuses to touch a read-only config.

If you already have a `~/.config/clonager/config.yaml`, move it into your config
repo (as `clonager.yaml` above) before rebuilding, or Home Manager will refuse
to replace it.

### With Go

Requires Go 1.23+.

```sh
go install github.com/adnathanail/clonager@latest
```

Or from a checkout, as a static binary:

```sh
CGO_ENABLED=0 go build -o clonager .
```

`git` must be on your `PATH`. [GitButler's `but` CLI](https://docs.gitbutler.com/cli-overview)
is used for repos in a GitButler workspace, and [`gh`](https://cli.github.com)
for `status --forge`; both are optional.

## Getting started

Build a config from the repos you already have:

```sh
clonager discover -n ~/Documents ~/Projects         # preview
clonager discover ~/Documents ~/Projects            # write the config
clonager status
```

## Config

The config lives at `~/.config/clonager/config.yaml` (or `$XDG_CONFIG_HOME/clonager/config.yaml`).
Override it with `--config`/`-c` or `$CLONAGER_CONFIG`.

It's a YAML tree that mirrors your folders:

```yaml
~/Documents:
  Projects:
    clonager: git@github.com:adnathanail/clonager.git
  ACME:
    mine: false               # not my repos: never suggest changing them on GitHub
    vip-proj:
      url: git@github.com:acmeltd/vip-proj.git
      gitbutler: true
      tags: [work]
  Uni:
    uni-work:
      url: git@github.com:me/uni-work.git
      remotes:
        upstream: git@github.com:my-uni/uni-work.git

~/.config/nix-darwin: git@github.com:me/my-nix-config.git
```

- **Top-level keys** are absolute paths (`~` is fine). Have as many as you like.
- **A string** is a repo, and the string is its clone URL (its `origin`).
- **A mapping with `url`** is a repo with options:
  - `remotes` — other remotes by name (`origin` comes from `url`)
  - `gitbutler: true` — the repo should be in a GitButler workspace
  - `tags` — labels for filtering, e.g. `clonager status -t work`
  - `mine: false` — the remote isn't yours to change, so clonager won't
    suggest deleting branches there or changing its settings (see `prune`)
- **Any other mapping** is a folder, and can nest as deep as you like. A
  folder can set `mine: false` for every repo inside it; a repo can override
  it with `mine: true`.

A repo's path is its chain of keys, so `vip-proj` above lives at
`~/Documents/ACME/vip-proj`. The option names (`url`, `remotes`, `gitbutler`,
`tags`, `mine`) can't be used as folder names, and repos can't contain other repos.

clonager edits this file itself (see `discover`), keeping your comments and
ordering.

## Commands

### `clonager status`

Shows every configured repo, grouped by folder: ✓ is fine, ● needs attention,
✗ has an error. It reports:

- **Setup** — not cloned, not a git repo, detached HEAD, `origin` or other
  remotes missing or different from the config
- **Working tree** — uncommitted changes (including untracked files), stashes
- **Branches** — local-only branches, unpushed commits, branches deleted on the
  remote, and branches behind their remote (shown dimmed; this doesn't count as
  needing attention)
- **Merged branches** — branches already in the default branch are listed
  separately, as safe to delete, rather than as local-only or unpushed
- **Remote branches** — `origin`'s branches that are merged. With `--forge`,
  also stale remote refs (branches already deleted on GitHub, still in your
  clone until `git remote prune`), and repos where GitHub doesn't delete
  merged branches automatically
- **GitButler** — applied branches, conflicted commits, branches needing a
  force push, and repos marked `gitbutler: true` that aren't in the workspace

| Flag | |
|---|---|
| `-v`, `--verbose` | list the branches behind each count |
| `-p`, `--problems` | only show repos that need attention |
| `-t`, `--tag <tag>` | only show repos with this tag (repeatable) |
| `-f`, `--forge` | also check GitHub, via `gh` (see below) |

Remote state is as of each repo's last fetch: `status` never fetches. With
`--forge` it takes a few seconds, mostly waiting on GitHub for repos with long
PR histories.

### `clonager discover <dir>...`

Finds git repos under each `<dir>` that aren't in the config and adds them,
with `origin` as the `url`, any other remotes, and `gitbutler: true` if the repo
is in a GitButler workspace. Repos without an `origin` are skipped, since
there'd be nothing to clone them from.

Each repo goes under the most specific top-level key containing it; if none
does, `<dir>` becomes a new top-level key. `<dir>` can itself be a repo.
Running it again only adds what's new.

| Flag | |
|---|---|
| `-n`, `--dry-run` | show what would be added without changing the config |
| `-d`, `--depth <n>` | how many folders deep to look below each `<dir>` (default 4) |

It doesn't look inside repos, or in `node_modules`, `.venv`, `Library` and
similar.

### `clonager prune`

Prints the commands to tidy up merged branches (see
[below](#how-merged-branches-are-detected)). It never deletes anything itself:
review the output, then run it.

```
$ clonager prune -f
# ~/Documents/Work/work-stuff
git -C ~/Documents/Work/work-stuff branch -D fix-login  # rebased into origin/main
git -C ~/Documents/Work/work-stuff branch -D new-reports  # merged in PR #42
# git -C ~/Documents/Work/work-stuff branch -D old-export  # PR #37 was merged, but not from this branch's tip; check before deleting
git -C ~/Documents/Work/work-stuff remote prune origin  # 3 refs deleted on GitHub: fix-login, new-reports, docs-typo
# git -C ~/Documents/Work/work-stuff push origin --delete add-logging  # rebased into origin/main
# gh repo edit acmeltd/work-stuff --delete-branch-on-merge  # delete branches when their PRs merge

# 2 merged local branches, 3 stale remote refs, 1 local commented out to review, 1 on GitHub commented out
```

- Commands use `git branch -D`, since rebase- and squash-merged branches aren't
  in the default branch's history and `git branch -d` would refuse them.
- Branches whose merged PR doesn't contain the local tip (only found with
  `--forge`) are commented out: the local copy may just be stale, or may hold
  work that never made it in.
- The checked-out branch, and branches applied in a GitButler workspace, are
  skipped with a note.

With `--forge` it also covers GitHub:

- `git remote prune origin` for refs to branches already deleted on GitHub.
  This only tidies your clone.
- `git push origin --delete` for merged branches still on GitHub, **always
  commented out**, as it changes the repo for everyone. Only for repos you can
  push to, never for protected branches, and not for branches pushed to since
  they were merged (e.g. bots reusing a branch for their next PR).
- `gh repo edit --delete-branch-on-merge`, commented out, for repos you admin
  that don't delete merged branches automatically.

The last two are only for repos that are yours: ones you can push to, and not
marked `mine: false`.

Repos that couldn't be checked are listed with the reason, e.g. `# couldn't
check: not cloned`, or `# couldn't check GitHub: …` alongside the local
results. A repo whose GitButler state can't be read is skipped entirely, since
clonager can't tell which branches are applied there. `prune` only prints
`# Nothing to prune` once there's nothing left to do and every check
succeeded.

To run everything that isn't commented out: `clonager prune -f | sh`.

| Flag | |
|---|---|
| `-f`, `--forge` | also check GitHub, and cover branches there |
| `-t`, `--tag <tag>` | only repos with this tag (repeatable) |

### `clonager completion <shell>`

Prints a shell completion script. For zsh:

```sh
clonager completion zsh > "${fpath[1]}/_clonager"
```

See `clonager completion --help` for bash, fish and PowerShell.

## How merged branches are detected

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

## Development

```sh
nix develop   # optional: a shell with go, goimports, golangci-lint, actionlint
go test ./...
go build -o clonager . && ./clonager status -c some-test-config.yaml
```

`clonager --version` shows the release (e.g. `v0.2.0`) for an install of a
release, the commit hash for an install of any other commit, and `dev` for a
build from your own checkout.

### Releasing

Run the **Release** workflow (Actions → Release → Run workflow) on `main`,
with the new version, e.g. `v0.2.0`. It makes a release commit on top of
`main` that adds a `VERSION` file, builds it with Nix (running the tests) and
checks it reports that version, then pushes the tag and creates a GitHub
release. Only the tag is pushed: `main` never has a `VERSION` file.

This is because Nix builds can't see git tags, so the version has to be in the
source; `go install` gets it from the tag itself. Tags made by hand still work
for `go install`, but Nix builds of them show the commit hash.

### Dependencies

If `go.mod` or `go.sum` change, update `vendorHash` in `flake.nix`: set it to
`pkgs.lib.fakeHash`, run `nix build`, and copy the hash from the error. CI's
Nix job fails until you do.

## Future plans

- **`clonager clone`** — set up a fresh laptop from the config. In keeping with
  clonager only ever reading, it would print the commands for repos that aren't
  cloned yet: `git clone <url> <path>`, `git remote add` for extra remotes, and
  (commented out) `but setup` for repos marked `gitbutler: true`.
- **Finding unmanaged repos** — `discover` only looks where it's pointed.
  `status` could also report clones inside configured folders that aren't in
  the config.
- **How stale the data is** — `status` never fetches, so its view of remotes is
  as of each repo's last fetch. It could show when that was (e.g. "last fetched
  3 weeks ago"), from the time of the last fetch that git records, without
  fetching anything itself.
- **Dismissing items** — a way to mark branches you're deliberately keeping
  (e.g. a `keep:` list on a repo), so `prune` can reach "Nothing to prune"
  with intentional exceptions.
- **Exit codes** — `status` (and `prune`) could exit non-zero when something
  needs attention, for use in scripts.
