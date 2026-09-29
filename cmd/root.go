package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/adnathanail/clonager/internal/config"
)

var configPath string

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

// editableConfig loads the config for commands that change it (discover):
// --config if given, else the source recorded by the Home Manager module
// (a file, or decrypt/encrypt commands), else the installed config. A config
// file that doesn't exist yet loads empty. fromSource reports the second
// case, where changes apply on the next rebuild.
func editableConfig() (cfg *config.Config, fromSource bool, err error) {
	if configPath == "" {
		src, err := config.ReadSource()
		if err != nil {
			return nil, false, err
		}
		if src != nil {
			cfg, err := src.Load()
			return cfg, true, err
		}
	}
	path := configFile()
	cfg, err = config.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return config.New(path), false, nil
	}
	return cfg, false, err
}

// unappliedNote returns a note if the config's source (see editableConfig)
// has changes the installed config doesn't have yet.
func unappliedNote(installed string) string {
	if configPath != "" {
		return ""
	}
	src, err := config.ReadSource()
	if err != nil || src == nil {
		return ""
	}
	a, errA := src.Read()
	b, errB := os.ReadFile(installed)
	if errA != nil || errB != nil || bytes.Equal(a, b) {
		return ""
	}
	label := src.Label()
	return strings.ToUpper(label[:1]) + label[1:] + " has changes not applied yet: rebuild (e.g. darwin-rebuild switch) to apply them."
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
