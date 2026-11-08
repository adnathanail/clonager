package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestShellInit runs the shell integration with a stand-in clonager that
// writes a folder to CLONAGER_CD_FILE when given "browse", and checks the
// shell ends up there for browse only, with the exit code passed through.
func TestShellInit(t *testing.T) {
	bin, target := t.TempDir(), t.TempDir()
	stub := `#!/bin/sh
case " $* " in *" browse "*) printf %s "$TARGET" > "$CLONAGER_CD_FILE";; esac
echo "ran $*"
exit 3
`
	for _, name := range []string{"clonager", "cg"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target, _ = filepath.EvalSymlinks(target) // macOS's /var is a link to /private/var

	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skip(shell + " isn't installed")
			}
			script, err := shellInit(shell)
			if err != nil {
				t.Fatal(err)
			}
			test := `alias cg=clonager
` + script + `
start=$PWD
clonager prune; echo "code $?"
[ "$PWD" = "$start" ] && echo "stayed"
cg -c x.yaml browse; echo "code $?"
pwd -P
`
			if shell == "fish" {
				test = script + `
set start $PWD
clonager prune; echo "code $status"
test "$PWD" = "$start"; and echo "stayed"
cg -c x.yaml browse; echo "code $status"
pwd -P
`
			}
			cmd := exec.Command(shell, "-c", test)
			if shell == "bash" {
				cmd.Args = []string{"bash", "-O", "expand_aliases", "-c", test}
			}
			cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "TARGET="+target)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			want := "ran prune\ncode 3\nstayed\nran -c x.yaml browse\ncode 3\n" + target + "\n"
			if string(out) != want {
				t.Errorf("got:\n%s\nwant:\n%s", out, want)
			}
		})
	}
}

func TestShellInitUnknown(t *testing.T) {
	if _, err := shellInit("tcsh"); err == nil || !strings.Contains(err.Error(), "tcsh") {
		t.Errorf("got %v, want an error naming the shell", err)
	}
}
