package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/adnathanail/clonager/internal/cli"
)

// Source is where commands that change the config (discover) read and write
// it, when that isn't the installed config itself. With the Home Manager
// module, the installed config is a read-only copy (in the Nix store, or
// decrypted by agenix), built from a source in the user's config repo; edits
// go to the source, and take effect on the next rebuild.
//
// The source is either a plain file (Path), or a pair of the user's own
// commands (Decrypt and Encrypt), e.g. for a source kept encrypted.
type Source struct {
	Path string `json:"path,omitempty"`
	// Decrypt prints the config (YAML) on stdout. Encrypt reads the new
	// config on stdin and stores it. Neither may prompt.
	Decrypt string `json:"decrypt,omitempty"`
	Encrypt string `json:"encrypt,omitempty"`
}

// SourceSettingsPath is where the Home Manager module records the Source.
func SourceSettingsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clonager", "source.json"), nil
}

// legacySourcePath is where older versions of the module recorded a plain
// source file's path, as the only thing in the file.
func legacySourcePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clonager", "source"), nil
}

// ReadSource returns the recorded Source, or nil if there isn't one.
func ReadSource() (*Source, error) {
	settings, err := SourceSettingsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(settings)
	switch {
	case err == nil:
		var s Source
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", TildePath(settings), err)
		}
		if err := s.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", TildePath(settings), err)
		}
		return &s, nil
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}

	legacy, err := legacySourcePath()
	if err != nil {
		return nil, err
	}
	data, err = os.ReadFile(legacy)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := Source{Path: strings.TrimSpace(string(data))}
	if s.Path == "" {
		return nil, nil
	}
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", TildePath(legacy), err)
	}
	return &s, nil
}

func (s *Source) validate() error {
	hooks := s.Decrypt != "" || s.Encrypt != ""
	switch {
	case s.Path != "" && hooks:
		return errors.New("give either a path or decrypt and encrypt commands, not both")
	case hooks && (s.Decrypt == "" || s.Encrypt == ""):
		return errors.New("decrypt and encrypt commands go together")
	case !hooks && s.Path == "":
		return errors.New("no path or commands")
	case s.Path != "":
		path, err := ExpandHome(s.Path)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%q isn't an absolute path", s.Path)
		}
		s.Path = path
	}
	return nil
}

// Label describes the source in messages.
func (s *Source) Label() string {
	if s.Path != "" {
		return TildePath(s.Path)
	}
	return "the config source"
}

// Read returns the source's contents.
func (s *Source) Read() ([]byte, error) {
	if s.Path != "" {
		return os.ReadFile(s.Path)
	}
	out, err := cli.ConfigHook(s.Decrypt, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting the config source: %w", err)
	}
	return out, nil
}

// Load reads the source for editing; Save then writes it back, through the
// Encrypt command if there is one. A plain file that doesn't exist yet is an
// empty config.
func (s *Source) Load() (*Config, error) {
	if s.Path != "" {
		cfg, err := Load(s.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return New(s.Path), nil
		}
		return cfg, err
	}
	data, err := s.Read()
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(s.Label(), data)
	if err != nil {
		return nil, err
	}
	// Compare later saves against this encoding, not the raw text: the source
	// may be formatted differently from how clonager writes YAML.
	if cfg.loaded, err = cfg.encode(); err != nil {
		return nil, err
	}
	cfg.source = s
	return cfg, nil
}

// saveThroughSource stores the config with the Source's Encrypt command,
// unless it's unchanged: re-encrypting gives different output every time, so
// that would show up as a change in the user's repo for no reason.
func (c *Config) saveThroughSource() error {
	data, err := c.encode()
	if err != nil {
		return err
	}
	if bytes.Equal(data, c.loaded) {
		return nil
	}
	if _, err := cli.ConfigHook(c.source.Encrypt, data); err != nil {
		return fmt.Errorf("encrypting the config source: %w", err)
	}
	c.loaded = data
	return nil
}
