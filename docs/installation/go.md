## Go

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
