package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// Exit codes, for scripts: status and prune exit exitAttention or
// exitErrors (by returning an exitCode) when there's something to deal with.
const (
	exitOK        = 0
	exitFailed    = 1 // clonager itself failed
	exitAttention = 2 // something needs attention (status) or pruning (prune)
	exitErrors    = 3 // a repo has an error, or couldn't be checked
)

// exitCode is returned by a command that ran fine but should exit non-zero.
// Execute exits with it without printing an error.
type exitCode int

func (c exitCode) Error() string { return fmt.Sprintf("exit status %d", int(c)) }

// codeFor returns exitCode(code), or nil for exitOK.
func codeFor(code int) error {
	if code == exitOK {
		return nil
	}
	return exitCode(code)
}

// Execute runs clonager and returns the code to exit with.
func Execute() int {
	// Cobra prints help, and the banner in it, through these, so styles are
	// stripped when output isn't a terminal.
	stderr := colorprofile.NewWriter(os.Stderr, os.Environ())
	rootCmd.SetOut(lipgloss.Writer)
	rootCmd.SetErr(stderr)
	err := rootCmd.Execute()
	var code exitCode
	if errors.As(err, &code) {
		return int(code)
	}
	if err != nil {
		fmt.Fprintln(stderr, styleError.Render("error:"), err)
		return exitFailed
	}
	return exitOK
}

// progName is what clonager was run as: clonager, or cg (the Nix package's
// short name, or an alias).
func progName() string {
	if len(os.Args) > 0 && filepath.Base(os.Args[0]) == "cg" {
		return "cg"
	}
	return "clonager"
}

// commandLabel is a command's name as help lists it, with the letter of a
// one-letter alias in brackets ("(s)tatus"), padded to line up with its
// siblings' labels.
func commandLabel(c *cobra.Command) string {
	width := c.NamePadding()
	if c.HasParent() {
		for _, s := range c.Parent().Commands() {
			width = max(width, len(shortcutName(s)))
		}
	}
	return fmt.Sprintf("%-*s", width, shortcutName(c))
}

func shortcutName(c *cobra.Command) string {
	name := c.Name()
	for _, a := range c.Aliases {
		if len(a) == 1 && strings.HasPrefix(name, a) {
			return "(" + a + ")" + name[1:]
		}
	}
	return name
}

func init() {
	rootCmd.Use = progName()
	cobra.AddTemplateFunc("commandLabel", commandLabel)
	rootCmd.SetUsageTemplate(strings.ReplaceAll(rootCmd.UsageTemplate(),
		"{{rpad .Name .NamePadding }}", "{{commandLabel .}}"))

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
