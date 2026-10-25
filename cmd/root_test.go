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

func TestHelpIsStyled(t *testing.T) {
	help := rootCmd.UsageString()
	for _, want := range []string{
		styleHeading.Render("Flags:"),
		styleBranch.Render("(p)rune"),
		styleBranch.Render("-a, --all") + "             also",
		styleBranch.Render("-t, --tag") + " " + styleDim.Render("strings") + "     only",
		styleBranch.Render("-v, --verbose"),
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help doesn't contain %q:\n%s", want, help)
		}
	}
}

func TestHelpTexts(t *testing.T) {
	files, err := helpFiles.ReadDir("help")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(f.Name(), ".txt")
		text := helpText(name)
		if strings.Contains(text, "`") {
			t.Errorf("%s: unmatched backtick, or a code span across lines:\n%s", f.Name(), text)
		}
	}
	if want := styleDim.Render("<dir>"); !strings.Contains(helpText("discover"), want) {
		t.Errorf("discover help doesn't style <dir> as code")
	}
}

func TestConfigHelp(t *testing.T) {
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
