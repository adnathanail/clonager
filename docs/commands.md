# Commands

`browse`, `prune` and `clone` have one-letter shortcuts, shown in brackets in
`--help`: `cg b` is `clonager browse`, `cg p` is `clonager prune` and `cg c`
is `clonager clone`.

## `clonager`

With no command, clonager shows the configured repos that need attention
(●) or have an error (✗), grouped by folder; `--all` also shows the ones that
are fine (✓). After the name is the checked-out branch or, for a repo in a
GitButler workspace, ⧓ (a stand-in for GitButler's logo) and its applied
branches (or the checked-out branch, if none are applied). A repo is in a
GitButler workspace once a branch has been made in it with GitButler: with
one branch applied, that branch is checked out, and with more, GitButler's
`gitbutler/workspace` branch. In terminals that support links
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
- **Remote branches** — `origin`'s branches that are merged. From GitHub,
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
  `gitbutler: true` (or, unless `--offline`, whose target branch or push
  remote to record), and airlifted branches, whether they're created
  here yet or not. Unless `--offline`, it also counts GitButler workspaces
  whose target branch or push remote differ from a `target` or `push` in
  the config, which [`clone`](#clonager-clone) prints the commands for, as
  tidy never changes them. Not shown with `--tag`.

| Flag | |
|---|---|
| `-v`, `--verbose` | list the branches behind each count |
| `-a`, `--all` | also show repos that are fine |
| `-t`, `--tag <tag>` | only show repos with this tag (repeatable) |
| `-o`, `--offline` | don't check GitHub |

Remote state is as of each repo's last fetch: clonager never fetches. The
column before each repo shows how long ago that was (`23m`, `7h`, `5d`), from
any remote and including GitButler's background fetches; it's yellow if over a
month, or `never`.

It also checks GitHub, via `gh`: merged PRs (which catch merges git alone
can't see), which of `origin`'s branches still exist, and whether the repo
deletes merged branches automatically. That takes a few seconds, mostly
waiting on GitHub for repos with long PR histories, so in a terminal the local
results are shown straight away, with a spinner below them, and redrawn once
GitHub answers. If they no longer all fit on screen (or the window was
resized), the new results are printed again below instead, after an
`=== Updated from GitHub ===` line. Ctrl-C stops
waiting and leaves the local results, with a warning that GitHub wasn't
checked (so it exits 3, as below). When output isn't a terminal, it waits
and prints once.

If GitHub can't be asked (no connection, `gh` not installed or not logged
in), a warning at the end says so, and clonager exits 3. `--offline` skips
GitHub; to skip it by default, set `alwaysOffline` in
`~/.config/clonager/settings.json` (`programs.clonager.alwaysOffline` with the
Home Manager module), and use `--offline=false` to check it anyway:

```json
{ "alwaysOffline": true }
```

## `clonager browse`

Opens a full-screen view of the configured repos, grouped by folder as in
`clonager`, with the selected repo's status beside them (or below them in a
terminal under 80 columns wide). Repos are inspected in the background, with
a spinner beside each until it's done, and GitHub is then checked for each,
as `clonager` does, unless `--offline` or `alwaysOffline` is set.

```
clonager · 4 repos
~/Documents/ACME         │ vip-proj
▸ ● vip-proj  ⧓ fix-auth │ ~/Documents/ACME/vip-proj
  ✓ site      main       │ git@github.com:acmeltd/vip-proj.git
                         │
~/Documents/Uni          │ ⧓ fix-auth  fetched 2h ago
  ✓ uni-work  main       │ • 3 uncommitted changes
  ✗ thesis               │ • 1 stash

enter cd · e VS Code · b GitButler · / filter · s sort · q quit
```

| Key | |
|---|---|
| ↑/↓ (or `k`/`j`), pgup/pgdown, home/end | move |
| `/` | filter by name, folder or tag: enter keeps the filter, esc clears it |
| `s` | switch between grouping by folder and listing the most recently changed first |
| `f` | when sorted by most recent change, hide or show the folder before each name |
| enter (or `c`) | cd to the repo, and exit |
| `e` | open the repo in the `openIn` app (the file manager by default; see [`clonager`](#clonager)) |
| `b` | open the repo in the GitButler app (for repos in a GitButler workspace) |
| `q`, esc | exit |

Clicking a repo selects it, and double-clicking cds to it.

| Flag | |
|---|---|
| `-t`, `--tag <tag>` | only show repos with this tag (repeatable) |
| `-s`, `--sort <order>` | `folder`, or `recent` for the most recently changed first (default from `browseSort` in settings.json, else `folder`) |
| `-o`, `--offline` | don't check GitHub |

To start sorted by most recent change every time, set `browseSort` in
`~/.config/clonager/settings.json` (`programs.clonager.browseSort` with the
Home Manager module):

```json
{ "browseSort": "recent" }
```

To start with the folders before names hidden, set `browseHideFolders`
(`programs.clonager.browseHideFolders`):

```json
{ "browseSort": "recent", "browseHideFolders": true }
```

Sorted by most recent change, the list has no folder headings: instead
each repo's name follows the end of its folder (`ACME/vip-proj`, or more
of it where two folders end the same way), and how long ago it changed
(`23m`, `7h`, `5d`) comes first. That's its newest commit on any local branch, by commit
date (so rebasing or amending counts), or the last edit to a file with
uncommitted changes, whichever is later. GitButler's own `gitbutler/*`
branches don't count. An untracked folder only counts as changed when files
are added to it or removed, not when a file inside it is edited.

A program can't change the directory of the shell that started it, so
cding needs the shell integration below, which the Home Manager module sets
up (`programs.clonager.enableShellIntegration`, on by default). Without it,
enter prints the repo's folder and exits, so `cd "$(clonager browse)"`
works too.

### `clonager shell-init <zsh|bash|fish>`

Prints the shell integration. Add it to your shell's startup file:

```sh
eval "$(clonager shell-init zsh)"    # ~/.zshrc
eval "$(clonager shell-init bash)"   # ~/.bashrc
clonager shell-init fish | source    # ~/.config/fish/config.fish
```

It defines shell functions named `clonager` and, if a `cg` program is
installed (as the Nix package does), `cg`, that run clonager as usual, with `CLONAGER_CD_FILE` set to a temporary file. When you pick a repo
in `browse`, its folder is written there, and the function cds to it once
browse exits. Every other command works as it does without the functions.

## `clonager prune`

Prints the commands to tidy up merged branches (see
[how merged branches are detected](./methodology/merged-branch-detection.md)). It never deletes anything itself:
review the output, then run it.

```
$ clonager prune
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
- Branches whose merged PR on GitHub doesn't contain the local tip are
  commented out: the local copy may just be stale, or may hold
  work that never made it in.
- The checked-out branch, and branches applied in a GitButler workspace, are
  skipped with a note.

It also checks GitHub, via `gh`, and covers branches there:

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
results. If GitHub can't be asked at all (no connection, `gh` not installed or
not logged in), that's noted once at the end instead. A repo whose GitButler state can't be read is skipped entirely, since
clonager can't tell which branches are applied there. `prune` only prints
`# Nothing to prune` once there's nothing left to do and every check
succeeded.

To run everything that isn't commented out: `clonager prune | sh`. While it
checks, a spinner shows on stderr (when that's a terminal), so it never gets
into the script, and it's cleared before the script is printed.

| Flag | |
|---|---|
| `-o`, `--offline` | don't check GitHub (see [`alwaysOffline`](#clonager)) |
| `-t`, `--tag <tag>` | only repos with this tag (repeatable) |

## `clonager clone`

Prints the commands to set up the repos in the config, e.g. on a new laptop.
Like `prune`, it never runs anything itself.

```
$ clonager clone
# ~/Documents/ACME/vip-proj
git clone git@github.com:acmeltd/vip-proj.git ~/Documents/ACME/vip-proj

# ~/Documents/Uni/uni-work
git -C ~/Documents/Uni/uni-work remote add -f upstream git@github.com:my-uni/uni-work.git
# git -C ~/Documents/Uni/uni-work branch --set-upstream-to=upstream/main main
# but -C ~/Documents/Uni/uni-work config target main --push-remote origin

# 1 repo to clone, 1 remote to add, 1 commented out to review
```

- `git clone` for repos that aren't cloned yet, or whose folder is empty.
- `git remote add -f` (which also fetches it) for remotes in the config that
  a clone doesn't have, including `origin` on an existing clone without one.
- `git branch <name> <remote>/<branch>` for branches recorded by
  [`airlift`](#clonager-config-airlift) that don't exist yet. These need the remote
  branch fetched: a new clone has it, an older one may need `git fetch`.
- `git remote set-url`, commented out, for remotes whose URL differs from the
  config: the config may be the one that's out of date.
- `but config target`, commented out, for repos marked `gitbutler: true`
  with a `target` or `push` in the config (see
  [the config format](./config.md)) that aren't in a GitButler workspace yet.
  GitButler needs no other setup. Its target is a local branch, so a target
  on another remote (`upstream/main`) is preceded by a `git branch
  --set-upstream-to` making the local branch of that name track it. As
  GitButler pushes to the target's remote unless told otherwise, these also
  give `--push-remote origin` where the config has no `push`.
- The same, and `but config push-remote`, commented out, for GitButler
  workspaces whose target branch or push remote differ from a `target` or
  `push` in the config: as with remote URLs, the config may be the one
  that's out of date.

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
remote differ from GitButler's defaults, a mapping of them (see
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

It only airlifts a tidy laptop. First it checks that `prune` has
nothing to do, and that nothing needs attention in status (checking GitHub): no
uncommitted changes, stashes, unpushed or local-only branches, and no repos in
the `discoverPaths` missing from the config. Otherwise it shows what it found
and leaves the config alone.

It can't skip checking GitHub. If [`alwaysOffline`](#clonager) is set, it asks
before checking, and leaves the config alone if you say no. Where there's no
terminal to ask in, pass `--offline=false` to check without asking.

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
  ones you've made a GitButler branch in since adding them. Where the
  workspace's target branch or push remote differ from GitButler's defaults
  (origin's default branch, and `origin`), they're
  recorded too (`gitbutler: {target: upstream/main}`), including for repos
  already marked `true`. A `target` or `push` already in the config is
  left as it is. Reading them needs GitButler's CLI (`but`); without it,
  repos are still marked `true` and tidy notes that the rest was skipped.
  They can only be recorded once the config has the remotes they're on:
  until then, repos are still marked `true`, and tidy lists the remotes to
  add under `remotes`.
  `gitbutler` is never removed from repos that aren't in a
  workspace (e.g. a fresh clone).
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
| 3 | `clonager`: a repo has an error (✗), or GitHub couldn't be checked; `prune`/`clone`/`airlift`: a check failed; `tidy`: a URL couldn't be checked over the network, or a repo couldn't be checked |

For `prune | sh` (or `clone | sh`), the pipeline's exit code is `sh`'s, not clonager's.

## `clonager completion <shell>`

Prints a shell completion script. Installing with Nix sets up bash, fish and
zsh completions (for `cg` too), so you only need this otherwise. For zsh:

```sh
clonager completion zsh > "${fpath[1]}/_clonager"
```

See `clonager completion --help` for bash, fish and PowerShell.
