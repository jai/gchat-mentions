package gchat

import (
	"strings"
	"time"
	"unicode"
)

const (
	CategoryImmediateResponse = "immediate_response"
	CategoryAnnouncement      = "announcement"
)

// SpaceMentions holds unread mentions for a single space.
type SpaceMentions struct {
	SpaceName string    `json:"space_name"`
	SpaceID   string    `json:"space_id"`
	Mentions  []Message `json:"mentions"`
}

// Report is the normalized unread-mentions result written to state and stdout.
type Report struct {
	CheckedAt     time.Time       `json:"checked_at"`
	Account       string          `json:"account"`
	UserName      string          `json:"user_name,omitempty"`
	TotalMentions int             `json:"total_mentions"`
	Spaces        []SpaceMentions `json:"spaces,omitempty"`
	Errors        []string        `json:"errors,omitempty"`
}

// Rule matches messages of interest.
type Rule interface {
	Match(msg Message) bool
}

// MentionRule matches messages that mention a user by ID token, display name, or @all.
type MentionRule struct {
	UserID      string
	DisplayName string
}

// Match returns true when msg mentions the user (excluding self-sent messages).
func (r *MentionRule) Match(msg Message) bool {
	if r.UserID != "" && msg.Sender == r.UserID {
		return false
	}
	if r.DisplayName != "" && strings.EqualFold(strings.TrimSpace(msg.Sender), strings.TrimSpace(r.DisplayName)) {
		return false
	}
	text := msg.Text
	if containsWord(text, "@all") {
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
	for i := 0; i <= len(lower)-len(target); i++ {
		if lower[i:i+len(target)] != target {
			continue
		}
		if i > 0 && isWordChar(rune(lower[i-1])) {
			continue
		}
		end := i + len(target)
		if end < len(lower) && isWordChar(rune(lower[end])) {
			continue
		}
		return true
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

// AtAllOnlyFilter suppresses @all-only matches in announcement spaces while
// keeping direct mentions by user ID or display name.
type AtAllOnlyFilter struct {
	SpaceNames  []string
	DisplayName string
	UserID      string
}

// Suppress returns true if msg is in an announcement space and only matched @all.
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
