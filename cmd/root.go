package cmd

import (
	"bytes"
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

// editableConfigFile is the config file discover should edit: --config if
// given, else the source file recorded by the Home Manager module, else the
// installed config. fromSource reports the second case.
func editableConfigFile() (path string, fromSource bool, err error) {
	if configPath != "" {
		return configFile(), false, nil
	}
	src, err := config.Source()
	if err != nil {
		return "", false, err
	}
	if src != "" {
		return src, true, nil
	}
	return configFile(), false, nil
}

// unappliedNote returns a note if the config's source file (see
// editableConfigFile) has changes the installed config doesn't have yet.
func unappliedNote(installed string) string {
	if configPath != "" {
		return ""
	}
	src, err := config.Source()
	if err != nil || src == "" {
		return ""
	}
	a, errA := os.ReadFile(src)
	b, errB := os.ReadFile(installed)
	if errA != nil || errB != nil || bytes.Equal(a, b) {
		return ""
	}
	return config.TildePath(src) + " has changes not applied yet: rebuild (e.g. darwin-rebuild switch) to apply them."
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
