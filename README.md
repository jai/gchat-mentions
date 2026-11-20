# Google Chat Mentions

> Poll unread Google Chat mentions from the terminal, write an agent-readable JSON status file, and optionally post a digest.

[![CI](https://github.com/jai/gchat-mentions/actions/workflows/lint-cli.yml/badge.svg)](https://github.com/jai/gchat-mentions/actions/workflows/lint-cli.yml)
[![Tests](https://github.com/jai/gchat-mentions/actions/workflows/test-unit-cli.yml/badge.svg)](https://github.com/jai/gchat-mentions/actions/workflows/test-unit-cli.yml)

`gchat-mentions` is a small Go command-line tool for people who miss `@mentions` across busy Google Chat spaces. It reads unread messages through `gog` (a Google Workspace CLI) and filters out noise. Each run saves the latest result to a local JSON file that you or your AI agent can read.

## Install with an AI agent

Copy and paste this prompt into Claude Code, Codex, Antigravity, or another coding agent on your computer:

```text
Set up https://github.com/jai/gchat-mentions on this computer so you can check my unread Google Chat mentions on demand.

1. Verify that Go (1.25+) and gog (https://github.com/steipete/gogcli) are installed. Install any missing prerequisite (on macOS, use Homebrew: brew install go steipete/tap/gogcli).
2. Install the binary with: go install github.com/jai/gchat-mentions/cmd/gchat-mentions@latest (ensure $(go env GOPATH)/bin is on PATH).
3. Run `gog auth list`. If my work Google account is not authorized for `chat`, guide me through running `gog auth add <my-email> --services chat`.
4. Run `gchat-mentions --init`, then ask me for my work email and Google Chat display name and write them into `~/.config/gchat-mentions/config.yaml`. Ask if I want to mute `@all` in any announcement spaces or post digests to a personal space.
5. Run `gchat-mentions --format compact` once to verify it works and confirm `~/.local/state/gchat-mentions/latest.json` was written.
6. Set up a recurring schedule (every 2 hours during my work hours via cron or launchd) so `~/.local/state/gchat-mentions/latest.json` stays fresh.
7. Whenever I ask for my Google Chat status or unread mentions, read `~/.local/state/gchat-mentions/latest.json` (or run `gchat-mentions --format json`) and summarize what needs my attention with links.
```

## Install manually

### Prerequisites

- macOS or Linux.
- [Go 1.25 or newer](https://go.dev/dl/).
- [`gog`](https://github.com/steipete/gogcli) (the Google Workspace CLI), installed with `brew install steipete/tap/gogcli` on macOS or from its release page.

### Steps

1. Authorize your Google Workspace account for Google Chat in `gog`:

   ```sh
   gog auth add you@company.com --services chat
   gog auth list
   ```

2. Install `gchat-mentions`:

   ```sh
   go install github.com/jai/gchat-mentions/cmd/gchat-mentions@latest
   ```

   If you prefer to build from source instead, clone the repository and run `make build`:

   ```sh
   git clone https://github.com/jai/gchat-mentions.git
   cd gchat-mentions
   make build
   ```

3. Generate a starter configuration file:

   ```sh
   gchat-mentions --init
   ```

4. Open `~/.config/gchat-mentions/config.yaml` and set `account` and `user_name`.

5. Run a first check:

   ```sh
   gchat-mentions --format compact
   ```

6. Optional: schedule recurring checks every two hours during work hours with `cron`:

   ```cron
   0 8-20/2 * * * gchat-mentions --format compact >/dev/null 2>&1
   ```

## Output for agents and scripts

Every run atomically writes the latest report to `~/.local/state/gchat-mentions/latest.json` (pass `--out none` to disable writing).

```json
{
  "checked_at": "2026-11-20T13:03:09Z",
  "account": "you@company.com",
  "user_name": "Your Name",
  "total_mentions": 1,
  "spaces": [
    {
      "space_name": "Engineering",
      "space_id": "spaces/AAA123",
      "mentions": [
        {
          "id": "spaces/AAA123/messages/t1.m1",
          "space_id": "spaces/AAA123",
          "space_name": "Engineering",
          "sender": "Teammate",
          "text": "@Your Name can you review this?",
          "time": "2026-11-20T12:55:00Z",
          "thread": "spaces/AAA123/threads/t1",
          "link": "https://chat.google.com/room/AAA123/t1/m1?cls=10",
          "unread": true
        }
      ]
    }
  ]
}
```

## Configuration and key flags

Settings live in `~/.config/gchat-mentions/config.yaml`. Command-line flags override config values for a single run. Run `gchat-mentions --help` for the full flag list.

| Setting | Flag | Purpose |
| --- | --- | --- |
| `account` | `--account` | Google Workspace email authorized in `gog` |
| `user_name` | `--user` | Display name for mention matching |
| `user_id` | `--user-id` | Optional Google Chat user ID (`users/123`) |
| `notify_space` | `--notify-space` | Optional space ID to post unread digests to |
| `output_path` | `--out` | JSON status file path (default `~/.local/state/gchat-mentions/latest.json`) |
| `exclude_spaces` | `--exclude` | Space name patterns to skip |
| `announcement_spaces` | `--atall-mute` | Spaces where `@all`-only messages are muted |
| `ignore_prefixes` | `--ignore-prefix` | Automated message prefixes to skip |

Exit codes: `0` on success, `1` on runtime or poll errors, and `2` on invalid flags or missing required settings.

## Development

```sh
make lint
make all-tests
make build
```

See [docs/workflow.md](docs/workflow.md) for the CI layout and repository workflow.
