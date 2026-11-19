package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testShells are the shells shell-init supports. Tests for those that
// aren't installed are skipped, except where CLONAGER_TEST_SHELLS is set (in
// CI, which installs them all), where they fail.
var testShells = []string{"bash", "zsh", "fish"}

func needShell(t *testing.T, shell string) {
	t.Helper()
	if _, err := exec.LookPath(shell); err != nil {
		if os.Getenv("CLONAGER_TEST_SHELLS") != "" {
			t.Fatal(shell + " isn't installed, and CLONAGER_TEST_SHELLS is set")
		}
		t.Skip(shell + " isn't installed")
	}
}

// stubPrograms makes a folder to be a shell's whole PATH: a stand-in for cg
// that writes $TARGET to CLONAGER_CD_FILE when given "browse", prints what
// it was run with, and exits 3, and links to the programs the shell
// integration runs. Linking them, rather than adding the folders they're in
// to the PATH, works wherever they are (in the Nix build's sandbox, not in
// /usr/bin), and keeps out anything else in those folders, such as a real
// cg.
func stubPrograms(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	for _, tool := range []string{"mktemp", "cat", "rm"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	stub := `#!/bin/sh
case " $* " in *" browse "*) printf %s "$TARGET" > "$CLONAGER_CD_FILE";; esac
echo "ran $*"
exit 3
`
	if err := os.WriteFile(filepath.Join(bin, "cg"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestShellInit runs the shell integration with stand-in programs, and
// checks the shell ends up in the folder for browse only, with the exit code
// passed through.
func TestShellInit(t *testing.T) {
	bin, target := stubPrograms(t), t.TempDir()
	target, _ = filepath.EvalSymlinks(target) // macOS's /var is a link to /private/var

	for _, shell := range testShells {
		t.Run(shell, func(t *testing.T) {
			needShell(t, shell)
			script, err := shellInit(shell)
			if err != nil {
				t.Fatal(err)
			}
			test := script + `
start=$PWD
cg prune; echo "code $?"
[ "$PWD" = "$start" ] && echo "stayed"
cg -c x.yaml browse; echo "code $?"
pwd -P
`
			if shell == "fish" {
				test = script + `
set start $PWD
cg prune; echo "code $status"
test "$PWD" = "$start"; and echo "stayed"
cg -c x.yaml browse; echo "code $status"
pwd -P
`
			}
			cmd := exec.Command(shell, "-c", test)
			cmd.Env = append(os.Environ(), "PATH="+bin, "TARGET="+target)
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
