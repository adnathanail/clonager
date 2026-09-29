package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
)

var configPath string

// version is set at build time (see flake.nix); "dev" for plain go builds.
var version = "dev"

var rootCmd = &cobra.Command{
	Use:           "clonager",
	Short:         "Manage your git clones",
	Version:       version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, styleError.Render("error:"), err)
	}
	return err
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "",
		"config file (default $CLONAGER_CONFIG or ~/.config/clonager/config.yaml)")
}

// configFile is the config path from --config, or the default.
func configFile() string {
	if configPath != "" {
		if p, err := config.ExpandHome(configPath); err == nil {
			return p
		}
		return configPath
	}
	p, err := config.DefaultPath()
	if err != nil {
		return configPath
	}
	return p
}

// loadConfig loads the config file. A missing file gives an error wrapping
// fs.ErrNotExist.
func loadConfig() (*config.Config, error) {
	path := configFile()
	if path == "" {
		return nil, errors.New("can't work out where the config file is; pass --config")
	}
	cfg, err := config.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no config file at %s (clonager discover <dir> creates one): %w", config.TildePath(path), err)
	}
	return cfg, err
}
