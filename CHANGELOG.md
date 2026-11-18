# CHANGELOG

## [Unreleased]

- Combine Read later and Reply later into one Later pile, preserving existing marks and removing them only when explicitly requested.

### Native menu shortcut column
- macOS shortcut hints now use AppKit's aligned right column instead of title tabs. Native hints match the same primary keyboard bindings as the in-app context menus, including v for Move, l/Shift+L for Later, ! for Spam and # for Trash. Existing webview shortcut ownership, context checks and native clipboard actions are preserved.
- Search all mail from the rail, refine results in a temporary message-list workspace, and save explicitly as an existing-format virtual folder. Find inside open messages with Cmd/Ctrl+F; press it again to widen the query to all mail. Cmd/Ctrl+Shift+F opens all-mail search.
- Hide folders locally from the folder menu or Account settings > Folders, with search and bulk hide/show. Hidden folders leave navigation and pickers and pause explicit refresh and body prefetch without deleting cached mail or changing provider subscriptions. Saved views keep cached mail with stale-source warnings; filters to hidden destinations pause until shown. Adds a folder visibility preference to the local database.
- Create local mail filters from the message menu or Settings. Prefill sender, recipients, subject and List-ID; scope rules to all or chosen accounts; move, mark read, archive or move to Trash. Preview stored matches before saving. Applying to existing mail is opt-in, capped at 500 matches, and queued by the helper. List-ID matching on older mail is limited to captured headers until they refresh.

## Postilaatikko 2026.11.17.1

- Attachment cache repair ignores unnamed inline plain-text and HTML bodies instead of exporting them as files, while preserving inline calendar invitations and vCards. Save All through the Linux file portal gives repeated filenames separate names and rejects duplicate destinations before writing.

- Mac test builds now stop before checkout if GitHub returns a malformed main commit SHA, including on the Bash 3.2 shipped with macOS. This restores the source-validation test on Mac build runners while keeping stale-source and non-arm64 requests blocked.

- Hidden mail readers and inline drafts no longer react to mail or Send shortcuts. Switching extensions finalizes read-on-leave or cancels delayed marking; returning resumes the normal read policy.

- Keep unfinished inline replies mounted when switching mail to Calendar or Contacts, while disabling hidden mail commands. Recent edits remain in the same composer when returning to Mail.

- Expand native menus across mail, composer formatting, attachments, folders, scheduled delivery, Calendar and Contacts. Menu actions reuse UI handlers and confirmations, disable ambiguous or unavailable targets, and reject stale selection requests. Existing keyboard bindings are unchanged. Inline replies block background mail commands; nonmodal event details keep Calendar navigation available. Explicit read-state actions include mixed conversations, and detached composers keep native image/rich-text paste.

- Keep message attachments on one line: show up to three as file chips, or a collapsed attachment count and Save All bar for larger sets. Expand to see every full filename and action. No attachment deduplication or data changes.

- Attachment cache repair leaves attached multipart files untouched rather than losing their named attachment during parsing. Move reconciliation and fallback cleanup protect repaired attachment rows, exact payloads and undo history; conflicts keep both local messages for explicit reconciliation.

- Repair repeated attachment rows on the local mailbox-owning app from a verified complete original IMAP message. Preserve every real MIME part and exact download payload, archive old cache rows and inline bytes transactionally, and provide a reversible local restore command. Unsupported/damaged MIME and remote-only desktops retain the original cache. See docs/attachment-cache-repair.md; native/live mailbox validation remains outstanding.

- Keep the message index visible beside opened mail in desktop windows, including 768-1024px tiled windows and with context auto-hide enabled. Phone drill-down and explicit focus mode are unchanged.

- Send Later can repeat (every N days, weeks or months; never, after N sends or until a date) and has a mailing-list mode that puts you in To and everyone else in one identical Cc or Bcc message. The Studio queues each run only after the previous one was accepted by the provider; a failed or uncertain run stops the series, missed runs are skipped, and every run gets its own Message-ID and Date. No unsubscribe link, per-recipient copies or merge fields. Go tests pass; the desktop app package and native Studio delivery were not compiled or run here.

- Clearly label paused Undo Send recovery with a persistent action-needed message in Finnish and English.

- Keep explicit Send now/Undo controls after a verified pre-claim failure, with the automatic countdown paused; unverifiable outcomes never expose a retry.

- Never restart an Undo Send countdown from a dispatch exception or failed reload; uncertain failures require explicit review rather than automatic resend. Remote desktops now check uncertain helper receipts while idle every minute, without submitting mail, and drain that worker before device storage closes.

- Keep local delivery failures out of the remote Undo Send retry path, preventing automatic resubmission after an uncertain local send.

- Remote Undo Send now releases draft locks after definitive helper rejection and keeps pending mail visible for retry when a device-database claim fails.

- Remote desktops now keep the Undo Send safety window in the device database instead of accessing an unopened mailbox database. Cancel and dispatch arbitrate atomically; the remote helper receives immutable MIME under the queue's stable request ID. Restart never auto-sends, uncertain provider outcomes are not resubmitted, and helper-owned draft cleanup does not delete a later draft edit. Native two-machine delivery remains to be verified.

- Allow an exactly-full remote Calendar window when later recurrence candidates add no occurrences; still reject any additional in-window instance.

- Route remote Calendar occurrence reads through bounded helper snapshots and the context-aware recurrence adapter; retain local-store-free desktop reads.
- Honor the desktop Calendar enabled setting before any remote read, without opening a local Calendar store.

- Preserve Calendar plaintext body rendering in remote mode while discarding helper-supplied HTML and using the shared rich-body policy.

- Prepare remote Calendar typed reads with bounded metadata snapshots and explicit sync worker cancellation/draining before store shutdown. The helper mounts source, calendar, single-event and bounded occurrence reads using the context-aware evaluator; the desktop sanitizes rich event bodies locally. Provider synchronization, source administration and alarms are not enabled by this read-only slice.

- Share extension-scoped Auth and secret-store adapters between desktop and future headless hosts. Headless interactive consent fails explicitly without a host callback; CardDAV host-secret reads reject malformed source identities. This does not enable remote Contacts or Calendar by itself.
- Remote mailbox service mode gains Contacts and CardDAV: a desktop connected to a mailbox helper gets the Contacts tab and can browse, search and page contacts, view photos, add or reauthorize Google/Microsoft contact sources (consent runs on the desktop, tokens are stored on the helper), manage CardDAV sources and sync them on the helper. The device grant must include `contacts.read` and `contacts.write`, plus `contacts.manage` for adding, renaming, removing or reauthorizing sources; grants created before these capabilities existed keep working for mail and need to be recreated to add contacts. Enabling write access on a source still requires local mode, and contact-change event subscriptions and MCP contact tools are not routed remotely yet.
- Avoid a recurrence mask panic for out-of-period ordinal weekdays in bounded Calendar reads, including missing fifth weekdays.

- Add an isolated recurrence evaluator with context/work checks inside generation loops and pre-construction option limits; bounded Calendar store expansion uses it without changing the existing local unbounded API. Remote occurrence routing remains a separate integration step.
- Remote contact mutations keep at most 64 unacknowledged requests awaiting the helper's receipt (keyed by a fixed-size digest, so photos are not retained while a send's outcome is unknown). Once earlier changes are acknowledged the retry key is dropped; while 64 remain unanswered, further contact changes are refused with a clear error instead of risking a duplicated create on retry.
- Apply remote Calendar read budgets only to overlapping non-recurring events and recurring candidates, so unrelated history does not block current reads; allow Calendar read grants to be registered.

- Add a bounded Calendar read API foundation for the remote helper, with a separate Calendar read grant, strict query limits and one shared recurrence-work budget across all selected events. The routes are not yet mounted by the helper or used by the desktop; remote Calendar remains unavailable until lifecycle and routing integration is complete.

## Postilaatikko 2026.11.12.1

- The Send Later helper accepts remote HTTPS deployments: non-loopback `--listen` now requires at least one `--allowed-host` naming the exact Host values the listener serves, and the DNS-rebinding Host guard honors them (port-stripped, case-insensitive, no wildcards); loopback-only validation is unchanged, and the installed launchd agent carries the allowed hosts.
- Add focus-visible styling, high-contrast focus rings, group role, and keyboard glyph disclosure to custom window controls (traffic lights).
- Virtual folders no longer fail with "database is locked (SQLITE_BUSY)" while sync is writing: virtual-folder writes retry transient lock contention, the cached-view endpoint answers with a bounded deadline and a distinct retryable busy code, and the view falls back to reading conversations directly when the cached index cannot refresh or the cache request times out.
- macOS signed releases now carry hardened-runtime resource entitlements matching the app's usage descriptions: the Developer ID signing steps pass `build/darwin/entitlements.plist` (currently the Contacts entitlement, without which the system blocks `CNContactStore` before its permission prompt), and a CI guard dumps the packaged app's signed entitlements and fails when a usage string lacks its entitlement.
- Design document for a zero-knowledge Postilaatikko service (`docs/plans/2026-11-11-zero-knowledge-server.md`): a hosted multi-tenant architecture where the operator cannot read mail content, with an explicit threat model, client-side key hierarchy, hybrid post-quantum transport, encrypted-search options with their leakage, multi-device enrolment and recovery, GDPR/provider-OAuth/abuse/assurance requirements, and a phased implementation plan. Documentation only; no behavior changes.
- Inbox month headings now follow a system time-zone change: the cached month formatter is keyed by the active time zone, so a message near a month boundary no longer keeps the previous zone's month name after travel or a zone change.
- Redirect destinations are cleaned when opened or copied; the link menu also allows resolving the original link via Cloudflare when a redirect requires tracking parameters.
- Correct the CI fallback source-list option so apt actually selects the temporary official Ubuntu mirrors.

- CI can recover from the Azure resolver outage using fresh Cloudflare DNS answers for fixed Ubuntu mirrors inside apt-only mount namespaces; global hosts and resolver files are unchanged.

- CI retries failed Ubuntu package installation against official Ubuntu mirrors with bounded timeouts, keeping all backend checks required.

- Select all in a plain-text message context menu now selects the whole message, while HTML messages keep their iframe selection.
- Cloudflare link checks refuse IPv4-mapped private targets and IPv6 translation/tunnel ranges to prevent requests reaching local services.

- Right-clicking a link offers "Open via Cloudflare DNS": the app resolves the link's host through Cloudflare DNS-over-HTTPS (1.1.1.1), bypassing DNS-level ad blockers, and follows the redirect chain without cookies to show where the link really goes before you open it. The dialog shows every hop with buttons to open or copy the final URL or open the original link. Explicit per-link action only - checking the link contacts the tracking service, same as opening it.

- The macOS disk image opens a dark welcome window with light icon wells and a drag-to-Applications guide.
- Link privacy: opening or copying a link from a message now strips known tracking parameters (utm_*, fbclid, gclid and similar) before the URL leaves the app. The stored message and the displayed link are never changed. On by default; a new General settings toggle turns it off, and the link context menu offers opening or copying the original URL when a link was cleaned, for the rare site that breaks without its parameters.
- Clicking a message in the message list now opens it in the open reader tab: the click retargets the tab, but the tab only watched for folder changes, so it kept showing the old message.
- Shared Postilaatikko/Solmu keyboard layer (approved keymap): search also on Cmd/Ctrl+F, forward on Cmd/Ctrl+L, Read later on Cmd/Ctrl+U, Reply later on Cmd/Ctrl+I, archive and mark read on Cmd/Ctrl+E, next/previous message on Cmd/Ctrl+Shift+]/[, the shortcuts overlay also opens with Cmd/Ctrl+/, previous/next account on Cmd/Ctrl+Alt+Left/Right, and the message-list quick filters on Cmd/Ctrl+Alt+1-4 (all, unread, reply later, read later). Load remote images moves to Cmd/Ctrl+Alt+L (always-load to Cmd/Ctrl+Shift+Alt+L) so Cmd/Ctrl+L can forward, and mark-as-read drops Cmd/Ctrl+U (Shift+I stays) so Cmd/Ctrl+U can queue for later. Every existing Gmail-style bare key keeps working, and Option-mangled keys on macOS now fall back to the physical key, so the new Option chords fire on any keyboard layout.
- Add checkbox semantics, contextual dynamic accessible name, focus-visible styling to conversation row checkbox, and focus-within disclosure plus focus rings to quick action buttons.
- The four quick-filter buttons at the top of the column (all, unread, read later, reply later) now filter the folder list while the Kaikki kansiot panel is open: only folders with matching mail stay, with their parent folders kept dimmed for context. The filter clears when the panel closes. Read later and reply later counts come from the backend, so folders holding queued mail are found even when the queue lives outside the synced folders.
- Switching folders no longer selects the top message: nothing is highlighted and the reading pane stays empty until you pick a conversation. Arrow keys still start at the first message, and returning to a folder you already visited restores the message you had selected.
- A background refresh that removes the selected conversation (snoozed or moved elsewhere) now clears the selection instead of leaving a phantom that Space could turn into a bulk action on nothing.
- Custom title bar: the green zoom button is no longer clipped in half; the quick-access rail now leaves room for all three traffic lights.
- Folder clicks no longer stall: the legacy/native Gmail duplicate-suppression clause now uses the Message-ID index instead of scanning the whole folder once per message. Opening a large folder (for example a Gmail archive) went from hitting the 4 s/8 s store deadlines to roughly a tenth of a second.
- Conversation view no longer flattens designed HTML email: headings, button fills, card backgrounds and link colors render as authored, like Gmail, instead of everything shrinking to plain body text with outline buttons. The bubble background still shows through around the message.

- Send Later helper health and router error responses use the same browser-Origin rejection and security headers as encrypted command responses.
- Pressing / focuses search in Contacts again, without allowing mail shortcuts to act on hidden messages. Calendar has no general search control, so / is left alone there.

- Add high-contrast focus rings and outline styling to sidebar favorite star buttons and conversation row action buttons for accessible keyboard navigation.
- Bare Gmail-style letter shortcuts (d trash, e archive+read, s star, ...) now fire only while the mail rail is active; pressing them in Calendar or Contacts no longer acts on the hidden mail list. Global single keys like `?` (shortcuts help) keep working on every pane.
- Archive and mark read (E, toolbar button, selection bar, secondary window) is now one atomic mailbox action: the helper archives and marks read in a single operation, and Undo reverses both together, moving the messages back and restoring their previous read state. If the read update fails after the move, the helper reverses the move and restores the original unread state itself; the secondary mail window captures the same unread snapshot (asking the backend when a row is mixed or unloaded) and actually restores it on Undo; a partially completed action is reported as a failure instead of a success with an incomplete Undo; and the legacy fallback still offers the archive Undo when only the read step fails. The frontend's unread-snapshot workaround only remains for that fallback.
- Bare mail letter shortcuts preserve the produced case: Caps Lock no longer turns uppercase D/E into Trash/Archive. Modifier chords remain case-insensitive.

- Command palette (Cmd/Ctrl+K, Cmd/Ctrl+P): undo, star, mark read, mark unread and the theme switch keep it open so you can repeat them. It lists the last 8 commands you ran from it first, and now includes Toggle read, Read later, Reply later, Move to and Copy to. The sidebar button still opens it. Ctrl/Cmd+K no longer archives: archive is `e`, which archives and marks as read. The viewer toolbar, selection bar and context menu do the same, and plain Archive (keeping read status) stays in the context menu.
- Keyboard shortcuts: the mail commands bound to Gmail-style keys and Ctrl/Cmd keys (reply, forward, archive, spam, star, read, trash, undo, sync, images and more) are now matched from the shortcut registry by one dispatcher instead of hand-written key cases in App.svelte. No key changes.
- Archive and mark read (E, toolbar button, selection bar) now puts the messages back as unread when you undo it. The palette's Enter-on-no-match search is also remembered in recents. The unread state is kept for as long as the operation can be undone (not by count), and search result rows stay in step with read changes so a search row you already read is not put back as unread.
- Keyboard shortcut audit: a test now fails on any duplicate binding in one context; every cross-group reuse is listed with its reason.
- Stop continuous CPU use while an email is open in the reader: the inline-image loading placeholder is now static (no animated SVG) and iframe height updates are skipped when unchanged.
- Skip meta-charset regex scans on HTML parts without a `<meta` and `charset` token, keeping long-s (`charſet`) spellings on the regex path.
## Postilaatikko 2026.11.10.4

- Add to Calendar now recognises Finnish dates: dotted dates such as 12.11.2026, month names such as 12. marraskuuta 2026, and times written "klo 14", "klo 14.30" or "klo 9:00-10.30". A bare number after a date (for example "June 5, 2026 14 people", or a price after a dotted date) is no longer read as an hour; use "at 3 pm", minutes or am/pm. Dates still need a four-digit year, and slash dates such as 12/05/2026 are still skipped because they are ambiguous. Finnish times may carry a zone (klo 14 UTC, klo 14 +02:00), and English hour ranges such as 2-3 pm are recognised; a time followed by an ambiguous zone name such as PST is skipped.
## Postilaatikko 2026.11.10.3

- Keyboard-focused mail actions and read/star state now follow the account-qualified row in unified lists, even when two accounts share a thread ID. A stale focus does not target another account.

- Snooze: pick when a conversation comes back. Presets (later today, this evening, tomorrow morning, this weekend, next week) or a custom date and time replace the old whole-days box. Press H, use the Snooze button in the reader toolbar or the selection bar, the message menu or the command palette. Works on several selected conversations at once, and Undo takes the whole batch back. A toast tells you when snoozed conversations return. Snoozing stays local: nothing changes on the mail server.

## Postilaatikko 2026.11.10.2

- One bar: the window now has a single top bar. The separate title bar and the back/forward row above the message list are gone, which gives back about 80 pt of height. The close, minimise and zoom buttons sit at the top left in the usual Mac order, above the rail. The back and forward arrows moved into the reading pane's top row, next to the "New window" button. Native title bar mode is unchanged.

## Postilaatikko 2026.11.10.1

- The Kansiot/Viestit switch is now animated: in narrow windows the folder overlay actually slides in and out over the message list (previously it appeared and vanished instantly despite the transition), and in wide windows the shared column slides between folders and messages instead of snapping. The covered message list is removed from the accessibility tree while the overlay is up. All of it is skipped when Reduce Motion is on, and divider drags still track the pointer exactly.

- Preserve the opened search UID when several rows share a conversation. Reader restoration matches the account and folder, and keyboard focus changes do not reapply an older reader selection.

- Keep server row UI identity mailbox-qualified after resolution too. Canonical conversation IDs are separate backend targets, so identical RFC thread IDs across accounts cannot change another row's checked state.

- Give unfetched server rows mailbox-qualified provisional IDs, so same-UID results in different accounts or folders remain independently checked while resolving.

- Carry checked server rows forward when their provisional IDs resolve to canonical conversations. Keyboard opening also pins Inbox card position like clicking.

- Preserve checked rows when opening the focused message with Enter, while using the same server-resolution and stale-request guards as clicking.

- Discard late server-message opens when the selection has changed, including keyboard selection. Keyboard opening uses the same canonical-resolution path as clicking.

- Resolve fetched server results by account and folder identity as well as UID, preventing same-UID rows in different mailboxes from being replaced. Validate the fetched message account before enabling queue actions.

- Use only checked rows from the active search source for queue readiness and targets, so hidden cached results cannot block valid local selections.

- Preserve ordinary mailbox action targets while blocking mixed resolved/unresolved queue selections. Local server results with no thread ID use the stored message ID as their canonical conversation.

- Keep queue actions disabled on unresolved IMAP search IDs until opening the result resolves its indexed conversation. A failed item in a multi-selection queue update no longer skips the remaining items.

- Add Read later and Reply later to the reader toolbar, message-row quick actions, per-message reader context menus, snoozed and multi-selected row menus, and HTML-body copy/link menus. Actions keep account-qualified conversation targets, queue state, shortcut hints and targeted Undo.

## Postilaatikko 2026.11.09.5

- Use Cmd-Left and Cmd-Right to go Back and Forward through visited messages. Search fields, recipient fields and text editors keep their normal line-start/end keys. No bracket shortcuts; mailbox Undo and composer history are unchanged.

## Postilaatikko 2026.11.09.4

- Make Undo Send quieter: show a thin draining bar along the toast edge instead of a numeric countdown. Undo still cancels the pending send and restores its draft; Send now and the existing send delay are unchanged.

## Postilaatikko 2026.11.09.3

- Go Back and Forward through messages you opened, using the new buttons above the shared folder/message column. History remembers the account, folder, search, and email-tab context for this session; opening another message after Back starts a new forward path. Navigation does not undo or redo mailbox actions or composer edits.

## Postilaatikko 2026.11.09.2

- Keep your place in the message list when new mail arrives: the visible row stays at the same screen position while mail sorts above it. An "N new above" button jumps to the newest mail without changing the message open in the reader. No docked message row.

## Postilaatikko 2026.11.09.1

- Press Cmd/Ctrl+D, or click the new Folders button at the bottom of the left rail, to open the full folder tree. In wide windows the tree and the message list share one column: the open one takes the space and the other folds to a thin strip you can click. Picking a folder, Escape, or Cmd/Ctrl+D again returns to the message list. In narrow windows they swap in the same pane. Open the composer in its own window with Cmd/Ctrl+Shift+D; Cmd/Ctrl+D no longer detaches it.
- The open message now moves to its correct chronological spot when the list refreshes, instead of staying pinned above newer mail. It stays selected and in view.

- Deduplicate thread messages sharing the same RFC Message-ID in conversation views, eliminating redundant multi-label copies (e.g. Gmail Inbox vs All Mail). Preserve downloaded bodies, reply targets and reaction chips on the retained copy; fetch only displayed bodies and keep connected-reader counts consistent.
- Show local sender avatars beside collapsed conversation bubble tails when viewer circles are enabled, without external favicon requests; keep HTML message widths readable.

## Postilaatikko 2026.11.08.1

- Give archive, trash, move and spam actions a short exit animation; keep cancelled Trash deletions visible and hold removed rows until refreshed data replaces them.

- Exercise the notarization password prompt and exact quoted-keychain path round-trip in release credential checks.

- Preserve existing macOS keychain search paths while importing the signing identity, and stop signing on security-tool diagnostics even if the process exits successfully.

- Preserve generated identity model instances when changing the default address, keeping keyboard focus without breaking packaged frontend type checks.

- Keep macOS signing and notarization passwords out of process arguments, and make draft DMG checksum files directly verifiable beside the downloaded image.

- Improve accessibility and keyboard navigation in Account Settings identity lists with account-specific native radio groups, focus retained after changing the default address, visible focus indicators, and focus disclosure for row action buttons. Failed default changes restore the selected address.
- Reject mailto URLs with percent-encoded leading dashes (%2D) in URL openers to prevent CLI option injection in system mail handlers.
- Browse and search unified Sent, Drafts, Trash, Spam, Starred, and Archive through a remote helper; the helper resolves account folders and merges Archive with All Mail while excluding Inbox copies.
- Replay bounded helper action and Undo events by authenticated client cursor after disconnects; refresh an affected open view or request one reset when retained history has expired.
- Route Cmd/Ctrl+V through the system clipboard fallback in composer windows and send composer Undo to its local draft history while editing; mailbox Undo remains scoped to durable helper operations.
- Reconcile helper-owned actions and Undo through bounded account/message/folder references, refresh only affected open views, and retain uncertain outcomes in Undo History with explicit status.
- Start a desktop configured for a remote mailbox helper without opening the local mailbox database. Route core navigation, saved-view cache/counts, folder counts, thread reads, and explicit body fetches through the helper.
- Route remote-only inline compose drafts, replies, and sends through helper-owned APIs, retaining draft revisions and send receipt keys across retries without opening local compose/provider stores.
- Load remote attachment metadata through account-scoped helper reads and fetch inline/file attachment bytes only when rendering inline content or opening/saving files.
- Serve virtual-folder sidebar unread counts from the authenticated helper for connected desktops while preserving existing count semantics and local-helper fallback.
- Connect the desktop to a user-operated remote mailbox helper over HTTPS with per-device credentials stored in the OS keychain. Routed service operations fail closed on remote errors; remote-only startup remains follow-up work.
- Keep virtual-folder result caches warm in the independent helper and serve cached conversations with freshness metadata to connected desktop clients.
- Store desktop TLS trust decisions in the device database while retaining legacy certificate rows in the mailbox database.
- Store explicit folder body-prefetch choices in the device database and remove them when the mailbox account is removed.
- Store desktop preferences and trusted remote-image entries in the device database, importing existing values without deleting their mailbox-database copies.
- Keep desktop flag state and events aligned with provider-confirmed partial bulk changes, and allow longer helper-backed mutations to be retried safely after an ambiguous timeout.
- Keep All Inboxes conversation lists and counts aligned by resolving the enabled accounts' selected Inbox folders for both helper-backed reads and local fallback.
- Keep thread actions and automatic read-on-leave limited to messages in the active view, preserve scope during body fetch, and keep service thread reads metadata-only with account-safe stale-reference resolution.
- Persist desktop window and navigation state in a device-owned database, importing existing state without deleting the service-database copy.
- Add helper-owned draft read/write, cached-body reply preparation, and explicit immediate send with revision checks and durable idempotency receipts.
- Hand OAuth callback tokens and per-account custom provider credentials to the authenticated helper, with compensating rollback and Microsoft shared-mailbox propagation.
- Route IMAP and SMTP password changes through authenticated helper account operations with explicit keep/replace/clear actions, credential snapshot compensation, and secret-safe idempotency fingerprints.
- Make the bundled helper the mailbox schema migrator. Desktop startup now requires a versioned helper/API/schema handshake, and active desktop, composer, and helper processes hold shared schema locks so upgrades cannot migrate while they use the database.
- Bundle the headless helper beside the desktop app in supported Linux, Windows, macOS, Flatpak, release, and development packages.
- Add macOS General Settings controls to install, start, and stop the independently supervised background mail service.
- Install the macOS helper agent with bounded mailbox sync and automatic filters enabled by default; use `--no-sync` for a Send Later-only agent. The foreground `serve` command keeps sync opt-in.
- Route physical-folder and All Inboxes conversation pages through the authenticated local mailbox service when available, including triage filters and refresh windows larger than one page; preserve local fallback only when the helper is unreachable.
- Add an authenticated, bounded conversation-list API for real folders and All Inboxes, with stable sorting, triage filters, and header-only summaries.
- Route explicit desktop message-body fetches through the authenticated localhost helper when connected, retaining local fetching only when the helper is unreachable.
- Reuse the existing reader tab when the same conversation is opened again, and share a bounded conversation snapshot across tab mounts so reopening recent threads is fast without retaining unbounded mail bodies.
- Mount idempotent authenticated mail actions in the headless API, including provider-backed flags, archive, trash, and account-qualified safe moves.
- Add optional Gmail Pub/Sub pull notifications and daily watch renewal to the localhost helper while retaining periodic sync recovery.
- Wire provider-aware search, thread reads, and explicit body fetch through the authenticated helper API using the shared desktop search operation.
- Start the authenticated mailbox API with the helper on localhost, persisting grants in the runtime-owned database and shutting its listener down with the service.
- Compose the headless mailbox read API behind the authenticated automation server, requiring explicit token storage.
- Add a bounded, account-qualified message-header API for headless mailbox reads; message bodies remain on demand.
- Allow the authenticated automation API to bind to user-operated remote hosts only with TLS and an explicit Host allowlist; keep the default listener loopback-only.
- When started with `--sync`, the headless helper now applies saved cross-account filter rules to newly synced messages, confirming the destination copy before moving the source to Trash.
- Coordinate desktop and helper background sync with one process lock; the helper continues serving Send Later while it waits for desktop sync to release ownership.
- Automatic filter jobs now persist in the mailbox database. Unstarted jobs resume after restart; interrupted moves are retained as uncertain and are not retried automatically.
- Persist local mailbox API client grants as token digests and add helper commands to create, inspect, and revoke credentials.
- Add capability-checked headless handlers for account, folder, and saved-view API reads, backed by the existing local stores.
- Let the helper use Gmail API access tokens from its local OAuth credential store for provider-aware mailbox operations; OAuth secrets remain local to the helper.
- Route saved-view and automatic filter-rule administration through authenticated helper API operations when the helper is connected.
- Route Reply Later queue reads and membership changes through the helper, preserving Gmail labels and desktop Undo history.
- Route Read Later and Snooze reads and mutations through the helper, preserving Gmail labels and local fallback only while the helper is unavailable.
- Expire due Snoozes in the helper independently of mailbox sync so deadlines are honored while the desktop is closed.
- Expose bounded, read-only inspection of automatic filter moves with uncertain provider outcomes; inspection never retries or changes mail.
- Route folder catalog reads, provider discovery, and subscription changes through the authenticated helper when available.
- Serve the desktop's account configuration reads from the authenticated helper while keeping credentials out of account responses.
- Add separately granted `account.manage` access for the desktop, preserving existing service capabilities when replacing its local grant.
- Route account creation, configuration, deletion, enable/order, and Microsoft shared-mailbox setup through the helper with durable idempotency receipts and recoverable credential cleanup.

## Postilaatikko 2026.11.07.1

- Add an opt-in arm64 draft test-release build that checks current merged main, runs frontend checks, verifies both executable architectures, signs and notarizes the DMG, and uploads directly to a draft GitHub Release without Actions artifacts or cache writes.

- Preserve provider-confirmed partial read/star changes on desktop errors and allow account removal before a custom OAuth fallback was ever saved.

- Fix Move To and Copy To command dispatch so the folder picker follows the currently focused message list or conversation instead of failing.
- Recognize supported unified system-folder scopes consistently with the mailbox service, while rejecting malformed non-string folder types.

- Reuse compiled attachment keyword patterns when checking a draft for forgotten attachments.
- Repair current-focus routing for Move To and Copy To and keep unified-folder scope checks aligned with the mailbox service.

- Make conversation bubbles narrower and align them by sender, with subtle participant colors that remain distinguishable in group threads.

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
