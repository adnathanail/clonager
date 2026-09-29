package cmd

import (
	"net/url"

	"charm.land/lipgloss/v2"
)

// Basic ANSI colours, so the output follows the terminal's own theme.
var (
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleBranch  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleGB      = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	styleDim     = lipgloss.NewStyle().Faint(true)
	styleHeading = lipgloss.NewStyle().Bold(true)
)

// fileLink makes text a link to a local folder, which terminals that support
// links (OSC 8) open in the file manager when it's cmd- or ctrl-clicked.
// Others show just the text, and it's stripped when output isn't a TTY.
func fileLink(path, text string) string {
	u := url.URL{Scheme: "file", Path: path}
	return lipgloss.NewStyle().Hyperlink(u.String()).Render(text)
}
