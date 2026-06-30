# Outlook TUI

A gorgeous, responsive, and fully-featured Terminal User Interface (TUI) client for Microsoft Outlook 365, built in Go using [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

## Features

- 📂 **Multi-Pane Layout**: Standard desktop/web email layout split into folders, message lists, and message detail views. Quoted original/forwarded messages and inline quotes are automatically detected and grayed out (dimmed) in the detail pane to separate new content from conversation history.
- 🔗 **URL & Link Highlighting**: Automatically parses HTML links (hiding URLs while styling the link text in a distinct cyan/blue with an underline) and styles all plain-text URLs in the message details view to make them easily recognizable.
- 🔑 **Device Code Flow Authentication**: Authenticate securely using Microsoft's standard OAuth2 device flow (no app credentials/passwords stored locally, only access/refresh tokens).
- 🔄 **Automatic Token Refresh**: The client automatically handles token expiration and refreshes OAuth2 access tokens in the background.
- 📥 **Background Fetching**: Syncs mail folders and automatically fetches new messages in the background (configurable interval, defaults to every 5 minutes).
- 🗄️ **SQLite Message Cache & Contacts Autocomplete** *(opt-in)*: When `use_sqlite` is set to `1`, messages are stored locally in `~/.cache/outlook-tui/db.db`. On next launch, cached messages display instantly while the API refreshes in the background. Deleting a message and marking it read are also reflected in the cache. **Additionally, this enables offline contact suggestions**: when composing an email, typing in the `To` field will show a popup with unique contact email addresses compiled from your locally cached messages, allowing fast auto-completion.
- 🧵 **Conversation Threading**: Messages are automatically grouped into conversation threads by Microsoft's `conversationId`. Threads with multiple messages are collapsed by default (showing the most recent), and can be expanded/collapsed with `Space`. Expanded threads show each reply indented with a `└` tree connector, sender name, and date. A `▶`/`▼` indicator and reply-count badge `[N]` mark collapsible threads.
- ✉️ **Send & Compose Mail**: Press `n` to open a full compose screen to draft and send new emails. Includes a `Cc` field for copying additional recipients. With SQLite caching enabled, it features interactive autocomplete for both the `To` and `Cc` fields (use `Up`/`Down` to navigate suggestions, `Enter` to select/autocomplete, and `Esc` to close suggestions popup). **You can press `Ctrl+v` (or `Ctrl+shift+v`/`Ctrl+V`) when composing the body to paste an image directly from the clipboard. You can also press `Ctrl+f` to open a file-browsing popup to choose local files to attach. Composed messages with pasted images will convert placeholders like `[Image X]` into inline images on send, and when rendering messages, the pasted inline images display as styled `[image]` labels in the viewport.**
- 🗑️ **Delete & Recover Messages**: Press `d` or `Delete` to move messages to the Trash (Deleted Items folder) on Outlook. Press `D` (Capital D) to delete the entire conversation thread of the selected message at once (with confirmation). Press `U` (Capital U) to recover/undelete a message from the Deleted Items folder back to the Inbox.
- 📎 **Attachments Support**: Press `a` to view a list of attachments on the current email (including inline images and external remote image URLs parsed from HTML bodies), download them locally to your `Downloads` directory, and automatically open them using `xdg-open`.
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

1. Clone or navigate to the directory:
   ```bash
   cd /home/robertn/projects/vag1/html/outlook-tui
   ```
2. Build the application:
   ```bash
   go build -o outlook-tui
   ```
3. Run the application:
   ```bash
   ./outlook-tui
   ```
4. Enter your Azure **Client ID** and **Tenant ID** (defaults to `common` which works for both corporate and personal accounts).
5. The TUI will show a link and an 8-character device code.
6. Open the link (`https://microsoft.com/devicelogin`), enter the code, and log in with your Outlook account.
7. The TUI will automatically detect the login, load your inbox, and you're ready to go!

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
* `image_viewer`: The command/executable used to open image attachments (e.g. `"sxiv"` or `"feh"`). If empty, not specified, or if the attachment is not an image, it defaults to using `xdg-open`.

Example `~/.config/outlook-tui/config.json` to use Layout 2 with SQLite caching, folder exclusions, 5-line scrolling, and sxiv for images:
```json
{
  "client_id": "your-azure-client-id",
  "tenant_id": "common",
  "refresh_time_min": 5,
  "layout": 2,
  "use_sqlite": 1,
  "excluded_folders": ["Junk Email", "RSS Feeds"],
  "scroll_lines": 5,
  "image_viewer": "sxiv"
}
```

---

## Key Bindings

| Key                           | Action                                                                                                                                      |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `Tab`                         | Switch focus between the Folders, Messages, and Message Detail panes                                                                        |
| `Shift+Tab`                   | Switch focus in reverse order                                                                                                               |
| `Up` / `Down` (or `k` / `j`)  | Navigate selection in the focused pane                                                                                                      |
| `K` / `J`                     | Navigate selection in Messages pane (when in Folders pane), or scroll message in Details pane (when in Messages pane)                       |
| `Space`                       | Toggle expand/collapse for the selected conversation thread (works when focused on either the Messages or Folders pane)                     |
| `PageUp` / `PageDown`         | Scroll message body up/down                                                                                                                 |
| `n`                           | Compose a new email                                                                                                                         |
| `A`                           | Reply to the selected message. Automatically replies to the sender if they are the only other participant, or prompts to choose Reply vs Reply All if there are multiple participants. |
| `Ctrl+s` (or `Ctrl+x`)        | Send the message (when in Compose view)                                                                                                     |
| `Ctrl+v` (or `Ctrl+V`/`Shift`)| Paste image from clipboard (when focused on Body in Compose view)                                                                           |
| `Ctrl+f`                      | Open file picker popup to attach local files (when in Compose view)                                                                         |
| `Ctrl+g`                      | Open the compose **body** in your external editor (`$EDITOR` / `$VISUAL` / `vi`). The TUI suspends while the editor is running; on exit the body is loaded back into the compose view. |
| `d` (or `Delete`)             | Move the selected message to Deleted Items (Trash)                                                                                          |
| `D`                           | Delete all messages in the selected thread (requires confirmation)                                                                           |
| `U`                           | Recover/undelete the selected message (moves it back to the Inbox)                                                                          |
| `R`                           | Toggle the selected message's Read/Unread status                                                                                            |
| `f`                           | Toggle Favorite status (adds/removes the selected message to/from the local Favorites folder)                                                |
| `r`                           | Reload/refresh messages in the selected folder                                                                                               |
| `M`                           | Load the next portion/page of 50 messages in the selected folder                                                                            |
| `a`                           | View and select attachments on the current email                                                                                            |
| `y`                           | Open the Yank menu/combinations to copy content to the clipboard (displays a selection dropdown):<br>• `ym`: Copy original message (without quoting)<br>• `ya`: Copy all message (with quoting)<br>• `yu`: Yank URL(s) from message body<br>• `ys`: Copy email subject |
| `o`                           | Extract YouTrack URLs from the selected message and open in the external `yt-tui` app (shows a popup list if multiple unique YouTrack URLs exist, ignoring quoted/original text) |
| `?`                           | Toggle help popup describing app functionality and shortcuts                                                                                |
| `Enter` (in Attachments list) | Save the selected attachment to your local `Downloads` directory and open it with `xdg-open`                                                |
| `Esc`                         | Go back (cancel compose [with confirmation if the body is filled], close attachments list, close help popup, or go back to config)          |
| `q` (or `Ctrl+C`)             | Quit the application                                                                                                                        |

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

## YouTrack TUI Integration (`yt-tui`)

Outlook TUI integrates with [yt-tui](https://github.com/nospor/yt-tui) to let you open YouTrack issue links directly in the terminal:
- Press **`o`** on a message containing YouTrack URLs.
- If there is a single YouTrack URL, it opens directly in `yt-tui`.
- If there are multiple, you will be prompted to select one from a list.
- To use this, you must have the `yt-tui` binary installed and available in your shell's `PATH`. If it is not installed, the app will show a popup with download and installation information.
