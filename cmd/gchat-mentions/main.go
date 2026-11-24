package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jai/gchat-mentions/internal/config"
	"github.com/jai/gchat-mentions/internal/gchat"
	"github.com/jai/gchat-mentions/internal/state"
	"github.com/jai/gchat-mentions/internal/urgent"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, nil))
}

func run(args []string, stdout, stderr io.Writer, exec gchat.Executor) int {
	fs := flag.NewFlagSet("gchat-mentions", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		configPath        = fs.String("config", "", "Path to YAML config file (default: ~/.config/gchat-mentions/config.yaml)")
		initConfig        = fs.Bool("init", false, "Create a starter config file if absent and exit")
		initPrompts       = fs.Bool("init-prompts", false, "Create a starter urgent-prompts.yaml file if absent and exit")
		promptsPath       = fs.String("prompts", "", "Path to YAML urgent prompts file (overrides config urgent.prompts_file)")
		account           = fs.String("account", "", "Google Workspace email authorized in gog (overrides config)")
		userName          = fs.String("user", "", "Display name for mention matching (overrides config)")
		userID            = fs.String("user-id", "", "Google Chat user ID, e.g. users/123 (overrides config)")
		spaces            = fs.String("spaces", "", "Comma-separated space name filters (overrides config)")
		exclude           = fs.String("exclude", "", "Comma-separated space name patterns to skip (overrides config)")
		atAllMute         = fs.String("atall-mute", "", "Comma-separated announcement spaces where @all/@here is muted (overrides config)")
		ignorePrefix      = fs.String("ignore-prefix", "", "Comma-separated message prefixes to ignore (overrides config)")
		includeDMs        = fs.Bool("include-dms", false, "Include direct messages when polling")
		includeGroupChats = fs.Bool("include-group-chats", false, "Include group chats when polling")
		activeWithin      = fs.String("active-within", "", "Only scan spaces active within this Go duration (e.g. 6h, 168h)")
		notifySpace       = fs.String("notify-space", "", "Post unread mention digest to this space ID (overrides config; pass 'none' to disable)")
		outputPath        = fs.String("out", "", "Path to write latest JSON report (default: ~/.local/state/gchat-mentions/latest.json; pass 'none' to disable)")
		format            = fs.String("format", "json", "Stdout format: json, compact, or text")
		timeout           = fs.Duration("timeout", 5*time.Minute, "Maximum runtime for the poll")
		showVersion       = fs.Bool("version", false, "Print version and exit")

		urgentEnable  = fs.Bool("urgent", false, "Enable urgency classification for this run")
		urgentDisable = fs.Bool("no-urgent", false, "Disable urgency classification for this run")
		urgentInit    = fs.Bool("urgent-init", false, "Mark currently visible mentions as seen without classifying")
		urgentMode    = fs.String("urgent-mode", "", "Urgent escalation mode override: auto, dry-run, or live")
		urgentMax     = fs.Int("urgent-max", 0, "Override max new mentions classified in this run")
		urgentTest    = fs.Bool("urgent-test", false, "Dry-run urgency classification and escalation on a synthetic urgent message")
		urgentOffline = fs.Bool("urgent-offline", false, "With --urgent-test: skip live provider HTTP calls and use canned verdicts")
	)

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		_, _ = fmt.Fprintf(stdout, "gchat-mentions %s\n", version)
		return 0
	}

	if *initConfig {
		target := *configPath
		if target == "" {
			var err error
			target, err = config.DefaultConfigPath()
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
		} else {
			var err error
			target, err = config.ExpandHome(target)
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
		}
		created, err := config.WriteStarter(target)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if created {
			_, _ = fmt.Fprintf(stdout, "Created starter config at %s\n", target)
		} else {
			_, _ = fmt.Fprintf(stdout, "Config already exists at %s\n", target)
		}
		return 0
	}

	if *initPrompts {
		target := *promptsPath
		if target == "" {
			var err error
			target, err = config.DefaultPromptsPath()
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
		} else {
			var err error
			target, err = config.ExpandHome(target)
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
		}
		created, err := config.WriteStarterPrompts(target)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if created {
			_, _ = fmt.Fprintf(stdout, "Created starter prompts at %s\n", target)
		} else {
			_, _ = fmt.Fprintf(stdout, "Prompts file already exists at %s\n", target)
		}
		return 0
	}

	switch *format {
	case "json", "compact", "text":
	default:
		_, _ = fmt.Fprintf(stderr, "Error: unknown format %q (expected json, compact, or text)\n", *format)
		return 2
	}
	if *timeout <= 0 {
		_, _ = fmt.Fprintln(stderr, "Error: timeout must be positive")
		return 2
	}
	if *urgentEnable && *urgentDisable {
		_, _ = fmt.Fprintln(stderr, "Error: cannot set both --urgent and --no-urgent")
		return 2
	}
	if *urgentMode != "" {
		switch strings.ToLower(strings.TrimSpace(*urgentMode)) {
		case "auto", "dry-run", "live":
		default:
			_, _ = fmt.Fprintf(stderr, "Error: unknown urgent mode %q (expected auto, dry-run, or live)\n", *urgentMode)
			return 2
		}
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if strings.TrimSpace(*promptsPath) != "" {
		cfg.Urgent.PromptsFile = strings.TrimSpace(*promptsPath)
		cfg.Urgent.Questions = nil
	}

	resolvedUserName := firstNonEmpty(*userName, cfg.UserName)

	// Synthetic urgent escalation dry-run does not require polling Google Chat.
	if *urgentTest {
		settings, err := urgent.ResolveSettings(cfg.Urgent, resolvedUserName)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		rec, err := urgent.RunTest(ctx, settings, *urgentOffline, urgent.RunOptions{})
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error running urgent test: %v\n", err)
			return 1
		}
		encoded, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error formatting JSON: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, string(encoded))
		return 0
	}

	runUrgent := (cfg.Urgent.Enabled || *urgentEnable || *urgentInit) && !*urgentDisable
	var urgentSettings *urgent.Settings
	if runUrgent {
		urgentSettings, err = urgent.ResolveSettings(cfg.Urgent, resolvedUserName)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 2
		}
	}

	// Determine active_within duration (explicit flag > config > urgent max_age default when urgent runs).
	var resolvedActiveWithin time.Duration
	rawActive := firstNonEmpty(*activeWithin, cfg.ActiveWithin)
	if rawActive != "" {
		d, err := time.ParseDuration(rawActive)
		if err != nil || d < 0 {
			_, _ = fmt.Fprintf(stderr, "Error: invalid active-within duration %q\n", rawActive)
			return 2
		}
		resolvedActiveWithin = d
	}

	resolvedIncludeDMs := cfg.IncludeDMs || *includeDMs
	if runUrgent && !resolvedIncludeDMs {
		if cfg.Urgent.IncludeDMs == nil || *cfg.Urgent.IncludeDMs {
			resolvedIncludeDMs = true
		}
	}

	opts := gchat.PollOptions{
		Account:            firstNonEmpty(*account, cfg.Account),
		UserName:           resolvedUserName,
		UserID:             firstNonEmpty(*userID, cfg.UserID),
		Spaces:             overrideCSV(*spaces, cfg.Spaces),
		ExcludeSpaces:      overrideCSV(*exclude, cfg.ExcludeSpaces),
		AnnouncementSpaces: overrideCSV(*atAllMute, cfg.AnnouncementSpaces),
		IgnorePrefixes:     overrideCSV(*ignorePrefix, cfg.IgnorePrefixes),
		IncludeDMs:         resolvedIncludeDMs,
		IncludeGroupChats:  cfg.IncludeGroupChats || *includeGroupChats,
		ActiveWithin:       resolvedActiveWithin,
		Concurrency:        cfg.Concurrency,
	}

	if opts.Account == "" {
		_, _ = fmt.Fprintln(stderr, "Error: account is required (set 'account' in config or pass --account)")
		return 2
	}
	if opts.UserName == "" && opts.UserID == "" {
		_, _ = fmt.Fprintln(stderr, "Error: user or user-id is required (set 'user_name' in config or pass --user)")
		return 2
	}

	resolvedNotify := firstNonEmpty(*notifySpace, cfg.NotifySpace)
	if strings.EqualFold(resolvedNotify, "none") {
		resolvedNotify = ""
	}
	resolvedOut := firstNonEmpty(*outputPath, cfg.OutputPath)
	if resolvedOut == "" {
		defaultOut, err := config.DefaultOutputPath()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		resolvedOut = defaultOut
	} else if !strings.EqualFold(resolvedOut, "none") {
		expanded, err := config.ExpandHome(resolvedOut)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		resolvedOut = expanded
	}

	if exec == nil {
		if strings.TrimSpace(cfg.GogEnvFile) != "" {
			extraEnv, err := config.ReadEnvFile(cfg.GogEnvFile)
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "Error reading gog_env_file: %v\n", err)
				return 1
			}
			exec = gchat.NewCLIExecutorWithEnv(extraEnv)
		} else {
			exec = gchat.NewCLIExecutor()
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fetcher := gchat.NewFetcher(exec, opts.Account)
	report, err := gchat.Poll(ctx, fetcher, opts)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error polling Google Chat: %v\n", err)
		return 1
	}

	if runUrgent {
		summary, err := urgent.Run(ctx, report, urgentSettings, urgent.RunOptions{
			InitOnly:     *urgentInit,
			ModeOverride: *urgentMode,
			MaxOverride:  *urgentMax,
			ContextFetch: fetcher,
		})
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error running urgent classification: %v\n", err)
			return 1
		}
		report.Urgent = summary
	}

	if !strings.EqualFold(resolvedOut, "none") {
		if err := state.WriteReport(resolvedOut, report); err != nil {
			_, _ = fmt.Fprintf(stderr, "Warning: failed to write state file %s: %v\n", resolvedOut, err)
		}
	}

	if resolvedNotify != "" && report.TotalMentions > 0 {
		if err := fetcher.Notify(ctx, resolvedNotify, report); err != nil {
			_, _ = fmt.Fprintf(stderr, "Warning: notify failed: %v\n", err)
		}
	}

	switch *format {
	case "json":
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error formatting JSON: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, string(encoded))
	case "compact":
		_, _ = fmt.Fprint(stdout, gchat.FormatCompact(report))
	case "text":
		_, _ = fmt.Fprint(stdout, gchat.FormatText(report))
	}

	return 0
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

func overrideCSV(flagVal string, fallback []string) []string {
	if strings.TrimSpace(flagVal) == "" {
		return fallback
	}
	parts := strings.Split(flagVal, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
