# Urgent Mentions

`gchat-mentions` can classify new unread mentions and direct messages for actionability and urgency using editable YAML prompts, and trigger optional, rate-limited escalations.

Urgent triage is disabled by default. Basic mention polling works without any classifier keys.

## How it works

1. `gchat-mentions` polls unread mentions (and direct messages when `include_dms` is enabled). Use `active_within` (for example `6h`) so frequent runs only inspect recently active spaces.
2. Each unseen mention newer than `max_age_hours` is queued for classification (up to `max_per_run`, newest first; overflow rolls into `pending.json` for the next run).
3. Recent thread messages are fetched on demand and sent as clean structured JSON `state` (`{space, context, target_message}`) alongside your editable `questions` from `~/.config/gchat-mentions/urgent-prompts.yaml`.
4. Every verdict is appended to `~/.local/state/gchat-mentions/urgent/verdicts.jsonl`, and actionable/urgent summaries are included in `latest.json`.
5. Deterministic code evaluates two independent `noul` scores:
   - **Actionable:** `needs_action >= action_threshold` (default `0.70`)
   - **Urgent escalation:** `(needs_action >= action_threshold && urgent >= urgent_threshold)` **or** `urgent >= urgent_override_threshold` (default `0.85`, for `@all` emergencies where nobody tagged you by name; set `0` to disable).

## Supported classifier providers

Configure one provider, or two or more in shadow mode to compare accuracy, latency, and cost:

- `cloudflare-clef`: Cloudflare Workers AI decision models (`@cf/cloudflare/clef` default, `@cf/cloudflare/clef-flash`, or `@cf/cloudflare/clef-omni`). Prefill-only calibrated probabilities (`output_tokens: 0`) using the native Clef `model` + structured JSON `state` + `questions` schema. `type: cloudflare` with a `@cf/cloudflare/clef*` model also routes here automatically.
- `typesafe`: TypeSafe SystemOne (`model` defaults to `jev-1.14.0`, `api_key_env` defaults to `TYPESAFE_API_KEY`).
- `cloudflare`: Cloudflare Workers AI text-generation models (`model` defaults to `@cf/meta/llama-3.1-8b-instruct-fast`, `account_id_env` defaults to `CLOUDFLARE_ACCOUNT_ID`, `api_key_env` defaults to `CLOUDFLARE_API_TOKEN`).
- `openai-compatible`: Any Chat Completions endpoint (`url`, `model`, and optional `api_key_env` for OpenAI, OpenRouter, LiteLLM, or local Ollama).

Keep API keys out of `config.yaml`. Point each provider's `env_file` to a `chmod 600` `KEY=VALUE` file, or export the environment variables before running.

## Editable prompts file

Generate an editable starter prompts file so you can tune questions without rebuilding the binary:

```sh
gchat-mentions --init-prompts
```

This creates `~/.config/gchat-mentions/urgent-prompts.yaml` (override with `urgent.prompts_file` or `--prompts`, or define inline `urgent.questions` in `config.yaml`). Placeholders `{{user_name}}`, `{{user_context}}`, and `{{recipient}}` are substituted from `config.yaml`.

```yaml
questions:
  needs_action:
    type: noul
    instructions: >-
      From the Google Chat messages provided, does target_message indicate that
      {{recipient}} needs to personally act, approve, decide, or reply?
    criteria:
      "true": >-
        Target message asks or expects {{user_name}} personally to act, approve,
        decide, unblock, or reply, and is not already resolved in context.
      "false": >-
        Informational update, status notice, automated digest, broadcast announcement,
        addressed to someone else, or already resolved in context.
  urgent:
    type: noul
    instructions: >-
      For {{recipient}}, would the request in target_message generally be considered urgent?
    criteria:
      "true": >-
        Time-sensitive issue that needs attention soon (active outage or incident,
        someone actively blocked, or a short deadline).
      "false": >-
        Can wait for normal async review, routine check-in, or informational only.
```

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
  user_context: "CTO at an early-stage fintech startup"
  prompts_file: ~/.config/gchat-mentions/urgent-prompts.yaml
  include_dms: true
  action_threshold: 0.70
  urgent_threshold: 0.70
  urgent_override_threshold: 0.85
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
    - name: clef
      type: cloudflare-clef
      model: "@cf/cloudflare/clef"
      env_file: ~/.credentials/cloudflare-workers-ai
  escalation:
    webhook:
      env_file: ~/.credentials/urgent-webhook
      url_env: URL
      # url_path: /alerts         # Optional path appended to the resolved base URL
      key_env: KEY
      key_header: Authorization
      key_scheme: Bearer
      # Optional custom JSON payload template (omit to send the default full event object).
      # String placeholders: {{sender}}, {{space}}, {{space_id}}, {{space_type}},
      # {{text}}, {{excerpt}}, {{link}}, {{message_id}}, {{ts_local}}, {{scores}}, {{paging_allowed}}.
      # {{scores}} is the classifier's internal scoring: keep it out of text a person reads
      # (verdicts.jsonl already records it for every mention).
      # payload:
      #   title: "Urgent Chat: {{sender}} ({{space}})"
      #   body: "{{excerpt}}\n{{link}}"
      #   repeat_minutes: 30
      # field_max_runes:
      #   title: 100
      #   body: 500
    command:
      argv:
        - my-readonly-agent
        - --brief-file
        - "{{brief_path}}"
      detach: true
```

## Safe rollout workflow

1. Create the editable prompts file and bootstrap seen history so old unread messages are not classified on the first run:

   ```sh
   gchat-mentions --init-prompts
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
