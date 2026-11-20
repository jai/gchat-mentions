package gchat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockCall struct {
	name string
	args []string
}

type mockExecutor struct {
	mu      sync.Mutex
	handler func(ctx context.Context, name string, args ...string) ([]byte, error)
	calls   []mockCall
}

func (m *mockExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cp := append([]string(nil), args...)
	m.mu.Lock()
	m.calls = append(m.calls, mockCall{name: name, args: cp})
	m.mu.Unlock()
	return m.handler(ctx, name, args...)
}

func TestFetchSpaces(t *testing.T) {
	exec := &mockExecutor{
		handler: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			tsv := strings.Join([]string{
				"RESOURCE\tDISPLAY_NAME\tTYPE",
				"spaces/AAA\tEngineering\tSPACE",
				"spaces/BBB\tDirect Chat\tDIRECT_MESSAGE",
				"malformed-line-without-tabs",
				"spaces/CCC\tAnnouncements\tSPACE",
			}, "\n")
			return []byte(tsv), nil
		},
	}

	f := NewFetcher(exec, "alice@example.com")
	spaces, err := f.FetchSpaces(context.Background())
	if err != nil {
		t.Fatalf("FetchSpaces() error: %v", err)
	}
	if len(spaces) != 3 {
		t.Fatalf("len(spaces) = %d, want 3: %+v", len(spaces), spaces)
	}
	if spaces[0] != (Space{ID: "spaces/AAA", Name: "Engineering", Type: "SPACE"}) {
		t.Fatalf("spaces[0] = %+v", spaces[0])
	}
	if spaces[1] != (Space{ID: "spaces/BBB", Name: "Direct Chat", Type: "DIRECT_MESSAGE"}) {
		t.Fatalf("spaces[1] = %+v", spaces[1])
	}
	if spaces[2] != (Space{ID: "spaces/CCC", Name: "Announcements", Type: "SPACE"}) {
		t.Fatalf("spaces[2] = %+v", spaces[2])
	}
}

func TestFetchMessages(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantLen  int
		wantLink string
	}{
		{
			name: "bare array",
			payload: `[{
				"resource": "spaces/AAA/messages/thr1.msg1",
				"sender": "Bob",
				"text": "Hey Alice",
				"createTime": "2026-01-02T03:04:05Z",
				"thread": "spaces/AAA/threads/thr1"
			}]`,
			wantLen:  1,
			wantLink: "https://chat.google.com/room/AAA/thr1/msg1?cls=10",
		},
		{
			name: "envelope object",
			payload: `{"messages":[{
				"resource": "spaces/AAA/messages/msg2",
				"sender": "Carol",
				"text": "Ping @all",
				"createTime": "2026-01-02T04:05:06Z",
				"thread": "spaces/AAA/threads/thr2"
			}]}`,
			wantLen:  1,
			wantLink: "https://chat.google.com/room/AAA/thr2/msg2?cls=10",
		},
		{
			name:    "empty output",
			payload: "   \n",
			wantLen: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exec := &mockExecutor{
				handler: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
					return []byte(tc.payload), nil
				},
			}
			f := NewFetcher(exec, "alice@example.com")
			msgs, err := f.FetchMessages(context.Background(), "spaces/AAA", FetchOptions{UnreadOnly: true, All: true})
			if err != nil {
				t.Fatalf("FetchMessages() error: %v", err)
			}
			if len(msgs) != tc.wantLen {
				t.Fatalf("len(msgs) = %d, want %d", len(msgs), tc.wantLen)
			}
			if tc.wantLen > 0 {
				if msgs[0].Link != tc.wantLink {
					t.Fatalf("Link = %q, want %q", msgs[0].Link, tc.wantLink)
				}
				if !msgs[0].Unread {
					t.Fatalf("Unread = false, want true")
				}
				if msgs[0].SpaceID != "spaces/AAA" {
					t.Fatalf("SpaceID = %q, want spaces/AAA", msgs[0].SpaceID)
				}
			}
		})
	}
}

func TestFilterSpacesByName(t *testing.T) {
	all := []Space{
		{ID: "spaces/1", Name: "Engineering", Type: "SPACE"},
		{ID: "spaces/2", Name: "Engineering TEST", Type: "SPACE"},
		{ID: "spaces/3", Name: "Direct Engineering", Type: "DIRECT_MESSAGE"},
		{ID: "spaces/4", Name: "Platform", Type: "SPACE"},
	}

	got := FilterSpacesByName(all, "eng", "test")
	if len(got) != 1 || got[0].ID != "spaces/1" {
		t.Fatalf("FilterSpacesByName() = %+v, want only spaces/1", got)
	}

	gotAll := FilterSpacesByName(all, "", "test")
	if len(gotAll) != 2 || gotAll[0].ID != "spaces/1" || gotAll[1].ID != "spaces/4" {
		t.Fatalf("FilterSpacesByName(all) = %+v, want spaces/1 and spaces/4", gotAll)
	}
}

func TestMessageLink(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want string
	}{
		{
			name: "missing thread",
			msg:  Message{ID: "spaces/AAA/messages/thr1.msg1"},
			want: "",
		},
		{
			name: "malformed thread",
			msg:  Message{ID: "spaces/AAA/messages/thr1.msg1", Thread: "spaces/AAA"},
			want: "",
		},
		{
			name: "dot-delimited message ID",
			msg: Message{
				ID:     "spaces/AAA/messages/thr1.msg9",
				Thread: "spaces/AAA/threads/thr1",
			},
			want: "https://chat.google.com/room/AAA/thr1/msg9?cls=10",
		},
		{
			name: "plain message ID",
			msg: Message{
				ID:     "spaces/AAA/messages/msg9",
				Thread: "spaces/AAA/threads/thr1",
			},
			want: "https://chat.google.com/room/AAA/thr1/msg9?cls=10",
		},
		{
			name: "missing message ID falls back to thread link",
			msg: Message{
				ID:     "",
				Thread: "spaces/AAA/threads/thr1",
			},
			want: "https://chat.google.com/room/AAA/thr1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MessageLink(tc.msg); got != tc.want {
				t.Fatalf("MessageLink() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMentionRule(t *testing.T) {
	rule := &MentionRule{
		UserID:      "users/1001",
		DisplayName: "Alice",
	}

	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "word-boundary display name matches",
			msg:  Message{Sender: "Bob", Text: "Hey alice, can you review this?"},
			want: true,
		},
		{
			name: "substring inside another word does not match",
			msg:  Message{Sender: "Bob", Text: "No malice intended"},
			want: false,
		},
		{
			name: "user ID token matches",
			msg:  Message{Sender: "Bob", Text: "Paging <users/1001>"},
			want: true,
		},
		{
			name: "@all matches",
			msg:  Message{Sender: "Bob", Text: "Heads up @all deploy starts soon"},
			want: true,
		},
		{
			name: "@all respects word boundary",
			msg:  Message{Sender: "Bob", Text: "See @alligator notes"},
			want: false,
		},
		{
			name: "self excluded by user ID",
			msg:  Message{Sender: "users/1001", Text: "Reminder for @all and Alice"},
			want: false,
		},
		{
			name: "self excluded by display name",
			msg:  Message{Sender: " alice ", Text: "Reminder for @all"},
			want: false,
		},
		{
			name: "multibyte UTF-8 punctuation adjacent to display name matches",
			msg:  Message{Sender: "Bob", Text: "“Alice”—please review"},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rule.Match(tc.msg); got != tc.want {
				t.Fatalf("Match(%+v) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

func TestAtAllOnlyFilter(t *testing.T) {
	f := &AtAllOnlyFilter{
		SpaceNames:  []string{"announcements"},
		DisplayName: "Alice",
		UserID:      "users/1001",
	}

	if !f.Suppress(Message{SpaceName: "Company Announcements", Text: "Reminder @all"}) {
		t.Fatal("expected @all-only message in announcement space to be suppressed")
	}
	if f.Suppress(Message{SpaceName: "Company Announcements", Text: "@all and Alice please check"}) {
		t.Fatal("expected direct display-name mention in announcement space not to be suppressed")
	}
	if f.Suppress(Message{SpaceName: "Company Announcements", Text: "@all cc <users/1001>"}) {
		t.Fatal("expected direct user-ID mention in announcement space not to be suppressed")
	}
	if f.Suppress(Message{SpaceName: "Engineering", Text: "Reminder @all"}) {
		t.Fatal("expected @all message outside announcement space not to be suppressed")
	}
}

func TestTextPrefixFilter(t *testing.T) {
	f := &TextPrefixFilter{Prefixes: []string{"[bot]", "Daily digest:"}}

	if !f.Suppress(Message{Text: "  [BOT] deploy complete"}) {
		t.Fatal("expected matching prefix (case-insensitive, trimmed) to be suppressed")
	}
	if !f.Suppress(Message{Text: "daily digest: 3 items"}) {
		t.Fatal("expected second prefix to be suppressed")
	}
	if f.Suppress(Message{Text: "Hello [bot] world"}) {
		t.Fatal("expected non-prefix occurrence not to be suppressed")
	}
}

func TestFormatNotification(t *testing.T) {
	if got := FormatNotification(nil); got != nil {
		t.Fatalf("FormatNotification(nil) = %v, want nil", got)
	}
	if got := FormatNotification(&Report{}); got != nil {
		t.Fatalf("FormatNotification(empty) = %v, want nil", got)
	}

	report := &Report{
		TotalMentions: 2,
		Spaces: []SpaceMentions{
			{
				SpaceName: "Engineering",
				SpaceID:   "spaces/AAA",
				Mentions: []Message{
					{
						Text: "Clean label message",
						Link: "https://chat.google.com/room/AAA/t1/m1?cls=10",
					},
					{
						Text: "Check <users/1001> | urgent",
						Link: "https://chat.google.com/room/AAA/t2/m2?cls=10",
					},
				},
			},
		},
	}

	chunks := FormatNotification(report)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	body := chunks[0]
	if !strings.Contains(body, "📬 *Unread mentions* — 2 across 1 space") {
		t.Fatalf("unexpected notification header:\n%s", body)
	}
	if !strings.Contains(body, "<https://chat.google.com/room/AAA/t1/m1?cls=10|Clean label message>") {
		t.Fatalf("expected inline chat link for safe label, got:\n%s", body)
	}
	if !strings.Contains(body, "Check <users/1001> | urgent\n    https://chat.google.com/room/AAA/t2/m2?cls=10") {
		t.Fatalf("expected fallback link on next line for label with <>|, got:\n%s", body)
	}

	// Chunking when total output exceeds 4000 characters.
	var many []Message
	for i := 0; i < 60; i++ {
		many = append(many, Message{
			Text: fmt.Sprintf("Message %02d %s", i, strings.Repeat("x", 70)),
			Link: fmt.Sprintf("https://chat.google.com/room/AAA/t%d/m%d?cls=10", i, i),
		})
	}
	bigChunks := FormatNotification(&Report{
		TotalMentions: len(many),
		Spaces: []SpaceMentions{
			{SpaceName: "Engineering", SpaceID: "spaces/AAA", Mentions: many},
		},
	})
	if len(bigChunks) < 2 {
		t.Fatalf("expected multiple chunks for large report, got %d", len(bigChunks))
	}
	for i, c := range bigChunks {
		if len(c) > 4000 {
			t.Fatalf("chunk %d length = %d, exceeds 4000", i, len(c))
		}
	}
}

func TestFormatCompactAndText(t *testing.T) {
	if got := FormatCompact(&Report{}); got != "Google Chat: 0 unread mentions\n" {
		t.Fatalf("FormatCompact(empty) = %q", got)
	}
	if got := FormatText(&Report{}); got != "No unread mentions.\n" {
		t.Fatalf("FormatText(empty) = %q", got)
	}

	report := &Report{
		TotalMentions: 2,
		Spaces: []SpaceMentions{
			{
				SpaceName: "Engineering",
				SpaceID:   "spaces/AAA",
				Mentions: []Message{
					{
						Sender: "Bob",
						Text:   "Review [PR #42]",
						Link:   "https://chat.google.com/room/AAA/t1/m1?cls=10",
					},
					{
						Text: "No link message",
					},
				},
			},
		},
	}

	compact := FormatCompact(report)
	if !strings.Contains(compact, "Google Chat: 2 unread mentions across 1 space\n") {
		t.Fatalf("unexpected compact header:\n%s", compact)
	}
	if !strings.Contains(compact, `- [Review \[PR #42\]](https://chat.google.com/room/AAA/t1/m1?cls=10) — Engineering`) {
		t.Fatalf("expected escaped markdown link in compact output:\n%s", compact)
	}
	if !strings.Contains(compact, "- No link message — Engineering") {
		t.Fatalf("expected plain bullet for message without link:\n%s", compact)
	}

	text := FormatText(report)
	if !strings.Contains(text, "Unread mentions: 2 across 1 space\n") {
		t.Fatalf("unexpected text header:\n%s", text)
	}
	if !strings.Contains(text, "  - Bob: Review [PR #42]\n    https://chat.google.com/room/AAA/t1/m1?cls=10\n") {
		t.Fatalf("expected sender + link in text output:\n%s", text)
	}
	if !strings.Contains(text, "  - No link message\n") {
		t.Fatalf("expected senderless bullet in text output:\n%s", text)
	}
}

func TestPollEndToEnd(t *testing.T) {
	fixedNow := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	exec := &mockExecutor{
		handler: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if len(args) >= 3 && args[0] == "chat" && args[1] == "spaces" && args[2] == "list" {
				tsv := strings.Join([]string{
					"RESOURCE\tDISPLAY_NAME\tTYPE",
					"spaces/Z\tZeta\tSPACE",
					"spaces/A\tAlpha\tSPACE",
					"spaces/ANN\tAnnouncements\tSPACE",
					"spaces/ERR\tBroken\tSPACE",
					"spaces/SKIP\tSandbox TEST\tSPACE",
				}, "\n")
				return []byte(tsv), nil
			}
			if len(args) >= 4 && args[0] == "chat" && args[1] == "messages" && args[2] == "list" {
				switch args[3] {
				case "spaces/Z":
					return []byte(`[
						{"resource":"spaces/Z/messages/t1.m1","sender":"Bob","text":"Hey Alice","createTime":"2026-05-10T11:00:00Z","thread":"spaces/Z/threads/t1"},
						{"resource":"spaces/Z/messages/t2.m2","sender":"Bot","text":"[bot] Alice automated notice","createTime":"2026-05-10T11:05:00Z","thread":"spaces/Z/threads/t2"}
					]`), nil
				case "spaces/A":
					return []byte(`[
						{"resource":"spaces/A/messages/t1.m1","sender":"Carol","text":"Paging <users/1001>","createTime":"2026-05-10T10:00:00Z","thread":"spaces/A/threads/t1"},
						{"resource":"spaces/A/messages/t2.m2","sender":"Alice","text":"Self ping @all","createTime":"2026-05-10T10:01:00Z","thread":"spaces/A/threads/t2"}
					]`), nil
				case "spaces/ANN":
					return []byte(`[
						{"resource":"spaces/ANN/messages/t1.m1","sender":"Ops","text":"Heads up @all","createTime":"2026-05-10T09:00:00Z","thread":"spaces/ANN/threads/t1"}
					]`), nil
				case "spaces/ERR":
					return nil, errors.New("permission denied")
				}
			}
			return nil, fmt.Errorf("unexpected command args: %v", args)
		},
	}

	fetcher := NewFetcher(exec, "alice@example.com")
	report, err := Poll(context.Background(), fetcher, PollOptions{
		Account:            "alice@example.com",
		UserName:           "Alice",
		UserID:             "users/1001",
		ExcludeSpaces:      []string{"TEST"},
		AnnouncementSpaces: []string{"Announcements"},
		IgnorePrefixes:     []string{"[bot]"},
		Concurrency:        3,
		Now:                func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("Poll() error: %v", err)
	}

	if !report.CheckedAt.Equal(fixedNow) {
		t.Fatalf("CheckedAt = %v, want %v", report.CheckedAt, fixedNow)
	}
	if report.TotalMentions != 2 {
		t.Fatalf("TotalMentions = %d, want 2 (got spaces: %+v)", report.TotalMentions, report.Spaces)
	}
	if len(report.Spaces) != 2 {
		t.Fatalf("len(report.Spaces) = %d, want 2", len(report.Spaces))
	}
	// Alphabetical sorting: Alpha before Zeta.
	if report.Spaces[0].SpaceName != "Alpha" || report.Spaces[1].SpaceName != "Zeta" {
		t.Fatalf("space order = [%s, %s], want [Alpha, Zeta]", report.Spaces[0].SpaceName, report.Spaces[1].SpaceName)
	}
	if len(report.Spaces[0].Mentions) != 1 || report.Spaces[0].Mentions[0].Text != "Paging <users/1001>" {
		t.Fatalf("Alpha mentions = %+v", report.Spaces[0].Mentions)
	}
	if len(report.Spaces[1].Mentions) != 1 || report.Spaces[1].Mentions[0].Text != "Hey Alice" {
		t.Fatalf("Zeta mentions = %+v", report.Spaces[1].Mentions)
	}
	if len(report.Errors) != 1 || !strings.Contains(report.Errors[0], "Broken (spaces/ERR)") {
		t.Fatalf("Errors = %v, want single error for Broken (spaces/ERR)", report.Errors)
	}
}

func TestPollAllSpacesFailedReturnsError(t *testing.T) {
	exec := &mockExecutor{
		handler: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if len(args) >= 3 && args[0] == "chat" && args[1] == "spaces" && args[2] == "list" {
				return []byte("RESOURCE\tDISPLAY_NAME\tTYPE\nspaces/A\tAlpha\tSPACE\n"), nil
			}
			return nil, errors.New("auth expired")
		},
	}

	fetcher := NewFetcher(exec, "alice@example.com")
	if _, err := Poll(context.Background(), fetcher, PollOptions{Account: "alice@example.com", UserName: "Alice"}); err == nil {
		t.Fatal("expected error when every space fetch fails, got nil")
	}
}
