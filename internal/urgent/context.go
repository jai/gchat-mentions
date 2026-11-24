package urgent

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jai/gchat-mentions/internal/gchat"
)

// Scrub neutralizes delimiter tokens inside untrusted chat content.
func Scrub(text string) string {
	s := strings.ReplaceAll(text, DataBegin, "[delimiter removed]")
	return strings.ReplaceAll(s, DataEnd, "[delimiter removed]")
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

// BuildState constructs the prompt-injection-delimited classifier input,
// fitting within 4000 characters by preserving the newest context messages.
func BuildState(target gchat.Message, ctx []ContextMessage, broadcast bool, loc *time.Location) string {
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

	head := []string{
		DataBegin,
		fmt.Sprintf("space: %s [type=%s]", spaceDisplay, spaceType),
		fmt.Sprintf("broadcast_mention: %t", broadcast),
		"",
	}
	tgt := []string{
		"TARGET message (classify this one):",
		fmt.Sprintf("from: %s at %s", target.Sender, FormatLocal(target.Time, loc)),
		Scrub(TruncateRunes(target.Text, maxMsgChars)),
		DataEnd,
	}

	used := 0
	for _, s := range head {
		used += len(s) + 1
	}
	for _, s := range tgt {
		used += len(s) + 1
	}
	headerMid := "CONTEXT (earlier messages, oldest first):"
	used += len(headerMid) + 2

	budget := maxStateChars - used
	var kept []string
	for i := len(ctx) - 1; i >= 0; i-- {
		m := ctx[i]
		label := m.Label
		if label == "" {
			label = "thread"
		}
		line := fmt.Sprintf("- [%s] %s at %s: %s",
			label, m.Sender, FormatLocal(m.At, loc), Scrub(TruncateRunes(m.Text, maxMsgChars)))
		if len(line)+1 > budget {
			break
		}
		budget -= len(line) + 1
		kept = append([]string{line}, kept...)
	}

	mid := []string{headerMid}
	if len(kept) == 0 {
		mid = append(mid, "(none)")
	} else {
		mid = append(mid, kept...)
	}
	mid = append(mid, "")

	all := make([]string, 0, len(head)+len(mid)+len(tgt))
	all = append(all, head...)
	all = append(all, mid...)
	all = append(all, tgt...)
	return strings.Join(all, "\n")
}
