// Package urgent classifies Google Chat mentions for urgency and runs
// deterministic, rate-limited dry-run or live escalations.
package urgent

import (
	"fmt"
	"strings"
	"time"

	"github.com/jai/gchat-mentions/internal/config"
	"github.com/jai/gchat-mentions/internal/gchat"
)

const (
	// DataBegin and DataEnd wrap untrusted chat text in classifier prompts.
	DataBegin = "<<<UNTRUSTED_CHAT_DATA_BEGIN>>>"
	DataEnd   = "<<<UNTRUSTED_CHAT_DATA_END>>>"

	DefaultThreshold         = 0.8
	DefaultRule              = "either"
	DefaultMaxPerRun         = 25
	DefaultMaxAgeHours       = 6.0
	DefaultSeenRetentionDays = 30
	DefaultMinGapMinutes     = 30.0
	DefaultMaxPerDay         = 6
	DefaultHTTPTimeout       = 20 * time.Second

	maxMsgChars   = 600
	maxStateChars = 4000
	maxContext    = 5
	minThreadCtx  = 3
)

// DefaultUrgencyDefinition returns a generic urgency rubric for the target user.
func DefaultUrgencyDefinition(userName, userContext string) string {
	who := strings.TrimSpace(userName)
	if who == "" {
		who = "the recipient"
	}
	if ctx := strings.TrimSpace(userContext); ctx != "" {
		who = fmt.Sprintf("%s (%s)", who, ctx)
	}
	return fmt.Sprintf(
		`"Urgent" means the message needs %s personally to act or reply within about one hour. `+
			`Urgent: a production incident or outage; a security incident; someone blocked on a decision `+
			`or approval that only %s can give, with a hard deadline; an executive, board, or customer `+
			`escalation with a deadline; an explicit time-critical ask addressed to %s. `+
			`NOT urgent: bot or automation reports, PR queue digests, stand-up or meeting notices, `+
			`@all/@here announcements, FYI, social chatter, and anything already handled later in the thread.`,
		who, who, who,
	)
}

// Settings holds resolved, validated runtime settings for urgent triage.
type Settings struct {
	UserName      string
	UserContext   string
	Definition    string
	Mode          string // dry-run | live | auto
	LiveFlagFile  string
	Location      *time.Location
	TimezoneName  string
	Threshold     float64
	Rule          string
	MaxPerRun     int
	MaxAge        time.Duration
	SeenRetention time.Duration
	StateDir      string
	QuietHours    *config.QuietHoursConfig
	MinGap        time.Duration
	MaxPerDay     int
	Providers     []config.ProviderConfig
	Escalation    config.EscalationConfig
}

// ResolveSettings validates cfg and fills defaults.
func ResolveSettings(cfg config.UrgentConfig, userName string) (*Settings, error) {
	tzName := strings.TrimSpace(cfg.Timezone)
	if tzName == "" {
		tzName = "UTC"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return nil, fmt.Errorf("load urgent timezone %q: %w", tzName, err)
	}

	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	if threshold > 1 {
		return nil, fmt.Errorf("urgent threshold %.2f must be in (0, 1]", threshold)
	}

	rule := strings.ToLower(strings.TrimSpace(cfg.Rule))
	if rule == "" {
		rule = DefaultRule
	}

	maxPerRun := cfg.MaxPerRun
	if maxPerRun <= 0 {
		maxPerRun = DefaultMaxPerRun
	}

	maxAgeHours := cfg.MaxAgeHours
	if maxAgeHours <= 0 {
		maxAgeHours = DefaultMaxAgeHours
	}

	retentionDays := cfg.SeenRetentionDays
	if retentionDays <= 0 {
		retentionDays = DefaultSeenRetentionDays
	}

	minGapMin := cfg.RateLimit.MinGapMinutes
	if minGapMin <= 0 {
		minGapMin = DefaultMinGapMinutes
	}

	maxPerDay := cfg.RateLimit.MaxPerDay
	if maxPerDay <= 0 {
		maxPerDay = DefaultMaxPerDay
	}

	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		if strings.TrimSpace(cfg.LiveFlagFile) != "" {
			mode = "auto"
		} else {
			mode = "dry-run"
		}
	}
	switch mode {
	case "dry-run", "live", "auto":
	default:
		return nil, fmt.Errorf("invalid urgent mode %q (expected dry-run, live, or auto)", mode)
	}

	stateDir := strings.TrimSpace(cfg.StateDir)
	if stateDir == "" {
		stateDir, err = config.DefaultUrgentStateDir()
		if err != nil {
			return nil, err
		}
	} else {
		stateDir, err = config.ExpandHome(stateDir)
		if err != nil {
			return nil, err
		}
	}

	liveFlag := strings.TrimSpace(cfg.LiveFlagFile)
	if liveFlag != "" {
		liveFlag, err = config.ExpandHome(liveFlag)
		if err != nil {
			return nil, err
		}
	}

	def := strings.TrimSpace(cfg.Definition)
	if def == "" {
		def = DefaultUrgencyDefinition(userName, cfg.UserContext)
	}

	if cfg.QuietHours != nil {
		if cfg.QuietHours.Start < 0 || cfg.QuietHours.Start > 23 || cfg.QuietHours.End < 0 || cfg.QuietHours.End > 23 {
			return nil, fmt.Errorf("quiet_hours start/end must be in 0..23, got %d..%d", cfg.QuietHours.Start, cfg.QuietHours.End)
		}
	}

	return &Settings{
		UserName:      strings.TrimSpace(userName),
		UserContext:   strings.TrimSpace(cfg.UserContext),
		Definition:    def,
		Mode:          mode,
		LiveFlagFile:  liveFlag,
		Location:      loc,
		TimezoneName:  tzName,
		Threshold:     threshold,
		Rule:          rule,
		MaxPerRun:     maxPerRun,
		MaxAge:        time.Duration(maxAgeHours * float64(time.Hour)),
		SeenRetention: time.Duration(retentionDays) * 24 * time.Hour,
		StateDir:      stateDir,
		QuietHours:    cfg.QuietHours,
		MinGap:        time.Duration(minGapMin * float64(time.Minute)),
		MaxPerDay:     maxPerDay,
		Providers:     cfg.Providers,
		Escalation:    cfg.Escalation,
	}, nil
}

// Candidate is a mention queued for urgency classification.
type Candidate struct {
	Message   gchat.Message `json:"message"`
	Broadcast bool          `json:"broadcast"`
}

// ContextMessage is an earlier thread or space message passed to the classifier.
type ContextMessage struct {
	Label  string    `json:"label"`
	Sender string    `json:"sender"`
	At     time.Time `json:"at"`
	Text   string    `json:"text"`
}

// ProviderVerdict records one classifier backend's structured answer.
type ProviderVerdict struct {
	Name       string   `json:"name"`
	Provider   string   `json:"provider"`
	Model      string   `json:"model"`
	Urgent     bool     `json:"urgent"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	Kind       string   `json:"kind,omitempty"`
	LatencyMS  int64    `json:"latency_ms"`
	CostUSD    *float64 `json:"cost_usd,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// EscalationInfo describes actions taken (or planned in dry-run) for an urgent mention.
type EscalationInfo struct {
	Mode          string   `json:"mode"`
	Actions       []string `json:"actions"`
	PagingAllowed bool     `json:"paging_allowed,omitempty"`
	Dir           string   `json:"dir,omitempty"`
	BriefPath     string   `json:"brief_path,omitempty"`
	Command       string   `json:"command,omitempty"`
}

// VerdictRecord is one JSONL line appended to verdicts.jsonl.
type VerdictRecord struct {
	TSUTC         string            `json:"ts_utc"`
	TSLocal       string            `json:"ts_local"`
	MessageAt     string            `json:"message_at"`
	MessageLink   string            `json:"message_link,omitempty"`
	MessageID     string            `json:"message_id"`
	SpaceID       string            `json:"space_id"`
	SpaceName     string            `json:"space_name"`
	SpaceType     string            `json:"space_type,omitempty"`
	Sender        string            `json:"sender"`
	Broadcast     bool              `json:"broadcast"`
	TextExcerpt   string            `json:"text_excerpt"`
	Verdicts      []ProviderVerdict `json:"verdicts"`
	Agree         bool              `json:"agree"`
	WouldEscalate bool              `json:"would_escalate"`
	Escalation    EscalationInfo    `json:"escalation"`
	Test          bool              `json:"test,omitempty"`
	Note          string            `json:"note,omitempty"`
}

// EscalationEntry is persisted in escalations.json for rate limiting.
type EscalationEntry struct {
	TSUTC     string   `json:"ts_utc"`
	LocalDate string   `json:"local_date"`
	Mode      string   `json:"mode"`
	MessageID string   `json:"message_id"`
	Dir       string   `json:"dir,omitempty"`
	Actions   []string `json:"actions,omitempty"`
}
