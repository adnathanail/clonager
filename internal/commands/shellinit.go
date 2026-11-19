package commands

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

// shellInit is the shell integration for shell: a function named cg that
// runs the program with CLONAGER_CD_FILE (cdFileEnv) set to a temporary
// file, and cds to the folder browse writes there, if any. Every other
// command runs as it would without it.
//
// `function cg` is used rather than `cg()`, as bash and zsh expand an alias
// in `cg()` (say, an old alias cg=clonager), breaking the definition.
func shellInit(shell string) (string, error) {
	switch shell {
	case "zsh", "bash":
		return `function cg {
  local file code dir
  file="$(mktemp "${TMPDIR:-/tmp}/clonager.XXXXXX")" || { command cg "$@"; return; }
  CLONAGER_CD_FILE="$file" command cg "$@"
  code=$?
  dir="$(cat -- "$file")"
  rm -f -- "$file"
  if [ -n "$dir" ] && [ "$dir" != "$PWD" ]; then
    cd -- "$dir" || return
  fi
  return "$code"
}
`, nil
	case "fish":
		return `function cg --wraps cg
    set -l file (mktemp "$TMPDIR/clonager.XXXXXX" 2>/dev/null; or mktemp /tmp/clonager.XXXXXX)
    or begin
        command cg $argv
        return
    end
    CLONAGER_CD_FILE=$file command cg $argv
    set -l code $status
    set -l dir (cat -- $file)
    rm -f -- $file
    if test -n "$dir"; and test "$dir" != "$PWD"
        cd -- $dir; or return
    end
    return $code
end
`, nil
	}
	return "", fmt.Errorf("unknown shell %q: shell-init supports zsh, bash and fish", shell)
}
