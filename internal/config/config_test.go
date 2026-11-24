package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadMissingDefaultReturnsEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") returned unexpected error: %v", err)
	}
	if !reflect.DeepEqual(*cfg, Config{}) {
		t.Fatalf("expected empty Config, got %+v", *cfg)
	}
}

func TestLoadExplicitMissingReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	if _, err := Load(missing); err == nil {
		t.Fatalf("Load(%q) expected error for missing explicit config, got nil", missing)
	}
}

func TestLoadValidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := `account: alice@example.com
user_name: Alice
user_id: users/1001
gog_env_file: ~/.config/gog/keyring.env
notify_space: spaces/NOTIFY1
output_path: /tmp/latest.json
include_dms: true
include_group_chats: true
active_within: 6h
spaces:
  - Engineering
  - Platform
exclude_spaces:
  - TEST
  - Archive
announcement_spaces:
  - General
ignore_prefixes:
  - "[bot]"
  - "Digest:"
concurrency: 4
urgent:
  enabled: true
  mode: dry-run
  timezone: Asia/Bangkok
  threshold: 0.85
  rule: either
  max_per_run: 20
  max_age_hours: 6
  quiet_hours:
    start: 22
    end: 7
  rate_limit:
    min_gap_minutes: 30
    max_per_day: 6
  providers:
    - name: typesafe
      type: typesafe
      model: jev-1.14.0
      api_key_env: TYPESAFE_API_KEY
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := Config{
		Account:            "alice@example.com",
		UserName:           "Alice",
		UserID:             "users/1001",
		GogEnvFile:         "~/.config/gog/keyring.env",
		NotifySpace:        "spaces/NOTIFY1",
		OutputPath:         "/tmp/latest.json",
		IncludeDMs:         true,
		IncludeGroupChats:  true,
		ActiveWithin:       "6h",
		Spaces:             []string{"Engineering", "Platform"},
		ExcludeSpaces:      []string{"TEST", "Archive"},
		AnnouncementSpaces: []string{"General"},
		IgnorePrefixes:     []string{"[bot]", "Digest:"},
		Concurrency:        4,
		Urgent: UrgentConfig{
			Enabled:     true,
			Mode:        "dry-run",
			Timezone:    "Asia/Bangkok",
			Threshold:   0.85,
			Rule:        "either",
			MaxPerRun:   20,
			MaxAgeHours: 6,
			QuietHours:  &QuietHoursConfig{Start: 22, End: 7},
			RateLimit:   RateLimitConfig{MinGapMinutes: 30, MaxPerDay: 6},
			Providers: []ProviderConfig{
				{Name: "typesafe", Type: "typesafe", Model: "jev-1.14.0", APIKeyEnv: "TYPESAFE_API_KEY"},
			},
		},
	}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("Load() mismatch:\ngot:  %+v\nwant: %+v", *cfg, want)
	}
}

func TestReadEnvFile(t *testing.T) {
	missing, err := ReadEnvFile(filepath.Join(t.TempDir(), "missing.env"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("ReadEnvFile(missing) = %v, %v", missing, err)
	}

	p := filepath.Join(t.TempDir(), "creds.env")
	raw := "# comment\nexport FOO=\"bar baz\"\nPLAIN=123\nSINGLE='val'\nINVALID_LINE\n"
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	kv, err := ReadEnvFile(p)
	if err != nil {
		t.Fatalf("ReadEnvFile() error: %v", err)
	}
	want := map[string]string{"FOO": "bar baz", "PLAIN": "123", "SINGLE": "val"}
	if !reflect.DeepEqual(kv, want) {
		t.Fatalf("ReadEnvFile() = %+v, want %+v", kv, want)
	}
}

func TestWriteStarterCreatesAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")

	created, err := WriteStarter(path)
	if err != nil {
		t.Fatalf("WriteStarter() error: %v", err)
	}
	if !created {
		t.Fatalf("WriteStarter() created = false, want true")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat starter config: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("starter config mode = %o, want 600", mode)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(starter) error: %v", err)
	}
	if cfg.Account == "" || cfg.UserName == "" {
		t.Fatalf("starter config missing default account/user_name: %+v", cfg)
	}

	custom := []byte("account: custom@example.com\nuser_name: Custom\n")
	if err := os.WriteFile(path, custom, 0o600); err != nil {
		t.Fatalf("overwrite config: %v", err)
	}

	created, err = WriteStarter(path)
	if err != nil {
		t.Fatalf("second WriteStarter() error: %v", err)
	}
	if created {
		t.Fatalf("second WriteStarter() created = true, want false")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after second WriteStarter: %v", err)
	}
	if string(got) != string(custom) {
		t.Fatalf("second WriteStarter() modified existing file:\ngot:  %q\nwant: %q", string(got), string(custom))
	}
}
