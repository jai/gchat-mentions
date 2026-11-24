package urgent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jai/gchat-mentions/internal/gchat"
)

// ContextFetcher optionally loads recent thread messages for a new candidate.
type ContextFetcher interface {
	FetchThreadContext(ctx context.Context, msg gchat.Message, maxMsgs int) ([]gchat.Message, error)
}

// RunOptions controls a single urgency classification run.
type RunOptions struct {
	InitOnly     bool
	ModeOverride string // "", "auto", "dry-run", "live"
	MaxOverride  int
	Now          func() time.Time
	HTTPClient   HTTPDoer
	CmdRunner    CommandRunner
	RetryDelay   time.Duration
	ContextFetch ContextFetcher
}

// EffectiveMode resolves live vs dry-run from mode override, settings, and live flag file.
func EffectiveMode(override string, settings *Settings) string {
	req := strings.ToLower(strings.TrimSpace(override))
	if req == "dry-run" {
		return "dry-run"
	}
	if req == "live" {
		return "live"
	}
	if req == "" {
		req = settings.Mode
	}
	if req == "live" {
		return "live"
	}
	if req == "auto" && settings.LiveFlagFile != "" {
		if _, err := os.Stat(settings.LiveFlagFile); err == nil {
			return "live"
		}
	}
	return "dry-run"
}

// Run processes mentions from report through deduplication, optional thread context,
// concurrent provider classification, and rate-limited escalation.
func Run(ctx context.Context, report *gchat.Report, settings *Settings, opts RunOptions) (*gchat.UrgentSummary, error) {
	started := time.Now()
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn().UTC()

	if err := os.MkdirAll(settings.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create urgent state dir: %w", err)
	}

	lockPath := filepath.Join(settings.StateDir, "run.lock")
	lock, err := AcquireLock(lockPath)
	if err != nil {
		return nil, err
	}
	if lock == nil {
		mode := EffectiveMode(opts.ModeOverride, settings)
		logLine := fmt.Sprintf("%s run=skipped reason=another-run-holds-lock exit=0", now.In(settings.Location).Format(time.RFC3339))
		_ = appendLine(filepath.Join(settings.StateDir, "run.log"), logLine)
		return &gchat.UrgentSummary{
			Mode:       mode,
			Skipped:    true,
			SkipReason: "another-run-holds-lock",
		}, nil
	}
	defer lock.Release()

	mode := EffectiveMode(opts.ModeOverride, settings)
	seenPath := filepath.Join(settings.StateDir, "seen.json")
	pendingPath := filepath.Join(settings.StateDir, "pending.json")
	escPath := filepath.Join(settings.StateDir, "escalations.json")
	verdictsPath := filepath.Join(settings.StateDir, "verdicts.jsonl")
	runLogPath := filepath.Join(settings.StateDir, "run.log")

	seen := map[string]string{}
	readJSONFile(seenPath, &seen)
	seen = pruneSeen(seen, now, settings.SeenRetention)

	var escalations []EscalationEntry
	readJSONFile(escPath, &escalations)
	escalations = pruneEscalations(escalations, now)

	// Flatten mentions from report and keep a space-message pool for fallback context.
	var found []Candidate
	spacePool := map[string][]gchat.Message{}
	if report != nil {
		for _, sm := range report.Spaces {
			for _, m := range sm.Mentions {
				if m.SpaceName == "" {
					m.SpaceName = sm.SpaceName
				}
				if m.SpaceType == "" {
					m.SpaceType = sm.SpaceType
				}
				spacePool[m.SpaceID] = append(spacePool[m.SpaceID], m)
				found = append(found, Candidate{
					Message:   m,
					Broadcast: gchat.IsBroadcast(m.Text),
				})
			}
		}
	}

	summary := &gchat.UrgentSummary{
		Mode:       mode,
		Candidates: len(found),
	}

	if opts.InitOnly {
		nowStr := now.Format(time.RFC3339)
		for _, c := range found {
			if c.Message.ID != "" {
				if _, exists := seen[c.Message.ID]; !exists {
					seen[c.Message.ID] = nowStr
				}
			}
		}
		if err := atomicWriteJSON(seenPath, seen); err != nil {
			return nil, err
		}
		if err := atomicWriteJSON(pendingPath, []Candidate{}); err != nil {
			return nil, err
		}
		logLine := fmt.Sprintf("%s run=init escalation=%s exit=0 candidates=%d seen=%d total_s=%.1f",
			now.In(settings.Location).Format(time.RFC3339), mode, len(found), len(seen), time.Since(started).Seconds())
		_ = appendLine(runLogPath, logLine)
		return summary, nil
	}

	// Merge overflow from prior run with unseen candidates from this poll.
	var priorPending []Candidate
	readJSONFile(pendingPath, &priorPending)

	byID := map[string]Candidate{}
	for _, p := range priorPending {
		if p.Message.ID != "" && seen[p.Message.ID] == "" {
			byID[p.Message.ID] = p
		}
	}
	for _, c := range found {
		if c.Message.ID != "" && seen[c.Message.ID] == "" {
			if _, exists := byID[c.Message.ID]; !exists {
				byID[c.Message.ID] = c
			}
		}
	}
	summary.New = len(byID)

	cutoff := now.Add(-settings.MaxAge)
	var todoAll []Candidate
	nowStr := now.Format(time.RFC3339)
	for id, c := range byID {
		if !c.Message.Time.IsZero() && c.Message.Time.Before(cutoff) {
			seen[id] = nowStr
			summary.Stale++
			continue
		}
		todoAll = append(todoAll, c)
	}
	if err := atomicWriteJSON(seenPath, seen); err != nil {
		return nil, err
	}

	// Newest first so urgent recent messages are prioritized under cap.
	sort.Slice(todoAll, func(i, j int) bool {
		return todoAll[i].Message.Time.After(todoAll[j].Message.Time)
	})

	limit := settings.MaxPerRun
	if opts.MaxOverride > 0 {
		limit = opts.MaxOverride
	}
	var todo, overflow []Candidate
	if len(todoAll) > limit {
		todo = todoAll[:limit]
		overflow = todoAll[limit:]
	} else {
		todo = todoAll
	}
	summary.Overflow = len(overflow)
	if err := atomicWriteJSON(pendingPath, overflow); err != nil {
		return nil, err
	}

	retryDelay := opts.RetryDelay
	if retryDelay == 0 {
		retryDelay = 1500 * time.Millisecond
	}

	for _, c := range todo {
		msg := c.Message
		var threadMsgs []gchat.Message
		if opts.ContextFetch != nil && msg.Thread != "" {
			if fetched, err := opts.ContextFetch.FetchThreadContext(ctx, msg, maxContext+1); err == nil {
				threadMsgs = fetched
			}
		}
		ctxMsgs := BuildContext(msg, threadMsgs, spacePool[msg.SpaceID])
		statePayload := BuildState(msg, ctxMsgs, settings.Location)

		verdicts := ClassifyAll(ctx, statePayload, settings, opts.HTTPClient, retryDelay)
		actionable := IsActionable(verdicts, settings.Rule)
		wouldEsc := ShouldEscalate(verdicts, settings.Rule, settings)

		rec := VerdictRecord{
			TSUTC:         nowFn().UTC().Format(time.RFC3339),
			TSLocal:       FormatLocal(msg.Time, settings.Location),
			MessageAt:     msg.Time.UTC().Format(time.RFC3339),
			MessageLink:   msg.Link,
			MessageID:     msg.ID,
			SpaceID:       msg.SpaceID,
			SpaceName:     msg.SpaceName,
			SpaceType:     msg.SpaceType,
			Sender:        msg.Sender,
			Broadcast:     c.Broadcast,
			TextExcerpt:   TruncateRunes(strings.ReplaceAll(msg.Text, "\n", " "), 160),
			Verdicts:      verdicts,
			Actionable:    actionable,
			Agree:         VerdictsAgree(verdicts),
			WouldEscalate: wouldEsc,
		}

		summary.Classified++
		for _, v := range verdicts {
			if v.Error != "" {
				summary.ProviderErrors++
			}
			if v.CostUSD != nil {
				summary.CostUSD += *v.CostUSD
			}
		}

		if wouldEsc {
			summary.UrgentCount++
			escNow := nowFn().UTC()
			ok, why := RateLimitOK(escalations, escNow, mode, settings)
			if !ok {
				rec.Escalation = EscalationInfo{
					Mode:    "rate-limited",
					Actions: []string{"rate-limited:" + why},
				}
			} else {
				info := Escalate(ctx, rec, msg.Text, ctxMsgs, settings, mode, false, escNow, opts.HTTPClient, opts.CmdRunner, retryDelay)
				rec.Escalation = info
				escalations = append(escalations, EscalationEntry{
					TSUTC:     escNow.Format(time.RFC3339),
					LocalDate: escNow.In(settings.Location).Format("2006-01-02"),
					Mode:      info.Mode,
					MessageID: msg.ID,
					Dir:       info.Dir,
					Actions:   info.Actions,
				})
				if err := atomicWriteJSON(escPath, escalations); err != nil {
					return nil, err
				}
				summary.Escalations++
			}
		} else {
			rec.Escalation = EscalationInfo{Mode: "skipped", Actions: []string{}}
		}

		item := buildSummaryItem(msg, rec, verdicts, c.Broadcast)
		if actionable {
			summary.ActionableCount++
			summary.ActionableItems = append(summary.ActionableItems, item)
		}
		if wouldEsc {
			summary.UrgentItems = append(summary.UrgentItems, item)
		}

		lineBytes, err := json.Marshal(rec)
		if err != nil {
			return nil, fmt.Errorf("marshal verdict record: %w", err)
		}
		if err := appendLine(verdictsPath, string(lineBytes)); err != nil {
			return nil, fmt.Errorf("append verdicts.jsonl: %w", err)
		}
		seen[msg.ID] = nowFn().UTC().Format(time.RFC3339)
		if err := atomicWriteJSON(seenPath, seen); err != nil {
			return nil, err
		}
	}

	if err := atomicWriteJSON(escPath, escalations); err != nil {
		return nil, err
	}

	summary.CostUSD = roundTo(summary.CostUSD, 6)
	logLine := fmt.Sprintf(
		"%s run=scheduled escalation=%s exit=0 candidates=%d new=%d stale=%d classified=%d overflow=%d actionable=%d urgent=%d provider_errors=%d escalations=%d cost_usd=%.6f total_s=%.1f",
		now.In(settings.Location).Format(time.RFC3339),
		mode, summary.Candidates, summary.New, summary.Stale, summary.Classified, summary.Overflow,
		summary.ActionableCount, summary.UrgentCount, summary.ProviderErrors, summary.Escalations, summary.CostUSD,
		time.Since(started).Seconds(),
	)
	_ = appendLine(runLogPath, logLine)

	return summary, nil
}

func buildSummaryItem(msg gchat.Message, rec VerdictRecord, verdicts []ProviderVerdict, broadcast bool) gchat.UrgentItem {
	var needsAct, urgScore *float64
	reason := ""
	for _, v := range verdicts {
		if v.Error != "" {
			continue
		}
		if needsAct == nil && v.NeedsAction != nil {
			needsAct = v.NeedsAction
		}
		if urgScore == nil && v.UrgentScore != nil {
			urgScore = v.UrgentScore
		}
		if reason == "" && v.Reason != "" {
			reason = v.Reason
		}
	}
	return gchat.UrgentItem{
		MessageID:   msg.ID,
		SpaceName:   msg.SpaceName,
		SpaceType:   msg.SpaceType,
		Sender:      msg.Sender,
		TextExcerpt: rec.TextExcerpt,
		Link:        msg.Link,
		Time:        rec.TSLocal,
		Broadcast:   broadcast,
		Actionable:  rec.Actionable,
		NeedsAction: needsAct,
		UrgentScore: urgScore,
		Escalation:  rec.Escalation.Mode,
		Reason:      reason,
	}
}

// RunTest exercises classification and dry-run escalation against a synthetic outage message.
// It never posts to webhooks or spawns commands live.
func RunTest(ctx context.Context, settings *Settings, offline bool, opts RunOptions) (*VerdictRecord, error) {
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn().UTC()

	if err := os.MkdirAll(settings.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create urgent state dir: %w", err)
	}

	who := settings.UserName
	if who == "" {
		who = "Recipient"
	}

	msg := gchat.Message{
		ID:        "spaces/TESTSPACE/messages/TESTTHREAD.TESTMSG0001",
		SpaceID:   "spaces/TESTSPACE",
		SpaceName: "TEST (synthetic, not a real space)",
		SpaceType: "SPACE",
		Sender:    "Synthetic Teammate",
		Text: fmt.Sprintf("@%s PROD DOWN: payments API returning 500 for all users for 10 minutes. "+
			"Need your approval to roll back today's deploy right now; nobody else can sign off.", who),
		Time:   now.Add(-2 * time.Minute),
		Thread: "spaces/TESTSPACE/threads/TESTTHREAD",
	}
	ctxMsg := gchat.Message{
		ID:        "spaces/TESTSPACE/messages/TESTTHREAD.TESTCTX0001",
		SpaceID:   "spaces/TESTSPACE",
		SpaceName: msg.SpaceName,
		SpaceType: "SPACE",
		Sender:    "Alertmanager",
		Text:      "FIRING: payments-api 5xx rate 98% (threshold 5%)",
		Time:      now.Add(-5 * time.Minute),
		Thread:    msg.Thread,
	}

	ctxMsgs := BuildContext(msg, []gchat.Message{ctxMsg}, nil)
	statePayload := BuildState(msg, ctxMsgs, settings.Location)

	var verdicts []ProviderVerdict
	if offline || len(settings.Providers) == 0 {
		actScore, urgScore := 0.96, 0.95
		if len(settings.Providers) == 0 {
			verdicts = []ProviderVerdict{{
				Name:        "synthetic",
				Provider:    "offline",
				Model:       "canned",
				Actionable:  true,
				Urgent:      true,
				NeedsAction: &actScore,
				UrgentScore: &urgScore,
				Confidence:  0.90,
				Reason:      "offline canned test verdict",
			}}
		} else {
			for _, p := range settings.Providers {
				name := p.Name
				if name == "" {
					name = p.Type
				}
				verdicts = append(verdicts, ProviderVerdict{
					Name:        name,
					Provider:    p.Type,
					Model:       p.Model,
					Actionable:  true,
					Urgent:      true,
					NeedsAction: &actScore,
					UrgentScore: &urgScore,
					Confidence:  0.90,
					Reason:      "offline canned test verdict",
				})
			}
		}
	} else {
		retryDelay := opts.RetryDelay
		if retryDelay == 0 {
			retryDelay = 1500 * time.Millisecond
		}
		verdicts = ClassifyAll(ctx, statePayload, settings, opts.HTTPClient, retryDelay)
	}

	actionable := IsActionable(verdicts, settings.Rule)
	wouldEsc := ShouldEscalate(verdicts, settings.Rule, settings)
	rec := VerdictRecord{
		TSUTC:         now.Format(time.RFC3339),
		TSLocal:       FormatLocal(msg.Time, settings.Location),
		MessageAt:     msg.Time.Format(time.RFC3339),
		MessageID:     msg.ID,
		SpaceID:       msg.SpaceID,
		SpaceName:     msg.SpaceName,
		SpaceType:     msg.SpaceType,
		Sender:        msg.Sender,
		Broadcast:     false,
		TextExcerpt:   TruncateRunes(msg.Text, 160),
		Verdicts:      verdicts,
		Actionable:    actionable,
		Agree:         VerdictsAgree(verdicts),
		WouldEscalate: wouldEsc,
		Test:          true,
	}
	if !wouldEsc {
		rec.Note = "providers did not reach threshold; forcing dry-run escalation path for test"
	}
	rec.Escalation = Escalate(ctx, rec, msg.Text, ctxMsgs, settings, "dry-run", true, now, opts.HTTPClient, opts.CmdRunner, opts.RetryDelay)
	return &rec, nil
}
