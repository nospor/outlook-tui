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
- [config.go](config.go) - Manages application settings (`~/.config/outlook-tui/config.json`), including Client ID, Tenant ID, refresh interval, and layout selection.
- [auth.go](auth.go) - Manages Device Flow authentication & OAuth2 roundtrippers (background token refresh).
- [graph.go](graph.go) - Custom Microsoft Graph API client for fetching mail, sending, deleting, and downloading.
- [db.go](db.go) - Optional SQLite caching Layer (`~/.cache/outlook-tui/db.db`). When enabled, caches downloaded messages per folder, provides offline/startup loading, handles database queries/transactions, updates read status locally, processes message deletions, and queries unique cached contacts/recipients for autocomplete.
- [tui.go](tui.go) - Contains layout rendering, multi-pane UI focus, navigation key bindings, and user interface updates.
  - **Layout 1** (default): Three panes side-by-side — `[Folders | Messages | Detail]`. Rendered via `renderLayout1()`.
  - **Layout 2**: Left column stacks Folders (~30% height) above Messages (~70% height); right column holds the Detail pane. Rendered via `renderLayout2()`, `renderFoldersViewWide()`, `renderMessagesViewWide()`.
  - Viewport sizing is split into `updateViewportSizeLayout1()` and `updateViewportSizeLayout2()`, dispatched by `updateViewportSize()` based on `config.Layout`.
  - **Thread Grouping**: Messages are grouped into `ThreadGroup` structs by `conversationId`. The Messages pane navigates a flat virtual list (`[]MessageListItem`) built by `buildVirtualList()`. Each item references a thread-group index and either a header row (`MemberIdx == -1`) or a specific reply (`MemberIdx >= 0`). Multi-message threads default to collapsed; `Space` toggles collapse state. Navigation uses `m.virtualSelected` (index into `m.virtualList`) instead of the old `m.selectedMessage`. The helper `activeMessage()` returns the `*Message` currently indicated by `virtualSelected`.
  - **Contact Suggestions**: Suggests unique contacts below the 'To' field input when SQLite is enabled, filtering suggestions in real-time as the user types. Key intercepts (Up/Down, Enter to select, Esc to close suggestions) are active when the suggestion dropdown is visible.
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

