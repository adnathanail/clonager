package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Settings are clonager's own options, as opposed to the repos in the config.
// The Home Manager module writes them from programs.clonager's options.
type Settings struct {
	// DiscoverPaths are where discover looks when it's given no dirs.
	DiscoverPaths []string `json:"discoverPaths,omitempty"`
}

// SettingsPath is where the Settings live.
func SettingsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clonager", "settings.json"), nil
}

// ReadSettings returns the Settings, empty if there's no settings file.
// DiscoverPaths come back with ~ expanded.
func ReadSettings() (Settings, error) {
	var s Settings
	path, err := SettingsPath()
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("%s: %w", TildePath(path), err)
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
