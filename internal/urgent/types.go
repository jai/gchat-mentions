// Package urgent classifies Google Chat mentions for actionability and urgency
// and runs deterministic, rate-limited dry-run or live escalations.
package urgent

import (
	"fmt"
	"strings"
	"time"

	"github.com/jai/gchat-mentions/internal/config"
	"github.com/jai/gchat-mentions/internal/gchat"
)

const (
	DefaultThreshold               = 0.8
	DefaultActionThreshold         = 0.70
	DefaultUrgentThreshold         = 0.70
	DefaultUrgentOverrideThreshold = 0.85
	DefaultRule                    = "either"
	DefaultMaxPerRun               = 25
	DefaultMaxAgeHours             = 6.0
	DefaultSeenRetentionDays       = 30
	DefaultMinGapMinutes           = 30.0
	DefaultMaxPerDay               = 6
	DefaultHTTPTimeout             = 20 * time.Second

	maxMsgChars   = 600
	maxStateChars = 4000
	maxContext    = 5
	minThreadCtx  = 3
)

// DefaultUrgencyDefinition returns a concise urgency summary for fallback text-gen models and briefs.
func DefaultUrgencyDefinition(userName, userContext string) string {
	who := formatRecipient(userName, userContext)
	return fmt.Sprintf(
		"Urgent means target_message requires %s to respond immediately, worth interrupting a meeting right now "+
			"(active production outage or customer-facing incident, live security or fraud emergency, or a hard same-hour deadline where waiting causes immediate harm). "+
			"Not urgent: routine PR reviews or CI overrides, IT access requests, roadmap or migration timelines days or weeks away, status updates, or async decisions.",
		who,
	)
}

func formatRecipient(userName, userContext string) string {
	who := strings.TrimSpace(userName)
	if who == "" {
		who = "the recipient"
	}
	if ctx := strings.TrimSpace(userContext); ctx != "" {
		return fmt.Sprintf("%s (%s)", who, ctx)
	}
	return who
}

// DefaultQuestions returns the clean 2-question Clef/Jev schema (needs_action + urgent)
// used when no custom prompts_file or inline urgent.questions are configured.
func DefaultQuestions(userName, userContext string) map[string]config.QuestionConfig {
	who := strings.TrimSpace(userName)
	if who == "" {
		who = "the recipient"
	}
	recipient := formatRecipient(userName, userContext)
	return map[string]config.QuestionConfig{
		"needs_action": {
			Type: "noul",
			Instructions: fmt.Sprintf(
				"From the Google Chat messages provided, does target_message indicate that %s needs to personally act, approve, decide, or reply?",
				recipient,
			),
			Criteria: map[string]any{
				"true": fmt.Sprintf(
					"Target message asks or expects %s personally to act, approve, decide, unblock, or reply, and is not already resolved in context.",
					who,
				),
				"false": "Informational update, status notice, automated digest, broadcast announcement, addressed to someone else, or already resolved in context.",
			},
		},
		"urgent": {
			Type: "noul",
			Instructions: fmt.Sprintf(
				"Does target_message require %s to respond immediately (worth interrupting a meeting or focus block right now)?",
				recipient,
			),
			Criteria: map[string]any{
				"true": fmt.Sprintf(
					"Needs %s's immediate attention right now, worth interrupting a meeting: active production outage or customer-facing incident, live security or fraud emergency, or a hard same-hour deadline where waiting causes immediate harm.",
					who,
				),
				"false": "Can wait a few hours or until a scheduled meeting later today: routine PR reviews or CI overrides, IT access requests, roadmap or migration timelines days or weeks away, status updates, or async decisions.",
			},
		},
	}
}

// RenderQuestions substitutes {{user_name}}, {{user_context}}, and {{recipient}} in question prompts.
func RenderQuestions(raw map[string]config.QuestionConfig, userName, userContext string) map[string]config.QuestionConfig {
	who := strings.TrimSpace(userName)
	if who == "" {
		who = "the recipient"
	}
	ctx := strings.TrimSpace(userContext)
	recipient := formatRecipient(userName, userContext)

	replacer := strings.NewReplacer(
		"{{user_name}}", who,
		"{{user_context}}", ctx,
		"{{recipient}}", recipient,
	)

	var renderAny func(v any) any
	renderAny = func(v any) any {
		switch x := v.(type) {
		case string:
			return replacer.Replace(x)
		case map[string]any:
			cp := make(map[string]any, len(x))
			for k, val := range x {
				cp[k] = renderAny(val)
			}
			return cp
		case map[any]any:
			cp := make(map[string]any, len(x))
			for k, val := range x {
				cp[fmt.Sprint(k)] = renderAny(val)
			}
			return cp
		case []any:
			cp := make([]any, len(x))
			for i, val := range x {
				cp[i] = renderAny(val)
			}
			return cp
		default:
			return v
		}
	}

	out := make(map[string]config.QuestionConfig, len(raw))
	for id, q := range raw {
		out[id] = config.QuestionConfig{
			Type:         strings.TrimSpace(q.Type),
			Instructions: replacer.Replace(strings.TrimSpace(q.Instructions)),
			Criteria:     renderAny(q.Criteria),
		}
	}
	return out
}

// Settings holds resolved, validated runtime settings for urgent triage.
type Settings struct {
	UserName                string
	UserContext             string
	Definition              string
	Questions               map[string]config.QuestionConfig
	Mode                    string // dry-run | live | auto
	LiveFlagFile            string
	Location                *time.Location
	TimezoneName            string
	Threshold               float64
	ActionThreshold         float64
	UrgentThreshold         float64
	UrgentOverrideThreshold float64
	Rule                    string
	MaxPerRun               int
	MaxAge                  time.Duration
	SeenRetention           time.Duration
	StateDir                string
	QuietHours              *config.QuietHoursConfig
	MinGap                  time.Duration
	MaxPerDay               int
	Providers               []config.ProviderConfig
	Escalation              config.EscalationConfig
}

// ResolveSettings validates cfg, loads prompts from inline config or prompts_file, and fills defaults.
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

	actionThr := cfg.ActionThreshold
	if actionThr <= 0 {
		actionThr = DefaultActionThreshold
	}
	if actionThr > 1 {
		return nil, fmt.Errorf("urgent action_threshold %.2f must be in (0, 1]", actionThr)
	}

	urgentThr := cfg.UrgentThreshold
	if urgentThr <= 0 {
		urgentThr = DefaultUrgentThreshold
	}
	if urgentThr > 1 {
		return nil, fmt.Errorf("urgent urgent_threshold %.2f must be in (0, 1]", urgentThr)
	}

	overrideThr := DefaultUrgentOverrideThreshold
	if cfg.UrgentOverrideThreshold != nil {
		overrideThr = *cfg.UrgentOverrideThreshold
		if overrideThr < 0 || overrideThr > 1 {
			return nil, fmt.Errorf("urgent urgent_override_threshold %.2f must be in [0, 1]", overrideThr)
		}
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

	// Resolve questions precedence: inline urgent.questions > urgent.prompts_file > default ~/.config/gchat-mentions/urgent-prompts.yaml (if present) > built-in defaults.
	var questions map[string]config.QuestionConfig
	if len(cfg.Questions) > 0 {
		questions = RenderQuestions(cfg.Questions, userName, cfg.UserContext)
	} else {
		explicitFile := strings.TrimSpace(cfg.PromptsFile) != ""
		loaded, err := config.LoadPromptsFile(cfg.PromptsFile, explicitFile)
		if err != nil {
			return nil, err
		}
		if len(loaded) > 0 {
			questions = RenderQuestions(loaded, userName, cfg.UserContext)
		} else {
			questions = DefaultQuestions(userName, cfg.UserContext)
		}
	}

	if cfg.QuietHours != nil {
		if cfg.QuietHours.Start < 0 || cfg.QuietHours.Start > 23 || cfg.QuietHours.End < 0 || cfg.QuietHours.End > 23 {
			return nil, fmt.Errorf("quiet_hours start/end must be in 0..23, got %d..%d", cfg.QuietHours.Start, cfg.QuietHours.End)
		}
	}

	return &Settings{
		UserName:                strings.TrimSpace(userName),
		UserContext:             strings.TrimSpace(cfg.UserContext),
		Definition:              def,
		Questions:               questions,
		Mode:                    mode,
		LiveFlagFile:            liveFlag,
		Location:                loc,
		TimezoneName:            tzName,
		Threshold:               threshold,
		ActionThreshold:         actionThr,
		UrgentThreshold:         urgentThr,
		UrgentOverrideThreshold: overrideThr,
		Rule:                    rule,
		MaxPerRun:               maxPerRun,
		MaxAge:                  time.Duration(maxAgeHours * float64(time.Hour)),
		SeenRetention:           time.Duration(retentionDays) * 24 * time.Hour,
		StateDir:                stateDir,
		QuietHours:              cfg.QuietHours,
		MinGap:                  time.Duration(minGapMin * float64(time.Minute)),
		MaxPerDay:               maxPerDay,
		Providers:               cfg.Providers,
		Escalation:              cfg.Escalation,
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

// StatePayload is the clean structured JSON state sent to Clef/Jev decision models.
type StatePayload struct {
	Space         StateSpace        `json:"space"`
	Context       []StateContextMsg `json:"context"`
	TargetMessage StateTargetMsg    `json:"target_message"`
}

// StateSpace identifies the Google Chat space in StatePayload.
type StateSpace struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// StateContextMsg is one preceding context message in StatePayload.
type StateContextMsg struct {
	Scope  string `json:"scope"`
	Sender string `json:"sender"`
	At     string `json:"at"`
	Text   string `json:"text"`
}

// StateTargetMsg is the target message being classified in StatePayload.
type StateTargetMsg struct {
	Sender string `json:"sender"`
	At     string `json:"at"`
	Text   string `json:"text"`
}

// ProviderVerdict records one classifier backend's structured answer.
type ProviderVerdict struct {
	Name        string         `json:"name"`
	Provider    string         `json:"provider"`
	Model       string         `json:"model"`
	Actionable  bool           `json:"actionable"`
	Urgent      bool           `json:"urgent"`
	NeedsAction *float64       `json:"needs_action,omitempty"`
	UrgentScore *float64       `json:"urgent_score,omitempty"`
	Confidence  float64        `json:"confidence"`
	Reason      string         `json:"reason"`
	Kind        string         `json:"kind,omitempty"`
	Answers     map[string]any `json:"answers,omitempty"`
	LatencyMS   int64          `json:"latency_ms"`
	CostUSD     *float64       `json:"cost_usd,omitempty"`
	Error       string         `json:"error,omitempty"`
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
	Actionable    bool              `json:"actionable"`
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
