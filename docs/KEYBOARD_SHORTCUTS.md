# Postilaatikko Keyboard Shortcuts

Complete reference of all keyboard shortcuts in Postilaatikko. Existing upstream shortcuts are retained; macOS also supports the standard Preferences shortcut below.

## In-app reference

Press `?` anywhere outside a text field (and while the composer is closed) to open the keyboard shortcuts overlay. It is also reachable from the keyboard icon in the Mail sidebar footer and the keyboard button in the Calendar and Contacts sidebar footers. The overlay lists the shortcuts for the active view, can show all views, is searchable by action or key, and renders `⌘` on macOS. Toolbar tooltips and message context-menu items show the same key labels.

The overlay and hints are generated from `frontend/src/lib/keyboard/registry.ts` (and `extensions/<name>/frontend/keyboard/registry.ts`). When you add or change a binding, update the registry; `npm run check` fails if a displayed key no longer matches its predicate, or if `App.svelte` / `Composer.svelte` gain or lose a key case that the registry does not describe. This document remains the long-form reference.


These shortcuts work anywhere in the application, except while the composer is open (see [Composer Blocking](#composer-blocking)). They are ignored while a context menu or modal dialog is open.

On macOS, `⌘` works wherever `Ctrl` is listed below, unless macOS reserves the chord for itself (for example `⌘+Tab`, which switches applications; use `Ctrl+Tab` to switch rail items).

### Application

| Shortcut | Action |
|----------|--------|
| `?` | Show the keyboard shortcuts overlay (not in text fields or while composing) |
| `Ctrl+P` / `Ctrl+Shift+P` (macOS: `⌘P` / `⌘⇧P`) | Open the command palette (Alfred-style quick launcher) |
| `⌘+,` (macOS) | Open Preferences |
| `Ctrl+Q` | Quit application |
| `Ctrl+N` | Compose new message |
| `Ctrl+S` | Focus search bar |
| `Ctrl+Z` | Undo the last flag change or move/archive/delete/spam action |
| `Ctrl+Shift+A` | Start sync of all accounts (cancels the sync if one is already running) |
| `Ctrl+Shift+S` | Start sync of the selected folder (cancels it if already running) |
| `Ctrl+Tab` | Switch to next extension on the rail (Mail / Contacts / ...) |
| `` Ctrl+` `` | Switch to previous extension on the rail |
| `Ctrl+1…9,0` (macOS: `⌘1…9,0`) | Open favorite 1–10 on the quick-access rail (star any folder in the sidebar cards, folder tree or All folders browser to set them) |

### Pane Navigation

| Shortcut | Action |
|----------|--------|
| `Alt+Left` / `Alt+H` | Focus previous pane (viewer -> list -> sidebar) |
| `Alt+Right` / `Alt+L` | Focus next pane (sidebar -> list -> viewer) |

All `Alt` pane and folder navigation is disabled while focus mode is on.

### Folder Navigation

| Shortcut | Action |
|----------|--------|
| `Alt+Up` / `Alt+K` | Navigate to previous folder |
| `Alt+Down` / `Alt+J` | Navigate to next folder |
| `Alt+G` | Jump to top of sidebar (All Inboxes when present) |
| `Alt+Shift+G` | Jump to last visible folder (last account header if all trees are collapsed) |
| `Alt+Enter` | Expand/collapse focused account folder tree |
| `Alt(L)+Alt(R)` | Brings up context menu for the focused folder (`ContextMenu` does the same while the sidebar is focused) |

### Message Actions (when message is selected/focused)

| Shortcut | Action |
|----------|--------|
| `Ctrl+R` | Reply to last message (requires viewed conversation) |
| `Ctrl+Shift+R` | Reply All to last message (requires viewed conversation) |
| `Ctrl+F` | Forward last message (requires viewed conversation) |
| `Ctrl+U` | Mark as read (keyboard-focused or checked messages) |
| `Ctrl+Shift+U` | Mark as unread (keyboard-focused or checked messages) |
| `e` | Archive and mark as read (keyboard-focused or checked messages) |
| `Ctrl+K` | Archive without changing read status (keyboard-focused or checked messages) |
| `l` | Add/remove from Read Later |
| `Shift+L` | Add/remove from Reply Later |
| `Ctrl+J` | Mark as spam (keyboard-focused or checked messages) |
| `Ctrl+L` | Load remote images in viewed message |
| `Ctrl+Shift+L` | Open "Always Load Images" dropdown |
| `Shift+F` | Toggle focus mode for the Tab-focused message (the last message if none is focused) |

---

## Pane-Specific Shortcuts

These shortcuts depend on which pane is focused. They are disabled when typing in input fields.

### Sidebar (Folder List)

| Shortcut | Action |
|----------|--------|
| `Up` / `K` | Navigate to previous folder |
| `Down` / `J` | Navigate to next folder |
| `Alt+G` / `Alt+Shift+G` | Jump to top of sidebar / last visible folder |
| `Alt + Enter` / `Space` | Expand/collapse account (when account header is focused) |
| `Alt(L) + Alt(R)` | Brings up context menu for the focused folder (`ContextMenu` does the same) |

### Message List

| Shortcut | Action |
|----------|--------|
| `Up` / `K` | Select previous conversation |
| `Down` / `J` | Select next conversation |
| `G` (lowercase) | Select + scroll to first conversation |
| `Shift+G` | Select + scroll to last loaded conversation (does not auto-load more) |
| `Shift+Up` / `Shift+K` | Select previous + toggle checkbox |
| `Shift+Down` / `Shift+J` | Select next + toggle checkbox |
| `Space` | Toggle checkbox on current conversation |
| `Enter` / `V` | Open selected conversation in viewer |
| `D` | Delete selected/checked message(s) — move to Trash (same as `Delete`) |
| `CTRL+A` | Select all messages in folder |
| `Alt+M` | Toggle "Move to folder" dialog for the focused message |
| `Alt+C` | Toggle "Copy to folder" dialog for the focused message |
| `ContextMenu` / `Alt(R)` | Brings up context menu for the selected message(s) |

`G` / `Shift+G` / `V` also work in extension list panes (e.g., Contacts list, Calendar agenda).

### Conversation Viewer

| Shortcut | Action |
|----------|--------|
| `Up` / `K` | Scroll up |
| `Down` / `J` | Scroll down |
| `Tab` | Cycle through messages when focused on conversation viewer |
| `Delete` / `Backspace` | Delete focused message when focused on conversation viewer |
| `Ctrl + A` | Select all text of message in viewport |
| `ContextMenu` / `Alt(R)` | Brings up context menu for the message focused |
| `F` | Toggles focus mode on the current thread (conversation) |

---

## Single-Key Shortcuts

These work when not in an input field. They apply to checked messages (bulk) or the keyboard-focused message in the list.

| Shortcut | Action |
|----------|--------|
| `S` | Toggle star |
| `M` | Toggle read / unread. If any target message is unread, marks them all read; otherwise marks them unread |
| `Backspace` / `Delete` | Move to trash |
| `Shift+Backspace` / `Shift+Delete` | Permanently delete |
| `Escape` | Leave focus mode or close the responsive viewer/sidebar overlay if one is open; otherwise clear checkboxes (first press), then close the conversation (second press) |

---

## Composer Shortcuts

These only work when the composer is open.

| Shortcut | Action |
|----------|--------|
| `Ctrl+Enter` | Send message |
| `Alt+T` | Activate/Deactivate toolbar mode |
| `Alt+A` | Attach a file |
| `Alt+P` | Toggle PGP security mode (S = sign, E = encrypt) |
| `Alt+S` | Toggle S/MIME security mode (S = sign, E = encrypt) |
| `Ctrl+D` | Pop out/detach composer to separate window |
| `Escape` | Close composer (prompts to save draft if unsaved) |

---

### Text Formatting

| Shortcut | Action |
|----------|--------|
| `Ctrl+B` | Bold |
| `Ctrl+I` | Italic |
| `Ctrl+U` | Underline |
| `Alt+T`  | Toggle toolbar and follow hint to choose |

---

## Quick Reference Card

```
NAVIGATION
  Alt + Arrow Keys    Pane focus (Left/Right) or Folder nav (Up/Down)
  Alt + H/J/K/L       Vim-style: pane (H/L) or folder (J/K)
  Alt + G             Jump to top of sidebar (All Inboxes)
  Alt + Shift + G     Jump to last visible folder
  Alt(L) + Alt(R)     Brings context menu up for the focused folder
  Alt + Enter         Expand/collapse account
  Ctrl + Tab          Switch to next extension on rail
  Ctrl + `            Switch to previous extension on rail
  Arrow Keys / HJKL   Navigate within focused pane
  Enter               Open conversation / Expand account
  Space               Toggle checkbox / Expand account

COMPOSE & REPLY
  ⌘ + ,             Preferences (macOS)
  Ctrl + N            New message
  Ctrl + R            Reply
  Ctrl + Shift + R    Reply All
  Ctrl + F            Forward
  Ctrl + Enter        Send (in composer)
  Ctrl + D            Detach composer
  Alt  + T            Toggle format toolbar mode
  Alt + P             PGP mode (S/E to toggle sign/encrypt)
  Alt + S             S/MIME mode (S/E to toggle sign/encrypt)

SELECTION
  Ctrl + A            Select all messages (list) / text (viewer)

MESSAGE ACTIONS
  S                   Star/Unstar
  Ctrl + U            Mark read
  Ctrl + Shift + U    Mark unread
  E                   Archive + mark read
  Ctrl + K            Archive only
  L                   Read Later
  Shift + L           Reply Later
  Ctrl + J            Spam
  V                   View message
  G / Shift + G       Jump to first / last loaded message
  Alt + M             Move to folder
  Alt + C             Copy to folder
  Delete / D          Trash
  Shift + Delete      Permanent delete
  ContextMenu / Alt(R)  Context Menu

OTHER
  Ctrl + Z            Undo last action
  Ctrl + Shift + A    Sync all accounts (again to cancel)
  Ctrl + Shift + S    Sync selected folder (again to cancel)
  Ctrl + S            Search
  Ctrl + L            Load images
  Ctrl + Q            Quit
  Escape              Clear/Close
```

---

## Behavior Notes

### Pane Focus Model

The UI has three panes with visual focus indication:
1. **Sidebar** - Account/folder list
2. **Message List** - Conversations in selected folder
3. **Conversation Viewer** - Selected conversation content

Clicking a pane focuses it. Focus is indicated by a subtle border flash animation.

### Folder Navigation

`Alt+Up/Down` navigates through all folders in visual order:
1. Unified Inbox (All Inboxes)
2. Individual account inboxes under Unified Inbox
3. Account 1 header
4. Account 1 folders (if expanded)
5. Account 2 header
6. Account 2 folders (if expanded)
7. ... and so on

Collapsed accounts show only their header (not folders) in navigation.

### Message Actions Hierarchy

Action shortcuts (Delete, Archive, Spam, Star, Read/Unread) follow this priority:

1. **Checked messages** - If any messages are checked (via Space or Shift+navigation), actions apply to ALL checked messages
2. **Keyboard-focused message** - Otherwise, actions apply to the message that's currently focused in the message list (the one with keyboard highlight from j/k navigation)
3. The message being viewed in the conversation pane is independent - you can navigate to and delete a different message without opening it first

First `Escape` clears checkboxes, second `Escape` closes the conversation viewer.

### Composer Blocking

When the composer is open:
- `Ctrl+R`, `Ctrl+Shift+R`, `Ctrl+F` are blocked to prevent accidental replies
- The global Mail shortcuts (`Ctrl+N`, `Ctrl+Q`, `Ctrl+Z`, `Ctrl+S`, sync, `Ctrl+Tab` rail switching, and `Alt` pane/folder navigation) do not fire; only the composer's own shortcuts work
- `⌘+,` (macOS Preferences) and the active extension's registered shortcuts still run, except while typing in a text field

### Extension Rail Navigation

`Ctrl+Tab` cycles forward through the rail items: Mail → first enabled extension → next → wrap to Mail.
`` Ctrl+` `` cycles backward. Both are no-ops when no extensions are enabled (only Mail in the rail). Active extension is persisted across launches.

Composer state is preserved across switches: switching to an extension and back does not unmount or clear the composer.

### Unified Inbox

When viewing Unified Inbox and replying:
- Reply uses the account associated with the selected message
- This ensures replies come from the correct email address

---

## Extensions

Extension shortcuts only fire when the corresponding extension is the active rail pane (selected via `Ctrl+Tab` / `` Ctrl+` ``). They never trigger while Mail is active — so shortcuts that overlap with Mail's (like `Ctrl+N`, `Ctrl+Shift+A`, `Ctrl+Shift+S`) are unambiguous: the active rail decides which handler runs. For example, `Ctrl+Shift+A` syncs accounts in Mail, syncs all calendar sources in Calendar, and syncs all contact sources in Contacts.

Pane-local navigation (Up/Down/J/K, Enter, Space, Alt+H/L for pane cycling, Alt+Up/Down for sidebar) uses the same kit-shared predicates Mail does — they're listed per-extension below for completeness, but they're not extension-specific re-implementations.

### Calendar

**Navigation (pane cycling)**

| Shortcut | Action |
|----------|--------|
| `Alt+Left` / `Alt+H` | Focus previous pane |
| `Alt+Right` / `Alt+L` | Focus next pane |

**Date / view navigation**

| Shortcut | Action |
|----------|--------|
| `T` | Jump the calendar view to today |
| `←` | Previous view-unit (prev month / week / day) |
| `→` | Next view-unit (next month / week / day) |
| `Alt+M` | Switch to month view |
| `Alt+W` | Switch to week view |
| `Alt+D` | Switch to day view |
| `Alt+A` | Switch to agenda view |

**Actions**

| Shortcut | Action |
|----------|--------|
| `Ctrl+N` | Open the new-event composer |
| `Ctrl+R` | Sync all calendar sources |
| `Ctrl+Shift+A` | Sync all calendar sources (same chord as Mail's "sync all accounts" — fires whichever handler matches the active rail) |
| `F` | Toggle focus mode for the selected event |

> The calendar sidebar uses checkbox toggles per calendar (not single-select navigation), so it doesn't follow the kit's `J`/`K` row-cycling pattern. Clicking a checkbox or row in the sidebar toggles visibility.

### Contacts

**Sidebar navigation (works from any pane)**

Mirrors mail's "Folder Navigation" shortcuts. These fire regardless of which contacts pane currently has keyboard focus — so you can scroll through addressbooks while the list or detail pane is focused.

| Shortcut | Action |
|----------|--------|
| `Alt+Up` / `Alt+K` | Move to previous source / addressbook in the sidebar |
| `Alt+Down` / `Alt+J` | Move to next source / addressbook in the sidebar |

**Pane cycling**

| Shortcut | Action |
|----------|--------|
| `Alt+Left` / `Alt+H` | Focus previous pane (detail → list → sidebar) |
| `Alt+Right` / `Alt+L` | Focus next pane (sidebar → list → detail) |

**Actions**

| Shortcut | Action |
|----------|--------|
| `E` | Edit the currently-focused contact |
| `Ctrl+N` | Open the new-contact dialog (pre-targeted to the sidebar-focused addressbook; falls back to local when the focused source isn't writable) |
| `Ctrl+Shift+A` | Sync all contact sources (same chord as Mail's "sync all accounts" — fires whichever handler matches the active rail) |
| `Ctrl+Shift+S` | Sync the contact source currently focused in the sidebar (no-op with warning toast when a built-in entry like "All" / "Local" is focused) |

> Within a focused pane, `Up`/`K` and `Down`/`J` cycle rows (contact list, sidebar sources) and `Enter` opens / activates — same kit predicates Mail's list and folder tree use.
