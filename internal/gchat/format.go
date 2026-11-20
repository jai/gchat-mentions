package gchat

import (
	"fmt"
	"strings"
)

// FormatNotification formats a Report into Google Chat message chunks (<= 4000 chars each).
func FormatNotification(report *Report) []string {
	if report == nil || report.TotalMentions == 0 {
		return nil
	}

	const maxChunk = 4000

	noun := "mentions"
	if report.TotalMentions == 1 {
		noun = "mention"
	}

	var allLines []string
	allLines = append(allLines, fmt.Sprintf("📬 *Unread %s* — %d across %d spaces\n\n",
		noun, report.TotalMentions, len(report.Spaces)))

	var immediateLines []string
	var announcementLines []string
	for _, sm := range report.Spaces {
		for _, m := range sm.Mentions {
			text := truncate(strings.ReplaceAll(m.Text, "\n", " "), 100)
			line := fmt.Sprintf("  → *%s:* %s\n", sm.SpaceName, text)
			link := m.Link
			if link == "" {
				link = MessageLink(m)
			}
			if link != "" {
				if canUseChatLinkLabel(text) {
					line = fmt.Sprintf("  → *%s:* <%s|%s>\n", sm.SpaceName, link, text)
				} else {
					line = fmt.Sprintf("%s    %s\n", line, link)
				}
			}
			if m.Category == CategoryAnnouncement {
				announcementLines = append(announcementLines, line)
				continue
			}
			immediateLines = append(immediateLines, line)
		}
	}

	appendBucket := func(title string, lines []string) {
		if len(lines) == 0 {
			return
		}
		allLines = append(allLines, fmt.Sprintf("*%s* (%d)\n", title, len(lines)))
		allLines = append(allLines, lines...)
		allLines = append(allLines, "\n")
	}
	appendBucket("Needs immediate response", immediateLines)
	appendBucket("Announcements", announcementLines)

	var chunks []string
	var current strings.Builder
	for _, line := range allLines {
		if current.Len()+len(line) > maxChunk && current.Len() > 0 {
			chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
			current.Reset()
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
	}
	return chunks
}

// FormatCompact renders a short Markdown summary with clickable message links.
func FormatCompact(report *Report) string {
	if report == nil || report.TotalMentions == 0 {
		return "Google Chat: 0 unread mentions\n"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Google Chat: %d unread mentions across %d spaces\n\n",
		report.TotalMentions, len(report.Spaces))
	for _, sm := range report.Spaces {
		for _, m := range sm.Mentions {
			text := truncate(strings.ReplaceAll(m.Text, "\n", " "), 70)
			link := m.Link
			if link == "" {
				link = MessageLink(m)
			}
			if link != "" {
				fmt.Fprintf(&sb, "- [%s](%s) — %s\n", escapeMarkdownLinkLabel(text), link, sm.SpaceName)
			} else {
				fmt.Fprintf(&sb, "- %s — %s\n", text, sm.SpaceName)
			}
		}
	}
	return sb.String()
}

// FormatText renders a plain-text summary suitable for terminal reading.
func FormatText(report *Report) string {
	if report == nil || report.TotalMentions == 0 {
		return "No unread mentions.\n"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Unread mentions: %d across %d spaces\n", report.TotalMentions, len(report.Spaces))
	for _, sm := range report.Spaces {
		fmt.Fprintf(&sb, "\n%s (%d)\n", sm.SpaceName, len(sm.Mentions))
		for _, m := range sm.Mentions {
			text := truncate(strings.ReplaceAll(m.Text, "\n", " "), 120)
			if m.Sender != "" {
				fmt.Fprintf(&sb, "  - %s: %s\n", m.Sender, text)
			} else {
				fmt.Fprintf(&sb, "  - %s\n", text)
			}
			link := m.Link
			if link == "" {
				link = MessageLink(m)
			}
			if link != "" {
				fmt.Fprintf(&sb, "    %s\n", link)
			}
		}
	}
	return sb.String()
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

func canUseChatLinkLabel(label string) bool {
	return !strings.ContainsAny(label, "<>|")
}

func escapeMarkdownLinkLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `[`, `\[`)
	s = strings.ReplaceAll(s, `]`, `\]`)
	return s
}
