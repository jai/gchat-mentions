// Package config loads and initializes gchat-mentions configuration.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds user settings for gchat-mentions.
type Config struct {
	Account            string       `yaml:"account"`
	UserName           string       `yaml:"user_name"`
	UserID             string       `yaml:"user_id,omitempty"`
	GogEnvFile         string       `yaml:"gog_env_file,omitempty"`
	NotifySpace        string       `yaml:"notify_space,omitempty"`
	OutputPath         string       `yaml:"output_path,omitempty"`
	Spaces             []string     `yaml:"spaces,omitempty"`
	ExcludeSpaces      []string     `yaml:"exclude_spaces,omitempty"`
	AnnouncementSpaces []string     `yaml:"announcement_spaces,omitempty"`
	IgnorePrefixes     []string     `yaml:"ignore_prefixes,omitempty"`
	IncludeDMs         bool         `yaml:"include_dms,omitempty"`
	IncludeGroupChats  bool         `yaml:"include_group_chats,omitempty"`
	ActiveWithin       string       `yaml:"active_within,omitempty"`
	Concurrency        int          `yaml:"concurrency,omitempty"`
	Urgent             UrgentConfig `yaml:"urgent,omitempty"`
}

// UrgentConfig configures optional urgency classification and escalation.
type UrgentConfig struct {
	Enabled           bool              `yaml:"enabled,omitempty"`
	Mode              string            `yaml:"mode,omitempty"`
	LiveFlagFile      string            `yaml:"live_flag_file,omitempty"`
	Timezone          string            `yaml:"timezone,omitempty"`
	UserContext       string            `yaml:"user_context,omitempty"`
	Definition        string            `yaml:"definition,omitempty"`
	IncludeDMs        *bool             `yaml:"include_dms,omitempty"`
	Threshold         float64           `yaml:"threshold,omitempty"`
	Rule              string            `yaml:"rule,omitempty"`
	MaxPerRun         int               `yaml:"max_per_run,omitempty"`
	MaxAgeHours       float64           `yaml:"max_age_hours,omitempty"`
	SeenRetentionDays int               `yaml:"seen_retention_days,omitempty"`
	StateDir          string            `yaml:"state_dir,omitempty"`
	QuietHours        *QuietHoursConfig `yaml:"quiet_hours,omitempty"`
	RateLimit         RateLimitConfig   `yaml:"rate_limit,omitempty"`
	Providers         []ProviderConfig  `yaml:"providers,omitempty"`
	Escalation        EscalationConfig  `yaml:"escalation,omitempty"`
}

// QuietHoursConfig defines local hours [Start, End) where paging is disallowed.
// Wrap-around windows such as Start: 22, End: 7 are supported.
type QuietHoursConfig struct {
	Start int `yaml:"start"`
	End   int `yaml:"end"`
}

// RateLimitConfig bounds escalation frequency.
type RateLimitConfig struct {
	MinGapMinutes float64 `yaml:"min_gap_minutes,omitempty"`
	MaxPerDay     int     `yaml:"max_per_day,omitempty"`
}

// ProviderConfig defines one urgency classifier backend.
type ProviderConfig struct {
	Name           string `yaml:"name"`
	Type           string `yaml:"type"` // typesafe | cloudflare | openai-compatible
	Model          string `yaml:"model,omitempty"`
	URL            string `yaml:"url,omitempty"`
	APIKeyEnv      string `yaml:"api_key_env,omitempty"`
	AccountIDEnv   string `yaml:"account_id_env,omitempty"`
	EnvFile        string `yaml:"env_file,omitempty"`
	TimeoutSeconds int    `yaml:"timeout_seconds,omitempty"`
}

// EscalationConfig configures optional webhook and fallback command dispatch.
type EscalationConfig struct {
	Webhook *WebhookConfig `yaml:"webhook,omitempty"`
	Command *CommandConfig `yaml:"command,omitempty"`
}

// WebhookConfig configures an HTTP JSON escalation webhook.
type WebhookConfig struct {
	URL            string `yaml:"url,omitempty"`
	URLEnv         string `yaml:"url_env,omitempty"`
	KeyEnv         string `yaml:"key_env,omitempty"`
	KeyHeader      string `yaml:"key_header,omitempty"`
	KeyScheme      string `yaml:"key_scheme,omitempty"`
	EnvFile        string `yaml:"env_file,omitempty"`
	TimeoutSeconds int    `yaml:"timeout_seconds,omitempty"`
}

// CommandConfig configures an optional fallback command (e.g. a read-only agent).
// Tokens "{{brief}}" and "{{brief_path}}" in Argv are substituted before running.
type CommandConfig struct {
	Argv           []string `yaml:"argv,omitempty"`
	EnvFile        string   `yaml:"env_file,omitempty"`
	TimeoutSeconds int      `yaml:"timeout_seconds,omitempty"`
	Detach         bool     `yaml:"detach,omitempty"`
}

// ExpandHome replaces a leading "~/" with the user's home directory.
func ExpandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
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

// DefaultUrgentStateDir returns $XDG_STATE_HOME/gchat-mentions/urgent or ~/.local/state/gchat-mentions/urgent.
func DefaultUrgentStateDir() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "gchat-mentions", "urgent"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "gchat-mentions", "urgent"), nil
}

// ReadEnvFile parses a simple KEY=VALUE file (supporting optional "export " prefix and quotes).
// Missing files return an empty map and nil error. Secret values are never logged.
func ReadEnvFile(path string) (map[string]string, error) {
	out := make(map[string]string)
	if strings.TrimSpace(path) == "" {
		return out, nil
	}
	expanded, err := ExpandHome(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	f, err := os.Open(expanded)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("read env file %s: %w", expanded, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		if key != "" {
			out[key] = val
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan env file %s: %w", expanded, err)
	}
	return out, nil
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
	} else {
		var err error
		path, err = ExpandHome(path)
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

# Optional: KEY=VALUE env file loaded for headless gog runs (e.g. keyring password)
# gog_env_file: ~/Library/Application Support/gogcli/keyring.env

# Optional: Google Chat space ID to post unread mention digests to (leave empty to skip posting)
# notify_space: spaces/YOUR_SPACE_ID

# Optional: Where to write the latest JSON report (defaults to ~/.local/state/gchat-mentions/latest.json)
# output_path: ""

# Optional: Only scan spaces active within this duration (e.g. 168h for 7 days, 6h for urgent polling)
# active_within: ""

# Optional: Include direct messages or group chats when polling
# include_dms: false
# include_group_chats: false

# Optional: Only scan spaces whose name contains one of these substrings (empty = all spaces)
# spaces: []

# Optional: Skip spaces whose name contains any of these substrings
exclude_spaces:
  - TEST

# Optional: Suppress @all-only mentions in noisy announcement spaces (direct mentions still come through)
announcement_spaces: []

# Optional: Ignore automated messages starting with any of these prefixes
ignore_prefixes: []

# Optional: Classify new mentions for urgency and run dry-run/live escalations.
# See docs/urgent-mentions.md for full configuration.
# urgent:
#   enabled: false
#   mode: dry-run
#   timezone: UTC
#   threshold: 0.8
#   rule: either
#   providers: []
`

// WriteStarter atomically creates a starter config file if it does not already exist.
// Returns true if the file was created, false if it already existed.
func WriteStarter(path string) (bool, error) {
	if path == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return false, err
		}
	} else {
		var err error
		path, err = ExpandHome(path)
		if err != nil {
			return false, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("create config directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("create config %s: %w", path, err)
	}
	if _, err := f.WriteString(starterConfigTemplate); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return false, fmt.Errorf("write config %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return false, fmt.Errorf("close config %s: %w", path, err)
	}
	return true, nil
}
