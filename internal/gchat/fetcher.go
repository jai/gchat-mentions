package gchat

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Space represents a Google Chat space.
type Space struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	LastActive time.Time `json:"last_active,omitempty"`
}

// Message represents a Google Chat message.
type Message struct {
	ID        string    `json:"id"`
	SpaceID   string    `json:"space_id"`
	SpaceName string    `json:"space_name"`
	SpaceType string    `json:"space_type,omitempty"`
	Sender    string    `json:"sender"`
	Text      string    `json:"text"`
	Time      time.Time `json:"time"`
	Thread    string    `json:"thread"`
	Link      string    `json:"link,omitempty"`
	Unread    bool      `json:"unread"`
}

// FetchOptions controls message fetching.
type FetchOptions struct {
	Max        int
	UnreadOnly bool
	All        bool
	Thread     string
	Order      string
}

// SpaceFilterOptions controls which Google Chat spaces are polled.
type SpaceFilterOptions struct {
	Query             string
	Exclude           []string
	IncludeDMs        bool
	IncludeGroupChats bool
}

// Fetcher retrieves and posts Google Chat messages via the gog CLI.
type Fetcher struct {
	exec    Executor
	account string
}

// NewFetcher creates a Google Chat fetcher for the given Workspace account.
func NewFetcher(exec Executor, account string) *Fetcher {
	return &Fetcher{exec: exec, account: account}
}

type gogMessagesResponse struct {
	Messages []gogMessage `json:"messages"`
}

type gogMessage struct {
	Resource   string `json:"resource"`
	Sender     string `json:"sender"`
	Text       string `json:"text"`
	CreateTime string `json:"createTime"`
	Thread     string `json:"thread"`
}

type apiSpacesResponse struct {
	Spaces        []apiSpace `json:"spaces"`
	NextPageToken string     `json:"nextPageToken"`
}

type apiSpace struct {
	Name           string `json:"name"`
	DisplayName    string `json:"displayName"`
	SpaceType      string `json:"spaceType"`
	LastActiveTime string `json:"lastActiveTime"`
}

// FetchSpaces returns all spaces the account belongs to.
func (f *Fetcher) FetchSpaces(ctx context.Context) ([]Space, error) {
	out, err := f.exec.Run(ctx, "gog", "chat", "spaces", "list",
		"--account", f.account, "--plain", "--all", "--no-input")
	if err != nil {
		return nil, fmt.Errorf("gchat spaces list: %w", err)
	}
	return parseSpacesPlain(string(out)), nil
}

// FetchActiveSpaces lists spaces via the Chat REST API (most recently active first)
// and stops paginating once lastActiveTime is older than since. If the API call fails
// (for example, on older gog versions), it falls back to FetchSpaces.
func (f *Fetcher) FetchActiveSpaces(ctx context.Context, since time.Time) ([]Space, error) {
	if since.IsZero() {
		return f.FetchSpaces(ctx)
	}

	const pageSize = 200
	var out []Space
	pageToken := ""
	for {
		params := map[string]any{"pageSize": pageSize}
		if pageToken != "" {
			params["pageToken"] = pageToken
		}
		rawParams, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal spaces.list params: %w", err)
		}
		raw, err := f.exec.Run(ctx, "gog", "api", "call", "chat", "v1", "spaces.list",
			"--params", string(rawParams),
			"--account", f.account,
			"--json", "--no-input")
		if err != nil {
			if len(out) == 0 {
				return f.FetchSpaces(ctx)
			}
			return out, nil
		}

		var resp apiSpacesResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			if len(out) == 0 {
				return f.FetchSpaces(ctx)
			}
			return out, nil
		}

		stop := false
		for _, sp := range resp.Spaces {
			var lastActive time.Time
			if sp.LastActiveTime != "" {
				if parsed, err := time.Parse(time.RFC3339Nano, sp.LastActiveTime); err == nil {
					lastActive = parsed.UTC()
					if lastActive.Before(since) {
						stop = true
						continue
					}
				}
			}
			out = append(out, Space{
				ID:         sp.Name,
				Name:       sp.DisplayName,
				Type:       sp.SpaceType,
				LastActive: lastActive,
			})
		}

		if stop || resp.NextPageToken == "" || len(resp.Spaces) == 0 {
			break
		}
		pageToken = resp.NextPageToken
	}
	return out, nil
}

// FetchMessages returns messages from a space.
func (f *Fetcher) FetchMessages(ctx context.Context, spaceID string, opts FetchOptions) ([]Message, error) {
	args := []string{
		"chat", "messages", "list", spaceID,
		"--account", f.account, "--json", "--results-only", "--no-input",
	}
	if opts.All {
		args = append(args, "--all")
	} else if opts.Max > 0 {
		args = append(args, "--max", strconv.Itoa(opts.Max))
	}
	if opts.UnreadOnly {
		args = append(args, "--unread")
	}
	if opts.Thread != "" {
		args = append(args, "--thread", opts.Thread)
	}
	if opts.Order != "" {
		args = append(args, "--order", opts.Order)
	}

	out, err := f.exec.Run(ctx, "gog", args...)
	if err != nil {
		return nil, fmt.Errorf("gchat messages list %s: %w", spaceID, err)
	}

	var gogMsgs []gogMessage
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		if err := json.Unmarshal([]byte(trimmed), &gogMsgs); err != nil {
			return nil, fmt.Errorf("gchat parse messages: %w", err)
		}
	} else {
		var resp gogMessagesResponse
		if err := json.Unmarshal([]byte(trimmed), &resp); err != nil {
			return nil, fmt.Errorf("gchat parse messages: %w", err)
		}
		gogMsgs = resp.Messages
	}

	msgs := make([]Message, 0, len(gogMsgs))
	for _, gm := range gogMsgs {
		t, _ := time.Parse(time.RFC3339Nano, gm.CreateTime)
		msg := Message{
			ID:      gm.Resource,
			SpaceID: spaceID,
			Sender:  gm.Sender,
			Text:    gm.Text,
			Time:    t,
			Thread:  gm.Thread,
			Unread:  opts.UnreadOnly,
		}
		msg.Link = MessageLink(msg)
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// FetchThreadContext returns up to maxMsgs recent messages from msg's thread
// for urgency classification context.
func (f *Fetcher) FetchThreadContext(ctx context.Context, msg Message, maxMsgs int) ([]Message, error) {
	if msg.SpaceID == "" || msg.Thread == "" {
		return nil, nil
	}
	if maxMsgs <= 0 {
		maxMsgs = 6
	}
	msgs, err := f.FetchMessages(ctx, msg.SpaceID, FetchOptions{
		Thread: msg.Thread,
		Max:    maxMsgs,
		Order:  "createTime desc",
	})
	if err != nil {
		return nil, err
	}
	for i := range msgs {
		msgs[i].SpaceName = msg.SpaceName
		msgs[i].SpaceType = msg.SpaceType
		msgs[i].Link = MessageLink(msgs[i])
	}
	return msgs, nil
}

// Notify posts a Report to a Google Chat space, chunked to fit message limits.
func (f *Fetcher) Notify(ctx context.Context, spaceID string, report *Report) error {
	for _, chunk := range FormatNotification(report) {
		_, err := f.exec.Run(ctx, "gog", "chat", "messages", "send", spaceID,
			"--account", f.account, "--text", chunk, "--json", "--no-input")
		if err != nil {
			return fmt.Errorf("gchat notify: %w", err)
		}
	}
	return nil
}

func parseSpacesPlain(output string) []Space {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var spaces []Space
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		if id == "" || id == "RESOURCE" {
			continue
		}
		spaces = append(spaces, Space{
			ID:   id,
			Name: strings.TrimSpace(parts[1]),
			Type: strings.TrimSpace(parts[2]),
		})
	}
	return spaces
}

// FilterSpacesByName returns SPACE-type spaces matching query (case-insensitive)
// and not matching any exclude pattern.
func FilterSpacesByName(spaces []Space, query string, exclude ...string) []Space {
	return FilterSpaces(spaces, SpaceFilterOptions{
		Query:   query,
		Exclude: exclude,
	})
}

// FilterSpaces returns spaces matching type, query, and exclude criteria.
func FilterSpaces(spaces []Space, opts SpaceFilterOptions) []Space {
	q := strings.ToLower(opts.Query)
	var filtered []Space
	for _, s := range spaces {
		switch s.Type {
		case "SPACE":
		case "DIRECT_MESSAGE":
			if !opts.IncludeDMs {
				continue
			}
		case "GROUP_CHAT":
			if !opts.IncludeGroupChats {
				continue
			}
		default:
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(s.Name), q) {
			continue
		}
		excluded := false
		for _, ex := range opts.Exclude {
			if ex != "" && strings.Contains(strings.ToLower(s.Name), strings.ToLower(ex)) {
				excluded = true
				break
			}
		}
		if !excluded {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// MessageLink builds a Google Chat deep link for a message.
func MessageLink(msg Message) string {
	if msg.Thread == "" {
		return ""
	}
	parts := strings.SplitN(msg.Thread, "/", 4)
	if len(parts) < 4 {
		return ""
	}
	spaceID := parts[1]
	threadID := parts[3]
	if msg.SpaceType == "DIRECT_MESSAGE" {
		return fmt.Sprintf("https://chat.google.com/dm/%s/%s", spaceID, threadID)
	}
	messageID := messageIDFromResource(msg.ID)
	if messageID == "" {
		return fmt.Sprintf("https://chat.google.com/room/%s/%s", spaceID, threadID)
	}
	return fmt.Sprintf("https://chat.google.com/room/%s/%s/%s?cls=10", spaceID, threadID, messageID)
}

func messageIDFromResource(resource string) string {
	parts := strings.Split(resource, "/")
	if len(parts) < 4 {
		return ""
	}
	last := parts[len(parts)-1]
	if last == "" {
		return ""
	}
	if idx := strings.LastIndex(last, "."); idx >= 0 && idx < len(last)-1 {
		return last[idx+1:]
	}
	return last
}
