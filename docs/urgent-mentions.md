# Urgent Mentions

`gchat-mentions` can classify new unread mentions and direct messages for urgency and trigger optional, rate-limited escalations.

Urgent triage is disabled by default. Basic mention polling works without any classifier keys.

## How it works

1. `gchat-mentions` polls unread mentions (and direct messages when `include_dms` is enabled). Use `active_within` (for example `6h`) so frequent runs only inspect recently active spaces.
2. Each unseen mention newer than `max_age_hours` is queued for classification (up to `max_per_run`, newest first; overflow rolls into `pending.json` for the next run).
3. Recent thread messages are fetched on demand and placed inside an untrusted data block (`<<<UNTRUSTED_CHAT_DATA_BEGIN>>>` ... `<<<UNTRUSTED_CHAT_DATA_END>>>`) before calling your configured classifier providers in parallel.
4. Every verdict is appended to `~/.local/state/gchat-mentions/urgent/verdicts.jsonl`.
5. Deterministic code checks `rule` (`either`, `both`, or a specific provider name) and `threshold`, applies rate limits and quiet hours, writes an audit brief to `escalations/<timestamp>/brief.md`, and runs dry-run or live escalation.

## Supported classifier providers

Configure one provider, or two or more in shadow mode to compare accuracy, latency, and cost:

- `typesafe`: TypeSafe SystemOne (`model` defaults to `jev-1.14.0`, `api_key_env` defaults to `TYPESAFE_API_KEY`).
- `cloudflare`: Cloudflare Workers AI (`model` defaults to `@cf/meta/llama-3.1-8b-instruct-fast`, `account_id_env` defaults to `CLOUDFLARE_ACCOUNT_ID`, `api_key_env` defaults to `CLOUDFLARE_API_TOKEN`).
- `openai-compatible`: Any Chat Completions endpoint (`url`, `model`, and optional `api_key_env` for OpenAI, OpenRouter, LiteLLM, or local Ollama).

Keep API keys out of `config.yaml`. Point each provider's `env_file` to a `chmod 600` `KEY=VALUE` file, or export the environment variables before running.

## Example configuration

Add an `urgent:` section to `~/.config/gchat-mentions/config.yaml`:

```yaml
account: you@company.com
user_name: Your Name
user_id: users/1234567890
gog_env_file: ~/Library/Application Support/gogcli/keyring.env
notify_space: spaces/YOUR_PERSONAL_SPACE
active_within: 168h

urgent:
  enabled: true
  mode: dry-run                 # dry-run (default) | live | auto
  # live_flag_file: ~/.config/gchat-mentions/escalation-live
  timezone: UTC                 # IANA timezone, e.g. Asia/Bangkok
  user_context: "Engineering Lead"
  include_dms: true
  threshold: 0.8
  rule: either                  # either | both | <provider-name>
  max_per_run: 25
  max_age_hours: 6
  quiet_hours:
    start: 22
    end: 7
  rate_limit:
    min_gap_minutes: 30
    max_per_day: 6
  providers:
    - name: typesafe
      type: typesafe
      model: jev-1.14.0
      env_file: ~/.credentials/typesafe
    - name: cloudflare
      type: cloudflare
      model: "@cf/meta/llama-3.1-8b-instruct-fast"
      env_file: ~/.credentials/cloudflare-workers-ai
  escalation:
    webhook:
      env_file: ~/.credentials/urgent-webhook
      key_header: Authorization
      key_scheme: Bearer
    command:
      argv:
        - my-readonly-agent
        - --brief-file
        - "{{brief_path}}"
      detach: true
```

## Safe rollout workflow

1. Bootstrap seen history so old unread messages are not classified on the first run:

   ```sh
   gchat-mentions --urgent-init --active-within 6h --notify-space none
   ```

2. Test classification and dry-run escalation with a synthetic outage message (never posts or spawns live):

   ```sh
   gchat-mentions --urgent-test
   gchat-mentions --urgent-test --urgent-offline
   ```

3. Run in shadow (`dry-run`) mode on a 5-minute schedule while keeping a 2-hour digest on the same config:

   ```sh
   # 5-minute urgent watcher (no digest post)
   gchat-mentions --urgent --active-within 6h --notify-space none --format compact

   # 2-hour digest (no urgent classification)
   gchat-mentions --no-urgent --format compact
   ```

4. Review `~/.local/state/gchat-mentions/urgent/verdicts.jsonl` and `run.log`. When verdicts match your expectations, switch `mode: live` (or create your `live_flag_file`).
