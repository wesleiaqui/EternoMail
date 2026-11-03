# Gmail-only Transition Implementation Plan

> **For Codex:** Work through the small tasks in order. Preserve mail data and delivery guarantees; keep each implementation slice on its own branch/worktree as recorded in TASKS.md.

**Goal:** Complete the Gmail API path as the only supported provider while preserving the existing application experience and user data.

**Architecture:** Keep the current Wails/Svelte application, local stores, UI contracts, compose experience and scheduling. Gradually make Gmail labels, thread IDs and history the canonical remote model, replacing Gmail's current IMAP/SMTP dependencies behind the existing app-facing operations. Migrate stored state with resumable checkpoints, then remove IMAP after operational parity and live macOS performance acceptance.

**Tech Stack:** Go, Wails, Svelte 5, SQLite, Gmail REST API, OAuth incremental scopes, Go pprof, macOS Instruments.

---

## Task 1: Baseline and dependency inventory (GMAIL-001)

- Capture Go CPU and heap profiles plus app/WebKit process CPU/RSS on macOS for idle, sync, search, navigation, large-message and multiple-reader workloads.
- Record app build, OS, account/mailbox scale, exact user actions, profile window and observed latency. Repeat each workload against the same account/build before and after changes.
- Use the integrated PERF-002 / PR #96 opt-in loopback pprof endpoint and process sampler. Use Instruments Time Profiler/Allocations when WebKit dominates.
- Inventory each Gmail operation's present transport, fallback and source files. Include initial sync, incremental history, search, list/read, body/attachment, actions/undo, drafts, send, filters, notifications, account setup, shared mailbox support and scheduled-send helper.
- Set resource and latency acceptance budgets from observed baselines; do not assume IMAP or any single subsystem caused the reported spike.
- Split implementation into independently claimable and reviewable tasks before starting behavior changes.

## Task 2: Gmail account and message model (GMAIL-002)

- Define stable account-qualified provider identity using Gmail message/thread IDs and label membership while preserving local IDs required by UI/API contracts.
- Migrate list/read/search/saved-view queries from IMAP folder/UID joins to Gmail-native IDs/labels and local cache indexes.
- Implement bounded initial metadata load, `history.list` incremental changes, cursor-expiration recovery, cancellation, retry/backoff and quota budgets.
- Keep body and attachment retrieval lazy and bounded. Verify Gmail functions without an IMAP connection or pre-populated IMAP cache.

## Task 3: Complete Gmail operations (GMAIL-003)

- Move actions and undo to Gmail label/message operations with account-qualified target validation.
- Use Gmail drafts for remote draft synchronization and Gmail send for delivery. Preserve local draft history, Undo Send, deduplication and scheduled-send helper trust/recovery guarantees.
- Complete native filter CRUD using incremental consent and P-060's criteria/preview safeguards.
- Replace IDLE notification triggers with bounded Gmail history refresh or a configured push mechanism; keep cancellation and refresh coalescing.
- Verify every Gmail account workflow has no IMAP/SMTP fallback before declaring operation parity.

## Task 4: Safe migration and release verification (GMAIL-004)

- Design migration while Tasks 2 and 3 are implemented; use resumable, versioned schema steps and checkpoints.
- Preserve credentials, account identity, cached originals/files, drafts, saved views, reply/undo queues and pending sends. Avoid duplicate rows and preserve Gmail labels.
- Keep non-Gmail stored data available for export/recovery before withdrawing account access. Never silently delete source or local data.
- Verify new install, existing Gmail account upgrade, interrupted/retried migration, multiple Gmail accounts and recovery/rollback on macOS.
- Compare measured CPU/RSS and folder/search/thread latency to Task 1 budgets on the same workloads.

## Task 5: IMAP retirement (GMAIL-005)

- Remove IMAP connection pools, IDLE, UID/folder sync, Gmail IMAP/SMTP credential assumptions, fallback branches and non-Gmail setup surfaces only after migration and parity gates pass.
- Remove obsolete UI and database fields in a separately reviewed schema cleanup after compatibility needs are understood.
- Confirm fresh install, upgrade, Gmail account add/remove, offline behavior, notifications, drafts, immediate/scheduled send and export/recovery.
- Update product documentation, support scope and release notes. Keep the final device-verification checklist with the task record.

## Completion gates

- No Gmail mail operation requires IMAP or SMTP; temporary compatibility remains explicitly tracked until removal.
- Existing data and delivery workflows survive upgrade and recover from interrupted work without duplication or loss.
- Live Gmail/macOS checks pass for multiple accounts and the measured resource/latency budgets.
- IMAP and non-Gmail support are removed only in the final gated task; no whole-app rewrite is required.
