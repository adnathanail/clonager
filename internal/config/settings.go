package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Settings are clonager's own options, as opposed to the repos in the config.
// The Home Manager module writes them from programs.clonager's options.
type Settings struct {
	// Source is where commands that change the config read and write it,
	// if not the installed config.
	Source *Source `json:"source,omitempty"`
	// DiscoverPaths are where discover looks when it's given no dirs.
	DiscoverPaths []string `json:"discoverPaths,omitempty"`
	// OpenIn is what repo names link to (see OpenInApps); empty means
	// DefaultOpenIn.
	OpenIn string `json:"openIn,omitempty"`
}

// OpenInApps are the values OpenIn can take: apps that open a folder from a
// link, "files" for the file manager, and "none" for no links.
var OpenInApps = []string{"vscode", "cursor", "zed", "files", "none"}

// DefaultOpenIn is what repo names link to when OpenIn isn't set.
const DefaultOpenIn = "files"

// SettingsPath is where the Settings live.
func SettingsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clonager", "settings.json"), nil
}

// ReadSettings returns the Settings, empty if there's no settings file.
// Paths come back with ~ expanded, and OpenIn set.
func ReadSettings() (Settings, error) {
	var s Settings
	path, err := SettingsPath()
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		s.OpenIn = DefaultOpenIn
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("%s: %w", TildePath(path), err)
	}
	if s.Source != nil {
		if err := s.Source.validate(); err != nil {
			return s, fmt.Errorf("%s: source: %w", TildePath(path), err)
		}
	}
	if s.OpenIn == "" {
		s.OpenIn = DefaultOpenIn
	} else if !slices.Contains(OpenInApps, s.OpenIn) {
		return s, fmt.Errorf("%s: openIn: %q isn't one of %s", TildePath(path), s.OpenIn, strings.Join(OpenInApps, ", "))
	}
	for i, p := range s.DiscoverPaths {
		expanded, err := ExpandHome(p)
		if err != nil {
			return s, err
		}
		if !filepath.IsAbs(expanded) {
			return s, fmt.Errorf("%s: discoverPaths: %q isn't an absolute path", TildePath(path), p)
		}
		s.DiscoverPaths[i] = expanded
	}
	return s, nil
}
