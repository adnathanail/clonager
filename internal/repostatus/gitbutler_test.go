package repostatus

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/adnathanail/clonager/internal/cli"
	"github.com/adnathanail/clonager/internal/config"
)

// TestGitButlerSettings sets up a real GitButler workspace, with HOME in a
// temporary directory so GitButler's project registry is a throwaway one.
func TestGitButlerSettings(t *testing.T) {
	if !cli.Installed("but") {
		if os.Getenv("CLONAGER_TEST_BUT") != "" { // CI, where it should be
			t.Fatal("but is not installed")
		}
		t.Skip("but is not installed")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, ".local/share"))
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	// With metrics on, each but command leaves a `but metrics` process
	// running, which can still be writing in dir when the test's cleanup
	// removes it.
	for _, settings := range []string{
		filepath.Join(dir, "Library/Application Support/gitbutler"), // macOS
		filepath.Join(dir, ".config/gitbutler"),                     // Linux
	} {
		if err := os.MkdirAll(settings, 0o755); err != nil {
			t.Fatal(err)
		}
		noMetrics := []byte(`{"telemetry": {"appMetricsEnabled": false}}`)
		if err := os.WriteFile(filepath.Join(settings, "settings.json"), noMetrics, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(dir, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	up, fork := filepath.Join(dir, "up"), filepath.Join(dir, "fork")
	run(dir, "git", "init", "-q", "-b", "trunk", up)
	run(up, "git", "commit", "-q", "--allow-empty", "-m", "init")
	run(dir, "git", "clone", "-q", up, fork)
	run(fork, "git", "remote", "add", "-f", "upstream", up)

	check := func(wantTarget, wantPush string) {
		t.Helper()
		target, push, err := GitButlerSettings(fork)
		if err != nil || target != wantTarget || push != wantPush {
			t.Errorf("got %q, %q, %v; want %q, %q", target, push, err, wantTarget, wantPush)
		}
	}
	mode := func(want GitButlerMode, wantBranches ...string) {
		t.Helper()
		s := Inspect(config.Repo{Path: fork}, Options{})
		var got []string
		for _, b := range s.GitButler.Branches {
			got = append(got, b.Name)
		}
		if s.Err != nil || s.GitButler.Err != nil || s.GitButler.Mode != want || !slices.Equal(got, wantBranches) {
			t.Errorf("got mode %v, branches %v (%v, %v); want %v, %v", s.GitButler.Mode, got, s.Err, s.GitButler.Err, want, wantBranches)
		}
	}
	mode(GitButlerNone)
	check("", "") // no target yet; creates GitButler's data, but uses nothing
	mode(GitButlerInactive)
	run(fork, "but", "status")
	check("", "")                             // GitButler's defaults
	run(fork, "but", "branch", "new", "feat") // HEAD is feat, not the workspace branch
	mode(GitButlerActive, "feat")
	run(fork, "but", "branch", "new", "other") // now the workspace branch
	mode(GitButlerActive, "other", "feat")
	run(fork, "git", "branch", "--set-upstream-to=upstream/trunk", "trunk")
	run(fork, "but", "config", "target", "trunk", "--push-remote", "origin")
	check("upstream/trunk", "")
	run(fork, "but", "config", "push-remote", "upstream")
	check("upstream/trunk", "upstream")
}
