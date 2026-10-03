package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Commands that change the config: discover, airlift and tidy",
	Long: `Commands that change the config: the one file clonager ever changes.

If the config is installed by the Home Manager module with configSource set,
they edit that source (e.g. a file in your nix-darwin repo, or one kept
encrypted there, through its decrypt and encrypt commands) instead of the
read-only installed copy. Review the diff there, then rebuild to apply it.`,
	Args: cobra.NoArgs,
}

func init() {
	rootCmd.AddCommand(configCmd)
}

// writableConfig loads the config to edit (see editableConfig), checking it
// can be saved unless dryRun. name is the command, for the error.
func writableConfig(name string, dryRun bool) (cfg *config.Config, fromSource bool, err error) {
	cfg, fromSource, err = editableConfig()
	if err != nil {
		return nil, false, err
	}
	if err := cfg.Writable(); !dryRun && errors.Is(err, config.ErrReadOnly) {
		return nil, false, fmt.Errorf("%w. If Home Manager installs it, set programs.clonager.configSource "+
			"(to the file in your checkout, or commands to decrypt and encrypt it), "+
			"and %s will edit that instead", err, name)
	}
	return cfg, fromSource, nil
}
