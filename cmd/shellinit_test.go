package cmd

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

// stubPrograms makes a folder of stand-ins for each of names that write
// $TARGET to CLONAGER_CD_FILE when given "browse", print what they were
// run with, and exit 3.
func stubPrograms(t *testing.T, names ...string) string {
	t.Helper()
	bin := t.TempDir()
	stub := `#!/bin/sh
case " $* " in *" browse "*) printf %s "$TARGET" > "$CLONAGER_CD_FILE";; esac
echo "ran $*"
exit 3
`
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

// TestShellInit runs the shell integration with stand-in programs, and
// checks the shell ends up in the folder for browse only, with the exit code
// passed through.
func TestShellInit(t *testing.T) {
	bin, target := stubPrograms(t, "clonager", "cg"), t.TempDir()
	target, _ = filepath.EvalSymlinks(target) // macOS's /var is a link to /private/var

	for _, shell := range testShells {
		t.Run(shell, func(t *testing.T) {
			needShell(t, shell)
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

// TestShellInitWithoutCg checks cg isn't defined where there's no cg
// program, so it's reported as not found as usual.
func TestShellInitWithoutCg(t *testing.T) {
	bin := stubPrograms(t, "clonager")
	check := map[string]string{
		"bash": `type -t cg || echo none`,
		"zsh":  `whence -w cg || true`,
		"fish": `functions -q cg; and echo function; or echo none`,
	}
	for _, shell := range testShells {
		t.Run(shell, func(t *testing.T) {
			needShell(t, shell)
			script, err := shellInit(shell)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(shell, "-c", script+check[shell])
			cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if got := strings.TrimSpace(string(out)); strings.Contains(got, "function") {
				t.Errorf("cg is defined (%s), with no cg program", got)
			}
		})
	}
}

func TestShellInitUnknown(t *testing.T) {
	if _, err := shellInit("tcsh"); err == nil || !strings.Contains(err.Error(), "tcsh") {
		t.Errorf("got %v, want an error naming the shell", err)
	}
}
