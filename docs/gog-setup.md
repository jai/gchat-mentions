# Setting Up the Google Workspace CLI (`gog`)

`gchat-mentions` reads Google Chat through [`gog`](https://github.com/steipete/gogcli), a local command-line tool that signs in with your own Google Workspace account.

> 💡 **Keep secrets out of AI chat.** Never paste your OAuth client secret into an AI agent prompt, and do not grant an agent access to your password manager. Enter the secret directly in a local Terminal window instead.

## 1. Install `gog`

On macOS with Homebrew:

```sh
brew install gogcli
```

On Linux or without Homebrew, download a release binary from [steipete/gogcli](https://github.com/steipete/gogcli).

## 2. Get your OAuth client credentials

`gog` needs a Google Cloud Desktop OAuth client (a Client ID and a Client Secret).

- **If your company shares an OAuth client:** Open the item your administrator shared in your password manager (for example, a 1Password link). Copy the Client ID and Client Secret, or download the JSON file.
- **If you are setting up your own:** Create a Desktop OAuth client in Google Cloud Console with the Google Chat API enabled, then download its JSON file.

## 3. Run the private Terminal setup helper

Run the interactive setup script in your own Terminal window:

```sh
./scripts/setup-gog-auth.sh
```

The script asks for three things:

1. **OAuth client credentials:** Paste the Client ID and Client Secret (input is hidden and never echoed) or provide the path to the downloaded JSON file. The script stores them in `gog` and deletes any temporary file immediately.
2. **Your work Google email:** Must be a Google Workspace account (personal `@gmail.com` accounts do not support the Google Chat API).
3. **What to connect:** Choose **Google Chat only** (smallest access, recommended to start) or **Google Chat and Calendar**. Your browser opens for the normal Google Sign-In approval screen.

### Using this with an AI agent on macOS

Your AI agent does not need your password manager or your OAuth client secret. Tell your agent to open the setup script in a separate macOS Terminal window for you:

```sh
open -a Terminal ./scripts/setup-gog-auth.sh
```

Complete the prompts in that Terminal window, close it when it prints `Setup complete`, and tell your agent to continue. Your agent can then verify access with `gog auth list`, which shows only your email and enabled services.

## 4. Changing permissions later

You are not locked into your first choice. You can add or remove Google services at any time:

- **Add or change services:** Run `gog auth add you@company.com --services chat,calendar --force-consent` (replace the list with the services you want).
- **Remove access completely:** Run `gog auth remove you@company.com`.
- **Check current status:** Run `gog auth list`.
