# Google Chat Mentions

> Poll unread Google Chat mentions from the terminal, write an agent-readable JSON status file, and optionally post a digest.

[![CI](https://github.com/jai/gchat-mentions/actions/workflows/lint-cli.yml/badge.svg)](https://github.com/jai/gchat-mentions/actions/workflows/lint-cli.yml)
[![Tests](https://github.com/jai/gchat-mentions/actions/workflows/test-unit-cli.yml/badge.svg)](https://github.com/jai/gchat-mentions/actions/workflows/test-unit-cli.yml)

`gchat-mentions` is a small Go command-line tool for people who miss `@mentions` across busy Google Chat spaces. It reads unread messages through `gog` (a Google Workspace CLI) and filters out noise. Each run saves the latest result to a local JSON file that you or your AI agent can read.

## Install with an AI agent

Copy and paste this one-liner into Claude Code, Codex, Antigravity, or another coding agent on your computer:

```text
Follow https://github.com/jai/gchat-mentions/blob/main/docs/agent-setup.md to set up Google Chat mention tracking on this computer.
```

See [docs/agent-setup.md](docs/agent-setup.md) for the full steps the agent follows.

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
| `gog_env_file` | — | Optional `KEY=VALUE` env file loaded for headless `gog` runs |
| `notify_space` | `--notify-space` | Optional space ID to post unread digests to (pass `none` to suppress) |
| `output_path` | `--out` | JSON status file path (default `~/.local/state/gchat-mentions/latest.json`) |
| `active_within` | `--active-within` | Only scan spaces active within this Go duration (for example `6h`, `168h`) |
| `include_dms` | `--include-dms` | Include unread direct messages |
| `exclude_spaces` | `--exclude` | Space name patterns to skip |
| `announcement_spaces` | `--atall-mute` | Spaces where `@all`/`@here`-only messages are muted |
| `ignore_prefixes` | `--ignore-prefix` | Automated message prefixes to skip |
| `urgent` | `--urgent`, `--no-urgent`, `--urgent-init`, `--urgent-test` | Optional LLM urgency classification and dry-run/live escalation |

## Urgent mentions

`gchat-mentions` includes optional urgency classification for mentions and direct messages, supporting Cloudflare Clef / Clef-flash, TypeSafe, Cloudflare Workers AI, and OpenAI-compatible models in single or shadow (multi-provider) mode with rate-limited webhook or fallback command escalation. It runs in `dry-run` mode by default.

See [docs/urgent-mentions.md](docs/urgent-mentions.md) for setup, provider configuration, and safe rollout steps.

Exit codes: `0` on success, `1` on runtime or poll errors, and `2` on invalid flags or missing required settings.

## Development

```sh
make lint
make all-tests
make build
```

See [docs/workflow.md](docs/workflow.md) for the CI layout and repository workflow.
