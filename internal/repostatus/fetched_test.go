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

func TestReadLastFetch(t *testing.T) {
	dir := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	lastFetch := func(path string) time.Time {
		t.Helper()
		s := Inspect(config.Repo{Path: path}, Options{})
		if s.Err != nil {
			t.Fatal(s.Err)
		}
		return s.LastFetch
	}

	remote, work := filepath.Join(dir, "remote.git"), filepath.Join(dir, "work")
	run(dir, "init", "-q", "-b", "main", "seed")
	// An old commit, so the clone's time can't be confused with the commit's.
	t.Setenv("GIT_COMMITTER_DATE", "2020-01-01T00:00:00Z")
	run(filepath.Join(dir, "seed"), "commit", "-q", "--allow-empty", "-m", "first")
	if err := os.Unsetenv("GIT_COMMITTER_DATE"); err != nil { // reflog entries take their time from it too
		t.Fatal(err)
	}
	run(dir, "clone", "-q", "--bare", "seed", remote)

	// Cloned, never fetched: the clone counts.
	before := time.Now().Add(-time.Minute)
	run(dir, "clone", "-q", remote, work)
	if got := lastFetch(work); got.Before(before) {
		t.Errorf("after clone: LastFetch = %v, want about now", got)
	}

	// Fetched: FETCH_HEAD's mtime.
	run(work, "fetch", "-q")
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(work, ".git", "FETCH_HEAD"), old, old); err != nil {
		t.Fatal(err)
	}
	if got := lastFetch(work); !got.Equal(old) {
		t.Errorf("after fetch: LastFetch = %v, want %v", got, old)
	}

	// A remote added but never fetched from.
	fresh := filepath.Join(dir, "fresh")
	run(dir, "init", "-q", fresh)
	run(fresh, "remote", "add", "origin", remote)
	if got := lastFetch(fresh); !got.IsZero() {
		t.Errorf("never fetched: LastFetch = %v, want zero", got)
	}
}
