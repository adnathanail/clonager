package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var shellInitCmd = &cobra.Command{
	Use:       "shell-init <zsh|bash|fish>",
	Short:     "Print the shell integration that lets browse cd",
	Long:      helpText("shell-init"),
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"zsh", "bash", "fish"},
	RunE: func(cmd *cobra.Command, args []string) error {
		script, err := shellInit(args[0])
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), script)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(shellInitCmd)
}

// shellInit is the shell integration for shell: functions named clonager
// and cg that run the program with CLONAGER_CD_FILE (cdFileEnv) set to a
// temporary file, and cd to the folder browse writes there, if any. Every
// other command runs as it would without them.
//
// cg is only defined where a cg program is on the PATH (the Nix package
// installs one), so that without one, cg still isn't found. That checks for
// a program rather than using `command -v`, which an alias (say
// cg=clonager, which reaches the clonager function anyway) also satisfies;
// in fish, an alias is a function, which defining cg would replace.
//
// `function name` is used rather than `name()`, as bash and zsh expand an
// alias in `name()` (e.g. alias cg=clonager), breaking the definition.
func shellInit(shell string) (string, error) {
	var script string
	switch shell {
	case "zsh", "bash":
		hasCg := `type -P cg >/dev/null` // bash
		if shell == "zsh" {
			hasCg = `(( $+commands[cg] ))`
		}
		script = `__clonager_cd() {
  local file code dir
  file="$(mktemp "${TMPDIR:-/tmp}/clonager.XXXXXX")" || { command "$@"; return; }
  CLONAGER_CD_FILE="$file" command "$@"
  code=$?
  dir="$(cat -- "$file")"
  rm -f -- "$file"
  if [ -n "$dir" ] && [ "$dir" != "$PWD" ]; then
    cd -- "$dir" || return
  fi
  return "$code"
}
function clonager { __clonager_cd clonager "$@"; }
if ` + hasCg + `; then
  function cg { __clonager_cd cg "$@"; }
fi
`
	case "fish":
		script = `function __clonager_cd
    set -l file (mktemp "$TMPDIR/clonager.XXXXXX" 2>/dev/null; or mktemp /tmp/clonager.XXXXXX)
    or begin
        command $argv
        return
    end
    CLONAGER_CD_FILE=$file command $argv
    set -l code $status
    set -l dir (cat -- $file)
    rm -f -- $file
    if test -n "$dir"; and test "$dir" != "$PWD"
        cd -- $dir; or return
    end
    return $code
end
function clonager --wraps clonager
    __clonager_cd clonager $argv
end
if command -q cg
    function cg --wraps cg
        __clonager_cd cg $argv
    end
end
`
	default:
		return "", fmt.Errorf("unknown shell %q: shell-init supports zsh, bash and fish", shell)
	}
	return script, nil
}
