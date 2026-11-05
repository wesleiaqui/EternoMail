# Gmail Triage Queues and Local Snooze Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Read Later and Reply Later durable Gmail-backed triage queues, retain app-local queue behavior for non-Gmail accounts, and add local snooze that temporarily hides a conversation and restores it to the UI after a chosen delay.

**Architecture:** Gmail accounts store queue membership as hidden, namespaced Gmail user labels through the existing Gmail API client; read/unread remains an independent system label. Other providers continue to use local queue storage. Snooze is local metadata for every provider: it records an absolute wake time, hides the conversation from normal app results while active, and expires on startup or when due without changing provider read state, labels, or folders. Queue view/count/sidebar actions use the existing navigation and undo patterns.

**Tech Stack:** Go Gmail REST client, SQLite migrations/stores, Wails app actions, Svelte 5, existing list/sidebar/i18n code.

---

### Task 1: Gmail user-label primitives

**Files:** `internal/gmail/client.go`, its tests, Gmail sync/provider mapping as needed.

1. Add create/lookup/modify support for application-owned user labels with hidden Gmail sidebar visibility.
2. Ensure special labels are not surfaced as ordinary navigable folders.
3. Preserve least-required OAuth scope (`gmail.modify`) and message/thread semantics.

### Task 2: Read Later and Reply Later provider integration

**Files:** queue stores/actions, Gmail and local mail search, sidebar/view actions, tests, en/fi localization.

1. Use Gmail user labels as source of truth for Gmail queue membership; retain local queue storage for non-Gmail providers.
2. Add Read Later membership and list/count/action surfaces; keep read/unread independent and permit either behavior when queuing.
3. Synchronize Gmail queue changes from provider metadata so changes made in Gmail are reflected in Postilaatikko.
4. Keep Reply Later behavior separate from replied/unreplied flags and from scheduled send.

### Task 3: Local snooze and triage visibility

**Files:** SQLite migration/store, snooze actions, query/view filtering, due-time wake-up, UI controls and tests.

1. Store account-qualified conversation snooze deadlines locally.
2. While snoozed, omit the conversation from normal triage results without mutating Gmail or IMAP state.
3. Expire due snoozes on app startup and at the next deadline; make the conversation visible again in its existing folders/views, preserving unread/read state.
4. Expose snoozed items/count and snooze/unsnooze actions with undo where compatible with the existing action framework.

### Task 4: Verification and handoff

1. Add regression tests for label CRUD/assignment, provider/local queue behavior, read-state independence, snooze expiry and query visibility.
2. Run relevant Go and frontend checks, inspect the diff, update ROADMAP/TASKS/CHANGELOG, commit and propose a PR against `ollisulopuisto/postilaatikko`.
3. Record that live Gmail consent/provider behavior and app closed/reopen timing require device verification.

**Acceptance criteria:** Gmail label changes persist across app restarts and clients; non-Gmail queues remain local; Read Later does not imply unread or unread does not imply Read Later; Reply Later remains a distinct explicit intention; local snooze hides until due then restores visibility without changing read state; all queues expose usable counts and navigation.
