package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
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
	// Cobra prints help, and the banner in it, through these, so styles are
	// stripped when output isn't a terminal.
	stderr := colorprofile.NewWriter(os.Stderr, os.Environ())
	rootCmd.SetOut(lipgloss.Writer)
	rootCmd.SetErr(stderr)
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(stderr, styleError.Render("error:"), err)
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

// unapplied returns the config's source (see editableConfig) if it has
// changes the installed config doesn't have yet, with a note saying so.
func unapplied(installed string) (pending *config.Config, note string) {
	if configPath != "" {
		return nil, ""
	}
	src, err := config.ReadSource()
	if err != nil || src == nil {
		return nil, ""
	}
	a, errA := src.Read()
	b, errB := os.ReadFile(installed)
	if errA != nil || errB != nil || bytes.Equal(a, b) {
		return nil, ""
	}
	label := src.Label()
	note = strings.ToUpper(label[:1]) + label[1:] + " has changes not applied yet: rebuild (e.g. darwin-rebuild switch) to apply them."
	pending, _ = config.Parse(label, a) // only used to recognise repos; nil if it doesn't parse
	return pending, note
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
