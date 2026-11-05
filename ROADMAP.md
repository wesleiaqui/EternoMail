# Roadmap

Planned, in flight, open questions, and parked items for this fork. Done items are marked with the version that shipped them. Suitable changes are intended to be proposed upstream to Eterno Mail after they are stable.

## Product direction

### Triage workspace navigation (2026-11-04)

The mail workspace uses three columns: a thin, persistent favorites/queue rail; one medium context column; and a wide reader. The context column has one row of All, Unread, Read Later, and Reply Later toggles at its top. It accordion-switches between folder browsing and the selected folder's messages; selecting a folder reveals its messages in that same column, and a clear back/collapse action returns to folders. Tabs represent individual open emails only and sit above the reader; switching tabs keeps the rail and context column visible and restores the corresponding folder/message context. A preference may auto-hide the context column, with the rail remaining visible and hover revealing the column. This supersedes the simultaneous folder sidebar + message-list columns in the main mail workspace.

The goal is a strong personal email client for one person using multiple accounts. Keep Gmail API support advancing where it improves Gmail, while preserving IMAP and other existing provider paths. Spark and Gmail remain interaction references; the accepted conversation/navigation design also borrows selected ideas from Spike, HEY, Shortwave, Unibox, MailMate and Superhuman. Keep the focus on one person's mail and workflows.

### Provider direction (updated 2026-11-03)

Keep IMAP support. The app now feels faster, so the earlier CPU/memory concern is not a reason to remove a working provider path. Pause IMAP retirement, provider migration, and Gmail-only setup changes. Continue measured performance work only when it helps diagnose a reproduced issue. Gmail-native features such as server search, labels, history refresh, and filters can continue incrementally for Gmail accounts, with IMAP retained for supported accounts and fallback paths. Revisit provider retirement only after new evidence and an explicit product decision. The Gmail-only transition queue in TASKS.md is deferred, not active.

### Accepted UI and agent-access direction (2026-10-26)

Detailed implementation plan: [D-002 — conversation, navigation and agent API](docs/plans/2026-10-26-conversation-navigation-agent-api.md). Its 41 small `UX-*` tasks, dependencies, owner slots and recommended model/effort are in [TASKS.md](TASKS.md#d-002-conversation-navigation-and-agent-api-queue). These are planned changes; a candidate branch or successful build does not mean they are integrated or released.

Execution checkpoint: UX-002's shared view/thread contract is integrated in PR #29 (`d3490b1`), and UX-004's extraction schema plus 22 synthetic fixtures in PR #30 (`664f630`). Those accepted models remain the foundation for parser/service work. PR #31 adds the coordination protocol. PR #32 (`2191cb8`) integrates the virtual-folder date/NULL-subject repair; actual-device verification and remaining header-refresh/exclude-mode work stay open. UX-016 and UX-019 can proceed from the shared contract. The former divergent local-main Send Later candidate remains on GitHub as `codex/p011-local-main-backup-20261026` through `cba3457`, with its live/security gates intact.

Parser checkpoint: PR #33 (`74ed7ff`) integrates plain-text extraction, then stacked PR #34 (`7436026`) integrates safe HTML extraction. UX-007 service/cache is reserved for Claude; its dependencies are the integrated reference contract and both parsers, independently of the remaining virtual-folder/live-device gate. No conversation-reader UI is shipped by these parser changes.

- **Compact reading refinement:** conversation mode uses directional bubbles with sender labels once per run and details/originals on demand; signature/source controls stay out of the reading flow. Double-click a message-list row to open an email-only tab; its pop-out action opens an independent reader window. Folder/browser views are never opened as tabs. Track implementation in READER-001 and P-073.
- **Email reactions:** display recognized Gmail reactions and delivered Outlook notification emails as reversible emoji chips on their exact source message; preserve unknown/unresolved messages and all originals. Sending reactions is a later task.
- **Two reader formats:** Conversation extracts each email's new contribution into a readable timeline; Traditional shows full emails. Provide a visible toggle and per-message Show original. Preserve inline replies, meaningful formatting, attachments and crypto status; extraction uncertainty falls back to complete content.
- **Unified and virtual views:** All Inboxes belongs inside the Virtual folders card. Real folders, saved queries and system views use consistent cross-account scope/filter semantics; query, grouping, reader format and panel layout remain independent.
- **Navigation on demand:** open folder cards in a dismissible drawer with Keep open. Cards support reorder, collapse, hide/restore and moving individual entries between cards. Preserve all-folder browsing and existing account-tree expansion. The rail holds chosen real folders and views with Cmd/Ctrl+1…0; Contacts/Calendar shortcuts default off for new layouts.
- **Thread Focus mode:** show only the open thread with nearby actions and reply controls. Exiting restores the previous layout, selection, scroll and keyboard focus. Build on the existing focus behavior.
- **Actions close to messages:** buttons on list rows and message headers, a bulk-selection bar, More menus and shortcut hints. They share explicit target resolution with the command panel, keyboard and backend API. Marking read must not remove the selected row from under the pointer.
- **Undo history:** Cmd-Z/Ctrl-Z and Edit → Undo always target the newest mailbox operation; toast Undo remains scoped to its own action. Edit → Undo History shows a bounded, privacy-minimal list of durable operations and permits undoing one operation directly. There is no redo or branching.
- **Inline replies:** reuse durable drafts, sender identities, signatures and templates; expand to full compose without losing the draft or changing recipients.
- **Recipient-safe replies:** make Reply, Reply All and the actual To/Cc recipients explicit. During the existing Undo Send countdown, offer “Edit recipients” that cancels the pending send and restores the same draft for correction; after SMTP delivery begins, never imply that Undo can recall it. When the user quits the app, dispatch pending Undo Send messages immediately and wait for the send attempt; a failure keeps its draft and leaves the app open. This does not alter long-term Send Later jobs.
- **Agent access as a foundation:** provide versioned authenticated operations/endpoints for scoped search, structured threads, authored/original content, explicit content fetch, drafts, mail actions and supported queues/scheduling. CLI/MCP are thin adapters. Report incomplete/unfetched data so external LLM summaries cannot silently assume a full thread.
- **Personal reply workflow:** Reply Later is an explicit, durable triage queue for conversations the user chooses to answer. It is distinct from a provider's replied/unreplied flag, Snooze, and critical Send Later delivery scheduling. Expose an in-folder quick filter for queue members and a first-class Reply Later mail view in the sidebar. Queue membership changes use the normal undo flow; only a confirmed immediate send completes a queued item.
- **Later options:** People grouping, correspondence attachment browsing, optional sender bundles, configurable columns/density, related searches and optional saved-view tabs. Track which defaults are recommendations in D-002; do not treat every inspiration feature as approved scope.
- **Resource discipline:** keep header/flag sync, explicit body/attachment fetch, bounded extraction/caches, cancellation and incremental rendering. Respect explicit per-folder body-download preferences without eager attachment downloads or unlimited backfill.

### Release-baseline checkpoint

REL-001 restores the Send Later code already present in the remote v0.3.16 tag (`73ca27a`) onto current main in an isolated review branch. The tag and main had diverged; earlier statements that this code was never released were incomplete. Delivery/security/live-device gates remain open. The tagged message-focus shortcut change (`e30b37e`) touches Claude-owned EmailBody and is deferred until UX-008 integration; it must be reconciled before promoting a replacement release.

### Historical requested build (2026-10-27; sequencing superseded above)

Implement sidebar cards/rail shortcuts, then Gmail-first operations, global server search and API-backed virtual folders, automatic metadata refresh, and a GitHub-built DMG. GLOBAL-001 owns this delivery on an isolated review branch; it is not merged into main. Gmail views must work from server results without requiring a pre-populated IMAP cache. Preserve bounded header/history backfill and explicit body/file downloads. Existing notification-only IMAP IDLE may remain where there is no configured Gmail push service; reads/mutations prefer Gmail REST.

### Priorities

1. **Recipient autocomplete and address-book sync.** Make compose suggestions useful from explicitly synced contact sources and locally available message headers, without fetching message bodies.
2. **Scheduled send — critical.** Compose now, choose a delivery time, and have Postilaatikko send it reliably.
3. **Gmail API integration.** Continue Gmail-native capabilities such as server search, history refresh and filters while retaining IMAP support and existing provider paths.
4. **Calendar from email:** recognize date/time mentions and offer links that open or create events in installed calendar applications.
5. **Reliable virtual folders and the accepted UI/API design:** first verify the empty-folder repair in the actual installed build, then implement D-002 backend contracts/query/extraction and agent access before dependent presentation. Add the navigation drawer, conversation/Traditional toggle, thread focus and nearby actions in small increments.
6. **Fast everyday mail:** preserve Gmail-style keyboard bindings and quick reply snippets; add full templates, Finnish-layout support and configurable bindings.
7. **Better compose controls:** reuse cross-account sender identities and durable drafts, with signatures selected from sender, recipient and available context.
8. **Cross-account automation:** extend the existing local rules when provider-side filters cannot cross accounts.
9. **Tabbed mail workspaces:** open independent mail views in tabs, preserve each tab’s folder/search/thread state, and detach a tab to a native window (P-043).
10. **Smart notifications:** optional sender- and rule-based controls. Useful, but lower priority than the core workflows above.

### Product boundaries

- This is a personal mail client. Shared inboxes, team assignment, collaboration features and built-in AI writing are out of scope. External LLM tools may use the authenticated API for summarization or user-directed workflows.
- Prefer local cross-account behavior when a provider API cannot perform the action, and never delete a source message until a cross-account copy has been confirmed.
- Keep notifications configurable and avoid making them a requirement for the core mail workflow.
- Prefer contextual action surfaces (selection bar, context menus, selection popovers) over a floating or dockable tool palette. All of them, and the command panel, draw on one shared action definition list tied to the shortcut registry. See `docs/plans/2026-10-25-contextual-toolbars-design.md` (D-001; follow-ups P-029 to P-034).

### Recommended implementation order

1. **Recipient lookup (P-065):** restore immediate compose suggestions using authorized contacts and locally available message headers.
2. **Gmail API capabilities:** continue incremental Gmail-specific features while preserving IMAP and existing provider paths.
3. **Scheduled send and existing workflows:** preserve the current IMAP path and Send Later trust boundaries while delivering reliability improvements.
4. **Remaining roadmap:** D-002 presentation/agent clients, calendar recognition, templates and notifications with their existing dependencies. Safety and delivery recovery fixes may proceed throughout.

## Guidance for agents

Read this before writing code in this repo. Full contributor rules are in `CONTRIBUTING.md`; these are the ones that matter most here.

- **Test first (red, then green).** For every bug fix or feature, write a test that reproduces it, run it and confirm it fails, then implement and confirm it passes. Do not fix and move on without a regression test. If the project lacks test infrastructure for a case, add it rather than skip the test.
- **Go tests are table-driven** where several cases share a shape. Run `go test ./...` (or `make test`, which sets the WebKit build tags) before committing.
- **Go style:** `gofmt`, explicit error handling with no silently dropped errors, zerolog for logging, a comment on every exported function.
- **Control flow:** avoid `else` and `else if` chains. Use guard clauses with early returns, or a `switch`.
- **Keep the CHANGELOG current.** Each commit that changes behaviour adds its entry under the version heading. Write it for whoever promotes the release, in terms of what changes for users or operators, not file names.
- **Keep this roadmap current in the same commit as the work.** Mark items done with the version, and list anything unfinished, untested or waiting on a human.
- **Commit messages** use the `feat:`, `fix:`, `docs:`, `refactor:`, `test:` or `chore:` prefixes from `CONTRIBUTING.md`. Commit locally; push only when asked.
- **Ask before deciding things that are the user's to decide** (e.g. the open questions below). Choose sensible defaults for everything else and say what you chose.

## PR-sized tasks

Use the [task board](TASKS.md) for current claims and integration status; the [D-002 plan](docs/plans/2026-10-26-conversation-navigation-agent-api.md#9-small-implementation-tasks) contains the new small delivery slices. Existing in-flight fixes are not complete until verified in their target environment and integrated.

### Easy

- **Bounded UI follow-ups:** reader toggle/original controls, rail hints, nearby action wiring, related-search controls and column/density preferences. These have individual UX task IDs and accepted contracts; Luna High is the starting recommendation where the task table says so.
- **Preserve completed conventions:** Finnish core/extension localization, account display names, stable Edit Account geometry and macOS Preferences/title-bar behavior must survive the redesign.

### Medium

- **Query and extraction integration:** preserve header-only sync and existing virtual-folder criteria while adding consistent saved queries, bounded derived-content caching and API reads.
- **Navigation and reply integration:** drawer/cards, restored Focus state and inline draft reuse. Sol 6.1 Medium is the default recommendation after the contracts are integrated.
- **macOS Contacts source.** Start with read-only browsing and recipient autocomplete. Offer a link/action to open the selected person in Apple's Contacts app for modifications; Postilaatikko will not write contact changes in the first version.

### Large

- **Sensitive work gets stronger review:** HTML/plain-text extraction fidelity, scope/target semantics, authorization and delivery/draft races start with Sol 6.1 High. See D-002 for escalation criteria; effort size and required reasoning capability are separate.
- **Gmail API extensions.** Build on the existing Gmail server-side search/spam baseline for Gmail accounts, distinguishing live-provider checks from compilation and preserving IMAP/provider fallbacks.
- **Scheduled send.** Durable E2EE queue plus the always-on Mac Studio helper, retries, cancellation, and delivery status. The helper is the trusted endpoint; a future paid hosted sender needs a separate, clearly disclosed trust model.
- **Calendar links from email.** Create a standard ICS event from a structured time, then recognize dates and times in email and offer the event for review/import in the installed calendar app.
- **Tabbed mail workspaces.** Open independent mail views in tabs, preserve each tab’s folder/search/thread state, and detach tabs to native windows; the primary workspace remains available (P-013/P-043).

## Must have

- **Gmail API integration.** Server-side search and native spam actions have a v0.3.13 baseline. Continue adding Gmail-native features with least-required scopes while preserving IMAP and existing provider paths. Track actual Gmail authorization/provider verification separately. See the [Gmail API overview](https://developers.google.com/workspace/gmail/api/guides) and [scope guide](https://developers.google.com/workspace/gmail/api/auth/scopes).
- **Scheduled send (critical).** Compose now and send at a chosen time. Candidate P-011 work exists on local/integration branches and retains live/security release gates; it is not integrated into the surveyed origin/main. Keep the durable E2EE queue and always-on Mac Studio helper: paired devices encrypt to the helper, relays retain ciphertext, and the helper holds its private key/sending credentials locally. Recover interruptions without unsafe duplicate sends. A future paid hosted sender is a separate trust mode if the service decrypts mail to send it.
- **Unified virtual folders (v0.3.10; criteria expanded in v0.3.15).** Saved sets of real folders across accounts, shown in the sidebar. Base folder-set behavior implemented in v0.3.10; criteria facets (include/exclude, read/unread, starred/unstarred, replied/unreplied, date windows) implemented in v0.3.15.
- **Reliable saved views.** Repair and verify virtual-folder population on the actual mail device, then share filtering/count/search semantics with All Inboxes and saved searches. A released criteria feature does not establish that the later empty-folder repair shipped.
- **Conversation/Traditional reader.** Shared email-specific extraction with original fallback, readable thread cards and a remembered format toggle; see UX-004–009.
- **Fast folder access and customizable cards.** Pins and section-order/visibility baselines exist from P-007/P-008. Extend them into the on-demand drawer, typed entries/cards, All Inboxes in Virtual folders and rail shortcuts; see UX-010–014. Preserve the all-folder browser and main's deletion confirmation behavior.
- **Header-only bulk sync with sidebar-aware prefetch.** Keep headers/flags for all folders. Prefetch bounded bodies for Inbox, Starred and real folders the user pins in the sidebar, unless explicitly disabled for that folder; folders outside the sidebar remain header-only unless explicitly enabled. Attachments stay on demand and historical backfill requires a bounded explicit action (UX-040/041).
- **Authenticated agent API.** Structured thread/search/original-content access and shared mail/draft/view/queue operations, with explicit fetching, capabilities, CLI/MCP adapters and honest completeness. Use a privacy-first permission model and require clear user authorization for consequential actions such as sending or deleting mail. This is a foundational requirement, not built-in AI writing; see UX-019–024 and UX-037–039.
- **Cross-account message moves.** Already supported. Preserve the safety rule: confirm the destination copy before removing the source.
- **Multiple sender identities across accounts.** Already supported in the composer. Improve it with contextual signature selection (sender, recipient, and message type); signatures can already be configured per identity.
- **Local cross-account filters.** Baseline shipped in v0.3.13. Extend the existing confirmed-copy move service when new rules need it.
- **Gmail-style keyboard bindings and nearby buttons.** Existing shortcuts/registry/command panel are the baseline. Complete reading/iframe coverage, Finnish layouts and configurable overrides while sharing targets with visible actions; disable single keys in editable fields. See UX-016/017/029 and `docs/KEYBOARD_SHORTCUTS.md`.
- **Gmail-native spam actions.** Existing v0.3.13 API baseline supports native spam/not-spam label operations with fallback. Verify actual provider behavior and undo; do not claim classifier-training guarantees without evidence.
- **Quick replies and email templates.** Short snippets shipped in v0.3.13; full email templates remain P-020. Reuse them in inline/full compose while preserving sender, recipients and unsaved draft content.
- **Calendar recognition.** Turn recognized date/time mentions in message content into an “Add to Calendar” action using the standard ICS event handoff. Open it for review/import and handle timezone ambiguity conservatively. Not started; build on `extensions/calendar`.
- **Tabbed mail workspaces.** Open independent mail-list/reader workspaces in tabs by default, preserving each tab’s folder, search and thread state. Allow detaching a tab into its own native mail window. Limit the number of extra live readers to keep memory bounded. P-013 provides independent mail-window support; P-043 implements the tab shell and detach action. Native lifecycle and real-mail checks remain before release.

## Planned

- **Reply Later and Focus & Reply.** Personal queue and focused reply sessions, preserving skipped items/drafts and completing entries only on confirmed send acceptance; UX-025–027. Separate from Send Later scheduling.
- **People and attachment browsing.** Optional correspondent grouping with topic boundaries, plus scoped attachment discovery with explicit downloads and incomplete-cache indicators; UX-030–032.
- **Layout/search refinements.** Related searches/save view, configurable columns/density and optional sender bundles; UX-028/033/034. Optional saved-view tabs and hiding empty views remain later decisions.
- **macOS Contacts integration.** Start with read-only address book browsing and recipient lookup/autocomplete, with macOS privacy authorization. For edits, link to the selected person in Apple's Contacts app; do not write contact changes from Postilaatikko in the initial version.
- **Smart notifications.** Per-sender and rule-based controls for deciding which messages produce notifications. Not started; lower priority.
- **Snooze and reminders.** Hide or remind about a conversation at a chosen time. Not started and lower priority than the core items above.

## Open questions

- **Paid hosted scheduled send.** It may serve users without an always-on computer, but the service would have to access message content and sending credentials at delivery time. Decide whether to offer this separate, non-E2EE trust mode; keep the initial Mac Studio helper mode E2EE.
- **Later layout options.** Whether to add saved-view tabs, hide empty views, per-view reader overrides or incoming-mail presentation schedules. Do not block the accepted core UI/API work on these decisions.

## Parked

- Nothing parked right now.

## Done

- **Reply extraction parsers (PRs #33/#34; developer foundation).** Conservative plain-text segmentation and DOM-based sanitized HTML segmentation share the accepted result schema. Combined email/views tests, vet and macOS app-package build pass. Service/cache wiring, reader UI and actual-mail fidelity checks remain subsequent work.
- **Reply-extraction schema and fixture corpus (PR #30; developer foundation).** Shared block/status/source-range validation and 22 synthetic text/HTML fixtures. No parser or conversation-reader UI is implemented by this change.
- **Virtual-folder timestamp/subject query repair (PR #32; unreleased).** Normalize legacy Go timestamps for windowed list/search/count/unread queries and tolerate NULL subjects. Affected Go tests/vet and macOS app-package build pass. Actual mailbox verification and header-refresh/exclude-mode follow-ups remain open.
- **Narrower default pane widths (PR #28; unreleased).** Sidebar and message list default to 260px and 320px. The migration applies once; widths chosen afterwards are preserved. PR CI and 26 focused pane/sidebar/deletion checks passed; the running layout remains unverified.

- **Virtual-folder criteria and default sync (v0.3.15).** Saved folder sets expanded into full cross-account views with include/exclude folder selection, read/unread, starred/unstarred, replied/unreplied, and date window (7d, 30d, 90d, 1y, all-time) criteria. Removed hardcoded unread-only constraint; criteria apply consistently across conversation lists and in-folder search. Default sync period updated to All Messages (0 days) across models, OAuth, and UI, with migration 52 upgrading legacy defaults.
- **Command palette and Gmail shortcuts (v0.3.14).** Alfred-style quick launcher (`⌘P` / `Ctrl+P`), Gmail standard shortcuts (`v`, `e`, `c`, `r`, `a`, `f`, `x`, `/`, `!`, `#`, `z`, `Shift+I`, `Shift+U`, `o`, `u`), shortcut dialog layout stabilization, configurable mark-as-read delay, sidebar subfolder tree navigation, and i18n cleanup.
- **Keyboard shortcut overlay and discoverability (v0.3.13).** Unified keyboard shortcut registry, searchable `?` reference dialog, and UI tooltips and key hints.
- **Gmail API integration (v0.3.13).** Server-side search using Gmail query syntax and native mark-as-spam / not-spam actions through the Gmail API, with graceful fallback to IMAP.
- **Composer quick replies (v0.3.13).** Saved snippets managed from the composer footer without mutating draft state.
- **Local cross-account filters (v0.3.13).** Automatic routing of incoming mail across accounts based on rules, using confirmed copy before source deletion.
- **ICS event handoff (v0.3.13).** Standard `.ics` event creation and handoff to system calendar.
- **Accessibility and localization (v0.3.13).** Localized accessible names across Contacts and shared UI kit controls.
- **Routine header-only IMAP sync (v0.3.12).** Body content loaded on demand.
- **Finnish localization and macOS window conventions (v0.3.12).** Finnish translations, `⌘,` shortcut, title-bar maximize.
- **Unified virtual folders (v0.3.10).** Saved views across selected folders and accounts, including sidebar management, message listing, and unread counts.
- **Postilaatikko product naming (v0.3.10).** Updated visible product branding while retaining compatibility-sensitive app identifiers.
