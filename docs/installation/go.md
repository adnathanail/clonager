# Go

Requires Go 1.25+.

```sh
go install github.com/adnathanail/clonager/cmd/cg@latest
```

Or from a checkout, as a static binary:

```sh
CGO_ENABLED=0 go build -o cg ./cmd/cg
```

`git` must be on your `PATH`. [GitButler's `but` CLI](https://docs.gitbutler.com/cli-overview)
is used for repos in a GitButler workspace, and [`gh`](https://cli.github.com)
for checking GitHub; both are optional (`cg --offline` doesn't use `gh`).
