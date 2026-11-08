package repostatus

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adnathanail/clonager/internal/config"
)

func TestLastChange(t *testing.T) {
	work := t.TempDir()
	run := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		cmd.Env = append(cmd.Env, env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(file string, mtime time.Time) {
		t.Helper()
		path := filepath.Join(work, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(file+mtime.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	commitAt := func(branch string, when time.Time) {
		t.Helper()
		date := []string{"GIT_COMMITTER_DATE=" + when.Format(time.RFC3339), "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z"}
		run(nil, "checkout", "-q", "-B", branch)
		run(date, "commit", "-q", "--allow-empty", "-m", branch)
	}
	lastChange := func() time.Time {
		t.Helper()
		s := Inspect(config.Repo{Path: work}, Options{})
		if s.Err != nil {
			t.Fatal(s.Err)
		}
		return s.LastChange
	}
	day := func(d int) time.Time { return time.Date(2024, 1, d, 12, 0, 0, 0, time.UTC) }

	run(nil, "init", "-q", "-b", "main")
	commitAt("main", day(1))
	commitAt("feature", day(5)) // by committer date: the author date is older
	commitAt("gitbutler/workspace", day(9))
	run(nil, "checkout", "-q", "main")
	if got := lastChange(); !got.Equal(day(5)) {
		t.Errorf("commits only: got %v, want the newest branch's commit, %v (gitbutler/* not counted)", got, day(5))
	}

	write("my file.txt", day(7)) // untracked, with a space, so quoted by git status
	if got := lastChange(); !got.Equal(day(7)) {
		t.Errorf("untracked file: got %v, want its mtime %v", got, day(7))
	}

	write("tracked", day(2))
	run(nil, "add", "tracked")
	run([]string{"GIT_COMMITTER_DATE=" + day(3).Format(time.RFC3339)}, "commit", "-q", "-m", "tracked")
	write("tracked", day(8)) // modified since
	if got := lastChange(); !got.Equal(day(8)) {
		t.Errorf("modified file: got %v, want its mtime %v", got, day(8))
	}

	run(nil, "mv", "tracked", "renamed")
	if err := os.Chtimes(filepath.Join(work, "renamed"), day(10), day(10)); err != nil {
		t.Fatal(err)
	}
	if got := lastChange(); !got.Equal(day(10)) {
		t.Errorf("renamed file: got %v, want its mtime %v", got, day(10))
	}
}

func TestChangedPath(t *testing.T) {
	cases := map[string]string{
		"# branch.head main": "",
		"1 .M N... 100644 100644 100644 abc abc src/main.go":                   "src/main.go",
		"1 .M N... 100644 100644 100644 abc abc a file with spaces":            "a file with spaces",
		"2 R. N... 100644 100644 100644 abc abc R100 new name.go\told name.go": "new name.go",
		"u UU N... 100644 100644 100644 100644 abc abc abc conflicted.go":      "conflicted.go",
		"? untracked/":                   "untracked/",
		`? "caf\303\251.txt"`:            "café.txt",
		"1 .M N... 100644 100644 100644": "",
	}
	for line, want := range cases {
		if got := changedPath(line); got != want {
			t.Errorf("%q: got %q, want %q", line, got, want)
		}
	}
}
