# Outlook TUI

A gorgeous, responsive, and fully-featured Terminal User Interface (TUI) client for Microsoft Outlook 365, built in Go using [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

## Features

- 📂 **Multi-Pane Layout**: Standard desktop/web email layout split into folders, message lists, and message detail views. Quoted original/forwarded messages and inline quotes are automatically detected and grayed out (dimmed) in the detail pane to separate new content from conversation history.
- 🔗 **URL & Link Highlighting**: Automatically parses HTML links (hiding URLs while styling the link text in a distinct cyan/blue with an underline) and styles all plain-text URLs in the message details view to make them easily recognizable.
- 💻 **Syntax Highlighting & Diff Coloring**: Automatically highlights code blocks (`<pre>`) in violet and inline code (`<code>`) in yellow. Additionally, developer emails containing Git/GitLab/GitHub unified diffs are fully colorized in the details view (additions in green, deletions in red, hunk headers in blue, and files/git metadata in yellow) for a clean review experience. Furthermore, it parses HTML inline style attributes (like `color` and `background-color`) and `font` / `bgcolor` tags to render colored text highlights directly inside the terminal detail pane (intelligently mapping background highlight colors to readable foreground text colors and applying contrast guards based on the dark/light theme).
- 🔑 **Device Code Flow Authentication**: Authenticate securely using Microsoft's standard OAuth2 device flow (no app credentials/passwords stored locally, only access/refresh tokens).
- 🔄 **Automatic Token Refresh**: The client automatically handles token expiration and refreshes OAuth2 access tokens in the background.
- 📥 **Background Fetching**: Syncs mail folders and automatically fetches new messages in the background (configurable interval, defaults to every 5 minutes).
- 🗄️ **SQLite Message Cache & Contacts Autocomplete** *(opt-in)*: When `use_sqlite` is set to `1`, messages are stored locally in `~/.cache/outlook-tui/db.db`. On next launch, cached messages display instantly while the API refreshes in the background. Deleting a message and marking it read are also reflected in the cache. **Additionally, this enables offline contact suggestions**: when composing an email, typing in the `To` field will show a popup with unique contact email addresses compiled from your locally cached messages, allowing fast auto-completion.
- 🧵 **Conversation Threading**: Messages are automatically grouped into conversation threads by Microsoft's `conversationId`. Threads with multiple messages are collapsed by default (showing the most recent), and can be expanded/collapsed with `Space`. Expanded threads show each reply indented with a `└` tree connector, sender name, and date. A `▶`/`▼` indicator and reply-count badge `[N]` mark collapsible threads.
- ✉️ **Send & Compose Mail**: Press `n` to open a full compose screen to draft and send new emails. Includes a `Cc` field for copying additional recipients. With SQLite caching enabled, it features interactive autocomplete for both the `To` and `Cc` fields (use `Up`/`Down` to navigate suggestions, `Enter` to select/autocomplete, and `Esc` to close suggestions popup). **You can press `Ctrl+v` (or `Ctrl+shift+v`/`Ctrl+V`) when composing the body to paste an image directly from the clipboard. You can also press `Ctrl+f` to open a file-browsing popup to choose local files to attach. Composed messages with pasted images will convert placeholders like `[Image X]` into inline images on send, and when rendering messages, the pasted inline images display as styled `[image]` labels in the viewport.**
- 🗑️ **Delete & Recover Messages**: Press `d` or `Delete` to move messages to the Trash (Deleted Items folder) on Outlook. Press `D` (Capital D) to delete the entire conversation thread of the selected message at once (with confirmation). Press `U` (Capital U) to recover/undelete a message from the Deleted Items folder back to the Inbox.
- 📎 **Attachments Support**: Press `a` to view a list of attachments on the current email (including inline images and external remote image URLs parsed from HTML bodies), download them locally to your `Downloads` directory, and automatically open them using `xdg-open`. Saved files are uniquely prefixed with a hash of the message ID to prevent duplicate downloads (e.g. `xyz (1).csv`, `xyz (2).csv`) and are opened instantly if already downloaded.
- 📜 **Smooth Navigation**: Tab between panes, scroll using arrow keys, page up/down, or the mouse wheel.

## Directory Structure

The project is structured as follows:
* [main.go](main.go) - Application launcher & Bubble Tea configuration.
* [config.go](config.go) - Manages application settings (`~/.config/outlook-tui/config.json`).
* [auth.go](auth.go) - Manages Device Flow authentication & OAuth2 roundtrippers.
* [graph.go](graph.go) - Custom Microsoft Graph API client for fetching mail, sending, deleting, and downloading.
* [db.go](db.go) - Optional SQLite cache layer (`~/.cache/outlook-tui/db.db`): stores messages per folder for instant display on startup.
* [tui.go](tui.go) - Contains the layout rendering, key bindings, and user interface updates.

---

## Azure Setup (How to Get a Client ID)

To connect to Outlook 365, you need to register a public client application in your Microsoft Azure / Entra portal. It is free and takes about 1-2 minutes:

1. Go to the [Microsoft Entra admin center](https://entra.microsoft.com/) (formerly Azure Portal -> Azure Active Directory).
2. Go to **Applications** -> **App registrations** -> **New registration**.
3. Fill in the details:
   - **Name**: `Outlook TUI` (or any name you prefer)
   - **Supported account types**: Choose `Accounts in any organizational directory (Any Microsoft Entra ID tenant - Multitenant) and personal Microsoft accounts (e.g. Skype, Xbox)` (This is standard for multi-tenant and personal Outlook.com / Hotmail mailboxes).
   - **Redirect URI**: Leave empty.
   - Click **Register**.
4. Once registered, copy the **Application (client) ID**. This is the Client ID you will paste into the TUI.
5. Enable Public Client Flows (Crucial for Device Code Flow):
   - Under **Authentication** in the left menu, scroll down to **Advanced settings** (or **Allow public client flows**).
   - Set **"Allow public client flows"** (or **"Enable the following mobile and desktop flows"**) to **Yes**.
   - Click **Save**.
6. Set delegated API Permissions:
   - Go to **API permissions** in the left menu -> **Add a permission** -> **Microsoft Graph** -> **Delegated permissions**.
   - Search for and check the following permissions:
     - `Mail.ReadWrite` (To load and mark read/unread)
     - `Mail.Send` (To send new messages)
     - `User.Read` (To read profile/user metadata)
     - `offline_access` (Normally added automatically, this lets you get refresh tokens so you don't have to re-login every hour).
   - Click **Add permissions**.

---

## How to Run

1. Build the application:
   ```bash
   go build -o outlook-tui
   ```
2. Run the application:
   ```bash
   ./outlook-tui

   # you may also want to copy the binary to your PATH (and run it from any place), e.g.:
   sudo cp outlook-tui /usr/local/bin/
   
   ```
3. Enter your Azure **Client ID** and **Tenant ID** (defaults to `common` which works for both corporate and personal accounts).
4. The TUI will show a link and an 8-character device code.
5. Open the link (`https://microsoft.com/devicelogin`), enter the code, and log in with your Outlook account.
6. The TUI will automatically detect the login, load your inbox, and you're ready to go!

### Resetting Config

If you want to log in with a different account or reset your client configuration:
```bash
./outlook-tui --reset
```

## Configuration

Configuration settings are stored in `~/.config/outlook-tui/config.json`. The supported parameters are:

* `client_id`: The Microsoft Azure App Registration Application (client) ID.
* `tenant_id`: The Microsoft Entra Tenant ID (defaults to `"common"`, which works for both corporate/school and personal Outlook accounts).
* `refresh_time_min`: The background refresh/fetching interval in minutes (defaults to `5`).
* `layout`: The UI layout mode (defaults to `1`).
  - **`1` (default) — Side-by-side**: Three panes arranged horizontally: `[Folders | Messages | Detail]`.
  - **`2` — Stacked left column**: Folders and Messages are stacked vertically on the left (~30% / ~70% height split), with the Detail pane occupying the wider right column. In this layout, the Messages pane displays the date and time of the message next to the author.
* `use_sqlite`: Enable the local SQLite message cache (defaults to `0` — disabled).
  - **`0` (default)** — No local cache; messages are always fetched fresh from the Microsoft Graph API.
  - **`1`** — Cache messages in `~/.cache/outlook-tui/db.db`. On subsequent launches, cached messages for the first folder are displayed immediately while a fresh fetch runs in the background. Switching folders also shows cached messages instantly. Deleting a message or opening it (marking read) updates the cache.
* `excluded_folders`: A list of folder names (display name or well-known name) that should not be shown in the Folders pane (e.g. `["Junk Email", "RSS Feeds"]`). Matching is case-insensitive.
* `scroll_lines`: The number of lines to scroll when scrolling the Message Detail pane (defaults to `1`).
* `image_viewer`: The command/executable used to open image attachments (e.g. `"sxiv"` or `"feh"`). If empty, not specified, or if the attachment is not an image, it defaults to using `xdg-open`. When set, pressing Enter on an image attachment downloads all image attachments in the current email and loads them all into the viewer, automatically starting at the selected one (with support for `sxiv`/`nsxiv`/`imv`'s `-n` and `feh`'s `--start-at` flags).
* `attachment_dir`: The directory where attachments are downloaded (defaults to `~/Downloads`, falling back to your user home directory if not found). Paths starting with `~/` are expanded to your home directory, and the folder will be automatically created if it does not exist.
* `terminal_bell`: Whether to sound the terminal bell (`\a`) when a new message notification is triggered (defaults to `1` — enabled).
  - **`0`** — Disabled.
  - **`1` (default)** — Enabled.
* `theme`: The UI color theme (defaults to `"catppuccin"`).
  - **`"catppuccin"` (default)** — A gorgeous theme based on Catppuccin Mocha colors.
  - **`"teams"`** — A theme mimicking teams-tui-go palette
* `browser_command`: The command/executable used to open URLs in the browser (defaults to `"xdg-open"`). You can change this to any browser command you prefer (e.g., `"google-chrome"`, `"firefox"`).

Example `~/.config/outlook-tui/config.json` to use Layout 2 with SQLite caching, folder exclusions, 5-line scrolling, custom download folder, sxiv for images, and Teams theme:
```json
{
  "client_id": "your-azure-client-id",
  "tenant_id": "common",
  "refresh_time_min": 5,
  "layout": 2,
  "use_sqlite": 1,
  "excluded_folders": ["Junk Email", "RSS Feeds"],
  "scroll_lines": 5,
  "image_viewer": "sxiv",
  "attachment_dir": "~/Downloads/attachments",
  "terminal_bell": 1,
  "theme": "teams",
  "browser_command": "xdg-open"
}
```

---

## Key Bindings

| Key                            | Action                                                                                                                                                                                                                                                                 |
| ------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Tab`                          | Switch focus between the Folders, Messages, and Message Detail panes                                                                                                                                                                                                   |
| `Shift+Tab`                    | Switch focus in reverse order                                                                                                                                                                                                                                          |
| `Up` / `Down` (or `k` / `j`)   | Navigate selection in the focused pane                                                                                                                                                                                                                                 |
| `K` / `J`                      | Navigate selection in Messages pane (when in Folders pane), or scroll message in Details pane (when in Messages pane)                                                                                                                                                  |
| `Space`                        | Toggle expand/collapse for the selected conversation thread (works when focused on either the Messages or Folders pane)                                                                                                                                                |
| `PageUp` / `PageDown`          | Scroll message body up/down                                                                                                                                                                                                                                            |
| `n`                            | Compose a new email                                                                                                                                                                                                                                                    |
| `A`                            | Reply to the selected message. Automatically replies to the sender if they are the only other participant, or prompts to choose Reply vs Reply All if there are multiple participants.                                                                                 |
| `Ctrl+s` (or `Ctrl+x`)         | Send the message (when in Compose view)                                                                                                                                                                                                                                |
| `Ctrl+v` (or `Ctrl+V`/`Shift`) | Paste image from clipboard (when focused on Body in Compose view)                                                                                                                                                                                                      |
| `Ctrl+f`                       | Open file picker popup to attach local files (when in Compose view)                                                                                                                                                                                                    |
| `Ctrl+g`                       | Open the compose **body** in your external editor (`$EDITOR` / `$VISUAL` / `vi`). The TUI suspends while the editor is running; on exit the body is loaded back into the compose view.                                                                                 |
| `d` (or `Delete`)              | Move the selected message to Deleted Items (Trash)                                                                                                                                                                                                                     |
| `D`                            | Delete all messages in the selected thread (requires confirmation)                                                                                                                                                                                                     |
| `U`                            | Recover/undelete the selected message (moves it back to the Inbox)                                                                                                                                                                                                     |
| `R`                            | Toggle the selected message's Read/Unread status                                                                                                                                                                                                                       |
| `f`                            | Toggle Favorite status (adds/removes the selected message to/from the local Favorites folder)                                                                                                                                                                          |
| `r`                            | Reload/refresh messages in the selected folder                                                                                                                                                                                                                         |
| `M`                            | Load the next portion/page of 50 messages in the selected folder                                                                                                                                                                                                       |
| `a`                            | View and select attachments on the current email                                                                                                                                                                                                                       |
| `y`                            | Open the Yank menu/combinations to copy content to the clipboard (displays a selection dropdown):<br>• `ym`: Copy original message (without quoting)<br>• `ya`: Copy all message (with quoting)<br>• `yu`: Yank URL(s) from message body<br>• `ys`: Copy email subject |
| `o`                            | Extract URLs from the selected message and open them (shows a selection popup if multiple unique URLs exist). YouTrack and GitLab URLs are opened in their respective TUI apps (`yt-tui` / `gitlab-tui`) if installed, and fall back to opening in the browser (via the configured `browser_command`) otherwise. All other links are opened directly in the browser. |
| `?`                            | Toggle help popup describing app functionality and shortcuts                                                                                                                                                                                                           |
| `Enter` (in Attachments list)  | Save the selected attachment to your local `Downloads` directory and open it with `xdg-open`                                                                                                                                                                           |
| `Esc`                          | Go back (cancel compose [with confirmation if the body is filled], close attachments list, close help popup, or go back to config)                                                                                                                                     |
| `q` (or `Ctrl+C`)              | Quit the application                                                                                                                                                                                                                                                   |

---

## External Editor for Compose Body

Press **`Ctrl+g`** while in the Compose view to open the email body in your preferred text editor. The TUI is cleanly suspended while the editor is active and restored after you quit the editor. This is ideal for writing long messages in your full-featured editor.

The editor is resolved in priority order:
1. `$EDITOR` environment variable
2. `$VISUAL` environment variable
3. `vi` (built-in fallback)

Example — configure neovim as the editor:

```sh
export EDITOR='/home/robertn/.local/share/bob/nvim-bin/nvim'
```

You can also pass extra arguments:

```sh
export EDITOR='nvim -u NONE'   # open without user config
```

Add the `export` line to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.) to make it permanent.

## URL Opening & TUI Integrations (`yt-tui` & `gitlab-tui`)

Outlook TUI lets you quickly open URLs found in messages:
- Press **`o`** on a message containing URLs.
- If there is a single URL, it opens directly.
- If there are multiple unique URLs, a popup dialog will display a list for you to select from (recognized GitLab and YouTrack URLs are automatically sorted to the top of the list).
- **TUI Integrations**:
  - GitLab Merge Request/Pipeline/Job URLs and YouTrack Issue URLs will automatically attempt to open in the external [gitlab-tui](https://github.com/nospor/gitlab-tui) or [yt-tui](https://github.com/nospor/yt-tui) apps, respectively, if they are available in your system `PATH`.
  - If the specialized TUI app is not found in your system `PATH`, these URLs will fall back to opening in your standard web browser.
- **Browser Fallback / Other Links**:
  - General links and TUI links (without their TUI apps installed) are opened in the browser using the configured `browser_command` (which defaults to `"xdg-open"` but can be changed in `config.json`).

---

## Automated Changelog Generation

This project uses `git-cliff` to automatically generate and maintain its `CHANGELOG.md`.

When a git tag starting with `v` (e.g., `v1.0.0`) is pushed to GitHub, a GitHub Actions workflow (`.github/workflows/changelog.yml`) runs to:
1. Generate the updated `CHANGELOG.md` using the rules configured in `cliff.toml`.
2. Commit and push the updated changelog back to the `main` branch.

You can also run `git-cliff` locally to generate or preview the changelog:
```bash
git-cliff -o CHANGELOG.md
```

## License

See [LICENSE](LICENSE).

## Thanks For Visiting
Hope you liked it. Wanna **[buy Me a coffee](https://www.buymeacoffee.com/nospor)**?

