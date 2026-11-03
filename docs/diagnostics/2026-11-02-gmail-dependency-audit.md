# Gmail dependency audit — 2026-11-02

This is a source inventory refreshed against `main` at `5bb5e5a`, not a live-provider test. The application already has a Gmail REST client and account sync path, but Gmail eligibility and several message operations still depend on IMAP configuration, local UIDs and fallbacks.

| Area | Current path | Transition work |
| --- | --- | --- |
| Account identity | `internal/sync/gmail_native.go:GmailClient` identifies Gmail by OAuth plus `IMAPHost == imap.gmail.com`. | Introduce provider identity/capability independent of an IMAP hostname; migrate account settings without losing OAuth credentials. |
| Labels and folders | `internal/sync/folders.go` / `gmail_native.go` map Gmail labels into local folder records. Some UI contracts still use folder IDs/paths. | Treat Gmail label IDs as provider identity; preserve local UI folder IDs only as a cache/view mapping. |
| Initial and incremental sync | `app/sync.go:SyncAccountComplete` syncs folders before `SyncGmailAccount`; `internal/sync/scheduler.go` selects Gmail sync based on `GmailClient`. `SyncGmailAccount` uses bounded metadata pages and Gmail history cursors. | Remove IMAP initialization and make bounded Gmail metadata/history refresh the account path; validate expired cursor/backfill behavior. |
| Search | `internal/sync/gmail_search.go:GmailSearch` still returns IMAP-shaped results and maps Gmail messages to local rows. `app/mail_search.go` falls back to `searchIMAPAccountPage` when an account is not Gmail-capable. | Use Gmail IDs/threads/labels in search result contracts and remove the Gmail-to-IMAP result adaptation. |
| Body and raw source | `internal/sync/fetch.go:FetchMessageBody` and `FetchRawMessage` branch to Gmail API when capable, otherwise fetch by IMAP UID. `internal/sync/gmail_body.go` already supports Gmail bodies and attachments. | Keep the Gmail API path, provider IDs and lazy fetch; remove UID dependence and unused IMAP fallback after parity. |
| Flags and actions | `app/actions.go` has Gmail REST calls for some flags, labels, spam, trash, move/copy and undo paths, with substantial IMAP operation/reconciliation fallback code. `app/gmail_actions.go` holds native actions. | Inventory and convert each operation separately; preserve explicit target validation, undo and copy-before-delete behavior. |
| Drafts | `app/draft.go:syncToIMAP` serializes drafts to RFC822 and APPENDs to an IMAP Drafts folder. Gmail draft IDs are also stored/resolved in this file, but Gmail API is not the normal save path. | Move save/update/delete to Gmail Drafts API; retain local draft history and ensure remote reconciliation is idempotent. |
| Sending and scheduling | OAuth Gmail immediate sends use `messages.send`, including Gmail `threadId` plus RFC threading headers for replies (PR #101). Other providers use SMTP. Scheduled-send has a local durable queue and helper/relay trust boundary. | Move Gmail drafts and scheduled delivery to Gmail API while preserving E2EE/helper guarantees, retry classification and duplicate-send prevention. Verify the reply path against a live Gmail thread. |
| Notifications and refresh | `app/app.go` initializes IMAP pools and an IDLE manager; account changes and shutdown maintain IDLE. The scheduler also periodically invokes Gmail metadata/history sync. | Replace Gmail IDLE with bounded history refresh or configured Gmail push; avoid duplicate polling and retain cancellation/coalescing. |
| Setup and credentials | Account forms, connection checks, OAuth setup, shared mailbox setup and keyring storage contain IMAP/SMTP fields and assumptions. | Design Gmail-only account setup and safe data export/recovery before removing non-Gmail surfaces. |

## Findings

- Gmail API support is real and covers account metadata/history, search, message bodies, labels/actions and attachment references. It is not a complete transport migration.
- Gmail is detected through an IMAP hostname. Several Gmail API operations still accept folder/UID-shaped values or fall back to IMAP when the Gmail client is unavailable/not capable.
- Draft sync explicitly uses IMAP APPEND. OAuth Gmail immediate delivery now uses Gmail REST; other providers use SMTP. Scheduled-send helper delivery still requires a separate safety review before transport changes.
- Global IMAP pools and IDLE start in the main app even when an account uses Gmail API for scheduled metadata refresh. This makes IMAP a concrete profiling target, not proof that it caused the reported CPU/RSS.
- PERF-002 / PR #96 is integrated at `8ed107f`; it provides opt-in loopback Go pprof and a macOS process sampler. The current Apple Silicon profiling DMG was built from `5bb5e5a` in [workflow run 41546560919](https://github.com/ollisulopuisto/postilaatikko/actions/runs/41546560919). No live macOS CPU/RSS baseline is recorded yet, so performance budgets are not set.

## GMAIL-001 handoff

The next implementation slices should first measure the same Gmail mailbox during idle, sync, search, folder/thread navigation, large-message reading and multiple-reader use. Record app version/build (`5bb5e5a`), macOS version, account and mailbox scale, duration, actions, Go heap/CPU and WebKit process RSS/CPU. Then use this inventory to refine GMAIL-002/003 task boundaries before provider behavior changes.
