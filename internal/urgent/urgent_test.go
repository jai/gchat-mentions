package urgent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jai/gchat-mentions/internal/config"
	"github.com/jai/gchat-mentions/internal/gchat"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type fakeCmdRunner struct {
	mu    sync.Mutex
	calls [][]string
	envs  []map[string]string
}

func (r *fakeCmdRunner) Run(_ context.Context, argv []string, _ string, extraEnv map[string]string, _ bool, _ time.Duration) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string(nil), argv...))
	cp := make(map[string]string, len(extraEnv))
	for k, v := range extraEnv {
		cp[k] = v
	}
	r.envs = append(r.envs, cp)
	return "completed", nil
}

func TestBuildStateCleanStructuredJSONBudget(t *testing.T) {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	target := gchat.Message{
		ID:        "spaces/AAA/messages/t1.m9",
		SpaceID:   "spaces/AAA",
		SpaceName: "Platform",
		SpaceType: "SPACE",
		Sender:    "Bob",
		Text:      "Can you approve this rollback?",
		Time:      base,
		Thread:    "spaces/AAA/threads/t1",
	}

	var ctxMsgs []ContextMessage
	for i := 0; i < 8; i++ {
		ctxMsgs = append(ctxMsgs, ContextMessage{
			Label:  "thread",
			Sender: "Carol",
			At:     base.Add(-time.Duration(8-i) * time.Minute),
			Text:   strings.Repeat("x", 550),
		})
	}

	state := BuildState(target, ctxMsgs, time.UTC)
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if len(raw) > maxStateChars {
		t.Fatalf("serialized state length = %d, exceeds %d", len(raw), maxStateChars)
	}
	if bytes.Contains(raw, []byte("UNTRUSTED_CHAT_DATA")) {
		t.Fatalf("classifier state should not contain UNTRUSTED_CHAT_DATA delimiters: %s", string(raw))
	}
	if state.Space.Name != "Platform" || state.TargetMessage.Sender != "Bob" || len(state.Context) == 0 {
		t.Fatalf("unexpected structured state: %+v", state)
	}
}

func TestBuildContextPadsFromSpaceWhenThreadShort(t *testing.T) {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	target := gchat.Message{
		ID:      "m-target",
		SpaceID: "spaces/AAA",
		Thread:  "spaces/AAA/threads/t1",
		Time:    base,
	}
	threadMsgs := []gchat.Message{
		{ID: "m-t1", SpaceID: "spaces/AAA", Thread: "spaces/AAA/threads/t1", Sender: "A", Text: "first", Time: base.Add(-5 * time.Minute)},
		{ID: "m-future", SpaceID: "spaces/AAA", Thread: "spaces/AAA/threads/t1", Sender: "A", Text: "after", Time: base.Add(2 * time.Minute)},
	}
	spaceMsgs := []gchat.Message{
		{ID: "m-s1", SpaceID: "spaces/AAA", Thread: "spaces/AAA/threads/other", Sender: "B", Text: "earlier space msg", Time: base.Add(-10 * time.Minute)},
		{ID: "m-s2", SpaceID: "spaces/BBB", Thread: "spaces/BBB/threads/other", Sender: "C", Text: "other space", Time: base.Add(-8 * time.Minute)},
	}

	ctx := BuildContext(target, threadMsgs, spaceMsgs)
	if len(ctx) != 2 {
		t.Fatalf("len(ctx) = %d, want 2: %+v", len(ctx), ctx)
	}
	if ctx[0].Label != "same space, other thread" || ctx[1].Label != "thread" {
		t.Fatalf("unexpected labels/order: %+v", ctx)
	}
}

func TestResolveSettingsLoadsExternalPromptsFile(t *testing.T) {
	pPath := filepath.Join(t.TempDir(), "prompts.yaml")
	raw := `questions:
  needs_action:
    type: noul
    instructions: "Does {{recipient}} need to act?"
    criteria:
      "true": "Yes for {{user_name}}"
      "false": "No"
  urgent:
    type: noul
    instructions: "Is this urgent for {{user_name}} ({{user_context}})?"
`
	if err := os.WriteFile(pPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write prompts: %v", err)
	}

	settings, err := ResolveSettings(config.UrgentConfig{
		UserContext: "CTO",
		PromptsFile: pPath,
	}, "Alice")
	if err != nil {
		t.Fatalf("ResolveSettings() error: %v", err)
	}
	gotAct := settings.Questions["needs_action"]
	if gotAct.Instructions != "Does Alice (CTO) need to act?" {
		t.Fatalf("unexpected rendered instructions: %q", gotAct.Instructions)
	}
	crit, ok := gotAct.Criteria.(map[string]any)
	if !ok || crit["true"] != "Yes for Alice" {
		t.Fatalf("unexpected rendered criteria: %#v", gotAct.Criteria)
	}
}

func TestClassifyAllProvidersAndRetry(t *testing.T) {
	t.Setenv("TEST_TS_KEY", "ts-secret")
	t.Setenv("TEST_CF_ACCT", "cf-acct")
	t.Setenv("TEST_CF_TOKEN", "cf-secret")
	t.Setenv("TEST_OAI_KEY", "oai-secret")

	var mu sync.Mutex
	tsAttempts := 0
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Host, "typesafe"):
			mu.Lock()
			tsAttempts++
			attempt := tsAttempts
			mu.Unlock()
			if attempt == 1 {
				return jsonResponse(503, "temporary error"), nil
			}
			return jsonResponse(200, `{"model":"jev-1.14.0","answers":{"needs_action":{"noul":0.95},"urgent":{"noul":0.90}},"usage":{"input_tokens":1000}}`), nil
		case strings.Contains(req.URL.Host, "cloudflare"):
			return jsonResponse(200, `{"result":{"response":{"needs_action":0.9,"urgent":true,"confidence":0.9,"reason":"outage"},"usage":{"neurons":2.5}}}`), nil
		case strings.Contains(req.URL.Host, "openai.local"):
			return jsonResponse(200, `{"choices":[{"message":{"content":"{\"needs_action\":0.1,\"urgent\":false,\"confidence\":80,\"reason\":\"fyi\"}"}}]}`), nil
		default:
			return jsonResponse(404, "not found"), nil
		}
	})

	settings, err := ResolveSettings(config.UrgentConfig{
		Threshold: 0.8,
		Rule:      "either",
		Providers: []config.ProviderConfig{
			{Name: "a", Type: "typesafe", APIKeyEnv: "TEST_TS_KEY"},
			{Name: "b", Type: "cloudflare", AccountIDEnv: "TEST_CF_ACCT", APIKeyEnv: "TEST_CF_TOKEN"},
			{Name: "c", Type: "openai-compatible", URL: "https://openai.local/v1/chat/completions", Model: "gpt-test", APIKeyEnv: "TEST_OAI_KEY"},
		},
	}, "Alice")
	if err != nil {
		t.Fatalf("ResolveSettings() error: %v", err)
	}

	state := BuildState(gchat.Message{SpaceName: "Ops", Sender: "Bob", Text: "help"}, nil, time.UTC)
	verdicts := ClassifyAll(context.Background(), state, settings, client, time.Millisecond)
	if len(verdicts) != 3 {
		t.Fatalf("len(verdicts) = %d, want 3", len(verdicts))
	}
	if tsAttempts != 2 {
		t.Fatalf("typesafe attempts = %d, want 2 (retry on 503)", tsAttempts)
	}
	if !verdicts[0].Actionable || !verdicts[0].Urgent || verdicts[0].NeedsAction == nil || *verdicts[0].NeedsAction != 0.95 {
		t.Fatalf("unexpected typesafe verdict: %+v", verdicts[0])
	}
	if !verdicts[1].Urgent || verdicts[1].Confidence != 0.9 || verdicts[1].Reason != "outage" {
		t.Fatalf("unexpected cloudflare verdict: %+v", verdicts[1])
	}
	if verdicts[2].Urgent || verdicts[2].Actionable {
		t.Fatalf("unexpected openai verdict: %+v", verdicts[2])
	}
	if VerdictsAgree(verdicts) {
		t.Fatal("expected VerdictsAgree = false when providers disagree")
	}
	if !ShouldEscalate(verdicts, "either", settings) {
		t.Fatal("expected ShouldEscalate(either) = true")
	}
	if ShouldEscalate(verdicts, "both", settings) {
		t.Fatal("expected ShouldEscalate(both) = false")
	}
	if !ShouldEscalate(verdicts, "a", settings) {
		t.Fatal("expected ShouldEscalate(a) = true")
	}
	if ShouldEscalate(verdicts, "c", settings) {
		t.Fatal("expected ShouldEscalate(c) = false")
	}
}

func TestCloudflareClefTwoQuestionThresholdsAndOverride(t *testing.T) {
	t.Setenv("TEST_CF_ACCT", "cf-acct")
	t.Setenv("TEST_CF_TOKEN", "cf-secret")

	var mu sync.Mutex
	seenPaths := map[string]bool{}
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return jsonResponse(400, "bad json"), nil
		}
		// Verify required native Clef fields: model, structured state object, questions map.
		if _, ok := body["model"].(string); !ok {
			return jsonResponse(400, "missing model"), nil
		}
		if _, ok := body["state"].(map[string]any); !ok {
			return jsonResponse(400, "expected structured object state"), nil
		}
		qs, ok := body["questions"].(map[string]any)
		if !ok || qs["needs_action"] == nil || qs["urgent"] == nil {
			return jsonResponse(400, "expected needs_action and urgent questions"), nil
		}
		mu.Lock()
		seenPaths[req.URL.Path] = true
		mu.Unlock()
		if strings.HasSuffix(req.URL.Path, "@cf/cloudflare/clef-flash") {
			// Actionable approval, not urgent.
			return jsonResponse(200, `{"result":{"model":"clef-flash","answers":{"needs_action":{"type":"noul","noul":0.98},"urgent":{"type":"noul","noul":0.30}},"usage":{"input_tokens":250,"output_tokens":0}},"success":true}`), nil
		}
		// @all outage: low needs_action (0.20), high urgent (0.88 >= 0.85 override).
		return jsonResponse(200, `{"result":{"model":"clef","answers":{"needs_action":{"type":"noul","noul":0.20},"urgent":{"type":"noul","noul":0.88}},"usage":{"input_tokens":250,"output_tokens":0}},"success":true}`), nil
	})

	settings, err := ResolveSettings(config.UrgentConfig{
		ActionThreshold: 0.70,
		UrgentThreshold: 0.70,
		Rule:            "either",
		Providers: []config.ProviderConfig{
			{Name: "flash", Type: "cloudflare-clef", Model: "@cf/cloudflare/clef-flash", AccountIDEnv: "TEST_CF_ACCT", APIKeyEnv: "TEST_CF_TOKEN"},
			{Name: "full", Type: "cloudflare", Model: "@cf/cloudflare/clef", AccountIDEnv: "TEST_CF_ACCT", APIKeyEnv: "TEST_CF_TOKEN"},
		},
	}, "Alice")
	if err != nil {
		t.Fatalf("ResolveSettings() error: %v", err)
	}

	state := BuildState(gchat.Message{SpaceName: "Ops", Sender: "Bob", Text: "@all login down"}, nil, time.UTC)
	verdicts := ClassifyAll(context.Background(), state, settings, client, time.Millisecond)
	if len(verdicts) != 2 {
		t.Fatalf("len(verdicts) = %d, want 2", len(verdicts))
	}
	// flash: actionable=true (0.98 >= 0.70), urgent=false (0.30 < 0.70).
	if !verdicts[0].Actionable || verdicts[0].Urgent {
		t.Fatalf("expected flash actionable=true urgent=false, got %+v", verdicts[0])
	}
	// full: actionable=false (0.20 < 0.70), urgent=true via 0.88 >= 0.85 override.
	if verdicts[1].Actionable || !verdicts[1].Urgent {
		t.Fatalf("expected full actionable=false urgent=true (override), got %+v", verdicts[1])
	}
	if !seenPaths["/client/v4/accounts/cf-acct/ai/run/@cf/cloudflare/clef-flash"] || !seenPaths["/client/v4/accounts/cf-acct/ai/run/@cf/cloudflare/clef"] {
		t.Fatalf("unexpected Cloudflare paths: %+v", seenPaths)
	}
}

func TestQuietHoursAndRateLimit(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	qh := &config.QuietHoursConfig{Start: 22, End: 7}

	late := time.Date(2026, 6, 1, 23, 0, 0, 0, loc)
	if PagingAllowed(late, true, false, loc, qh) {
		t.Fatal("expected paging disallowed at 23:00")
	}
	early := time.Date(2026, 6, 2, 6, 59, 0, 0, loc)
	if PagingAllowed(early, true, false, loc, qh) {
		t.Fatal("expected paging disallowed at 06:59")
	}
	day := time.Date(2026, 6, 2, 10, 0, 0, 0, loc)
	if !PagingAllowed(day, true, false, loc, qh) {
		t.Fatal("expected paging allowed at 10:00 live")
	}
	if PagingAllowed(day, false, false, loc, qh) {
		t.Fatal("expected paging disallowed in dry-run")
	}
	if PagingAllowed(day, true, true, loc, qh) {
		t.Fatal("expected paging disallowed in test mode")
	}

	settings, err := ResolveSettings(config.UrgentConfig{
		Timezone: "Asia/Bangkok",
		RateLimit: config.RateLimitConfig{
			MinGapMinutes: 30,
			MaxPerDay:     2,
		},
	}, "Alice")
	if err != nil {
		t.Fatalf("ResolveSettings: %v", err)
	}
	entries := []EscalationEntry{
		{TSUTC: day.Add(-10 * time.Minute).UTC().Format(time.RFC3339), LocalDate: "2026-06-02", Mode: "live"},
	}
	if ok, _ := RateLimitOK(entries, day, "live", settings); ok {
		t.Fatal("expected min-gap to block escalation 10m after prior")
	}
	if ok, _ := RateLimitOK(entries, day, "dry-run", settings); !ok {
		t.Fatal("expected rate limit to isolate modes")
	}

	entries = []EscalationEntry{
		{TSUTC: day.Add(-3 * time.Hour).UTC().Format(time.RFC3339), LocalDate: "2026-06-02", Mode: "live"},
		{TSUTC: day.Add(-1 * time.Hour).UTC().Format(time.RFC3339), LocalDate: "2026-06-02", Mode: "live"},
	}
	if ok, why := RateLimitOK(entries, day, "live", settings); ok || !strings.Contains(why, "daily cap") {
		t.Fatalf("expected daily cap rejection, got ok=%v why=%q", ok, why)
	}
}

func TestRunnerLifecycleDedupeStaleDryRunAndLive(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)

	t.Setenv("TEST_TS_KEY", "ts-secret")
	var webhookHits int
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Host, "webhook.local") {
			webhookHits++
			if req.Header.Get("Authorization") != "Bearer wh-secret" {
				return jsonResponse(401, "bad auth"), nil
			}
			return jsonResponse(200, `{"ok":true}`), nil
		}
		return jsonResponse(200, `{"model":"jev-1.14.0","answers":{"needs_action":{"noul":0.95},"urgent":{"noul":0.92}},"usage":{"input_tokens":500}}`), nil
	})

	whEnv := filepath.Join(t.TempDir(), "wh.env")
	if err := os.WriteFile(whEnv, []byte("URL=https://webhook.local/urgent\nKEY=wh-secret\n"), 0o600); err != nil {
		t.Fatalf("write wh env: %v", err)
	}

	settings, err := ResolveSettings(config.UrgentConfig{
		Mode:        "dry-run",
		StateDir:    stateDir,
		MaxPerRun:   1,
		MaxAgeHours: 6,
		Providers:   []config.ProviderConfig{{Name: "ts", Type: "typesafe", APIKeyEnv: "TEST_TS_KEY"}},
		Escalation: config.EscalationConfig{
			Webhook: &config.WebhookConfig{EnvFile: whEnv},
			Command: &config.CommandConfig{Argv: []string{"echo", "{{brief_path}}"}},
		},
	}, "Alice")
	if err != nil {
		t.Fatalf("ResolveSettings: %v", err)
	}

	report := &gchat.Report{
		CheckedAt:     now,
		TotalMentions: 3,
		Spaces: []gchat.SpaceMentions{{
			SpaceID:   "spaces/AAA",
			SpaceName: "Engineering",
			SpaceType: "SPACE",
			Mentions: []gchat.Message{
				{ID: "m-fresh-1", SpaceID: "spaces/AAA", SpaceName: "Engineering", SpaceType: "SPACE", Sender: "Bob", Text: "@Alice prod down", Time: now.Add(-5 * time.Minute), Thread: "spaces/AAA/threads/t1"},
				{ID: "m-fresh-2", SpaceID: "spaces/AAA", SpaceName: "Engineering", SpaceType: "SPACE", Sender: "Carol", Text: "@Alice also down", Time: now.Add(-10 * time.Minute), Thread: "spaces/AAA/threads/t2"},
				{ID: "m-stale", SpaceID: "spaces/AAA", SpaceName: "Engineering", SpaceType: "SPACE", Sender: "Dave", Text: "@Alice old msg", Time: now.Add(-12 * time.Hour), Thread: "spaces/AAA/threads/t3"},
			},
		}},
	}

	sum1, err := Run(context.Background(), report, settings, RunOptions{
		Now:        func() time.Time { return now },
		HTTPClient: client,
		RetryDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run 1 error: %v", err)
	}
	if sum1.Stale != 1 || sum1.Classified != 1 || sum1.Overflow != 1 || sum1.ActionableCount != 1 || sum1.UrgentCount != 1 || sum1.Escalations != 1 {
		t.Fatalf("unexpected Run 1 summary: %+v", sum1)
	}
	if webhookHits != 0 {
		t.Fatalf("expected 0 webhook calls in dry-run, got %d", webhookHits)
	}

	sum2, err := Run(context.Background(), report, settings, RunOptions{
		ModeOverride: "live",
		Now:          func() time.Time { return now.Add(time.Minute) },
		HTTPClient:   client,
		RetryDelay:   time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run 2 error: %v", err)
	}
	if sum2.Classified != 1 || sum2.Overflow != 0 || sum2.Escalations != 1 {
		t.Fatalf("unexpected Run 2 summary: %+v", sum2)
	}
	if webhookHits != 1 {
		t.Fatalf("expected 1 webhook call in live mode, got %d", webhookHits)
	}

	sum3, err := Run(context.Background(), report, settings, RunOptions{
		Now:        func() time.Time { return now.Add(2 * time.Minute) },
		HTTPClient: client,
		RetryDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run 3 error: %v", err)
	}
	if sum3.New != 0 || sum3.Classified != 0 {
		t.Fatalf("expected 0 new/classified on Run 3, got %+v", sum3)
	}

	rawVerdicts, err := os.ReadFile(filepath.Join(stateDir, "verdicts.jsonl"))
	if err != nil {
		t.Fatalf("read verdicts.jsonl: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(rawVerdicts), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("verdicts.jsonl lines = %d, want 2", len(lines))
	}
	if bytes.Contains(rawVerdicts, []byte("ts-secret")) || bytes.Contains(rawVerdicts, []byte("wh-secret")) {
		t.Fatal("secret leaked into verdicts.jsonl")
	}
}

func TestRunnerFallbackCommandAndInitAndLock(t *testing.T) {
	stateDir := t.TempDir()
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	cmdEnv := filepath.Join(t.TempDir(), "cmd.env")
	if err := os.WriteFile(cmdEnv, []byte("RUNNER_SECRET=shh\n"), 0o600); err != nil {
		t.Fatalf("write cmd env: %v", err)
	}
	runner := &fakeCmdRunner{}
	settings, err := ResolveSettings(config.UrgentConfig{
		Mode:      "live",
		StateDir:  stateDir,
		Providers: []config.ProviderConfig{{Name: "oai", Type: "openai-compatible", URL: "https://oai.local/v1", Model: "m"}},
		Escalation: config.EscalationConfig{
			Command: &config.CommandConfig{
				Argv:    []string{"my-agent", "--brief-file", "{{brief_path}}"},
				EnvFile: cmdEnv,
			},
		},
	}, "Alice")
	if err != nil {
		t.Fatalf("ResolveSettings: %v", err)
	}

	client := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"choices":[{"message":{"content":"{\"needs_action\":0.95,\"urgent\":true,\"confidence\":0.95,\"reason\":\"outage\"}"}}]}`), nil
	})

	report := &gchat.Report{
		Spaces: []gchat.SpaceMentions{{
			SpaceID:   "spaces/AAA",
			SpaceName: "Ops",
			SpaceType: "SPACE",
			Mentions:  []gchat.Message{{ID: "m-init", SpaceID: "spaces/AAA", Sender: "Bob", Text: "@Alice hi", Time: now.Add(-time.Minute)}},
		}},
	}
	sumInit, err := Run(context.Background(), report, settings, RunOptions{InitOnly: true, Now: func() time.Time { return now }, HTTPClient: client})
	if err != nil {
		t.Fatalf("init run error: %v", err)
	}
	if sumInit.Classified != 0 {
		t.Fatalf("init should not classify, got %+v", sumInit)
	}

	report.Spaces[0].Mentions = append(report.Spaces[0].Mentions, gchat.Message{
		ID: "m-live", SpaceID: "spaces/AAA", SpaceName: "Ops", SpaceType: "SPACE", Sender: "Bob", Text: "@Alice prod down", Time: now.Add(-30 * time.Second),
	})
	sumLive, err := Run(context.Background(), report, settings, RunOptions{
		Now:        func() time.Time { return now },
		HTTPClient: client,
		CmdRunner:  runner,
		RetryDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("live run error: %v", err)
	}
	if sumLive.Escalations != 1 || len(runner.calls) != 1 {
		t.Fatalf("expected 1 command escalation, got sum=%+v calls=%v", sumLive, runner.calls)
	}
	if runner.envs[0]["RUNNER_SECRET"] != "shh" {
		t.Fatalf("expected RUNNER_SECRET injected into command env")
	}

	lock, err := AcquireLock(filepath.Join(stateDir, "run.lock"))
	if err != nil || lock == nil {
		t.Fatalf("AcquireLock failed: lock=%v err=%v", lock, err)
	}
	defer lock.Release()

	sumSkip, err := Run(context.Background(), report, settings, RunOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("locked run error: %v", err)
	}
	if !sumSkip.Skipped || sumSkip.SkipReason != "another-run-holds-lock" {
		t.Fatalf("expected skipped summary under lock, got %+v", sumSkip)
	}
}

func TestRunTestOfflineNeverRunsLive(t *testing.T) {
	settings, err := ResolveSettings(config.UrgentConfig{
		Mode:     "live",
		StateDir: t.TempDir(),
		Escalation: config.EscalationConfig{
			Command: &config.CommandConfig{Argv: []string{"false"}},
		},
	}, "Alice")
	if err != nil {
		t.Fatalf("ResolveSettings: %v", err)
	}
	rec, err := RunTest(context.Background(), settings, true, RunOptions{})
	if err != nil {
		t.Fatalf("RunTest() error: %v", err)
	}
	if !rec.Test || rec.Escalation.Mode != "dry-run" || rec.Escalation.PagingAllowed {
		t.Fatalf("unexpected test record: %+v", rec)
	}
	if rec.Escalation.BriefPath == "" {
		t.Fatalf("expected brief.md to be written during test")
	}
	raw, err := json.Marshal(rec)
	if err != nil || !bytes.Contains(raw, []byte("command:dry-run-not-spawned")) {
		t.Fatalf("expected dry-run command action, got %s", string(raw))
	}
}
