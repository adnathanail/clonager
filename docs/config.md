# Config

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
  - `gitbutler: true` — the repo should be in a GitButler workspace. Set by
    [`discover`](./commands.md#clonager-config-discover-dir) and
    [`tidy`](./commands.md#clonager-config-tidy) for repos that are in one
  - `tags` — labels for filtering, e.g. `clonager -t work`
  - `mine: false` — the remote isn't yours to change, so clonager won't
    suggest deleting branches there or changing its settings (see [`prune`](./commands.md#clonager-prune))
  - `branches` — local branches to recreate on another laptop, written by
    [`airlift`](./commands.md#clonager-config-airlift): each a name, for the same-named branch on
    `origin`, or `name: remote/branch`
  - `ssh: false` — the repo's HTTPS URLs can't be switched to SSH, written by
    [`tidy`](./commands.md#clonager-config-tidy) so it doesn't try again
- **Any other mapping** is a folder, and can nest as deep as you like. A
  folder can set `mine: false` for every repo inside it; a repo can override
  it with `mine: true`.

A repo's path is its chain of keys, so `vip-proj` above lives at
`~/Documents/ACME/vip-proj`. The option names (`url`, `remotes`, `gitbutler`,
`tags`, `mine`, `branches`, `ssh`) can't be used as folder names, and repos can't contain other repos.

clonager edits this file itself (see [`clonager config`](./commands.md#clonager-config)),
keeping your comments and ordering.
