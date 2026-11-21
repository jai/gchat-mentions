#!/usr/bin/env bash
# Interactive, agent-safe setup for gog OAuth credentials and Google account sign-in.
# Run this in your own Terminal window so your OAuth client secret never passes
# through an AI agent's chat or logs.
set -euo pipefail

if ! command -v gog >/dev/null 2>&1; then
  echo "gog is not installed yet."
  echo "On macOS, install it with: brew install gogcli"
  echo "Otherwise see: https://github.com/steipete/gogcli"
  exit 1
fi

echo "=== Google Workspace (gog) Setup ==="
echo "Your OAuth client secret stays on this computer and is never echoed."
echo

TMP_JSON=""
cleanup() {
  if [ -n "$TMP_JSON" ] && [ -f "$TMP_JSON" ]; then
    rm -f "$TMP_JSON"
  fi
}
trap cleanup EXIT

# 1. OAuth client credentials
if gog auth credentials list --plain --no-input 2>/dev/null | grep -q .; then
  read -r -p "OAuth client credentials are already configured in gog. Replace them? [y/N]: " replace_creds
else
  replace_creds="y"
fi

if [[ "${replace_creds:-n}" =~ ^[Yy]$ ]]; then
  echo
  echo "How do you have your Google OAuth client credentials?"
  echo "  1) Paste Client ID and Client Secret (e.g. from 1Password)"
  echo "  2) Use a downloaded credentials JSON file"
  read -r -p "Choose [1/2] (default 1): " cred_mode
  cred_mode="${cred_mode:-1}"

  read -r -p "Work email domain to map this client to (optional, e.g. company.com): " work_domain
  domain_args=()
  if [ -n "$work_domain" ]; then
    domain_args=(--domain "$work_domain")
  fi

  if [ "$cred_mode" = "2" ]; then
    read -r -p "Path to credentials JSON file (you can drag and drop it here): " json_path
    # Trim surrounding whitespace and single/double quotes from drag-and-drop
    json_path="${json_path#"${json_path%%[![:space:]]*}"}"
    json_path="${json_path%"${json_path##*[![:space:]]}"}"
    json_path="${json_path#\'}"
    json_path="${json_path%\'}"
    json_path="${json_path#\"}"
    json_path="${json_path%\"}"
    if [ ! -f "$json_path" ]; then
      echo "Error: file not found: $json_path" >&2
      exit 1
    fi
    gog auth credentials set "$json_path" "${domain_args[@]}"
  else
    read -r -p "Paste OAuth Client ID: " client_id
    read -r -s -p "Paste OAuth Client Secret (hidden): " client_secret
    echo
    if [ -z "$client_id" ] || [ -z "$client_secret" ]; then
      echo "Error: both Client ID and Client Secret are required." >&2
      exit 1
    fi
    TMP_JSON=$(mktemp "${TMPDIR:-/tmp}/gog-oauth.XXXXXX")
    chmod 600 "$TMP_JSON"
    python3 -c '
import json, sys
payload = {
    "installed": {
        "client_id": sys.argv[1],
        "client_secret": sys.argv[2],
        "auth_uri": "https://accounts.google.com/o/oauth2/auth",
        "token_uri": "https://oauth2.googleapis.com/token",
        "redirect_uris": ["http://localhost"],
    }
}
with open(sys.argv[3], "w", encoding="utf-8") as f:
    json.dump(payload, f)
' "$client_id" "$client_secret" "$TMP_JSON"
    gog auth credentials set "$TMP_JSON" "${domain_args[@]}"
  fi
  echo "Saved OAuth client credentials to gog."
fi

# 2. Account and services
echo
read -r -p "Your work Google email address: " email
if [ -z "$email" ]; then
  echo "Error: email address is required." >&2
  exit 1
fi

echo
echo "What would you like to connect? (You can add or remove permissions anytime later.)"
echo "  1) Google Chat only (recommended for gchat-mentions)"
echo "  2) Google Chat and Calendar"
echo "  3) Custom list (for example: chat,calendar,gmail)"
read -r -p "Choose [1/2/3] (default 1): " scope_choice
scope_choice="${scope_choice:-1}"

case "$scope_choice" in
  1) services="chat" ;;
  2) services="chat,calendar" ;;
  3)
    read -r -p "Comma-separated services (e.g. chat,calendar,gmail,drive): " services
    ;;
  *)
    echo "Error: invalid choice." >&2
    exit 1
    ;;
esac

if [ -z "$services" ]; then
  echo "Error: at least one service is required." >&2
  exit 1
fi

echo
echo "Opening your browser to sign in as $email ($services)..."
gog auth add "$email" --services "$services" --force-consent

echo
echo "=== Setup complete ==="
gog auth list
echo
echo "You can close this Terminal window and tell your AI agent to continue."
