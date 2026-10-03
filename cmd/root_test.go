package cmd

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHelpShowsShortcuts(t *testing.T) {
	var out strings.Builder
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil) })
	if err := rootCmd.Usage(); err != nil {
		t.Fatal(err)
	}
	help := ansi.Strip(out.String())
	for _, want := range []string{
		"  config      Commands",
		"  (p)rune     Print",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help doesn't contain %q:\n%s", want, help)
		}
	}
}

func TestConfigHelpShowsShortcuts(t *testing.T) {
	var out strings.Builder
	configCmd.SetOut(&out)
	t.Cleanup(func() { configCmd.SetOut(nil) })
	if err := configCmd.Usage(); err != nil {
		t.Fatal(err)
	}
	help := ansi.Strip(out.String())
	for _, want := range []string{
		"  airlift     Record",
		"  discover    Find",
		"  tidy        Switch",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help doesn't contain %q:\n%s", want, help)
		}
	}
}
