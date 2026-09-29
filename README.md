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

Requires Go 1.26+.

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
- **Any other mapping** is a folder, and can nest as deep as you like.

A repo's path is its chain of keys, so `vip-proj` above lives at
`~/Documents/ACME/vip-proj`. The option names (`url`, `remotes`, `gitbutler`,
`tags`) can't be used as folder names, and repos can't contain other repos.

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
- **GitButler** — applied branches, conflicted commits, branches needing a
  force push, and repos marked `gitbutler: true` that aren't in the workspace

| Flag | |
|---|---|
| `-v`, `--verbose` | list the branches behind each count |
| `-p`, `--problems` | only show repos that need attention |
| `-t`, `--tag <tag>` | only show repos with this tag (repeatable) |
| `-f`, `--forge` | also check GitHub for merged PRs (see below) |

Remote state is as of each repo's last fetch: `status` never fetches.

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

### `clonager completion <shell>`

Prints a shell completion script. For zsh:

```sh
clonager completion zsh > "${fpath[1]}/_clonager"
```

See `clonager completion --help` for bash, fish and PowerShell.

## How merged branches are detected

Rebase and squash merges create new commits, so a merged branch isn't
necessarily in the default branch's history. For each branch that would
otherwise be flagged, clonager compares it with the default branch
(`origin/HEAD`, else `origin/main` or `origin/master`):

1. **Ancestor** — the branch tip is in the default branch's history (merge
   commits, fast-forwards).
2. **Rebased** — every commit on the branch has a patch-identical commit on the
   default branch, using `git patch-id` (as `git cherry` does).
3. **Squashed** — the branch's whole diff matches one commit on the default
   branch.

These never give false positives, but a squash merge is missed if the default
branch later changed the same lines.

With `--forge`, branches still unexplained are checked against merged GitHub
PRs using `gh pr list`. A branch counts as merged if a merged PR's head is its
local tip or contains it. If a PR for the branch was merged but doesn't contain
the local tip, it's flagged as differing from its merged PR: there may be local
work that never made it in. Only GitHub remotes are checked.

## Development

```sh
go test ./...
go build -o clonager . && ./clonager status -c some-test-config.yaml
```
