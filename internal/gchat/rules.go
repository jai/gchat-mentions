package gchat

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// SpaceMentions holds unread mentions for a single space.
type SpaceMentions struct {
	SpaceName string    `json:"space_name"`
	SpaceID   string    `json:"space_id"`
	SpaceType string    `json:"space_type,omitempty"`
	Mentions  []Message `json:"mentions"`
}

// UrgentSummary captures optional urgency classification results attached to a Report.
type UrgentSummary struct {
	Mode            string       `json:"mode"`
	Candidates      int          `json:"candidates"`
	New             int          `json:"new"`
	Stale           int          `json:"stale"`
	Classified      int          `json:"classified"`
	Overflow        int          `json:"overflow"`
	ActionableCount int          `json:"actionable_count"`
	UrgentCount     int          `json:"urgent_count"`
	Escalations     int          `json:"escalations"`
	ProviderErrors  int          `json:"provider_errors"`
	CostUSD         float64      `json:"cost_usd"`
	Skipped         bool         `json:"skipped,omitempty"`
	SkipReason      string       `json:"skip_reason,omitempty"`
	ActionableItems []UrgentItem `json:"actionable_items,omitempty"`
	UrgentItems     []UrgentItem `json:"urgent_items,omitempty"`
}

// UrgentItem is a concise summary of a mention flagged as actionable or urgent.
type UrgentItem struct {
	MessageID   string   `json:"message_id"`
	SpaceName   string   `json:"space_name"`
	SpaceType   string   `json:"space_type,omitempty"`
	Sender      string   `json:"sender"`
	TextExcerpt string   `json:"text_excerpt"`
	Link        string   `json:"link,omitempty"`
	Time        string   `json:"time"`
	Broadcast   bool     `json:"broadcast"`
	Actionable  bool     `json:"actionable"`
	NeedsAction *float64 `json:"needs_action,omitempty"`
	UrgentScore *float64 `json:"urgent_score,omitempty"`
	Escalation  string   `json:"escalation"`
	Reason      string   `json:"reason,omitempty"`
}

// Report is the normalized unread-mentions result written to state and stdout.
type Report struct {
	CheckedAt     time.Time       `json:"checked_at"`
	Account       string          `json:"account"`
	UserName      string          `json:"user_name,omitempty"`
	TotalMentions int             `json:"total_mentions"`
	Spaces        []SpaceMentions `json:"spaces,omitempty"`
	Errors        []string        `json:"errors,omitempty"`
	Urgent        *UrgentSummary  `json:"urgent,omitempty"`
}

// Rule matches messages of interest.
type Rule interface {
	Match(msg Message) bool
}

// MentionRule matches messages that mention a user by ID token, display name,
// broadcast (@all/@here), or direct message (when IncludeDMs is true).
type MentionRule struct {
	UserID      string
	DisplayName string
	IncludeDMs  bool
}

// IsBroadcast reports whether text contains a word-bounded @all or @here mention.
func IsBroadcast(text string) bool {
	return containsWord(text, "@all") || containsWord(text, "@here")
}

// Match returns true when msg mentions the user (excluding self-sent messages).
func (r *MentionRule) Match(msg Message) bool {
	if r.UserID != "" && msg.Sender == r.UserID {
		return false
	}
	if r.DisplayName != "" && strings.EqualFold(strings.TrimSpace(msg.Sender), strings.TrimSpace(r.DisplayName)) {
		return false
	}
	if r.IncludeDMs && msg.SpaceType == "DIRECT_MESSAGE" {
		return true
	}
	text := msg.Text
	if IsBroadcast(text) {
		return true
	}
	if r.UserID != "" && strings.Contains(text, "<"+r.UserID+">") {
		return true
	}
	if r.DisplayName != "" && containsWord(text, r.DisplayName) {
		return true
	}
	return false
}

func containsWord(text, word string) bool {
	lower := strings.ToLower(text)
	target := strings.ToLower(word)
	if target == "" {
		return false
	}
	for offset := 0; offset <= len(lower)-len(target); {
		idx := strings.Index(lower[offset:], target)
		if idx < 0 {
			return false
		}
		start := offset + idx
		end := start + len(target)

		leftOK := true
		if start > 0 {
			prev, _ := utf8.DecodeLastRuneInString(lower[:start])
			leftOK = !isWordChar(prev)
		}
		rightOK := true
		if end < len(lower) {
			next, _ := utf8.DecodeRuneInString(lower[end:])
			rightOK = !isWordChar(next)
		}
		if leftOK && rightOK {
			return true
		}
		_, width := utf8.DecodeRuneInString(lower[start:])
		if width <= 0 {
			width = 1
		}
		offset = start + width
	}
	return false
}

func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Filter suppresses false-positive matches after rules run.
type Filter interface {
	Suppress(msg Message) bool
}

// AtAllOnlyFilter suppresses @all/@here-only matches in announcement spaces while
// keeping direct mentions by user ID or display name.
type AtAllOnlyFilter struct {
	SpaceNames  []string
	DisplayName string
	UserID      string
}

// Suppress returns true if msg is in an announcement space and only matched @all/@here.
func (f *AtAllOnlyFilter) Suppress(msg Message) bool {
	if !f.matchesSpace(msg.SpaceName) {
		return false
	}
	if f.UserID != "" && strings.Contains(msg.Text, "<"+f.UserID+">") {
		return false
	}
	if f.DisplayName != "" && containsWord(msg.Text, f.DisplayName) {
		return false
	}
	return true
}

func (f *AtAllOnlyFilter) matchesSpace(spaceName string) bool {
	lower := strings.ToLower(spaceName)
	for _, name := range f.SpaceNames {
		if name != "" && strings.Contains(lower, strings.ToLower(name)) {
			return true
		}
	}
	return false
}

// TextPrefixFilter suppresses automated messages starting with any configured prefix.
type TextPrefixFilter struct {
	Prefixes []string
}

// Suppress returns true if msg.Text begins with one of Prefixes (case-insensitive).
func (f *TextPrefixFilter) Suppress(msg Message) bool {
	lower := strings.ToLower(strings.TrimSpace(msg.Text))
	for _, p := range f.Prefixes {
		if p != "" && strings.HasPrefix(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// ApplyFilters removes suppressed messages from report in place.
func ApplyFilters(report *Report, filters []Filter) {
	if report == nil || len(filters) == 0 {
		return
	}
	var kept []SpaceMentions
	total := 0
	for _, sm := range report.Spaces {
		var filtered []Message
		for _, m := range sm.Mentions {
			suppressed := false
			for _, f := range filters {
				if f.Suppress(m) {
					suppressed = true
					break
				}
			}
			if !suppressed {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) > 0 {
			sm.Mentions = filtered
			kept = append(kept, sm)
			total += len(filtered)
		}
	}
	report.Spaces = kept
	report.TotalMentions = total
}
