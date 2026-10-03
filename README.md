# clonager

Keep track of the git clones on your laptop. One config file lists every repo
and where it lives, so you can check them all at a glance and (soon) recreate
them on a fresh machine.

```
$ clonager
~/Documents/Projects
  23m ● you-cool-blog  ⧓ new-post  1 stash · 2 local-only branches

~/Documents/Work
   5d ● work-stuff     main        5 branches deleted on remote · 26 branches merged · 1 branch behind

4 repos · 2 ok (-a to show) · 2 need attention
```

## Install

### With Nix

clonager is a flake. Try it with:

```sh
nix run github:adnathanail/clonager          # latest commit on main
nix run github:adnathanail/clonager/v0.2.0   # a release
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
    # The same file in your checkout, for `clonager config` to edit.
    configSource = "${config.home.homeDirectory}/.config/nix-darwin/clonager.yaml";
    # Where `clonager config discover` looks when given no dirs.
    discoverPaths = [ "~/Documents" "~/.config/nix-darwin" ];
    # What cmd-clicking a repo's name opens it in (default the file manager).
    openIn = "vscode";
  };
}
```

The installed config is a read-only copy, so it only changes when you rebuild.
With `configSource` set, `clonager config discover` adds new repos to that
file instead, so they show up as a diff in your config repo; rebuild to apply them.
Until you do, `clonager` reminds you there are changes not applied yet.
Without `configSource`, the `config` commands refuse to touch a read-only
config.

If you already have a `~/.config/clonager/config.yaml`, move it into your config
repo (as `clonager.yaml` above) before rebuilding, or Home Manager will refuse
to replace it.

#### Keeping the config encrypted

The config lists every repo's URL, which you may not want in a public config
repo. To keep it encrypted (e.g. with [agenix](https://github.com/ryantm/agenix)),
give `configSource` commands instead of a path, and point `configFile` at the
decrypted copy, as a string so it's linked to rather than copied into the
store:

```nix
programs.clonager = {
  enable = true;
  configFile = "/run/agenix/clonager-config";  # decrypted at activation
  configSource = {
    # Prints the config.
    decrypt = "cd ~/.config/nix-darwin/secrets && agenix -d clonager.age -i ~/.config/age/keys.txt";
    # Reads the new config on stdin, and stores it.
    encrypt = "cd ~/.config/nix-darwin/secrets && agenix -e clonager.age -i ~/.config/age/keys.txt";
  };
};
```

Commands that change the config (`clonager config ...`) then decrypt it, make their
changes in memory, and encrypt it again, so the plaintext is never written to
disk. `encrypt` only runs if something changed, since re-encrypting would
change the encrypted file even when its contents are the same. Neither command
may prompt; `clonager` runs `decrypt` too, to tell you about changes not
applied yet.

### With Go

Requires Go 1.23+.

```sh
go install github.com/adnathanail/clonager@latest
```

Or from a checkout, as a static binary:

```sh
CGO_ENABLED=0 go build -o clonager .
```

The Nix package also installs clonager as `cg`; with Go, add
`alias cg=clonager` to your shell's config to get the same.

`git` must be on your `PATH`. [GitButler's `but` CLI](https://docs.gitbutler.com/cli-overview)
is used for repos in a GitButler workspace, and [`gh`](https://cli.github.com)
for `clonager --forge`; both are optional.

## Getting started

Build a config from the repos you already have:

```sh
clonager config discover -n ~/Documents ~/Projects  # preview
clonager config discover ~/Documents ~/Projects     # write the config
clonager config tidy                                # switch URLs to SSH
clonager
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
  - `tags` — labels for filtering, e.g. `clonager -t work`
  - `mine: false` — the remote isn't yours to change, so clonager won't
    suggest deleting branches there or changing its settings (see `prune`)
  - `branches` — local branches to recreate on another laptop, written by
    [`airlift`](#clonager-config-airlift): each a name, for the same-named branch on
    `origin`, or `name: remote/branch`
- **Any other mapping** is a folder, and can nest as deep as you like. A
  folder can set `mine: false` for every repo inside it; a repo can override
  it with `mine: true`.

A repo's path is its chain of keys, so `vip-proj` above lives at
`~/Documents/ACME/vip-proj`. The option names (`url`, `remotes`, `gitbutler`,
`tags`, `mine`, `branches`) can't be used as folder names, and repos can't contain other repos.

clonager edits this file itself (see [`clonager config`](#clonager-config)),
keeping your comments and ordering.

## Commands

Each command has a one-letter shortcut, shown in brackets in `--help`:
`cg p` is `clonager prune`, `cg c` is `clone` and `cg config d` is `config
discover`.

### `clonager`

With no command, clonager shows the configured repos that need attention
(●) or have an error (✗), grouped by folder; `--all` also shows the ones that
are fine (✓). After the name is the checked-out branch or, for a repo in a
GitButler workspace, ⧓ (a stand-in for GitButler's logo) and its applied
branches (just ⧓ if none are applied). In terminals that support links
(iTerm2, Ghostty, WezTerm, Kitty, VS Code…), cmd-clicking a repo's name opens
its folder in the file manager, and cmd-clicking ⧓ opens it in the GitButler
app. To open folders in an editor instead, set `openIn`
in `~/.config/clonager/settings.json` (`programs.clonager.openIn` with the Home
Manager module) to `vscode`, `cursor` or `zed`, or to `none` for no links:

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

### `clonager clone`

Prints the commands to set up the repos in the config, e.g. on a new laptop.
Like `prune`, it never runs anything itself.

```
$ clonager clone
# ~/Documents/ACME/vip-proj
git clone git@github.com:acmeltd/vip-proj.git ~/Documents/ACME/vip-proj
# but -C ~/Documents/ACME/vip-proj setup  # switches to GitButler's workspace branch

# ~/Documents/Uni/uni-work
git -C ~/Documents/Uni/uni-work remote add -f upstream git@github.com:my-uni/uni-work.git

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
- `but setup`, commented out, for repos marked `gitbutler: true` that aren't
  in a GitButler workspace, as it switches the repo to GitButler's
  `gitbutler/workspace` branch.

Paths that exist but aren't a git repo are listed with the reason. `clone`
only prints `# Nothing to clone` once there's nothing left to do and every
check succeeded. To run everything that isn't commented out:
`clonager clone | sh`.

### `clonager config`

The commands that change the config, the one file clonager ever changes. They
keep your comments and ordering, and with the Home Manager module's
`configSource` set they edit that source instead of the read-only installed
copy (see [With Nix](#with-nix)).

#### `clonager config discover [<dir>...]`

Finds git repos under each `<dir>` that aren't in the config and adds them,
with `origin` as the `url`, any other remotes, and `gitbutler: true` if the repo
is in a GitButler workspace. Repos without an `origin` are skipped, since
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

#### `clonager config airlift`

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

#### `clonager config tidy`

Tidies up the config:

- **SSH URLs** — HTTPS URLs on GitHub, GitLab, Bitbucket and Codeberg
  (`https://github.com/acmeltd/vip-proj`) are switched to SSH
  (`git@github.com:acmeltd/vip-proj.git`), for `url` and `remotes` alike.
  Each is checked first with `git ls-remote`, so only URLs you can read over
  SSH are switched; the rest are listed with the reason (e.g. `Permission
  denied (publickey)`) and left alone. URLs on other hosts aren't touched,
  since there's no telling what their SSH URL is. Existing clones keep their
  old URLs: [`clone`](#clonager-clone) prints the `git remote set-url`
  commands for them, commented out to check first.
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
```

| Flag | |
|---|---|
| `-n`, `--dry-run` | show what would change without changing the config |

### Exit codes

`clonager`, `prune`, `clone`, `config airlift` and `config tidy` exit
non-zero when there's something to deal with, for use in scripts:

| Code | |
|---|---|
| 0 | all fine: nothing needs attention, `# Nothing to prune` or `# Nothing to clone` |
| 1 | clonager itself failed (e.g. no config file) |
| 2 | `clonager`: a repo needs attention (●), or there are repos not in the config; `prune`/`clone`: there's something to do, even if only commented out; `airlift`: something needs tidying first; `tidy`: airlifted branches aren't created here yet |
| 3 | `clonager`: a repo has an error (✗); `prune`/`clone`/`airlift`: a check failed; `tidy`: a URL can't be switched to SSH, or a repo couldn't be checked |

For `prune | sh` (or `clone | sh`), the pipeline's exit code is `sh`'s, not clonager's.

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
go build -o clonager . && ./clonager -c some-test-config.yaml
```

`clonager --version` shows the release (e.g. `v0.2.0`) for an install of a
release, the commit hash for an install of any other commit, and `dev` for a
build from your own checkout.

### Releasing

Run the **Release** workflow (Actions → Release → Run workflow) on `main`,
choosing `patch`, `minor` or `major`. It bumps the latest release tag by that
(from `v0.1.0`: `v0.1.1`, `v0.2.0` or `v1.0.0`), makes a release commit on top
of `main` that adds a `VERSION` file, builds it with Nix (running the tests) and
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

- **Dismissing items** — a way to mark branches you're deliberately keeping
  (e.g. a `keep:` list on a repo), so `prune` can reach "Nothing to prune"
  with intentional exceptions.
