package cli

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	ok := [][]string{
		{"git", "status", "--porcelain=v2", "--branch"},
		{"git", "log", "-p", "--no-merges", "main"},
		{"git", "config", "--get-regexp", `^remote\..*\.url$`},
		{"git", "stash", "list"},
		{"git", "remote"},
		{"git", "symbolic-ref", "--short", "-q", "HEAD"},
		{"gh", "pr", "list", "--repo", "o/r", "--state", "merged"},
		{"gh", "api", "--paginate", "repos/o/r/branches", "--jq", ".[].name"},
		{"gh", "api", "-X", "GET", "repos/o/r"},
		{"but", "status", "--json"},
	}
	for _, c := range ok {
		if err := Check(c[0], c[1:]); err != nil {
			t.Errorf("%s: unexpected error %v", strings.Join(c, " "), err)
		}
	}

	refused := [][]string{
		{"rm", "-rf", "/"},
		{"git"},
		{"git", "push", "origin", "--delete", "x"},
		{"git", "branch", "-D", "x"},
		{"git", "fetch", "--prune"},
		{"git", "remote", "prune", "origin"},
		{"git", "remote", "add", "x", "y"},
		{"git", "stash", "drop"},
		{"git", "stash"}, // bare stash pushes
		{"git", "config", "user.name", "x"},
		{"git", "config", "--unset", "x"},
		{"git", "symbolic-ref", "HEAD", "refs/heads/x"},
		{"git", "log", "--output=/tmp/x"},
		{"git", "diff", "--output", "/tmp/x"},
		{"git", "-c", "core.pager=evil", "log"},
		{"gh", "pr", "merge", "1"},
		{"gh", "pr", "close", "1"},
		{"gh", "repo", "edit", "--delete-branch-on-merge"},
		{"gh", "api", "-X", "DELETE", "repos/o/r/git/refs/heads/x"},
		{"gh", "api", "--method=PATCH", "repos/o/r"},
		{"gh", "api", "-XPOST", "repos/o/r"},
		{"gh", "api", "repos/o/r/issues", "-f", "title=x"},
		{"gh", "api", "graphql", "-fquery=mutation{}"},
		{"gh", "api", "repos/o/r", "--input", "body.json"},
		{"but", "commit", "-m", "x"},
		{"but", "push"},
	}
	for _, c := range refused {
		if err := Check(c[0], c[1:]); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: got %v, want ErrNotAllowed", strings.Join(c, " "), err)
		}
	}
}

func TestRefusedBeforeRunning(t *testing.T) {
	_, err := Git(t.TempDir(), "init")
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("got %v, want ErrNotAllowed", err)
	}
}

// TestOnlyPackageRunsPrograms fails if anything outside this package imports
// os/exec, so every external call goes through the allowlist. Tests are
// exempt, as they build throwaway repos.
func TestOnlyPackageRunsPrograms(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	here, _ := filepath.Abs(".")
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Hidden directories, and vendored dependencies (Nix builds vendor
		// them into the source tree), aren't clonager's code.
		if d.IsDir() && path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor") {
			return fs.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Dir(path) == here {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "os/exec" {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s imports os/exec; run programs through internal/cli instead", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
