# gchat-mentions

A small, self-contained Go CLI that polls Google Chat for unread `@mentions` (and `@all`), writes a clean JSON status file for your AI agent or scripts, and optionally posts a digest to a Google Chat space.

## Requirements

- [Go 1.25+](https://go.dev/dl/) (to build from source)
- [`gog`](https://github.com/steipete/gogcli) authenticated with your Google Workspace account (`chat` scope)

## Quick start (human or AI agent)

1. **Build and install**

   ```sh
   git clone https://github.com/jai/gchat-mentions.git
   cd gchat-mentions
   go install ./cmd/gchat-mentions
   ```

2. **Verify Google Chat auth in `gog`**

   ```sh
   gog auth list
   ```

   If your work account is not listed with the `chat` service, authorize it:

   ```sh
   gog auth add you@company.com --services chat
   ```

3. **Create your config**

   ```sh
   gchat-mentions --init
   ```

   Edit `~/.config/gchat-mentions/config.yaml` and set your `account` and `user_name` (and optionally `notify_space`, `announcement_spaces`, `exclude_spaces`, `ignore_prefixes`).

4. **Run once**

   ```sh
   gchat-mentions --format compact
   ```

   Every run atomically updates `~/.local/state/gchat-mentions/latest.json` (override with `--out` or `output_path`; pass `--out none` to skip writing).

## Output for AI agents

By default, `gchat-mentions` writes the latest snapshot to:

```text
~/.local/state/gchat-mentions/latest.json
```

Schema:

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

Any local agent can read `~/.local/state/gchat-mentions/latest.json` directly (or invoke `gchat-mentions --format json`) to answer "what unread Google Chat mentions do I have?" with direct links.

## Scheduling (macOS / Linux)

Run every 2 hours during work hours with `cron`:

```cron
0 8-20/2 * * * gchat-mentions --format compact >/dev/null 2>&1
```

Add `--notify-space spaces/YOUR_SPACE_ID` (or set `notify_space` in `config.yaml`) if you also want a digest posted into a personal Google Chat space.

## CLI flags

| Flag | Default | Description |
| --- | --- | --- |
| `--init` | `false` | Create starter config at `~/.config/gchat-mentions/config.yaml` and exit |
| `--config` | `~/.config/gchat-mentions/config.yaml` | Path to YAML config file |
| `--account` | from config | Google Workspace email authorized in `gog` |
| `--user` | from config | Display name for mention matching |
| `--user-id` | from config | Optional Google Chat user ID (`users/123`) |
| `--spaces` | all `SPACE`s | Comma-separated space name filters |
| `--exclude` | from config | Comma-separated space name patterns to skip |
| `--atall-mute` | from config | Comma-separated announcement spaces where `@all`-only is muted |
| `--ignore-prefix` | from config | Comma-separated automated message prefixes to ignore |
| `--notify-space` | from config | Optional Google Chat space ID to post digests to |
| `--out` | `~/.local/state/gchat-mentions/latest.json` | JSON status file path (`none` to disable) |
| `--format` | `json` | Stdout format: `json`, `compact`, or `text` |
| `--timeout` | `5m` | Maximum runtime |
| `--version` | `false` | Print version and exit |

## Development

```sh
make lint
make test
make build
```
