package gchat

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// PollOptions configures an unread-mentions poll run.
type PollOptions struct {
	Account            string
	UserName           string
	UserID             string
	Spaces             []string
	ExcludeSpaces      []string
	AnnouncementSpaces []string
	IgnorePrefixes     []string
	Concurrency        int
	Now                func() time.Time
}

// Poll fetches unread messages across matching spaces, filters for mentions,
// and returns an alphabetically sorted Report.
func Poll(ctx context.Context, fetcher *Fetcher, opts PollOptions) (*Report, error) {
	spaces, err := fetcher.FetchSpaces(ctx)
	if err != nil {
		return nil, err
	}

	if len(opts.Spaces) > 0 {
		var filtered []Space
		seen := make(map[string]bool)
		for _, query := range opts.Spaces {
			for _, s := range FilterSpacesByName(spaces, query, opts.ExcludeSpaces...) {
				if !seen[s.ID] {
					seen[s.ID] = true
					filtered = append(filtered, s)
				}
			}
		}
		spaces = filtered
	} else {
		spaces = FilterSpacesByName(spaces, "", opts.ExcludeSpaces...)
	}

	var rules []Rule
	if opts.UserID != "" || opts.UserName != "" {
		rules = append(rules, &MentionRule{
			UserID:      opts.UserID,
			DisplayName: opts.UserName,
		})
	}

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var results []SpaceMentions
	var fetchErrors []string
	var wg sync.WaitGroup

	for _, space := range spaces {
		wg.Add(1)
		go func(s Space) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				fetchErrors = append(fetchErrors, fmt.Sprintf("%s (%s): %v", s.Name, s.ID, ctx.Err()))
				mu.Unlock()
				return
			}

			unread, err := fetcher.FetchMessages(ctx, s.ID, FetchOptions{UnreadOnly: true, All: true})
			if err != nil {
				mu.Lock()
				fetchErrors = append(fetchErrors, fmt.Sprintf("%s (%s): %v", s.Name, s.ID, err))
				mu.Unlock()
				return
			}
			for i := range unread {
				unread[i].SpaceName = s.Name
			}

			var mentions []Message
			for _, msg := range unread {
				for _, rule := range rules {
					if rule.Match(msg) {
						mentions = append(mentions, msg)
						break
					}
				}
			}

			if len(mentions) > 0 {
				mu.Lock()
				results = append(results, SpaceMentions{
					SpaceName: s.Name,
					SpaceID:   s.ID,
					Mentions:  mentions,
				})
				mu.Unlock()
			}
		}(space)
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("poll cancelled: %w", err)
	}
	if len(spaces) > 0 && len(fetchErrors) == len(spaces) {
		sort.Strings(fetchErrors)
		return nil, fmt.Errorf("all %d spaces failed to fetch (first: %s)", len(spaces), fetchErrors[0])
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].SpaceName != results[j].SpaceName {
			return results[i].SpaceName < results[j].SpaceName
		}
		return results[i].SpaceID < results[j].SpaceID
	})
	sort.Strings(fetchErrors)

	total := 0
	for _, sm := range results {
		total += len(sm.Mentions)
	}

	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	report := &Report{
		CheckedAt:     nowFn().UTC(),
		Account:       opts.Account,
		UserName:      opts.UserName,
		TotalMentions: total,
		Spaces:        results,
		Errors:        fetchErrors,
	}

	var filters []Filter
	if len(opts.AnnouncementSpaces) > 0 {
		filters = append(filters, &AtAllOnlyFilter{
			SpaceNames:  opts.AnnouncementSpaces,
			DisplayName: opts.UserName,
			UserID:      opts.UserID,
		})
	}
	if len(opts.IgnorePrefixes) > 0 {
		filters = append(filters, &TextPrefixFilter{
			Prefixes: opts.IgnorePrefixes,
		})
	}
	ApplyFilters(report, filters)

	return report, nil
}
