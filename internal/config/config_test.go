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
notify_space: spaces/NOTIFY1
output_path: /tmp/latest.json
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
		NotifySpace:        "spaces/NOTIFY1",
		OutputPath:         "/tmp/latest.json",
		Spaces:             []string{"Engineering", "Platform"},
		ExcludeSpaces:      []string{"TEST", "Archive"},
		AnnouncementSpaces: []string{"General"},
		IgnorePrefixes:     []string{"[bot]", "Digest:"},
		Concurrency:        4,
	}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("Load() mismatch:\ngot:  %+v\nwant: %+v", *cfg, want)
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
