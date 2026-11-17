# Postilaatikko Keyboard Shortcuts

Complete reference of all keyboard shortcuts in Postilaatikko. Existing upstream shortcuts are retained; macOS also supports the standard Preferences shortcut below.

## In-app reference

Press `?` anywhere outside a text field (and while the composer is closed) to open the keyboard shortcuts overlay. It is also reachable from the keyboard icon in the Mail sidebar footer and the keyboard button in the Calendar and Contacts sidebar footers. The overlay lists the shortcuts for the active view, can show all views, is searchable by action or key, and renders `⌘` on macOS. Toolbar tooltips and message context-menu items show the same key labels.

The overlay and hints are generated from `frontend/src/lib/keyboard/registry.ts` (and `extensions/<name>/frontend/keyboard/registry.ts`). When you add or change a binding, update the registry; `npm run check` fails if a displayed key no longer matches its predicate, or if `App.svelte` / `Composer.svelte` gain or lose a key case that the registry does not describe.

The shortcut tables in this document are generated from the same registry. After changing a binding, run `npm run docs:shortcuts` in `frontend/` and commit the result; `npm run check` fails when the committed tables are stale. Only the text between the `GENERATED` markers is rewritten. The prose around the tables is hand-written.


These shortcuts work anywhere in the application, except while the composer is open (see [Composer Blocking](#composer-blocking)). They are ignored while a context menu or modal dialog is open.

On macOS, `⌘` works wherever `Ctrl` is listed below, unless macOS reserves the chord for itself (for example `⌘+Tab`, which switches applications; use `Ctrl+Tab` to switch rail items).

<!-- BEGIN GENERATED: core (run `npm run docs:shortcuts` in frontend/; do not edit by hand) -->

### General

| Shortcut | Action |
|----------|--------|
| `Ctrl+K` (macOS `⌘K`) / `Ctrl+P` (macOS `⌘P`) / `Ctrl+Shift+P` (macOS `⌘⇧P`) | Command palette |
| `?` / `Ctrl+/` (macOS `⌘/`) | Show keyboard shortcuts |
| `⌘,` (macOS only) | Open settings |
| `c` / `Ctrl+N` (macOS `⌘N`) | New message |
| `/` / `Ctrl+S` (macOS `⌘S`) / `Ctrl+Shift+F` (macOS `⌘⇧F`) | Search |
| `Ctrl+F` (macOS `⌘F`) | Find in message |
| `Ctrl+D` (macOS `⌘D`) | Show folder tree or message list |
| `z` / `Ctrl+Z` (macOS `⌘Z`) | Undo last action |
| `Ctrl+Shift+A` (macOS `⌘⇧A`) | Sync all accounts (again to cancel) |
| `Ctrl+Shift+S` (macOS `⌘⇧S`) | Sync current folder (again to cancel) |
| `Ctrl+Tab` (macOS `⌃⇥`) | Next view on the rail |
| `` Ctrl+` `` (macOS `` ⌃` ``) | Previous view on the rail |
| `Ctrl+W` (macOS `⌘W`) | Close active tab |
| `Ctrl+Q` (macOS `⌘Q`) | Quit |

### Panes and folders

| Shortcut | Action |
|----------|--------|
| `⌘←` (macOS only) | Back to previous message |
| `⌘→` (macOS only) | Forward to next message |
| `Alt+→` (macOS `⌥→`) / `Alt+L` (macOS `⌥L`) | Focus next pane |
| `Alt+←` (macOS `⌥←`) / `Alt+H` (macOS `⌥H`) | Focus previous pane |
| `Alt+↓` (macOS `⌥↓`) / `Alt+J` (macOS `⌥J`) | Next folder |
| `Alt+↑` (macOS `⌥↑`) / `Alt+K` (macOS `⌥K`) | Previous folder |
| `Alt+G` (macOS `⌥G`) | First folder |
| `Alt+Shift+G` (macOS `⌥⇧G`) | Last visible folder |
| `Alt+Enter` (macOS `⌥↩`) | Expand or collapse account |
| `Ctrl+Alt+←` (macOS `⌘⌥←`) | Previous account |
| `Ctrl+Alt+→` (macOS `⌘⌥→`) | Next account |
| `Left Alt` + `Right Alt` pressed together | Folder context menu |

### Folder list

| Shortcut | Action |
|----------|--------|
| `↓` / `j` | Next folder |
| `↑` / `k` | Previous folder |
| `Enter` (macOS `↩`) / `Space` | Expand or collapse account |

### Message list

| Shortcut | Action |
|----------|--------|
| `↓` / `j` / `Ctrl+Shift+]` (macOS `⌘⇧]`) | Next item |
| `↑` / `k` / `Ctrl+Shift+[` (macOS `⌘⇧[`) | Previous item |
| `g` | First item |
| `Shift+G` (macOS `⇧G`) | Last loaded item |
| `Shift+↓` (macOS `⇧↓`) / `Shift+J` (macOS `⇧J`) | Select next and check |
| `Shift+↑` (macOS `⇧↑`) / `Shift+K` (macOS `⇧K`) | Select previous and check |
| `Space` / `x` | Check or uncheck |
| `Enter` (macOS `↩`) / `o` | Open |
| `Ctrl+A` (macOS `⌘A`) | Select all messages |
| `Ctrl+Alt+1` (macOS `⌘⌥1`) | Filter: All messages |
| `Ctrl+Alt+2` (macOS `⌘⌥2`) | Filter: Unread |
| `Ctrl+Alt+3` (macOS `⌘⌥3`) | Filter: Reply Later |
| `Ctrl+Alt+4` (macOS `⌘⌥4`) | Filter: Read Later |
| `Menu` / `Right Alt` (macOS `Right ⌥`) | Context menu |
| `Esc` / `u` | Clear checked messages, then close conversation |

### Message actions

| Shortcut | Action |
|----------|--------|
| `Ctrl+R` (macOS `⌘R`) / `r` | Reply |
| `Ctrl+Shift+R` (macOS `⌘⇧R`) / `a` | Reply all |
| `f` / `Ctrl+L` (macOS `⌘L`) | Forward |
| `e` / `Ctrl+E` (macOS `⌘E`) | Archive and mark as read |
| `l` / `Ctrl+U` (macOS `⌘U`) | Add to Read Later |
| `Shift+L` (macOS `⇧L`) / `Ctrl+I` (macOS `⌘I`) | Add to Reply Later |
| `h` | Snooze conversation |
| `!` / `Ctrl+J` (macOS `⌘J`) | Mark as spam |
| `#` / `Delete` (macOS `⌦`) / `Backspace` (macOS `⌫`) / `d` | Move to trash |
| `Shift+Delete` (macOS `⇧⌦`) / `Shift+Backspace` (macOS `⇧⌫`) | Delete permanently |
| `s` | Star or unstar |
| `Shift+I` (macOS `⇧I`) | Mark as read |
| `Shift+U` (macOS `⇧U`) / `Ctrl+Shift+U` (macOS `⌘⇧U`) | Mark as unread |
| `m` | Toggle read / unread |
| `v` / `Alt+M` (macOS `⌥M`) | Move to folder |
| `Alt+C` (macOS `⌥C`) | Copy to folder |

### Conversation viewer

| Shortcut | Action |
|----------|--------|
| `↓` / `j` | Scroll down |
| `↑` / `k` | Scroll up |
| `Shift+F` (macOS `⇧F`) | Focus mode for conversation |
| `Shift+F` (macOS `⇧F`) | Focus mode for message |
| `Ctrl+A` (macOS `⌘A`) | Select all message text |
| `Ctrl+Alt+L` (macOS `⌘⌥L`) | Load remote images |
| `Ctrl+Shift+Alt+L` (macOS `⌘⇧⌥L`) | Always load images from… |

### Composer

| Shortcut | Action |
|----------|--------|
| `Ctrl+Enter` (macOS `⌘↩`) | Send |
| `Alt+A` (macOS `⌥A`) | Attach file |
| `Ctrl+Shift+D` (macOS `⌘⇧D`) | Open composer in its own window |
| `Alt+T` (macOS `⌥T`) | Formatting toolbar |
| `Alt+P` (macOS `⌥P`) | PGP mode (then S to sign, E to encrypt) |
| `Alt+S` (macOS `⌥S`) | S/MIME mode (then S to sign, E to encrypt) |
| `Ctrl+B` (macOS `⌘B`) | Bold |
| `Ctrl+I` (macOS `⌘I`) | Italic |
| `Ctrl+U` (macOS `⌘U`) | Underline |
| `Ctrl+Z` (macOS `⌘Z`) | Undo last action |
| `Ctrl+Shift+Z` (macOS `⌘⇧Z`) | Redo |
| `Esc` / `Ctrl+W` (macOS `⌘W`) | Close composer |

### Lists

| Shortcut | Action |
|----------|--------|
| `↓` / `j` | Next item _(Calendar, Contacts only)_ |
| `↑` / `k` | Previous item _(Calendar, Contacts only)_ |
| `g` | First item _(Calendar, Contacts only)_ |
| `Shift+G` (macOS `⇧G`) | Last loaded item _(Calendar, Contacts only)_ |
| `Enter` (macOS `↩`) / `o` | Open _(Calendar, Contacts only)_ |
| `Delete` (macOS `⌦`) / `Backspace` (macOS `⌫`) | Delete _(Contacts only)_ |

<!-- END GENERATED: core -->

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
- The reply, reply all and forward shortcuts are blocked to prevent accidental replies
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

<!-- BEGIN GENERATED: extensions (run `npm run docs:shortcuts` in frontend/; do not edit by hand) -->

### Calendar

#### Calendar

| Shortcut | Action |
|----------|--------|
| `t` | Go to today |
| `←` | Previous month, week or day |
| `→` | Next month, week or day |
| `Alt+M` (macOS `⌥M`) | Month view |
| `Alt+W` (macOS `⌥W`) | Week view |
| `Alt+D` (macOS `⌥D`) | Day view |
| `Alt+A` (macOS `⌥A`) | Agenda view |
| `Ctrl+N` (macOS `⌘N`) | New event |
| `Ctrl+R` (macOS `⌘R`) / `Ctrl+Shift+A` (macOS `⌘⇧A`) | Sync all calendars |
| `f` | Focus mode for selected event |

### Contacts

#### Contacts

| Shortcut | Action |
|----------|--------|
| `e` | Edit focused contact |
| `Ctrl+N` (macOS `⌘N`) | New contact |
| `Ctrl+Shift+A` (macOS `⌘⇧A`) | Sync all contact sources |
| `Ctrl+Shift+S` (macOS `⌘⇧S`) | Sync focused contact source |

<!-- END GENERATED: extensions -->

Notes on extension shortcuts:

- The calendar sidebar uses checkbox toggles per calendar, so it does not follow the `J`/`K` row-cycling pattern. Clicking a checkbox or row toggles visibility.
- `Ctrl+Shift+A` is the same chord as Mail's "sync all accounts"; whichever handler matches the active rail runs.
- In Contacts, sidebar navigation (`Alt+Up/K`, `Alt+Down/J`) works from any pane. `Ctrl+N` targets the sidebar-focused addressbook and falls back to local when that source is not writable. `Ctrl+Shift+S` is a no-op with a warning toast on built-in entries like "All" or "Local".
- Within a focused pane, `Up`/`K` and `Down`/`J` cycle rows and `Enter` opens or activates, using the same predicates Mail's list and folder tree use.

## Collision rules

`frontend/tests/shortcutCollisions.test.mjs` fails when two actions share a key in one context (rail pane plus registry group). Reuse across focus groups (sidebar, list, viewer, composer) must be listed there with the reason. Cmd/Ctrl+K is the palette only; E is archive and mark read.

## One list, three surfaces

Each command is defined once in `registry.ts`. Its key is matched by `resolveShortcut()` (`keyboard/dispatch.ts`) and the host runs the handler for that id (`KEY_HANDLERS` in `App.svelte`). Do not add a `case 'x':` for a command. Rules: every command needs a menu entry (`menu`, tested in `commandMenuCoverage`), a button is optional, a key is optional and shown in the menu. Commands still read by the old inline code are tagged `inline`; the rest of the migration moves them to `dispatch`.
