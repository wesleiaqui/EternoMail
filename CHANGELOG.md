# CHANGELOG

## [Unreleased]

## Postilaatikko 2026.11.06.3

- Improve attachment-action keyboard focus and accessible labels.
- Harden CLI path validation against option injection.
- Make search-query preparation faster with fewer allocations and correct Unicode whitespace handling.

## Postilaatikko 2026.11.06.2

- Keep the conversation currently being read in its original inbox card and row position when delayed mark-as-read updates or list refreshes reorder messages; it returns to normal grouping after you select another conversation.
- Prefetch message bodies for visible and focused conversations, and cache recently opened conversations to make opening and switching between messages feel faster.

## Postilaatikko 2026.11.06.1

- Keep reader toolbar tooltips above the scrolling message pane so they remain fully visible.
- Add Send now beside Cancel during the Undo Send countdown.
- Show Reply, Reply All, and Forward when right-clicking a single conversation row, targeting its latest message.

## Postilaatikko 2026.11.05.1

- Bring folder browsing and its message list into one context column, with All, Unread, Read Later, and Reply Later controls at the top.
- Keep the favorites rail visible while browsing, place open email tabs above the wide reader, and optionally auto-hide the context column until the rail is hovered; folder views no longer open as tabs.
- Store Read Later and Reply Later in Gmail labels for Gmail accounts, with local queues for other providers; snooze conversations locally until their wake time.
- Repair schema-64 databases created by either concurrent migration branch without deleting their queued or move-history data.

## Postilaatikko 2026.11.04
- Avoid integer-overflowing capacity calculations while matching email thread references.
- Decode escaped characters in message-list and global-search snippets as text.
- Load Reply Later conversations directly from the durable queue instead of scanning every mailbox folder.
- Show unread message counts beside sidebar destinations, with total counts when nothing is unread.
- Open double-clicked conversations in reader-only tabs; keep folder browsing in workspace tabs and detach reader tabs into reader windows.
- Cycle between mail workspace tabs with Ctrl+Tab / Ctrl+Shift+Tab, close secondary tabs with Cmd/Ctrl+W, and expand reader-only tabs to the available width.
- Continue editing a draft directly from its message in conversation view.
- Suggest message participants while composing, using indexed headers only; keep explicit address-book authorization separate.
- Skip remote-image sanitization work for messages without remote image URLs, while retaining existing blocking behavior.
- Restore draft-history attachments over the composer bridge as base64 content, and ignore unsupported signature-rule message types when loading identities.

## Postilaatikko 2026.11.03

- Keep Gmail replies in the original conversation by sending with Gmail's provider thread ID, including replies reopened from drafts.
- Preserve complete `In-Reply-To` and `References` headers from the detached reply composer.
- Open replies inline beneath conversation-style threads, keeping the chat history visible while composing.
- Put the message's account first in the move destination picker for faster same-account moves.
- Preserve the current virtual-folder rows and cursor when the background index refresh fails.
- Keep the open unread message pending until navigating away after archiving, so unread-only views and the reader stay aligned.

## Postilaatikko 2026.11.01.2

- Warm a bounded first page of message bodies asynchronously when opening Inbox, Starred, pinned folders, or All Inboxes; headers render immediately, and other folders plus attachments stay on demand.
- Open the move-to-folder / copy-to-folder picker for the open conversation from the focused reader with `v` / Alt+M / Alt+C, so Gmail-style message actions work from both the message list and the reader. Single-key shortcuts stay disabled in editable fields and while composing.
- Fix message-list folder badges showing internal JavaScript source instead of the folder name.
- Open double-clicked conversations in a tab when macOS's Desktop & Dock preference calls for tabs; avoid blocking detached mail-window startup on the primary window's UI state.
- Reorder favorite destinations in the left quick-access rail; Cmd/Ctrl+1…0 follows the displayed order.
- Refresh Gmail labels from the virtual-folder editor, and apply Gmail label scopes correctly to online virtual-folder searches.
- Add ordered contextual signature rules for sender, recipient, and message type, with placement controls and manual-edit preservation.
- Add bulk select/clear controls to virtual-folder setup, wrap long sidebar labels, and make the cross-account move picker a searchable expandable folder tree.
- Restore Cmd+C and the standard Edit menu copy command for selected text in the email reader.

## Postilaatikko 2026.11.01.1

- Integrate the latest main-branch changes for tab-aware double-click opening, favorite ordering and shortcuts, message-list folder labels, and non-blocking detached-window startup.
- Restore copy shortcuts for selected email text and include the Intel macOS build compatibility fix.

## Postilaatikko 2026.10.30

- Add Reply Later controls on conversation rows and context menus, with an undoable sidebar queue and count. Apply unread and Reply Later quick filters to online virtual-folder results.
- Keep Cmd-Z/Ctrl-Z and Edit → Undo on the latest mailbox action; add Edit → Undo History with a compact, privacy-minimal list and per-operation undo.
- Share exact online saved-view count scans across windows, isolate cancellation, and invalidate counts after mail changes or explicit refresh.

## Postilaatikko 2026.10.29.1

- Replace half-second dialog-guard polling in message lists and readers with event-driven refreshes when the last blocking dialog closes.
- Keep the CalVer release check compatible with same-day patch releases.

## Postilaatikko 2026.10.29

- Open independent mail workspaces in tabs, keep folder/search/thread state while switching, and detach a tab into its own native window.
- Prefetch bodies for Inbox, Starred and pinned folders by default, with per-folder overrides; other folders remain header-only unless enabled.
- Clear stale message rows on folder changes while caching a small bounded list per recently visited folder.
- Wrap long sidebar labels across two lines and expose their full names.
- Select multiple conversations and apply read/unread, star, archive, move, spam or delete across their messages. The action bar adapts to Spam and Trash.

## Postilaatikko v0.3.21

- Add opt-in, read-only macOS Contacts browsing and recipient autocomplete; open a selected person in Contacts.app to edit.
- Recognize date/time mentions in messages and offer a reviewable Calendar event.
- Open independent mail windows with their own navigation and layout.
- Show Gmail online virtual-folder counts and continue IMAP searches across folders/pages.
- Fix Send Later to report missing Gmail credentials as an authorization problem before sending. Add regression coverage for recovery, replay, cancellation, and concurrent actions.
- Reduce repeated CI work and add an artifact-only macOS hotfix build path.


- Report missing or unavailable helper-local Gmail OAuth credentials as requiring authorization in Send Later, preserving the encrypted job instead of claiming an unknown delivery outcome before submission.

- Browse macOS Contacts in Settings → Contacts and include authorized addresses in main-window and detached-composer recipient suggestions. Request access only from an explicit action; open a person in Contacts.app for edits. Postilaatikko never writes native contacts. Settings explains denied access and reports lookup/open failures.

- Show fully enumerated Gmail virtual-folder conversation/unread counts with progress and incomplete/error states, including saved-search queries. Reply/date counts reuse header metadata for flags to avoid redundant account scans. Continue non-Gmail searches through older UID pages and additional folders with retryable failures; complete non-Gmail conversation counts remain follow-up.
- Add a New mail window action for parallel mail views, each with its own folder, search, selection, reader mode and list width. Replies reuse the detached composer; all windows share the existing mail services.
- Reduce repeated pull-request CI installs/builds, select affected language checks conservatively, run frontend regression tests, and cancel superseded checks. Keep existing required status names and an explicit artifact-only macOS hotfix build with architecture selection.

## Postilaatikko v0.3.20

- Add branching composer undo, per-keystroke autosave and a 30-second undo-send countdown.
- Fix detached-window bindings and restart-durable attachment history keys.
- Preserve attachment membership when navigating history and clear history at the correct composer lifecycle boundaries.
- Save any search as a virtual folder with an optional stored query (Gmail and IMAP) and a cached ordered index with five-minute background refresh and last-refresh display.
- Prepare fresh desktop packages from the merged changes. Native build and DMG download verification remain release steps.

## Postilaatikko — v0.3.19

- Avoid duplicate legacy/Gmail inbox entries without deleting cached content.
- Correct recurring Finnish character encoding errors in plain-text messages while preserving originals.
- Support Finnish Option/AltGr symbol shortcuts and keep `#` from triggering permanent deletion.

- Settle pending spelling checks and suggestions when spellcheck is disabled or its worker fails, instead of leaving callers waiting indefinitely. Late events from a replaced worker are ignored.
- Bound the reader's inline-image session cache to 128 messages and 32 MiB of accounted string payloads, evicting least-recently-used entries. Images already open remain visible; evicted copies are reloaded from local storage when needed.

- Sidebar cards can be reordered, collapsed and hidden/restored. Move entries between cards by dragging or keyboard controls; add custom groups. All Inboxes lives in Virtual folders. Star up to ten entries for the rail and Cmd/Ctrl+1…0. Contacts/Calendar rail shortcuts default off.
- Add cross-account global server search, available from the sidebar or `/`, with account scopes, native Gmail queries, pagination and visible account errors. All folders and move/copy destinations have account-spanning folder search.
- Gmail OAuth accounts prefer Gmail API for metadata/history, labels, body/source/attachments, flags, move/copy/archive/spam/trash/delete, drafts, sending/read receipts and helper delivery. Preserve Bcc recipients and avoid transport fallback after ambiguous sends/imports.
- Virtual folders query Gmail directly and apply folder unions/exclusions and conversation status/latest-activity criteria. Cached IMAP content is no longer a prerequisite. Normal Gmail bodies and file downloads are separate requests; routine/background sync fetches metadata only.
- Opening folders automatically refreshes headers without clearing the cache or restarting an active manual sync. Background sync visits all folders, with bounded account-wide Gmail backfill/history.
- Cache migration 54 stores provider identifiers separately from local/IMAP identifiers. Live Google and native macOS acceptance checks remain for the test release.


- Preserve the previously tagged Send Later implementation while integrating the new conversation reader.

- The reader has a Conversation / Traditional switch in its toolbar. Traditional shows every message in full, as before; Conversation folds quoted replies and signatures. Your choice is remembered, switching keeps your place in the thread, and each message has Show original.
- The reader shows each message's new text and folds quoted replies and signatures behind a button, so long threads take less room. Text it is not sure about is never folded, and Show full message restores the whole message. Threads open at the first unread message instead of the bottom.
- Fixed: virtual folders with a date window (7 days, 30 days, 90 days or 1 year) showed no conversations and no unread count. Message dates are now read correctly for the window and the sort order.
- Fixed: deleting a virtual folder from the sidebar did nothing on macOS, because the browser's native confirm dialog is not available in the app's webview. Deletion now asks in the app's own dialog. The delete, edit and move buttons on a virtual folder row are also reachable with the keyboard, not only on hover.
- The sidebar and message list default to narrower columns (260px and 320px, from 320px and 360px), so the message has more room. This is applied once on the first launch after the update; widths you set afterwards are kept.
- Added virtual folder search facets and editable criteria (P-006): select Include/Exclude mode for member folders, filter by read/unread, starred/unstarred, replied/unreplied, and date window (7 days, 30 days, 90 days, 1 year, or all time). Removed hardcoded unread-only filtering so virtual folders display all matching conversations consistently across lists and search.
- Default sync period is now All Messages (0 days, P-028): routine sync is lightweight and headers-only, so the default sync window across OAuth, settings dialogs, and new accounts no longer truncates history to 180 days. Added database Migration 52 to upgrade existing accounts with legacy defaults to All Messages, and added visual spinning feedback on manual sync.
- Added pinned folders: right-click any folder (or the unified inbox account row) and choose Pin to sidebar. Pinned folders appear in a Pinned section for one-click access, across accounts, and stay pinned after restart.
- The virtual folder editor has a search field above the folder list, so long lists of accounts and folders can be filtered by typing a few words. Folders stay selected while the list is filtered.
- Pressing `M` toggles the read state of the checked messages, or of the focused message when nothing is checked. If any of them is unread, they are all marked read; otherwise they are all marked unread.
- The sidebar's More button is now labelled All folders, since it opens the full folder browser.
- Sidebar sections can be reordered and hidden from Settings > General > Sidebar sections. Hidden sections can be shown again there, and the layout is kept after restart.

## Postilaatikko — v0.3.15

- Account settings now show whether a Gmail account is using the Gmail API (in use, unavailable, sign in again, or not used), and the log records when it starts working or falls back to IMAP.
- Fixed: the command panel's Archive, Spam, Star, Mark read and Mark unread commands acted on the conversation's thread id instead of the selected messages, so they did nothing. They now use the same target as the keyboard (checked messages, else the focused row) and are hidden when there is nothing to act on.

## Postilaatikko — v0.3.14

- Added a floating Command Palette (`⌘P` / `Ctrl+P`, Alfred-style quick launcher) with instant fuzzy search, arrow navigation, and direct action dispatch for accounts, folders, navigation, settings, and themes.
- Added standard Gmail single-key keyboard shortcuts (`v` for Move to folder, `e` for archive, `c` for compose, `r` for reply, `a` for reply all, `f` for forward, `x` for select/check, `/` for search, `!` for spam, `#` for trash, `z` for undo, `Shift+I` for mark as read, `Shift+U` for mark as unread, `o`/`Enter` for open, `u` for back to list).
- Added a configurable mark-as-read delay setting in Settings > General: immediately on open (0s), after 1s, 2s, 3s, or 5s viewing delay, when navigating away from the conversation, or manually only.
- Stabilized the keyboard shortcuts reference dialog height (`h-[min(640px,85vh)]`) so that live search filtering no longer resizes the window or causes input focus to be lost.
- Fixed sidebar All Folders subfolder tree expanding and one-click folder selection.
- Localized remaining Portuguese interface strings (*"Caixa de entrada"* -> `settings.inbox`, *"selecionadas"* -> `messageList.selectedActions` / `messageList.selectedCount`) across supported locales.

## Postilaatikko — v0.3.13

- Added an in-app keyboard shortcuts overlay: press `?` (outside text fields) or use the new Keyboard shortcuts entry in the Mail sidebar or the Calendar/Contacts sidebar footer. It lists the shortcuts for the current view (or all views), is searchable, and shows `⌘` on macOS. Toolbar tooltips and message context-menu items now show their shortcut too.
- Gmail accounts now search on the server with the Gmail API, so Gmail operators such as `from:`, `has:attachment` and `older_than:` work in folder search; other providers, and Gmail accounts where the API is unavailable, keep using IMAP search.
- Marking Gmail mail as spam (and not spam) now changes the Gmail labels through the Gmail API when it is available, and falls back to the previous IMAP move otherwise. Undo works as before.
- Added composer quick replies: create, edit, delete and insert reusable plain-text replies from the composer footer without touching the draft, recipients or sender. Stored on this computer only.
- Added ICS event handoff: create an ICS event from structured date/time details and open it for review/import in the user's calendar application.
- Icon-only controls in the Contacts list and the shared detail overlay and rich-text editor now have localized accessible names (screen readers).
- Completed audit of keyboard shortcut documentation against current behavior.

## Postilaatikko — v0.3.12

- Routine IMAP sync now fetches headers and flags only; full conversation content loads when opened.
- Added Finnish interface translations, including the Calendar and Contacts extensions.
- Added the macOS `⌘,` Preferences shortcut and double-click title-bar maximize/restore.
- Updated visible window branding and release-note lookup to use Postilaatikko.
- Kept the Edit Account dialog size and action buttons stable when switching tabs.
- Show configured account names under Inbox and in account rows, falling back to the email address.
- Added ICS event handoff: create an ICS event from structured date/time details and open it for review/import in the user's calendar application. No email timestamp detection (that is P-022).
- Recognized date/time mentions in email bodies and offered "Add to Calendar" chips in the conversation viewer; parsing is conservative (explicit year required, fixed zone designators only) and opens the ICS event for review/import rather than adding silently.

## Postilaatikko — v0.3.11

- Added macOS DMG installers with an Applications shortcut for drag-and-drop installation.
- Added local cross-account filters: match incoming messages by sender, recipient, or subject and route them to a folder on another account. Reuses the safe cross-account move path (copy confirmed before source deletion).

## Postilaatikko — v0.3.10

- Added unified virtual folders: save, edit, reorder, and delete sets of folders across accounts; show unread conversations and counts in the sidebar; search within a virtual folder.
- Renamed the product to Postilaatikko while retaining compatibility-sensitive package and desktop application identifiers from the upstream lineage.

## Eterno Mail — v0.3.9

- feat: Added Virtual Folders, allowing a saved set of folders across accounts to be filtered to unread and listed in the sidebar. Unread mail in these folders can be used as a to-do list.

## Eterno Mail — v0.3.6

- Improved expanded and compact sidebar sizing, icon alignment, tooltips and the Compose button for clearer navigation.
- Fixed unstable message-list scrolling and scrollbar behavior on Linux/WebKitGTK.
- Made message moves and Trash actions update the local view promptly, with safer IMAP reconciliation.
- Improved Undo reliability when restoring moved messages.
- Fixed Gmail Trash and Spam actions targeting incorrect destination mailboxes.
- Refined conversation-viewer interactions and action feedback.
- Added release notes for the installed version directly to the What's New dialog, using the project's changelog.
- Fixed HTML and JavaScript injection through OAuth callback error codes and descriptions.
- Fixed concurrent OAuth session access during login and cancellation, and prevented an older callback from clearing a newer session.
- Required SMTP STARTTLS connections now close with an error when the server does not offer encryption, preventing plaintext fallback.
- Hardened attachment saving against unsafe filenames, parent-directory traversal and symlinks escaping the destination; Save All sanitizes filenames before building paths.
- Fixed attachment downloads with empty or short message IDs and prevented simultaneous downloads from overwriting the same default filename.
- Added S/MIME verification against system trust roots and stopped caching certificates classified as unknown signers. Existing self-signed and expired-certificate exceptions now require a valid cryptographic signature.

### Security
- `internal/crypto`: device key now uses a versioned format (v2). Existing
  installations with the legacy format (v1) are automatically migrated on
  first startup: all database credentials are transparently re-encrypted with
  the new key derivation — no user action required.
  Legacy v1 support will be removed in Aerion 0.5.0.

## Eterno Mail — v0.3.5

- Fixed first-launch interaction becoming blocked after dismissing the startup dialogs.
- Fixed Enter and Space activation for focused interface buttons.
- Improved startup dialog sequencing and cleanup to prevent stale modal input locks.
- Improved external-link handling in the Terms and OAuth dialogs.
- Updated the What's New dialog to use the actual runtime version.
- Improved Flatpak development builds inside container environments.

## Eterno Mail — v0.3.4

- Added unified Drafts, Sent, Trash, Starred, Archived, Spam/Blocked and All Mail views across enabled accounts.
- Added account-specific special-folder drill-down and Gmail-compatible virtual Archived views.
- Improved draft safety with canonical draft references, sender identity persistence and attachment-safe Save & Close.
- Improved detached composer persistence so quick closes no longer lose or duplicate pending drafts.
- Added automatic background and manual GitHub release update checks.
- Added Priority, Categories and Chronological inbox display selection.
- Added localized tooltips to conversation viewer actions.
- Improved KDE Wayland/X11 titlebar compatibility.
- Updated public project documentation and interface refinements.


## Eterno Mail — v0.3.3 development

- Introduced Eterno Mail branding, public legal documentation and updated public links.
- Refined sidebar navigation, sizing and icon alignment.
- Improved inbox actions, read-state handling and continuous message loading.
- Hardened IMAP pool concurrency and migration test coverage.


**v0.3.3 - 08-06-2026**
---

- Message list redesign - [#70](https://github.com/hkdb/aerion/issues/70) + [#340](https://github.com/hkdb/aerion/issues/340)
- Added swipe gestures - right select and left delete - [#68](https://github.com/hkdb/aerion/issues/68)
- Added manual config of IMAP/SMTP auth mech
- Added digest auth for contacts and calendars - [#313](https://github.com/hkdb/aerion/issues/313)
- Added Polish translation - PR [#374](https://github.com/hkdb/aerion/pull/374)
- Fixed IMAP/SMTP auto auth mech - [#365](https://github.com/hkdb/aerion/issues/355)
- Fixed glitch when rapid deleting messages
- Refactored to engine-level serialization
- Improved toast notification logic - [#115](https://github.com/hkdb/aerion/issues/115)
- Added states to fix unwanted message list reset - [#348](https://github.com/hkdb/aerion/issues/348)
- Improved CalDAV discovery - [#363](https://github.com/hkdb/aerion/issues/363)
- Basic CarDAV (Mailfence) support - [#366](https://github.com/hkdb/aerion/issues/366)
- Fixed typo in autostart code - [#33](https://github.com/hkdb/aerion/issues/33)
- Updated nb translations - [PR #371](https://github.com/hkdb/aerion/pull/371)
- Added g, G, alt+g, alt+G, alt+c, alt+m shortcuts - [#351](https://github.com/hkdb/aerion/issues/351)
- Backfilled v to contact list
- Fixed .desktop - [#356](https://github.com/hkdb/aerion/issues/356) - [#367](https://github.com/hkdb/aerion/issues/367)
- Fixed attachment leak on reply - [#381](https://github.com/hkdb/aerion/issues/381)


**v0.3.2 - 07-16-2026**
---

- Added Spellcheck for languages using Latin alphabet - [#277](https://github.com/hkdb/aerion/issues/277) 
- Improved profile pic support - [#183](https://github.com/hkdb/issues/183) - Requires force resync of contacts
- Added re-auth button to contact write
- Improved unread count and IDLE sync - [#327](https://github.com/hkdb/aerion/issues/327)
- True fix for duplicate e-mail add error - [#318](https://github.com/hkdb/aerion/issues/318)
- Fixed calendar auth on add - [#337](https://github.com/hkdb/aerion/issues/337)
- Added guard against wails bridge saturation dervied from calendar rapid view switch
- Fixed CarDAV sync profile pic bug - May require a force resync of contacts
- Fixed toast cutoff in mobile layout - [#339](https://github.com/hkdb/aerion/issues/339)
- Updated i18n translation


**v0.3.1 - 07-08-2026**
---

- Added custom oauth for imap, cardav, and caldav (designed for and tested with [Stalwart](https://stalw.art))
- Custom oauth refresh token handling fix
- Use TOFU cert store for DAV certs
- Added Google meet link support - needs force resync
- Timezone config fix
- Multi-day week and day view fix
- Multi-day monthly view fix - [#304](https://github.com/hkdb/aerion/issues/304)
- Composer body config - [#216](https://github.com/hkdb/aerion/issues/216)
- Attachment parsing improvements - [#307](https://github.com/hkdb/aerion/issues/307)
- Fixed replying with correct identity [#325](https://github.com/hkdb/aerion/issues/325)
- Added error message for adding account with same e-mail - [#318](https://github.com/hkdb/aerion/issues/318)
- Made number of events per day in month view dynamics - [#323](https://github.com/hkdb/aerion/issues/323)
- Fixed icon rendering with newer DEs and compositors - [#316](https://github.com/hkdb/aerion/issues/316)
- Fixed diff tz time display in calendar
- Improved calendar about field rendering - needs force resync
- Fixed calendar link handling


**v0.3.0 - 06-23-2026**
---

- Prepared CardDav infra for extensibility
- Added Extension infrastructure
- ALPHA: Added Contacts extension - shipped disabled
- ALPHA: Added Calendar extension - shipped disabled - [#28](https://github.com/hkdb/aerion/issues/28)
- Updated extension translations:
    - Czech
    - French
    - German
    - Italian
    - Vietnamese
    - Chinese - CN, HK, TW
- Added shortcuts: V for View Message and D for Delete Message
- Added runtime config of client id/secret - [#138](https://github.com/hkdb/aerion/issues/138)
- Added force re-sync of contacts
- Added Vietnamese translation - PR [#232](https://github.com/hkdb/aerion/pull/232)
- Added separate smtp credentials option - [#264](https://github.com/hkdb/aerion/issues/264)
- Added no outgoing server option - [#132](https://github.com/hkdb/aerion/issues/132) [(#134)](https://github.com/hkdb/aerion/pull/134)
- Added fallback for Mailfence and other non-quote-compliant providers - [#209](https://github.com/hkdb/aerion/issues/209)
- Added smtp auto-pre-fill of smtp from imap input - [#179](https://github.com/hkdb/aerion/issues/179)
- Added delete account button in settings accounts tab
- Fixed CardDav remove provider code path to not leave orphaned contacts in db
- Fixed Sent/Draft folder message listing - [#227](https://github.com/hkdb/aerion/issues/227)
- Fixed unified inbox actions - [#234](https://github.com/hkdb/aerion/issues/234)
- Fixed Microsoft admin pre-approved oauth - [#29](https://github.com/hkdb/aerion/issues/29)
- Fixed read/star polluting undo - [#243](https://github.com/hkdb/aerion/issues/243)
- Fixed duplicate unified inbox freeze - [#241](https://github.com/hkdb/aerion/issues/241)
- Fixed unparsible body fetch handling - [#240](https://github.com/hkdb/aerion/issues/240)
- Added incremental flag sync - [#240](https://github.com/hkdb/aerion/issues/240)
- Fixed drag-n-drop inline image - [#224](https://github.com/hkdb/aerion/issues/224)
- Fixed duplicate inline image rendering
- Fixed post action blank conversation pane - [#271](https://github.com/hkdb/aerion/issues/271)
- Fixed separate smtp creds persistence - [#270](https://github.com/hkdb/aerion/issues/270)
- Fixed plaintext reply/fwd - [#285](https://github.com/hkdb/aerion/issues/285)
- Fixed print feature - [#280](https://github.com/hkdb/aerion/issues/280)
- Fixed Windows links - [#261](https://github.com/hkdb/aerion/issues/261)
- Hardened Windows URL/attachment opening - Reported by @freemans32
- Fixed mail with no body + attachment - [#293](https://github.com/hkdb/aerion/issues/293)
- Bumped flatpak build to Gnome 50 runtime


**v0.2.5 - 05-27-2026**
---

- Sync progress indication redesign and shifting folder tree fix - [#204](https://github.com/hkdb/aerion/issues/204)
- Added German translation - PR [#194](https://github.com/hkdb/aerion/pull/194)
- Added Italian translation - PR [#208](https://github.com/hkdb/aerion/pull/208)
- Dark content auto bg color and overrides - [#195](https://github.com/hkdb/aerion/issues/195)
- Added guard rails to prevent accidental close of dialogs - [#201](https://github.com/hkdb/aerion/issues/201) - [#198](https://github.com/hkdb/aerion/issues/198)
- Fixed message list on folder switch bug - [#200](https://github.com/hkdb/aerion/issues/200)
- Fixed detached composer draft ops - [#213](https://github.com/hkdb/aerion/issues/213) - [#214](https://github.com/hkdb/aerion/issues/214)
- Fixed send receipt feature
- Fixed dark themes composer lists - [#215](https://github.com/hkdb/aerion/issues/215)
- Fixed setting dialog layout - [#203](https://github.com/hkdb/aerion/issues/203)
- Fixed (workaround) folder subscription for non-compliant providers (Microsoft 365, etc) - [#218](https://github.com/hkdb/aerion/issues/218)
- Code cleanup prior to diving into v0.3.0


**v0.2.4 - 05-20-2026**
---

- Improved oAuth browser open - [#120](https://github.com/hkdb/aerion/issues/120)
- Added copy link for oAuth - [#120](https://github.com/hkdb/aerion/issues/120)
- Added dark mail content option - [#49](https://github.com/hkdb/aerion/issues/49)
- Use desktop portal for email links first and fallback to xdg-open if it fails
- Added -version flag - [#167](https://github.com/hkdb/aerion/issues/167)
- Added setup exe and default app registration for Windows - [#149](https://github.com/hkdb/aerion/issues/149)
- Added Norwegian translation - [#150](https://github.com/hkdb/aerion/issues/150)
- Fixed dark to light core theme switch bug - [#187](https://github.com/hkdb/aerion/issues/187)


**v0.2.3 - 05-14-2026**
---

- Added Czech translation
- Added drag-and-drop to move messages to another folder
- Added cross account copy/move mail - [#108](https://github.com/hkdb/aerion/issues/108)
- Added draggable recipients in composer - [#111](https://github.com/hkdb/aerion/issues/111)
- Added auto-commit recipient on lost focus - [#85](https://github.com/hkdb/aerion/issues/85)
- Added composer del/backspace guard to prevent accidental message delete
- Fixed detached composer system theme detection - [#153](https://github.com/hkdb/aerion/issues/153)
- Fixed launch flow - [#154](https://github.com/hkdb/aerion/issues/154)
- Fixed dark theme rendering - [#155](https://github.com/hkdb/aerion/issues/155)
- Added unread count update after background sync to ensure accuracy


**v0.2.2 - 05-09-2026**
---

- Made contact circles themeable
- Added live theme change preview
- Added Adwaita themes (Light/Dark)
- Added Breeze themes (Light/Dark)
- Added Catppuccin themes (Latte/Frappe/Macchiato/Mocha)
- Added Dracula theme
- Added Github themes (Light/Soft Dark/Dark)
- Added Tokyo Night theme
- Added Nord themes (Light/Dark)
- Added Pop! themes (Light/Dark)
- Added VS Code themes (Light/Dark)
- Added Yaru themes (Light/Dark)


**v0.2.1 - 05-07-2026**
---

- Added thread and message focus mode - [#129](https://github.com/hkdb/aerion/issues/129)
- Added contact circle enable/disable
- Added French translation
- Fixed keyboard expand/collapse of focused messages
- Fixed untranslated drop down menu items
- Fixed iCloud Empty Trash - [#136](https://github.com/hkdb/aerion/issues/136)
- Added ESLint for frontend linting


**v0.2.0 - 04-29-2026**
---

- Refactored N+1 query on folder switch - [#117](https://github.com/hkdb/aerion/issues/117)
- Fixed IMAP server side search regression
- Fixed settings tab icons - [#125](https://github.com/hkdb/aerion/issues/125)
- Fixed plain text links - [#113](https://github.com/hkdb/aerion/issues/113)


**v0.1.39 - 04-27-2026**
---

As of 2026-04-26, Eterno Mail is CASA Tier 2 certified and verified by Google so oAuth2 sign-ins will no longer be blocked.

v0.1.39 is a major milestone that includes some remaining originally planned basic features and a substantial amount of bug fixes/refinements focused on making existing features and functions much more reliable/stable. It will serve as a solid foundation for us to continue the further development of this mail client.

- Added image block logic to composer to avoid leaks
- Added folder subscription for auto sync - [#83](https://github.com/hkdb/aerion/issues/83)
- Added optional accent bar for unread messages - [#92](https://github.com/hkdb/aerion/issues/92)
- Added copy text in viewer context menu - [#77](https://github.com/hkdb/aerion/issues/77)
- Added select all in viewer context menu - [#77](https://github.com/hkdb/aerion/issues/77)
- Enable native context menu in composer - [#77](https://github.com/hkdb/aerion/issues/77)
- Added Shared Mailbox support for Microsoft 365 - [#93](https://github.com/hkdb/aerion/issues/93)
- Improved copy/move to folder selection dialog
- Improved invalid encoding handling
- Fixed another ghost message issue
- Fixed invalid e-mail date hang
- Fixed identity switch on replies and forwards
- Fixed draft save and send race condition
- Added proper smtp connect timeout
- Added proper IMAP STARTTLS connect timeout
- Fixed inefficient serilization for inline images and attachments
- Added inline image and attachment size limit
- Fixed inline images on replies and forwards
- Fixed reply/forward signature placement
- Added AttachConsole to for Windows builds for debug output
- Fixed murena.io CardDav - [#86](https://github.com/hkdb/aerion/issues/86)
- Fixed some HK translations
- Fixed non-UTF filename attachment open and download
- Fixed save to sent folder behavior - [#98](https://github.com/hkdb/aerion/issues/98)
- Improved composer formating
- Fixed unchecked rand.Read() in Cryptographic Code
- Backfilled missing DB error handling
- Added proper panic recovery
- Fixed pasted inline image sending
- Fixed undo delete regression
- Fixed name and subject preview decoding - [#104](https://github.com/hkdb/aerion/issues/104)
- Fixed provider icons consistency - [#102](https://github.com/hkdb/aerion/issues/102)
- Added better pgp and s/mime error feedback
- Added a wider range of PGP keys and S/MIME certs support - [#107](https://github.com/hkdb/aerion/issues/107)

**v0.1.38 - 03-22-2026**
---

- Fixed message list refresh on IDLE sync
- Fixed orphaned deleted messages in message list
- Fixed orphaned sync error messages
- Increased go test coverage
- Bumped to Node 24 (LTS)
- GA: skip manifest commit if test build


**v0.1.37 - 03-18-2026**
---

- Changed copy to and move to folder selection to dialog box instead
- Improved moved message handling
- Fixed copy and delete logic for Gmail
- Fixed threading copies of message across folders together
- Fixed post bulk delete focus - [#81](https://github.com/hkdb/aerion/issues/81)
- Fixed post send conversation refresh
- Fixed default from identity for replies


**v0.1.36 - 03-17-2026**
---

- Fixed username auto-fill in add account dialog
- Fixed attachment warning logic - [#79](https://github.com/hkdb/aerion/issues/79)
- Added u-inbox reload guards for better post sync behavior
- Added additional display render error detection - [#74](https://github.com/hkdb/aerion/issues/74)
- Fixes for [#78](https://github.com/hkdb/aerion/issues/78) and [#76](https://github.com/hkdb/aerion/issues/76):
    - Eliminated duplicate event emission from IDLE body fetch 
    - Eliminated redundant webkit calls
    - Cache image allowlist on frontend
    - Handle messages still downloading better
    - Fixed Timer Leak in scheduleMarkAsRead
    - Increased max concurrent db connections
    - Added stale guards
    - Reload necessary messages only during sync
    - Filter unified inbox reloads to inbox folders only

**Note:** Bumped GA and Flatpak node version to 22


**v0.1.35 - 03-13-2026**
---

- Fixed spinning wheel of death in Image Tab of Settings - [#73](https://github.com/hkdb/aerion/issues/73)
- Bumping Github Actions version


**v0.1.34 - 03-13-2026**
---

- Added Images tab to Settings Dialog to manage Always Load lists
- Security - Remove OAuth debug
- Security - Fix attachment file perms
- Security - Sanitize attachment filename
- Security - Validate paths in OpenFile and OpenFolder
- Security - Fix IPC socket TOCTOU with umask
- Security - Strip CRLF in writeHeader
- Cleanup - Removed dead UnblockRemoteImages function
- Cleanup - image loading logic
- Added CardDAV returning time.RFC1123Z (purelymail) workaround - [#71](https://github.com/hkdb/aerion/issues/71)
- Added CardDAV returning unquoted Etag (mailbox.org) workaround - [#26](https://github.com/hkdb/aerion/issues/26)
- Fixed message list checkboxes not responding to shift click - [#67](https://github.com/hkdb/aerion/issues/67)

**Note:** This release is compiled with a new Client ID from the Microsoft newly verified account. However, as per [#29](https://github.com/hkdb/aerion/issues/29), it still doesn't completely solve the oauth "Admin Approval" problem unless your Microsoft 365 administrator has intentionally switched to approve Microsoft verified apps (not the default) in the org settings.


**v0.1.33 - 03-11-2026**
---

- Added dynamic title to detached composer
- Fixed frontend warning
- Updated npm packages
- Improved flags values guarding
- Fixed show attachment in folder - [#69](https://github.com/hkdb/aerion/issues/69)
- Fixed downloading synthetically named attachments
- Added proper flatpak attachments opening from toast logic
- Improved has_attachment marking


**v0.1.32 - 03-10-2026**
---

- Fix - Use reply-to on replies - [#64](https://github.com/hkdb/aerion/issues/64)
- Fixed shift + click selection regression - [#67](https://github.com/hkdb/aerion/issues/67)
- Fixed detached composer title bar - [#65](https://github.com/hkdb/aerion/issues/65)
- Fixed start hidden busy cursor - [#66](https://github.com/hkdb/aerion/issues/66)


**v0.1.31 - 03-05-2026**
---

- Fixed title bar setting regression - [#57](https://github.com/hkdb/aerion/issues/57)


**v0.1.30 - 03-05-2026**
---

- Extracted theme logic from App.svelte into a dedicated Svelte store
- Added a Dark (Balanced) theme
- Added a Light (Balanced) theme
- Added tables and HTML mode to signature composer
- Added option to use native title bar/decorations - [#53](https://github.com/hkdb/aerion/issues/53)
- Added display of reply-to, cc, and bcc if not empty - [#54](https://github.com/hkdb/aerion/issues/54)
- Added always load image setting - [#40](https://github.com/hkdb/aerion/issues/40)
- Fixed Cosmic Desktop bug - needs testing - [#55](https://github.com/hkdb/aerion/issues/55)
- Added workaround instructions for GPU driver bugs - [#56](https://github.com/hkdb/aerion/issues/56)


**v0.1.29 - 02-26-2026**
---

- Toast message to provide feedback for successful link clicks
- Cross accounts from field
- Handle external mailto calls
- Added composer tab in settings 
- Allow setting detached composer as default
- Choose default or detached composer to handle mailto links
- Allow setting plaintext as default
- Moved read receipt setting to composer tab
- Cross account from field
- Fixed drag and drop inline images and attachments - [#41](https://github.com/hkdb/aerion/issues/41)
- Fixed star buttons and states - [#42](https://github.com/hkdb/aerion/issues/42)
- Fixed links in threads [#48](https://github.com/hkdb/aerion/issues/48)
- Fixed attachment logic and extraction for non-text parts - needs a force resync to apply
- Fixed orphaned drafts - [#47](https://github.com/hkdb/aerion/issues/47)
- Fixed flatpak attachment download - [#51](https://github.com/hkdb/aerion/issues/51)
- Consolidated duplicate code between composer and detached composer
- Close conversation viewer if deleted
- Don't auto-open next message if in vertical mobile layout - [#30](https://github.com/hkdb/aerion/issues/30)
- Fixed empty from field - [#39](https://github.com/hkdb/aerion/issues/39)


**v0.1.28 - 02-24-2026**
---

- Slight visual adjustments to the message list checkboxes
- Always show checkboxes on message list when in vertical mobile layout - [#30](https://github.com/hkdb/aerion/issues/30)
- Added per folder filters for unread, starred, and attachments - [#37](https://github.com/hkdb/aerion/issues/37)
- Refactored MessageList.svelte for better maintainability and performance
- Resuming Flathub submission

**Note** to **Flathub** users: A massive amount of features and fixes were in v0.1.25 - v0.1.27 which were not released to Flathub. Check the [Release Page](https://github.com/hkdb/aerion/releases) to see these changes.


**v0.1.27 - 02-23-2026**
---

- Made IMAP folders with sub-folders collapsible
- Identity aware PGP and S/MIME
- Improved guard rails for PGP and S/MIME import, sign, encrypt, and decrypt
- Added multi-language support for missing dynamic message translations
- Proper flatpak implementation of autostart on login - [#33](https://github.com/hkdb/aerion/issues/33)
- Fixed nested IMAP folders fetching - [#34](https://github.com/hkdb/aerion/issues/34)
- Fixed empty or encrypted body preview in message list


**v0.1.26 - 02-22-2026**
---

- Fixed delete silently failing on proton and other generic providers - [#31](https://github.com/hkdb/aerion/issues/31)


**v0.1.25 - 02-21-2026**
---

- Added run in background - [#15](https://github.com/hkdb/aerion/issues/15)
- Added launch hidden - [#15](https://github.com/hkdb/aerion/issues/15)
- Added launch on startup - [#15](https://github.com/hkdb/aerion/issues/15)
- Added Wake and net detection for Windows and Mac
- Added Clickable notifications for Windows and Mac
- Added Empty Trash button for Trash folders - [#21](https://github.com/hkdb/aerion/issues/21)
- Added multi-language support foundation - [#10](https://github.com/hkdb/aerion/issues/10)
- Added 中文(香港), 中文(台灣), 中文(中国)
- Added IMAP search - [#24](https://github.com/hkdb/aerion/issues/24)
- Added Responsive layout to handle both tiling and mobile - [#8](https://github.com/hkdb/aerion/issues/8)
- Fixed sync race condition when moving message during post move sync
- Fixed Trash folder detection to include Bin
- Cleaned up and reorganized sync engine code to be more maintainable

**Note:** Not submitting this release to Flathub until [this issue](https://github.com/flathub/io.github.hkdb.Eterno Mail/issues/6) is resolved.


**v0.1.24 - 02-18-2026**
---

- GMail app password fix - [#22](https://github.com/hkdb/aerion/issues/22)
- Fixed dialog boxes blurry fonts - [#23](https://github.com/hkdb/aerion/issues/23)
- Added context menu to folder pane - [#21](https://github.com/hkdb/aerion/issues/21)
- Close conversation viewer when a message is marked as unread
- Added right alt for triggering context menu with keyboard 


**v0.1.23 - 02-16-2026**
---

- Fixed race condition on marking message read when notification clicked


**v0.1.22 - 02-16-2026**
---

- Fixed wake from sleep flow - [#17](https://github.com/hkdb/aerion/issues/17)
- Added proper network state monitoring
- Improved wake, scheduled syncs, idle, and status logic with net state
- Added proper logic for offline mode
- Fixed S/MIME algo - [#13](https://github.com/hkdb/aerion/issues/13)


**v0.1.21 - 02-14-2026**
---

- Added PGP support - needs more testing
- Added S/MIME support - needs more testing
- Fixed composer rapid enter lag issue with 0 margin `<p>` instead of `<br>`
- Added auto refresh of draft folder on discard
- Added logic to prevent uneccessary reloads of loaded conversations if there's no change
- Fixed draft synced to server indication regression
- Fixed inserted images and attachments saved in draft folder
- Max window size fix [#4](https://github.com/hkdb/aerion/issues/4)
- Auto-focus to the To: field on launch of new composer and on forwards
- Fixed reliability issues with attach file and insert image
- Fixed deletion while syncing
- Improved dead connections handling which makes wake from sleep more reliable & should fix [#9](https://github.com/hkdb/aerion/issues/9)
- Fixed delete mail from trash [#9](https://github.com/hkdb/aerion/issues/9)
- Added reply, reply-all, and forward of a specific message
- Fixed move mail from trash back to inbox
- Improved Sent Folder detection (Wrong sent folder mapping will break threading)
- Ctrl+A when focused on message list will select all messages [#14](https://github.com/hkdb/aerion/issues/14)
- Ctrl+A when focused on conversation viewer will select all text of the expanded email in viewport
    

**v0.1.20 - 02-11-2026**
---

- Added resolution change detection - [#4](https://github.com/hkdb/aerion/issues/4)
- Added trusted self-signed cert flow and store - [#6](https://github.com/hkdb/aerion/issues/6)
- Improved imap login logic
- Improved image blocking to include CSS loaded images
- Enabled horizontal scroll in conversation viewer
    

**v0.1.19 - 02-09-2026**
---

- Fixed terms acceptance visibility
- Enhanced system theme detection
- Fixed idle.go/server.go
- Implemented a workaround for calling dialog through portal
- Removed redundant desktop-file-edit commands from Flatpak manifest
    

**v0.1.18 - 02-08-2026**
---

- Converted to Flathub build from source


**v0.1.17 - 02-07-2026**
---

- Added refresh conversation viewer if new mail arrives in the thread
- Added auto scroll to the bottom (newest mail) in conversation viewer on long threads
- GA/Flathub submission fix


**v0.1.16 - 02-07-2026**
---

- Removed flatpak perm that's already allowed by default
- Fixed hash calculation for Flatpak build and Flathub submission


**v0.1.15 - 02-05-2026**
---

- Refactored Linux notifications to use org.freedesktop.portal.Desktop
- Kept DBUS direct notifications if launched with --dbus-notify
- Added trigger to refocus to Eterno Mail if notification is clicked
- Added `install.sh` and `uninstall.sh` to Linux binary release
- Distribute binary tarballs with assets instead of just binary for Linux
- Fixed flatpak app ID
- Flathub submission fixes
- New Github Actions worksflow that makes much more sense


**v0.1.14 - 02-05-2026**
---

- Finalized flatpak submission


**v0.1.13 - 02-04-2026**
---

- Fixed links that don't open in browser (ie. Linkedin, etc)
- Added show link on hover
- Added context menu for links so users can choose to copy the link instead of clicking it directly


**v0.1.12 - 02-03-2026**
---

- Removed AppImage build
- Implemented Flatpak build


**v0.1.11 - 02-02-2026**
---

- Fixed detached composer theme
- Fixed message focus on refresh
- Improved transitions for smoother UX


**v0.1.10 - 02-02-2026**
---

- Added other themes:
    - Dark (Gray)
    - Light (Blue)
    - Light (Orange)


**v0.1.9 - 01-29-2026**
---

- Ability to disable window title bar in settings
- Added an AppImage just for Immutable/Atomic distros [#1](https://github.com/hkdb/aerion/issues/1)


**v0.1.8 - 01-29-2026**
---

- Fixed AppImage support for more popular immutable/atomic distros


**v0.1.7 - 01-29-2026**
---

- Fixed AppImage regression for non-atomic distros
- Sticking with 22.04 LTS to build since 20.04 doesn't have webkit2gtk-4.1 and 20.04 is only a few months away from EOS.


**v0.1.6 - 01-28-2026**
---

- Fixed signature insertion on reply
- Fixed replies not being tracked in conversations
- Fixed ghost recipient on reply-All 
- Cleaned up console.log/warn in frontend
- Added ability to delete single message from conversation
- Sync draft folder after saving draft from inline composer
- Reload conversation viewer after saving draft
- Added keyboard driven single message delete (focus on conversation viewer pane --> tab to msg --> delete)


**v0.1.5 - 01-27-2026**
---

- Bundle icons instead of downloading on launch
- Improved AppImage compatibility


**v0.1.4 - 01-26-2026**
---

- Fixed delete flow regression
- Fixed null reference errors


**v0.1.3 - 01-25-2026**
---

- Added "Mark as NOT Spam" to spam folders
- Improved Google contact sync error handling
- Auto-focus on the first message of search results on enter
- Added cancel folder sync
- Added shortcut keys for sync all accounts and folder sync


**v0.1.2 - 01-22-2026**
---

- Looses keyboard control if e-mail content was clicked
- Autofocus on first message when switched to new folder
- Disable focus on conversation viewer when links are clicked


**v0.1.1 - 01-19-2026**
---

- Compile AppImage with Ubuntu 22.04 instead to improve compatibility with older systems


**v0.1.0 - 01-16-2026**
---

- First release - ALPHA

- Double-clicking a message opens its thread in an in-app tab when macOS is set to prefer tabs; the tab keeps the thread's account and folder scope.
- Detached mail and reader windows no longer wait for the primary window's database-backed UI state before first paint; each keeps its UI state local.
