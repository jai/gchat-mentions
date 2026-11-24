# Google Chat Mentions

> Poll unread Google Chat mentions from the terminal, save JSON status for agents, and escalate urgent messages.

[![CI](https://github.com/jai/gchat-mentions/actions/workflows/lint-cli.yml/badge.svg)](https://github.com/jai/gchat-mentions/actions/workflows/lint-cli.yml)
[![Tests](https://github.com/jai/gchat-mentions/actions/workflows/test-unit-cli.yml/badge.svg)](https://github.com/jai/gchat-mentions/actions/workflows/test-unit-cli.yml)

`gchat-mentions` is a Go command-line tool for people who miss `@mentions` in busy Google Chat spaces. It reads unread messages through `gog` (the Google Workspace CLI) and filters out automated noise. Each run saves the latest report to a local JSON file, posts an optional digest, and can send urgent messages to a webhook or command.

## Install with an AI agent

Paste this instruction into Claude Code, Codex, Antigravity, or another coding agent on your computer:

```text
Follow https://github.com/jai/gchat-mentions/blob/main/docs/agent-setup.md to set up Google Chat mention tracking on this computer.
```

Read [the agent setup guide](docs/agent-setup.md) to see the full steps the agent follows.

## Install manually

### Prerequisites

- macOS or Linux.
- [Go 1.25 or newer](https://go.dev/dl/).
- [`gog`](https://github.com/steipete/gogcli) (the Google Workspace CLI) signed in to your Google Workspace account. Read [the `gog` setup guide](docs/gog-setup.md) for setup steps.

### Setup steps

1. Sign in to `gog` by following [the `gog` setup guide](docs/gog-setup.md), then verify that your account lists `chat`:

   ```sh
   gog auth list
   ```

2. Install the `gchat-mentions` binary:

   ```sh
   go install github.com/jai/gchat-mentions/cmd/gchat-mentions@latest
   ```

   To build from source instead, clone the repository and run `make build`:

   ```sh
   git clone https://github.com/jai/gchat-mentions.git
   cd gchat-mentions
   make build
   ```

3. Create a starter configuration file:

   ```sh
   gchat-mentions --init
   ```

4. Open `~/.config/gchat-mentions/config.yaml` and set `account` and `user_name`.

5. Run your first check:

   ```sh
   gchat-mentions --format compact
   ```

6. Schedule recurring digest checks with `cron` (optional):

   ```cron
   0 8-20/2 * * * gchat-mentions --no-urgent --format compact >/dev/null 2>&1
   ```

## Output for agents and scripts

Each run writes the latest report atomically to `~/.local/state/gchat-mentions/latest.json`. Pass `--out none` to skip writing the file.

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

## Configuration and flags

Settings live in `~/.config/gchat-mentions/config.yaml`. Command-line flags override config values for one run. Run `gchat-mentions --help` for the complete flag list.

| Setting | Flag | Purpose |
| --- | --- | --- |
| `account` | `--account` | Google Workspace email authorized in `gog` |
| `user_name` | `--user` | Display name used for mention matching |
| `user_id` | `--user-id` | Google Chat user ID (`users/123`) |
| `gog_env_file` | (config only) | `KEY=VALUE` file loaded for headless `gog` runs |
| `notify_space` | `--notify-space` | Google Chat space ID for unread digests (pass `none` to disable) |
| `output_path` | `--out` | JSON report path (default `~/.local/state/gchat-mentions/latest.json`) |
| `active_within` | `--active-within` | Only scan spaces active within this duration (`6h`, `168h`) |
| `include_dms` | `--include-dms` | Include unread direct messages |
| `exclude_spaces` | `--exclude` | Space name patterns to skip |
| `announcement_spaces` | `--atall-mute` | Spaces where `@all` and `@here` messages are muted |
| `ignore_prefixes` | `--ignore-prefix` | Automated message prefixes to skip |
| `urgent` | `--urgent`, `--no-urgent`, `--urgent-init`, `--urgent-test` | Urgency classification and dry-run or live escalation |

## Urgent triage

`gchat-mentions` can score new mentions and direct messages for actionability and urgency. You edit the classifier questions in `~/.config/gchat-mentions/urgent-prompts.yaml` without rebuilding the binary.

Supported classifiers include:

- Cloudflare Clef and Clef-flash (Cloudflare Workers AI decision models)
- TypeSafe SystemOne
- Cloudflare Workers AI text models
- Any OpenAI-compatible chat completions endpoint

Urgent triage defaults to `dry-run` mode. In `live` mode, rate-limited escalations post JSON to a webhook (with custom URL paths, payload templates, and field limits) or run a fallback command. API keys stay in local `chmod 600` env files referenced from `config.yaml`.

Read [the urgent mentions guide](docs/urgent-mentions.md) for provider setup, prompt editing, and rollout steps.

## Exit codes

- `0`: Poll completed.
- `1`: Runtime or poll error.
- `2`: Invalid flags or missing required settings.

## Development

Run lint, tests, and build locally:

```sh
make lint
make all-tests
make build
```

Read [the repository workflow guide](docs/workflow.md) for CI details.
