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

var rootCmd = &cobra.Command{
	Use:           "clonager",
	Short:         "Manage your git clones",
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

func loadConfig() (*config.Config, error) {
	path := configPath
	if path == "" {
		var err error
		if path, err = config.DefaultPath(); err != nil {
			return nil, err
		}
	} else {
		var err error
		if path, err = config.ExpandHome(path); err != nil {
			return nil, err
		}
	}
	cfg, err := config.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no config file at %s", config.TildePath(path))
	}
	return cfg, err
}
