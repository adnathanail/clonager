package cmd

import (
	"embed"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// helpFiles are the commands' long help, in help/<command>.txt (status.txt
// is the root command's, for a bare clonager). Code is marked with
// backticks, as in Markdown.
//
//go:embed help/*.txt
var helpFiles embed.FS

// codeSpan matches text in backticks, on one line.
var codeSpan = regexp.MustCompile("`([^`\n]+)`")

// helpText returns help/<name>.txt with its code spans styled dim and the
// backticks removed.
func helpText(name string) string {
	b, err := helpFiles.ReadFile("help/" + name + ".txt")
	if err != nil {
		panic(err) // a missing file is a bug, which any test of the cmd package catches
	}
	text := strings.TrimRight(string(b), "\n")
	return codeSpan.ReplaceAllStringFunc(text, func(span string) string {
		return styleDim.Render(span[1 : len(span)-1])
	})
}

// usageTemplate is Cobra's default usage template (without command groups,
// which clonager doesn't use), with headings, commands and flags styled.
const usageTemplate = `{{heading "Usage:"}}{{if .Runnable}}
  {{command .UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{command (print .CommandPath " [command]")}}{{end}}{{if gt (len .Aliases) 0}}

{{heading "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{heading "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

{{heading "Available Commands:"}}{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{commandLabel .}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{heading "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces | flags}}{{end}}{{if .HasAvailableInheritedFlags}}

{{heading "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces | flags}}{{end}}{{if .HasHelpSubCommands}}

{{heading "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{command (rpad .CommandPath .CommandPathPadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

{{dim (printf "Use \"%s [command] --help\" for more information about a command." .CommandPath)}}{{end}}
`

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
	name := shortcutName(c)
	return styleBranch.Render(name) + strings.Repeat(" ", width-len(name))
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

// flagLine matches a line of pflag's FlagUsages: the indent, the flag's names
// ("-c, --config"), and its value type ("string") if it takes one.
var flagLine = regexp.MustCompile(`^(\s+)((?:-\w, )?--[\w-]+)(?: (\w+))?( {2,}|$)`)

// styleFlags styles the names and value types in pflag's FlagUsages. Styling
// after pflag has lined the descriptions up keeps them lined up.
func styleFlags(usages string) string {
	lines := strings.Split(usages, "\n")
	for i, line := range lines {
		m := flagLine.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		styled := line[:m[3]] + styleBranch.Render(line[m[4]:m[5]])
		if m[6] >= 0 {
			styled += " " + styleDim.Render(line[m[6]:m[7]])
		}
		lines[i] = styled + line[m[8]:]
	}
	return strings.Join(lines, "\n")
}

func init() {
	cobra.AddTemplateFunc("commandLabel", commandLabel)
	cobra.AddTemplateFunc("heading", func(s string) string { return styleHeading.Render(s) })
	cobra.AddTemplateFunc("command", func(s string) string { return styleBranch.Render(s) })
	cobra.AddTemplateFunc("dim", func(s string) string { return styleDim.Render(s) })
	cobra.AddTemplateFunc("flags", styleFlags)
	rootCmd.SetUsageTemplate(usageTemplate)
}
