package cmd

import (
	"net/url"

	"charm.land/lipgloss/v2"

	"github.com/adnathanail/clonager/internal/config"
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

// openIn is the app folderLink's links open folders in (the openIn setting,
// see config.OpenInApps). Commands that print links set it with loadOpenIn.
var openIn = config.DefaultOpenIn

// loadOpenIn sets openIn from the settings.
func loadOpenIn() error {
	settings, err := config.ReadSettings()
	if err != nil {
		return err
	}
	openIn = settings.OpenIn
	return nil
}

// folderLink makes text a link that opens a local folder in the openIn app
// when it's cmd- or ctrl-clicked, in terminals that support links (OSC 8).
// Others show just the text, and it's stripped when output isn't a TTY.
func folderLink(path, text string) string {
	var u url.URL
	switch openIn {
	case "none":
		return text
	case "files":
		u = url.URL{Scheme: "file", Path: path}
	case "zed":
		u = url.URL{Scheme: openIn, Host: "file", Path: path}
	default: // vscode://file/<path>, and the same for editors forked from it
		// windowId=_blank opens a new window rather than replacing the
		// folder open in the current one.
		u = url.URL{Scheme: openIn, Host: "file", Path: path, RawQuery: "windowId=_blank"}
	}
	return lipgloss.NewStyle().Hyperlink(u.String()).Render(text)
}

// gitButlerLink makes text (styled as GitButler) a link that opens the repo
// at path in the GitButler app, as `but gui` does. No link when openIn is
// "none".
func gitButlerLink(path, text string) string {
	if openIn == "none" {
		return styleGB.Render(text)
	}
	u := url.URL{Scheme: "but", Host: "open", RawQuery: url.Values{"path": {path}}.Encode()}
	return styleGB.Hyperlink(u.String()).Render(text)
}
