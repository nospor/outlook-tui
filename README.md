# Outlook TUI

A gorgeous, responsive, and fully-featured Terminal User Interface (TUI) client for Microsoft Outlook 365, built in Go using [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

## Features

- 📂 **Multi-Pane Layout**: Standard desktop/web email layout split into folders, message lists, and message detail views.
- 🔑 **Device Code Flow Authentication**: Authenticate securely using Microsoft's standard OAuth2 device flow (no app credentials/passwords stored locally, only access/refresh tokens).
- 🔄 **Automatic Token Refresh**: The client automatically handles token expiration and refreshes OAuth2 access tokens in the background.
- 📥 **Background Fetching**: Syncs mail folders and automatically fetches new messages in the background (configurable interval, defaults to every 5 minutes).
- ✉️ **Send & Compose Mail**: Press `n` to open a full compose screen to draft and send new emails.
- 🗑️ **Delete Messages**: Press `d` or `Delete` to move messages to the Trash (Deleted Items folder) on Outlook.
- 📎 **Attachments Support**: Press `a` to view a list of attachments on the current email, download them locally to your `Downloads` directory, and automatically open them using `xdg-open`.
- 📜 **Smooth Navigation**: Tab between panes, scroll using arrow keys, page up/down, or the mouse wheel.

## Directory Structure

The project is structured as follows:
* [main.go](main.go) - Application launcher & Bubble Tea configuration.
* [config.go](config.go) - Manages application settings (`~/.config/outlook-tui/config.json`).
* [auth.go](auth.go) - Manages Device Flow authentication & OAuth2 roundtrippers.
* [graph.go](graph.go) - Custom Microsoft Graph API client for fetching mail, sending, deleting, and downloading.
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

---

## Key Bindings

| Key | Action |
| --- | --- |
| `Tab` | Switch focus between the Folders, Messages, and Message Detail panes |
| `Shift+Tab` | Switch focus in reverse order |
| `Up` / `Down` (or `k` / `j`) | Navigate selection in the focused pane |
| `PageUp` / `PageDown` | Scroll message body up/down |
| `n` | Compose a new email |
| `A` | Reply / Answer to the currently selected message (pre-fills sender, subject with Re:, quotes body, and focuses body field at the beginning) |
| `d` (or `Delete`) | Move the selected message to Deleted Items (Trash) |
| `r` | Toggle the selected message's Read/Unread status |
| `a` | View and select attachments on the current email |
| `Enter` (in Attachments list) | Save the selected attachment to your local `Downloads` directory and open it with `xdg-open` |
| `Esc` | Go back (cancel compose, close attachments list, or go back to config) |
| `q` (or `Ctrl+C`) | Quit the application |
