package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jai/gchat-mentions/internal/gchat"
)

type fakeExecutor struct {
	mu          sync.Mutex
	spacesTSV   string
	messagesMap map[string]string
	sendCalls   [][]string
}

func (f *fakeExecutor) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if len(args) >= 3 && args[0] == "chat" && args[1] == "spaces" && args[2] == "list" {
		return []byte(f.spacesTSV), nil
	}
	if len(args) >= 4 && args[0] == "chat" && args[1] == "messages" && args[2] == "list" {
		if payload, ok := f.messagesMap[args[3]]; ok {
			return []byte(payload), nil
		}
		return []byte("[]"), nil
	}
	if len(args) >= 4 && args[0] == "chat" && args[1] == "messages" && args[2] == "send" {
		cp := append([]string(nil), args...)
		f.mu.Lock()
		f.sendCalls = append(f.sendCalls, cp)
		f.mu.Unlock()
		return []byte(`{"resource":"spaces/NOTIFY/messages/1"}`), nil
	}
	return nil, fmt.Errorf("unexpected args: %v", args)
}

func TestRunInitCreatesConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	var stdout, stderr bytes.Buffer

	code := run([]string{"--init", "--config", cfgPath}, &stdout, &stderr, &fakeExecutor{})
	if code != 0 {
		t.Fatalf("--init exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected config file at %s: %v", cfgPath, err)
	}
	if !strings.Contains(stdout.String(), "Created starter config") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}

	stdout.Reset()
	code = run([]string{"--init", "--config", cfgPath}, &stdout, &stderr, &fakeExecutor{})
	if code != 0 {
		t.Fatalf("second --init exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Config already exists") {
		t.Fatalf("unexpected second stdout: %q", stdout.String())
	}
}

func TestRunMissingRequiredArgsReturnsExit2(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	tests := []struct {
		name string
		args []string
	}{
		{name: "missing account", args: []string{"--user", "Alice"}},
		{name: "missing user and user-id", args: []string{"--account", "alice@example.com"}},
		{name: "invalid format rejected before polling", args: []string{"--account", "alice@example.com", "--user", "Alice", "--format", "xml"}},
		{name: "non-positive timeout rejected", args: []string{"--account", "alice@example.com", "--user", "Alice", "--timeout", "0s"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exec := &fakeExecutor{}
			code := run(tc.args, &stdout, &stderr, exec)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2 (stderr: %s)", code, stderr.String())
			}
			if len(exec.sendCalls) != 0 {
				t.Fatalf("expected no notify calls on invalid args, got %v", exec.sendCalls)
			}
		})
	}
}

func TestRunHappyPathOutputsStateAndNotify(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	outPath := filepath.Join(t.TempDir(), "state", "latest.json")

	execWithMention := &fakeExecutor{
		spacesTSV: "RESOURCE\tDISPLAY_NAME\tTYPE\nspaces/AAA\tEngineering\tSPACE\n",
		messagesMap: map[string]string{
			"spaces/AAA": `[{
				"resource":"spaces/AAA/messages/t1.m1",
				"sender":"Bob",
				"text":"Hey Alice please take a look",
				"createTime":"2026-05-10T12:00:00Z",
				"thread":"spaces/AAA/threads/t1"
			}]`,
		},
	}

	// JSON output + state file + notify when mentions > 0.
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--account", "alice@example.com",
		"--user", "Alice",
		"--out", outPath,
		"--notify-space", "spaces/NOTIFY",
		"--format", "json",
	}, &stdout, &stderr, execWithMention)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}

	var stdoutReport gchat.Report
	if err := json.Unmarshal(stdout.Bytes(), &stdoutReport); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if stdoutReport.TotalMentions != 1 {
		t.Fatalf("stdout TotalMentions = %d, want 1", stdoutReport.TotalMentions)
	}

	stateBytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var stateReport gchat.Report
	if err := json.Unmarshal(stateBytes, &stateReport); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	if stateReport.TotalMentions != 1 || len(stateReport.Spaces) != 1 {
		t.Fatalf("unexpected state report: %+v", stateReport)
	}
	if len(execWithMention.sendCalls) != 1 || execWithMention.sendCalls[0][3] != "spaces/NOTIFY" {
		t.Fatalf("expected 1 notify send to spaces/NOTIFY, got %v", execWithMention.sendCalls)
	}

	// Compact output without --notify-space does not send notifications.
	execNoNotify := &fakeExecutor{
		spacesTSV:   execWithMention.spacesTSV,
		messagesMap: execWithMention.messagesMap,
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{
		"--account", "alice@example.com",
		"--user", "Alice",
		"--out", "none",
		"--format", "compact",
	}, &stdout, &stderr, execNoNotify)
	if code != 0 {
		t.Fatalf("compact exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Google Chat: 1 unread mention across 1 space") {
		t.Fatalf("unexpected compact output:\n%s", stdout.String())
	}
	if len(execNoNotify.sendCalls) != 0 {
		t.Fatalf("expected no notify calls when --notify-space is unset, got %v", execNoNotify.sendCalls)
	}

	// --notify-space with 0 mentions does not send notifications.
	execZeroMentions := &fakeExecutor{
		spacesTSV: "RESOURCE\tDISPLAY_NAME\tTYPE\nspaces/AAA\tEngineering\tSPACE\n",
		messagesMap: map[string]string{
			"spaces/AAA": `[{"resource":"spaces/AAA/messages/t1.m1","sender":"Bob","text":"Hello team","createTime":"2026-05-10T12:00:00Z","thread":"spaces/AAA/threads/t1"}]`,
		},
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{
		"--account", "alice@example.com",
		"--user", "Alice",
		"--out", "none",
		"--notify-space", "spaces/NOTIFY",
		"--format", "compact",
	}, &stdout, &stderr, execZeroMentions)
	if code != 0 {
		t.Fatalf("zero-mentions exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if len(execZeroMentions.sendCalls) != 0 {
		t.Fatalf("expected no notify calls when mentions == 0, got %v", execZeroMentions.sendCalls)
	}
}

func TestRunUrgentTestOfflineAndNotifyNone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--urgent-test", "--urgent-offline", "--user", "Alice"}, &stdout, &stderr, &fakeExecutor{})
	if code != 0 {
		t.Fatalf("--urgent-test exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"test": true`) || !strings.Contains(stdout.String(), `"mode": "dry-run"`) {
		t.Fatalf("unexpected --urgent-test output:\n%s", stdout.String())
	}

	// Verify --notify-space none overrides config notify_space.
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfgYAML := "account: alice@example.com\nuser_name: Alice\nnotify_space: spaces/SHOULD_NOT_SEND\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	exec := &fakeExecutor{
		spacesTSV: "RESOURCE\tDISPLAY_NAME\tTYPE\nspaces/AAA\tEngineering\tSPACE\n",
		messagesMap: map[string]string{
			"spaces/AAA": `[{"resource":"spaces/AAA/messages/t1.m1","sender":"Bob","text":"Hey Alice","createTime":"2026-05-10T12:00:00Z","thread":"spaces/AAA/threads/t1"}]`,
		},
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--config", cfgPath, "--notify-space", "none", "--out", "none", "--format", "compact"}, &stdout, &stderr, exec)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if len(exec.sendCalls) != 0 {
		t.Fatalf("expected --notify-space none to suppress notification, got %v", exec.sendCalls)
	}
}
