# Workspace Guidelines for AI Agents

Welcome! This workspace contains **Outlook TUI**, a gorgeous, responsive, and fully-featured Terminal User Interface (TUI) client for Microsoft Outlook 365. It is built in Go using the Bubble Tea framework.

## Project Overview

- **Purpose**: Provides a fast, desktop-like terminal client for Microsoft Outlook 365, featuring authentication, folder management, message loading, composing, deleting, attachments processing, HTML mail support, and desktop notifications.
- **Languages**: Go (Golang).
- **Core Libraries**:
  - [Bubble Tea](https://github.com/charmbracelet/bubbletea) for runtime and UI loop.
  - [Bubbles](https://github.com/charmbracelet/bubbles) for built-in TUI components.
  - [Lip Gloss](https://github.com/charmbracelet/lipgloss) for terminal styling and layout.
  - Microsoft Graph API (via custom client in `graph.go`).

## Codebase Architecture

- [main.go](main.go) - Application launcher & Bubble Tea program entrypoint.
- [config.go](config.go) - Manages application settings (`~/.config/outlook-tui/config.json`), including Client ID, Tenant ID, refresh interval, layout selection, and attachment download directory.
- [auth.go](auth.go) - Manages Device Flow authentication & OAuth2 roundtrippers (background token refresh).
- [clipboard.go](clipboard.go) - Platform-independent clipboard utilities for Wayland/X11 (Linux), macOS, and Windows to retrieve image bytes and content types from the system clipboard.
- [graph.go](graph.go) - Custom Microsoft Graph API client for fetching mail, sending, deleting, and downloading. Also handles formatting HTML bodies with inline images (`makeImageAttachments`) and sending them with `contentId` and `isInline` fields.
- [db.go](db.go) - Optional SQLite caching Layer (`~/.cache/outlook-tui/db.db`). When enabled, caches downloaded messages per folder, provides offline/startup loading, handles database queries/transactions, updates read status locally, processes message deletions, and queries unique cached contacts/recipients for autocomplete. Also implements the persistent `favorite_messages` table and its helpers for the internal Favorites feature (always enabled).
- [tui.go](tui.go) - Contains layout rendering, multi-pane UI focus, navigation key bindings, and user interface updates.
  - **Layout 1** (default): Three panes side-by-side — `[Folders | Messages | Detail]`. Rendered via `renderLayout1()`.
  - **Layout 2**: Left column stacks Folders (~30% height) above Messages (~70% height); right column holds the Detail pane. Rendered via `renderLayout2()`, `renderFoldersViewWide()`, `renderMessagesViewWide()`.
  - Viewport sizing is split into `updateViewportSizeLayout1()` and `updateViewportSizeLayout2()`, dispatched by `updateViewportSize()` based on `config.Layout`.
  - **Thread Grouping**: Messages are grouped into `ThreadGroup` structs by `conversationId`. The Messages pane navigates a flat virtual list (`[]MessageListItem`) built by `buildVirtualList()`. Each item references a thread-group index and either a header row (`MemberIdx == -1`) or a specific reply (`MemberIdx >= 0`). Multi-message threads default to collapsed; `Space` toggles collapse state. Navigation uses `m.virtualSelected` (index into `m.virtualList`) instead of the old `m.selectedMessage`. The helper `activeMessage()` returns the `*Message` currently indicated by `virtualSelected`. Pressing `D` (capital D) prompts a confirmation view (`stateDeleteThreadConfirm`) to delete the entire selected thread with all its messages using the concurrent `deleteMultipleMailsCmd` helper.
  - **Contact Suggestions**: Suggests unique contacts below the 'To' field input when SQLite is enabled, filtering suggestions in real-time as the user types. Key intercepts (Up/Down, Enter to select, Esc to close suggestions) are active when the suggestion dropdown is visible.
  - **External Editor (Ctrl+G)**: When composing, pressing `Ctrl+g` invokes `openEditorCmd()` which writes the current body to a temp file and uses `tea.ExecProcess` to suspend the TUI and launch the editor specified by `$EDITOR` (falling back to `$VISUAL`, then `vi`). On exit the file is read back and returned as `editorBodyLoadedMsg`, which updates `m.composeBody` and focuses the body field (`composeStep = 3`). The temp file is always cleaned up via `defer os.Remove`.
  - **Clipboard Pasting (Ctrl+V)**: When composing the body, pressing `Ctrl+v` (or variants) calls `GetClipboardImage()`. If an image is present on the clipboard, it is stored in `m.composedImages` and an `[Image X]` placeholder is inserted. Standard text copy-pasting falls through if no image is found on the clipboard.
  - **File Picker Popup (Ctrl+F)**: Pressing `Ctrl+f` while composing opens the file picker popup (`stateFileBrowse`), allowing browsing and attaching local files. Pressing `s` toggles sorting by Name/Datetime, and `o` toggles Ascending/Descending. These settings and the last directory are persisted in `filepicker_settings.json`. Attached files are stored in `m.composedFiles` as `PendingFile` objects and uploaded as standard non-inline attachments on send.
  - **Favorites (f key)**: Pressing `f` on a selected message toggles its entry in the `favorite_messages` database table. The Favorites folder is always prepended to the top of the folders pane (index 0) and operates entirely locally (no Graph API calls are made when viewing or reloading the Favorites folder).
  - **Help Popup (? key)**: Pressing `?` toggles the help overlay screen (`stateHelp`), which displays a comprehensive reference of keyboard shortcuts and app functionalities in a beautifully formatted, scrollable viewport.
  - **Body Rendering / Image Placeholders**: When displaying email content in the detail viewport, any `<img>` tag is replaced with a styled `[image]` tag (bold and in ColorViolet) using `imagePlaceholderStyle` prior to HTML tag stripping. Remote/external images (with `http`/`https` URLs) are parsed, represented as virtual attachments with the `@odata.type` `#outlook-tui.remoteImage`, stored/cached, and downloaded on-demand over HTTP/HTTPS when selected in the attachments list.
- [notification.go](notification.go) - Triggers OS desktop notifications using `notify-send` for new messages.

---

## Developer Instructions for AI Agents

To ensure the codebase remains clean, well-documented, and easy to maintain, all AI agents must follow these rules:

1. **Update README.md for Feature Additions**:
   Whenever you add a new feature, config option, key binding, or dependency, update the project [README.md](README.md) to explain the usage, configuration, and design changes.
   
2. **Update AGENTS.md for Architecture / Rule Changes**:
   If there are new architectural decisions, structural modifications, coding style guidelines, or workspace rules added to this codebase, update this file ([AGENTS.md](AGENTS.md)) to reflect the updated guidelines.

3. **Preserve Code Integrity**:
   - Keep existing comments and docstrings intact unless they are directly contradicted by your changes.
   - Ensure you do not break the Device Code Flow authentication or OAuth2 background refresh mechanism.
   - Maintain the look and feel of the multi-pane TUI layout.

4. **Conventional Commits for Changelog**:
   - Write commit messages adhering to the Conventional Commits specification (e.g., `feat:`, `fix:`, `refactor:`, `style:`, `chore:`) to ensure `git-cliff` parses and categorizes them correctly in the changelog.


