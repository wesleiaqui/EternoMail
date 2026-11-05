# Triage Workspace UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the main mail view's simultaneous folder sidebar and message-list columns with the accepted three-column triage workspace, preserving existing mailbox behavior.

**Architecture:** Keep the extension/favorites rail persistent. Recompose the current sidebar and message list into one medium context pane that can browse folders or show the active folder's messages, with the four status controls pinned above it. Keep the reader wide and place email-only reader tabs above it; switching an email tab restores its folder/message context. Persist optional context-pane auto-hide as a UI preference and reveal the pane on rail hover.

**Tech Stack:** Svelte 5, TypeScript, Tailwind utility classes, existing Wails mail/navigation APIs, existing settings/UI-state stores and i18n.

---

## Acceptance criteria

- Exactly three main mail columns: persistent thin rail, medium context pane, wide reader.
- The context pane has one top row for All, Unread, Read Later and Reply Later; status selection is scoped to the current folder/view.
- Folder browsing and folder messages occupy the same context column; entering a folder exposes its message list with a clear return-to-folders affordance.
- Reader tabs represent individual emails only. Opening or switching tabs does not hide the rail/context pane; the context pane follows the active email's source folder and list selection.
- An optional persisted auto-hide preference collapses only the context pane; hovering the rail reveals it.
- Existing search, selection, keyboard actions, folder navigation, responsive mode and reader actions remain available.

## Implementation steps

1. Trace existing primary mail layout, folder navigation state, workspace-tab state, and status/queue filters. Reuse existing stores and callbacks; avoid a parallel query system.
2. Add the context-column view state and top-level triage controls. Map controls onto existing list filters/queue views, and preserve the current folder scope when changing status.
3. Recompose Sidebar and MessageList into the single medium pane. Selecting a folder enters its list; the return control restores folder browsing without losing the folder selection.
4. Convert the primary/tab bar behavior to email-only reader tabs. Switching tabs must restore the email and its source folder context; remove folder-workspace tab creation from the + action.
5. Add persisted context-pane auto-hide preference and rail hover reveal, preserving the existing always-visible favorites rail.
6. Localize labels/tooltips and update the changelog/task ledger. Run the repository's appropriate frontend checks and record results before commit.

## Likely implementation files

- `frontend/src/App.svelte`
- `frontend/src/MailWindowApp.svelte`
- `frontend/src/lib/components/sidebar/Sidebar.svelte`
- `frontend/src/lib/components/list/MessageList.svelte`
- `frontend/src/lib/stores/settings.svelte.ts` or `frontend/src/lib/stores/uiState.svelte.ts`
- English/Finnish locale files under `frontend/src/lib/i18n/`
- `CHANGELOG.md`, `ROADMAP.md`, and `TASKS.md`

## Verification

- Run the focused frontend type check, lint, and production build for changed UI surfaces.
- Inspect the final diff for whitespace and confirm the three-column and tab-scope states are represented by the same context model.
- Live mailbox and native WKWebView interaction remain a user-side verification gate if the environment cannot launch the app.
