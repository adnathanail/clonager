# Commands

`prune` and `clone` have one-letter shortcuts, shown in brackets in `--help`:
`cg p` is `clonager prune` and `cg c` is `clonager clone`.

## `clonager`

With no command, clonager shows the configured repos that need attention
(●) or have an error (✗), grouped by folder; `--all` also shows the ones that
are fine (✓). After the name is the checked-out branch or, for a repo in a
GitButler workspace, ⧓ (a stand-in for GitButler's logo) and its applied
branches (just ⧓ if none are applied). In terminals that support links
(iTerm2, Ghostty, WezTerm, Kitty, VS Code…), cmd-clicking a repo's name opens
its folder in the file manager, and cmd-clicking ⧓ opens it in the GitButler
app. To open folders in an editor instead, set `openIn`
in `~/.config/clonager/settings.json` (`programs.clonager.openIn` with the Home
Manager module) to `vscode`, `cursor` or `zed`, to `files` for the file
manager (the default), or to `none` for no links:

```json
{ "openIn": "vscode" }
```

It reports:

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
- **GitButler** — conflicted commits, branches needing a
  force push, and repos marked `gitbutler: true` that aren't in the workspace
- **Repos not in the config** — with `discoverPaths` set (see
  [`config discover`](#clonager-config-discover-dir)), clones in those folders that aren't
  in the config yet, including ones with no `origin` (which `discover` can't
  add). Not shown with `--tag`.
- **Config to tidy** — what [`config tidy`](#clonager-config-tidy) has to
  do, as far as can be told offline: HTTPS URLs to try switching to SSH
  (other than in repos marked `ssh: false`), clone remotes to switch to the
  SSH URLs in the config, repos in a GitButler workspace to mark
  `gitbutler: true` (or, with `--forge`, whose target branch or push
  remote to record), and airlifted branches, whether they're created
  here yet or not. With `--forge`, it also counts GitButler workspaces
  whose target branch or push remote differ from a `target` or `push` in
  the config, which [`clone`](#clonager-clone) prints the commands for, as
  tidy never changes them. Not shown with `--tag`.

| Flag | |
|---|---|
| `-v`, `--verbose` | list the branches behind each count |
| `-a`, `--all` | also show repos that are fine |
| `-t`, `--tag <tag>` | only show repos with this tag (repeatable) |
| `-f`, `--forge` | also check GitHub, via `gh` (see below) |

Remote state is as of each repo's last fetch: clonager never fetches. The
column before each repo shows how long ago that was (`23m`, `7h`, `5d`), from
any remote and including GitButler's background fetches; it's yellow if over a
month, or `never`. With
`--forge` it takes a few seconds, mostly waiting on GitHub for repos with long
PR histories.

## `clonager prune`

Prints the commands to tidy up merged branches (see
[how merged branches are detected](./methodology/merged-branch-detection.md)). It never deletes anything itself:
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

## `clonager clone`

Prints the commands to set up the repos in the config, e.g. on a new laptop.
Like `prune`, it never runs anything itself.

```
$ clonager clone
# ~/Documents/ACME/vip-proj
git clone git@github.com:acmeltd/vip-proj.git ~/Documents/ACME/vip-proj
# but -C ~/Documents/ACME/vip-proj setup  # switches to GitButler's workspace branch

# ~/Documents/Uni/uni-work
git -C ~/Documents/Uni/uni-work remote add -f upstream git@github.com:my-uni/uni-work.git
# but -C ~/Documents/Uni/uni-work setup  # switches to GitButler's workspace branch
# but -C ~/Documents/Uni/uni-work config target upstream/main

# 1 repo to clone, 1 remote to add, 2 commented out to review
```

- `git clone` for repos that aren't cloned yet, or whose folder is empty.
- `git remote add -f` (which also fetches it) for remotes in the config that
  a clone doesn't have, including `origin` on an existing clone without one.
- `git branch <name> <remote>/<branch>` for branches recorded by
  [`airlift`](#clonager-config-airlift) that don't exist yet. These need the remote
  branch fetched: a new clone has it, an older one may need `git fetch`.
- `git remote set-url`, commented out, for remotes whose URL differs from the
  config: the config may be the one that's out of date.
- `but setup`, commented out, for repos marked `gitbutler: true` that aren't
  in a GitButler workspace, as it switches the repo to GitButler's
  `gitbutler/workspace` branch. It's followed by `but config target` (or
  `but config push-remote`) for repos with a `target` or `push` in the
  config (see [the config format](./config.md)).
- `but config target` and `but config push-remote`, commented out, for
  GitButler workspaces whose target branch or push remote differ from a
  `target` or `push` in the config: as with remote URLs, the config may be
  the one that's out of date.

Paths that exist but aren't a git repo are listed with the reason. `clone`
only prints `# Nothing to clone` once there's nothing left to do and every
check succeeded. To run everything that isn't commented out:
`clonager clone | sh`.

## `clonager config`

The commands that change the config, the one file clonager ever changes. They
keep your comments and ordering, and with the Home Manager module's
`configSource` set they edit that source instead of the read-only installed
copy (see the [Nix flake docs](./installation/nix-flake.md)).

### `clonager config discover [<dir>...]`

Finds git repos under each `<dir>` that aren't in the config and adds them,
with `origin` as the `url`, any other remotes, and `gitbutler: true` if the repo
is in a GitButler workspace: or, where the workspace's target branch or push
remote differ from what `but setup` picks, a mapping of them (see
[the config format](./config.md)). If those can't be read (e.g. `but` isn't
installed), it adds `gitbutler: true` with a note, and
[`tidy`](#clonager-config-tidy) fills them in later. Repos without an `origin` are skipped, since
there'd be nothing to clone them from.

Each repo goes under the most specific top-level key containing it; if none
does, `<dir>` becomes a new top-level key. `<dir>` can itself be a repo.
Running it again only adds what's new.

With no `<dir>`, it looks in the `discoverPaths` listed in
`~/.config/clonager/settings.json`, skipping any that don't exist. The Home
Manager module writes that file from `programs.clonager.discoverPaths`;
without it, write it yourself:

```json
{ "discoverPaths": ["~/Documents", "~/.config/nix-darwin"] }
```

| Flag | |
|---|---|
| `-n`, `--dry-run` | show what would be added without changing the config |
| `-d`, `--depth <n>` | how many folders deep to look below each `<dir>` (default 4) |

It doesn't look inside repos, or in `node_modules`, `.venv`, `Library` and
similar. Repos are added with the URLs their remotes have;
[`tidy`](#clonager-config-tidy) switches them to SSH.

### `clonager config airlift`

Records this laptop's local branches in the config, so they can be recreated
on another one: run `airlift` on the old laptop, then `clone` on the new one.

It only airlifts a tidy laptop. First it checks that `prune --forge` has
nothing to do, and that nothing needs attention in status (with `--forge`): no
uncommitted changes, stashes, unpushed or local-only branches, and no repos in
the `discoverPaths` missing from the config. Otherwise it shows what it found
and leaves the config alone.

Each repo's `branches` list is then replaced with its local branches, other
than the default branch (which a clone checks out anyway):

```yaml
  Uni:
    uni-work:
      url: git@github.com:me/uni-work.git
      remotes:
        upstream: git@github.com:my-uni/uni-work.git
      branches:
        - fix-typos
        - lecture-notes: upstream/lecture-notes
```

On the new laptop, `clonager clone` prints a `git branch` for each one that
isn't there yet. Once they're created, [`clonager config
tidy`](#clonager-config-tidy) removes them from the config.

Don't run `config tidy` on the old laptop until its clones are deleted: the
branches still exist there, so it would remove them from the config.

### `clonager config tidy`

Tidies up the config:

- **SSH URLs** — HTTPS URLs on GitHub, GitLab, Bitbucket and Codeberg
  (`https://github.com/acmeltd/vip-proj`) are switched to SSH
  (`git@github.com:acmeltd/vip-proj.git`), for `url` and `remotes` alike.
  Each is checked first with `git ls-remote`, so only URLs you can read over
  SSH are switched. Repos where one can't be (e.g. `Repository not found`,
  `Permission denied (publickey)`) are listed with the reason and marked
  `ssh: false`, so neither `tidy` nor status brings them up again; remove it
  to have `tidy` try again. Network failures (no connection, timeouts) are
  only reported, as they may work next time. URLs on other hosts aren't touched,
  since there's no telling what their SSH URL is.
- **Clones' remotes** — last, it prints the `git remote set-url` commands to
  switch clones to the SSH URLs now in the config, for remotes still using
  the HTTPS URL they replaced (whether this run or an earlier one switched
  it). It doesn't run them. Clones with some other URL are left for
  [`clone`](#clonager-clone) to point out.
- **GitButler** — repos in a GitButler workspace get `gitbutler: true`, e.g.
  ones you've run `but setup` in since adding them. Where the workspace's
  target branch or push remote differ from what `but setup` picks, they're
  recorded too (`gitbutler: {target: upstream/main}`), including for repos
  already marked `true`. A `target` or `push` already in the config is
  left as it is. Reading them needs GitButler's CLI (`but`); without it,
  repos are still marked `true` and tidy notes that the rest was skipped.
  `gitbutler` is never removed from repos that aren't in a
  workspace: [`clone`](#clonager-clone) prints the `but setup` command for
  those instead.
- **Airlifted branches** — branches recorded by
  [`airlift`](#clonager-config-airlift) that now exist on this laptop are
  removed from the config (a repo with nothing else set goes back to just its
  URL). The rest stay listed until they're created.

It never changes a clone itself, and it's the one command that goes over the
network to check things (`git ls-remote`, with `ssh` in batch mode unless you
have your own `core.sshCommand`, so it can't stop at a prompt).

```
$ clonager config tidy
Switched to SSH:
  → ~/Documents/ACME/vip-proj  origin  git@github.com:acmeltd/vip-proj.git
  → ~/Documents/Uni/uni-work   upstream  git@github.com:my-uni/uni-work.git

Removed from the config, as they exist here:
  - ~/Documents/Uni/uni-work   fix-typos, lecture-notes

Updated ~/.config/nix-darwin/clonager.yaml.

Switch the clones too with:
  git -C ~/Documents/ACME/vip-proj remote set-url origin git@github.com:acmeltd/vip-proj.git
```

| Flag | |
|---|---|
| `-n`, `--dry-run` | show what would change without changing the config |

## Exit codes

`clonager`, `prune`, `clone`, `config airlift` and `config tidy` exit
non-zero when there's something to deal with, for use in scripts:

| Code | |
|---|---|
| 0 | all fine: nothing needs attention, `# Nothing to prune` or `# Nothing to clone` |
| 1 | clonager itself failed (e.g. no config file) |
| 2 | `clonager`: a repo needs attention (●), there are repos not in the config, or there's config to tidy; `prune`/`clone`: there's something to do, even if only commented out; `airlift`: something needs tidying first; `tidy`: airlifted branches aren't created here yet, or clones' remotes to switch |
| 3 | `clonager`: a repo has an error (✗); `prune`/`clone`/`airlift`: a check failed; `tidy`: a URL couldn't be checked over the network, or a repo couldn't be checked |

For `prune | sh` (or `clone | sh`), the pipeline's exit code is `sh`'s, not clonager's.

## `clonager completion <shell>`

Prints a shell completion script. Installing with Nix sets up bash, fish and
zsh completions (for `cg` too), so you only need this otherwise. For zsh:

```sh
clonager completion zsh > "${fpath[1]}/_clonager"
```

See `clonager completion --help` for bash, fish and PowerShell.
