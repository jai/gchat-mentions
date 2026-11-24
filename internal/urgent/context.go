package urgent

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/jai/gchat-mentions/internal/gchat"
)

const briefUntrustedMarker = "=== END UNTRUSTED DATA ==="

// ScrubBrief neutralizes end-marker spoofing inside downstream agent briefs.
func ScrubBrief(text string) string {
	return strings.ReplaceAll(text, briefUntrustedMarker, "[marker removed]")
}

// TruncateRunes trims s to at most maxLen runes.
func TruncateRunes(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

// FormatLocal formats t in loc with timezone abbreviation.
func FormatLocal(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}
	lt := t.In(loc)
	return lt.Format("2006-01-02 15:04 Mon (MST)")
}

// BuildContext selects up to 5 preceding thread messages, padding from same-space
// messages when the thread has fewer than 3 preceding messages.
func BuildContext(target gchat.Message, threadMsgs, spaceMsgs []gchat.Message) []ContextMessage {
	seen := map[string]bool{target.ID: true}
	var thread []gchat.Message
	for _, m := range threadMsgs {
		if m.ID == "" || seen[m.ID] {
			continue
		}
		if !target.Time.IsZero() && !m.Time.IsZero() && !m.Time.Before(target.Time) {
			continue
		}
		if target.Thread != "" && m.Thread != "" && m.Thread != target.Thread {
			continue
		}
		seen[m.ID] = true
		thread = append(thread, m)
	}
	sort.Slice(thread, func(i, j int) bool { return thread[i].Time.Before(thread[j].Time) })
	if len(thread) > maxContext {
		thread = thread[len(thread)-maxContext:]
	}

	ctx := make([]ContextMessage, 0, maxContext)
	for _, m := range thread {
		ctx = append(ctx, ContextMessage{
			Label:  "thread",
			Sender: m.Sender,
			At:     m.Time,
			Text:   TruncateRunes(m.Text, maxMsgChars),
		})
	}

	if len(thread) < minThreadCtx && len(spaceMsgs) > 0 {
		var others []gchat.Message
		for _, m := range spaceMsgs {
			if m.ID == "" || seen[m.ID] {
				continue
			}
			if m.SpaceID != "" && target.SpaceID != "" && m.SpaceID != target.SpaceID {
				continue
			}
			if !target.Time.IsZero() && !m.Time.IsZero() && !m.Time.Before(target.Time) {
				continue
			}
			seen[m.ID] = true
			others = append(others, m)
		}
		sort.Slice(others, func(i, j int) bool { return others[i].Time.Before(others[j].Time) })
		need := maxContext - len(thread)
		if len(others) > need {
			others = others[len(others)-need:]
		}
		for _, m := range others {
			ctx = append(ctx, ContextMessage{
				Label:  "same space, other thread",
				Sender: m.Sender,
				At:     m.Time,
				Text:   TruncateRunes(m.Text, maxMsgChars),
			})
		}
		sort.Slice(ctx, func(i, j int) bool { return ctx[i].At.Before(ctx[j].At) })
	}

	return ctx
}

// BuildState constructs a clean structured JSON StatePayload for Clef/Jev and LLM classifiers,
// keeping serialized size within 4000 bytes by preserving the newest context messages.
func BuildState(target gchat.Message, ctx []ContextMessage, loc *time.Location) StatePayload {
	if loc == nil {
		loc = time.UTC
	}
	spaceDisplay := strings.TrimSpace(target.SpaceName)
	if spaceDisplay == "" {
		if target.SpaceType == "DIRECT_MESSAGE" {
			spaceDisplay = "(direct message)"
		} else {
			spaceDisplay = target.SpaceID
		}
	}
	spaceType := target.SpaceType
	if spaceType == "" {
		spaceType = "SPACE"
	}

	payload := StatePayload{
		Space: StateSpace{
			Name: spaceDisplay,
			Type: spaceType,
		},
		Context: []StateContextMsg{},
		TargetMessage: StateTargetMsg{
			Sender: target.Sender,
			At:     formatISOInLoc(target.Time, loc),
			Text:   TruncateRunes(target.Text, maxMsgChars),
		},
	}

	// Add context newest-first until the serialized state reaches maxStateChars.
	for i := len(ctx) - 1; i >= 0; i-- {
		m := ctx[i]
		scope := m.Label
		if scope == "" {
			scope = "thread"
		}
		entry := StateContextMsg{
			Scope:  scope,
			Sender: m.Sender,
			At:     formatISOInLoc(m.At, loc),
			Text:   TruncateRunes(m.Text, maxMsgChars),
		}
		candidate := append([]StateContextMsg{entry}, payload.Context...)
		probe := payload
		probe.Context = candidate
		raw, err := json.Marshal(probe)
		if err != nil || len(raw) > maxStateChars {
			break
		}
		payload.Context = candidate
	}

	return payload
}

func formatISOInLoc(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format(time.RFC3339)
}
