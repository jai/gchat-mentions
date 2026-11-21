# Google Chat Mentions

> Poll unread Google Chat mentions from the terminal, write an agent-readable JSON status file, and optionally post a digest.

[![CI](https://github.com/jai/gchat-mentions/actions/workflows/lint-cli.yml/badge.svg)](https://github.com/jai/gchat-mentions/actions/workflows/lint-cli.yml)
[![Tests](https://github.com/jai/gchat-mentions/actions/workflows/test-unit-cli.yml/badge.svg)](https://github.com/jai/gchat-mentions/actions/workflows/test-unit-cli.yml)

`gchat-mentions` is a small Go command-line tool for people who miss `@mentions` across busy Google Chat spaces. It reads unread messages through `gog` (a Google Workspace CLI) and filters out noise. Each run saves the latest result to a local JSON file that you or your AI agent can read.

## Install with an AI agent

Copy and paste this prompt into Claude Code, Codex, Antigravity, or another coding agent on your computer:

```text
Set up https://github.com/jai/gchat-mentions on this computer so you can check my unread Google Chat mentions on demand.

1. Check if Go (1.25+) and gog (https://github.com/steipete/gogcli) are installed. Install any missing prerequisite (on macOS: brew install go gogcli).
2. Clone https://github.com/jai/gchat-mentions (or install with `go install github.com/jai/gchat-mentions/cmd/gchat-mentions@latest` and ensure `$(go env GOPATH)/bin` is on PATH).
3. Check `gog auth list`. If my work Google account is not signed in with `chat`, follow `docs/gog-setup.md`:
   - Never ask me to paste an OAuth client secret into chat, and do not access my password manager.
   - Ask me in plain language what I want to connect (just Google Chat, or Chat and Calendar) and remind me I can add or remove permissions anytime later.
   - Open a separate macOS Terminal window running `./scripts/setup-gog-auth.sh` (with `open -a Terminal`) so I can paste the OAuth Client ID and Secret directly from my password manager and complete Google Sign-In privately. Wait for me to say I am done, then verify with `gog auth list`.
4. Run `gchat-mentions --init`, ask for my work email and Google Chat display name, and save them in `~/.config/gchat-mentions/config.yaml`. Ask if I want to mute `@all` in any announcement spaces or post digests to a personal space.
5. Run `gchat-mentions --format compact` once to verify it works and confirm `~/.local/state/gchat-mentions/latest.json` was written.
6. Set up a recurring schedule (every 2 hours during my work hours via cron or launchd) so `~/.local/state/gchat-mentions/latest.json` stays fresh.
7. Whenever I ask for my Google Chat status or unread mentions, read `~/.local/state/gchat-mentions/latest.json` (or run `gchat-mentions --format json`) and summarize what needs my attention with links.
```

## Install manually

### Prerequisites

- macOS or Linux.
- [Go 1.25 or newer](https://go.dev/dl/).
- [`gog`](https://github.com/steipete/gogcli) (the Google Workspace CLI) signed in to your Google Workspace account. See [the `gog` setup guide](docs/gog-setup.md) for step-by-step instructions.

### Steps

1. Install and sign in to `gog` by following [docs/gog-setup.md](docs/gog-setup.md), then confirm your work account appears with `chat`:

   ```sh
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
