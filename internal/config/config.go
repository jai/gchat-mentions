// Package config loads and initializes gchat-mentions configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds user settings for gchat-mentions.
type Config struct {
	Account            string   `yaml:"account"`
	UserName           string   `yaml:"user_name"`
	UserID             string   `yaml:"user_id,omitempty"`
	NotifySpace        string   `yaml:"notify_space,omitempty"`
	OutputPath         string   `yaml:"output_path,omitempty"`
	Spaces             []string `yaml:"spaces,omitempty"`
	ExcludeSpaces      []string `yaml:"exclude_spaces,omitempty"`
	AnnouncementSpaces []string `yaml:"announcement_spaces,omitempty"`
	IgnorePrefixes     []string `yaml:"ignore_prefixes,omitempty"`
	Concurrency        int      `yaml:"concurrency,omitempty"`
}

// DefaultConfigPath returns $XDG_CONFIG_HOME/gchat-mentions/config.yaml or ~/.config/gchat-mentions/config.yaml.
func DefaultConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "gchat-mentions", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "gchat-mentions", "config.yaml"), nil
}

// DefaultOutputPath returns $XDG_STATE_HOME/gchat-mentions/latest.json or ~/.local/state/gchat-mentions/latest.json.
func DefaultOutputPath() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "gchat-mentions", "latest.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "gchat-mentions", "latest.json"), nil
}

// Load reads a YAML config file. If path is empty, it checks DefaultConfigPath
// and returns an empty Config (no error) when the default file does not exist.
func Load(path string) (*Config, error) {
	explicit := path != ""
	if !explicit {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

const starterConfigTemplate = `# gchat-mentions configuration
# Google Workspace account authorized in gog (run: gog auth list)
account: you@company.com

# Your Google Chat display name for mention matching
user_name: Your Name

# Optional: Google Chat user ID (e.g. users/1234567890)
# user_id: ""

# Optional: Google Chat space ID to post unread mention digests to (leave empty to skip posting)
# notify_space: spaces/YOUR_SPACE_ID

# Optional: Where to write the latest JSON report (defaults to ~/.local/state/gchat-mentions/latest.json)
# output_path: ""

# Optional: Only scan spaces whose name contains one of these substrings (empty = all spaces)
# spaces: []

# Optional: Skip spaces whose name contains any of these substrings
exclude_spaces:
  - TEST

# Optional: Suppress @all-only mentions in noisy announcement spaces (direct mentions still come through)
announcement_spaces: []

# Optional: Ignore automated messages starting with any of these prefixes
ignore_prefixes: []
`

// WriteStarter writes a starter config file if it does not already exist.
// Returns true if the file was created, false if it already existed.
func WriteStarter(path string) (bool, error) {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return false, err
		}
	}
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat config %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(starterConfigTemplate), 0o600); err != nil {
		return false, fmt.Errorf("write config %s: %w", path, err)
	}
	return true, nil
}
