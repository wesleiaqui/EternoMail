# Inactive Reader Memory Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Reduce memory retained by inactive mail tabs while preserving workspace state and reader position, then safely raise the extra-tab limit.

**Architecture:** Keep each tab's `MailWindowApp` and message list mounted so folder, search, selection, and list scroll state remain intact. Mount the expensive `ConversationViewer` only while its tab is active, saving its scroll position before it is destroyed and restoring it when the tab becomes active again. Increase the bounded extra-tab limit from four to eight; detached native windows use a separate process path and are outside this limit.

**Tech Stack:** Svelte 5, TypeScript, existing frontend check/lint/build tooling.

---

### Task 1: Suspend inactive conversation readers

**Files:** `frontend/src/MailWindowApp.svelte`, `frontend/src/lib/components/viewer/ConversationViewer.svelte`

1. Add reader scroll snapshot props/callback and restore the saved position after the selected conversation loads.
2. Conditionally mount `ConversationViewer` only when the workspace is active; retain `MailWindowApp` and `MessageList` mounts.
3. Keep the existing read-on-leave behavior when a workspace becomes inactive.
4. Validate through Svelte checks and focused regression coverage where existing component test infrastructure permits.

### Task 2: Increase the bounded extra-tab limit

**Files:** `frontend/src/App.svelte`, localization files if the tab-limit text needs adjustment.

1. Raise `maxMailWorkspaceTabs` from four to eight extra in-app tabs.
2. Keep the existing maximum-count message accurate and ensure button disabling still uses the shared constant.

### Task 3: Verify and hand off

1. Run focused frontend lint/type checks and a production build; run regression tests for reader suspend/restore if practical.
2. Inspect the complete diff and `git diff --check`.
3. Update this task row with commit and PR status. Note that live WebKit RSS profiling with the user's mailbox remains a device-side measurement.

**Acceptance criteria:** Inactive tab viewers unmount and release their rendered conversation bodies; reactivating a tab restores its selected thread and reader scroll; list/navigation state remains mounted; eight secondary in-app tabs are allowed; detached windows are unchanged.
