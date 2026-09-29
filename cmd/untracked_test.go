package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adnathanail/clonager/internal/config"
)

func TestFindUntracked(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	root := t.TempDir()
	repo := func(rel string, origin bool) string {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		args := [][]string{{"init", "-q"}}
		if origin {
			args = append(args, []string{"remote", "add", "origin", "https://example.com/me/" + filepath.Base(rel)})
		}
		for _, a := range args {
			if out, err := exec.Command("git", append([]string{"-C", path}, a...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", a, err, out)
			}
		}
		return path
	}
	tracked := repo("work/tracked", true)
	pendingRepo := repo("work/pending", true)
	untracked := repo("work/untracked", true)
	local := repo("scratch/local", false)

	cfg := &config.Config{Repos: []config.Repo{{Path: tracked}}}
	pending := &config.Config{Repos: []config.Repo{{Path: tracked}, {Path: pendingRepo}}}

	got, err := findUntracked([]string{root}, cfg, pending)
	if err != nil {
		t.Fatal(err)
	}
	want := []untrackedRepo{{path: local, noOrigin: true}, {path: untracked}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	got, err = findUntracked([]string{root}, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("without the pending config, got %+v, want 3 repos", got)
	}
}
