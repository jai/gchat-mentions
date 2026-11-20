package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jai/gchat-mentions/internal/gchat"
)

func TestWriteReportCreatesDirsModeAndAtomicOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	path := filepath.Join(dir, "latest.json")

	first := &gchat.Report{
		CheckedAt:     time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		Account:       "alice@example.com",
		UserName:      "Alice",
		TotalMentions: 1,
		Spaces: []gchat.SpaceMentions{
			{
				SpaceName: "Engineering",
				SpaceID:   "spaces/AAA",
				Mentions:  []gchat.Message{{ID: "m1", Text: "Hey Alice"}},
			},
		},
	}

	if err := WriteReport(path, first); err != nil {
		t.Fatalf("WriteReport(first) error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("state file mode = %o, want 600", mode)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read first report: %v", err)
	}
	var decoded gchat.Report
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal first report: %v", err)
	}
	if decoded.TotalMentions != 1 || decoded.Account != "alice@example.com" {
		t.Fatalf("decoded first report mismatch: %+v", decoded)
	}

	second := &gchat.Report{
		CheckedAt:     time.Date(2026, 5, 10, 13, 0, 0, 0, time.UTC),
		Account:       "alice@example.com",
		UserName:      "Alice",
		TotalMentions: 0,
	}
	if err := WriteReport(path, second); err != nil {
		t.Fatalf("WriteReport(second) error: %v", err)
	}

	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read second report: %v", err)
	}
	decoded = gchat.Report{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal second report: %v", err)
	}
	if decoded.TotalMentions != 0 || !decoded.CheckedAt.Equal(second.CheckedAt) {
		t.Fatalf("decoded second report mismatch: %+v", decoded)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("leftover temp file found: %s", entry.Name())
		}
	}
}
