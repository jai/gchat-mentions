# Agent Setup Instructions

Follow these steps to install and configure `gchat-mentions` on the user's computer. Keep all replies to the user plain and non-technical, and do not dump raw CLI commands in chat unless asked.

## 1. Install prerequisites

1. Check if Go (1.25 or newer) and `gog` ([steipete/gogcli](https://github.com/steipete/gogcli)) are installed.
2. Install anything missing. On macOS with Homebrew, run `brew install go gogcli`.

## 2. Clone and install `gchat-mentions`

1. Clone `https://github.com/jai/gchat-mentions` (needed for the Terminal setup script).
2. Install the binary with `go install ./cmd/gchat-mentions` (or `make build`) and ensure `gchat-mentions` is on `PATH`.

## 3. Connect Google Workspace via `gog` (never touch secrets)

Run `gog auth list`. If the user's work Google account is not signed in with `chat`, follow [docs/gog-setup.md](gog-setup.md):

- Never ask the user to paste an OAuth Client ID or Client Secret into chat.
- Never access the user's password manager (such as 1Password).
- Ask the user in plain language what they want to connect: **Google Chat only** (recommended, smallest access) or **Google Chat and Calendar**. Remind them they can add or remove permissions anytime later.
- When the user is ready, open a separate macOS Terminal window running `./scripts/setup-gog-auth.sh` (with `open -a Terminal ./scripts/setup-gog-auth.sh`) so they can paste their OAuth Client ID and Secret directly from their password manager and complete Google Sign-In in their browser.
- Wait for the user to confirm they finished, then verify with `gog auth list`.

## 4. Configure `gchat-mentions`

1. Run `gchat-mentions --init` to create `~/.config/gchat-mentions/config.yaml`.
2. Ask the user for:
   - Their work Google email address.
   - Their Google Chat display name (what teammates type after `@`).
   - Optional: any announcement spaces where `@all` should be muted.
   - Optional: whether they want digests posted to a personal Google Chat space. If yes, check if they already have one (`gog chat spaces list --account <email> --plain`) or offer to create a new private space for them (for example, `gog chat spaces create "<FirstName>'s Personal Space" --account <email> --json --no-input`) and save its `spaces/...` ID as `notify_space`.
3. Save those values to `~/.config/gchat-mentions/config.yaml`.

## 5. Verify and schedule

1. Run `gchat-mentions --format compact` once and confirm `~/.local/state/gchat-mentions/latest.json` was written.
2. Ask for the user's preferred work hours and set up a recurring check every 2 hours (via `launchd` on macOS or `cron`) so `~/.local/state/gchat-mentions/latest.json` stays current.

## 6. Ongoing status checks

Whenever the user asks for their Google Chat status or unread mentions, read `~/.local/state/gchat-mentions/latest.json` (or run `gchat-mentions --format json` for a fresh check) and summarize what needs their attention with clickable links.
