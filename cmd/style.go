package cmd

import (
	"net/url"
	"runtime"

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
// see config.OpenInApps). Commands that print links set it with loadSettings.
var openIn = config.DefaultOpenIn

// loadSettings reads the settings, and sets openIn from them.
func loadSettings() (config.Settings, error) {
	settings, err := config.ReadSettings()
	if err != nil {
		return settings, err
	}
	openIn = settings.OpenIn
	return settings, nil
}

// folderLink makes text a link that opens a local folder in the openIn app
// when it's cmd- or ctrl-clicked, in terminals that support links (OSC 8).
// Others show just the text, and it's stripped when output isn't a TTY.
func folderLink(path, text string) string {
	u := folderURL(path)
	if u == "" {
		return text
	}
	return lipgloss.NewStyle().Hyperlink(u).Render(text)
}

// folderURL is the URL that opens the folder at path in the openIn app, or
// "" when openIn is "none".
func folderURL(path string) string {
	var u url.URL
	switch openIn {
	case "none":
		return ""
	case "files":
		u = url.URL{Scheme: "file", Path: path}
	case "zed":
		u = url.URL{Scheme: openIn, Host: "file", Path: path}
	default: // vscode://file/<path>, and the same for editors forked from it
		// windowId=_blank opens a new window rather than replacing the
		// folder open in the current one.
		u = url.URL{Scheme: openIn, Host: "file", Path: path, RawQuery: "windowId=_blank"}
	}
	return u.String()
}

// openInName is the openIn app's name, for saying where folderURL opens.
func openInName() string {
	switch openIn {
	case "vscode":
		return "VS Code"
	case "cursor":
		return "Cursor"
	case "zed":
		return "Zed"
	case "files":
		if runtime.GOOS == "darwin" {
			return "Finder"
		}
		return "the file manager"
	}
	return openIn
}

// gitButlerLink makes text (styled as GitButler) a link that opens the repo
// at path in the GitButler app, as `but gui` does. No link when openIn is
// "none".
func gitButlerLink(path, text string) string {
	if openIn == "none" {
		return styleGB.Render(text)
	}
	return styleGB.Hyperlink(gitButlerURL(path)).Render(text)
}

// gitButlerURL is the URL that opens the repo at path in the GitButler app.
func gitButlerURL(path string) string {
	u := url.URL{Scheme: "but", Host: "open", RawQuery: url.Values{"path": {path}}.Encode()}
	return u.String()
}
