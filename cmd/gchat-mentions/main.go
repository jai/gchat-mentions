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
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, gchat.NewCLIExecutor()))
}

func run(args []string, stdout, stderr io.Writer, exec gchat.Executor) int {
	fs := flag.NewFlagSet("gchat-mentions", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		configPath   = fs.String("config", "", "Path to YAML config file (default: ~/.config/gchat-mentions/config.yaml)")
		initConfig   = fs.Bool("init", false, "Create a starter config file if absent and exit")
		account      = fs.String("account", "", "Google Workspace email authorized in gog (overrides config)")
		userName     = fs.String("user", "", "Display name for mention matching (overrides config)")
		userID       = fs.String("user-id", "", "Google Chat user ID, e.g. users/123 (overrides config)")
		spaces       = fs.String("spaces", "", "Comma-separated space name filters (overrides config)")
		exclude      = fs.String("exclude", "", "Comma-separated space name patterns to skip (overrides config)")
		atAllMute    = fs.String("atall-mute", "", "Comma-separated announcement spaces where @all is muted (overrides config)")
		ignorePrefix = fs.String("ignore-prefix", "", "Comma-separated message prefixes to ignore (overrides config)")
		notifySpace  = fs.String("notify-space", "", "Post unread mention digest to this space ID (overrides config)")
		outputPath   = fs.String("out", "", "Path to write latest JSON report (default: ~/.local/state/gchat-mentions/latest.json; pass 'none' to disable)")
		format       = fs.String("format", "json", "Stdout format: json, compact, or text")
		timeout      = fs.Duration("timeout", 5*time.Minute, "Maximum runtime for the poll")
		showVersion  = fs.Bool("version", false, "Print version and exit")
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

	cfg, err := config.Load(*configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	opts := gchat.PollOptions{
		Account:            firstNonEmpty(*account, cfg.Account),
		UserName:           firstNonEmpty(*userName, cfg.UserName),
		UserID:             firstNonEmpty(*userID, cfg.UserID),
		Spaces:             overrideCSV(*spaces, cfg.Spaces),
		ExcludeSpaces:      overrideCSV(*exclude, cfg.ExcludeSpaces),
		AnnouncementSpaces: overrideCSV(*atAllMute, cfg.AnnouncementSpaces),
		IgnorePrefixes:     overrideCSV(*ignorePrefix, cfg.IgnorePrefixes),
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
	resolvedOut := firstNonEmpty(*outputPath, cfg.OutputPath)
	if resolvedOut == "" {
		defaultOut, err := config.DefaultOutputPath()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		resolvedOut = defaultOut
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fetcher := gchat.NewFetcher(exec, opts.Account)
	report, err := gchat.Poll(ctx, fetcher, opts)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error polling Google Chat: %v\n", err)
		return 1
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
	default:
		_, _ = fmt.Fprintf(stderr, "Error: unknown format %q (expected json, compact, or text)\n", *format)
		return 2
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
