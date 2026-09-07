# Release 0.3.6 readiness — 2026-09-07

This is a product-validation pass over the completed Round 3 workspace. It is not a new general audit. Existing worktree changes were preserved; no commit, tag, dependency auto-fix, or release metadata rewrite was made.

## E2E / smoke

| Flow | Result | Evidence / scope |
|---|---|---|
| Product binary and version | PASS | Production-tagged binary built from this checkout reports `0.3.6` with `--version`. |
| First startup through DB/credentials preflight | PASS | A clean, temporary XDG profile created its database and a 65-byte, mode-0600 v2 `device.key`; paths, database open, and migrations completed. |
| Second startup / crypto migration idempotence | PASS | The same temporary profile reopened successfully. It retained the same v2 key and produced no migration/decrypt error. This was an empty-profile smoke; the full legacy-data upgrade case remains covered by the Round 3 process tests. |
| Desktop window and normal close | NOT TESTED | No DISPLAY, Wayland session, Xvfb, or Wails executable is available in this environment. Wails panics with `failed to init GTK` only after preflight succeeds; this is the expected result of trying to create GTK without a display, not evidence of a desktop-session startup defect. |
| Account listing and rapid account switching | NOT TESTED | No test accounts or graphical session were available. Ownership and cache-generation regression tests pass under `-race`; they are not claimed as interactive E2E. |
| Message list, conversation, HTML/plain text, links | NOT TESTED | GUI interaction unavailable. Frontend Node tests pass for HTTP/HTTPS/mailto handling, escaping, no nested links, Unicode, and rejected local/script schemes. |
| Attachment list/download/Save As/Save All/Unicode | NOT TESTED | No interactive test mailbox. Email regression tests pass, including Unicode-safe paths and an attachment whose exact saved bytes remain `SGVsbG8=` rather than being decoded to `Hello`. |
| OAuth start/cancel/live provider | NOT TESTED | No real provider credentials were used. Local tests cover cancellation and a 401 retry that preserves a consumed POST body. |
| PGP/S/MIME UI with test keys | NOT TESTED | No test key material was provisioned for a desktop UI. Store tests verify foreign account IDs are rejected and own-account defaults succeed. |
| Contacts/photos/account change | NOT TESTED | No configured sources or UI. Contact cancellation and stale-photo-response tests pass. |
| Calendar view/range/source change | NOT TESTED | No enabled calendar source or UI. Backend tests cover interval isolation and exclusion of disabled sources. |
| Shutdown during sync/contacts/CardDAV and repeated requests | NOT TESTED as GUI E2E | No desktop process could be created. Race-tested lifecycle tests cover repeated shutdown, scheduler Stop waiting for a worker, cancellation propagation, and restart/double-stop. |

## Startup, migration, shutdown, and logs

The production-tagged smoke binary ran preflight twice against `/tmp/anclo-release-smoke-036-product`, isolated from the user profile. Both runs opened the database and completed SQL migrations. The generated `device.key` is v2-sized (65 bytes) and 0600. The test intentionally did not use an existing user database or credentials.

Both runs then terminated at GTK initialization because the environment has no graphical display. The corresponding `panic: failed to init GTK` is recorded as an environmental test limitation, not ignored. It occurs after database and credentials preflight and prevents validating the visible UI, normal GUI close, or live account flows here.

The smoke logs were searched for `panic`, `fatal`, `race`, `database locked`, `migration`, `decrypt`, `credential`, `timeout`, `leaked goroutine`, and `unexpected EOF`. The only relevant entries were the expected headless GTK panic, successful migration messages, and the expected fallback warning that the container has no desktop keyring. Synthetic smoke data produced no password, access/refresh token, OAuth client secret, private key, or full ciphertext in the logs.

## npm dependency review

`npm audit --json` reports 34 moderate entries, zero high and zero critical. They reduce to two advisories:

1. `@tiptap/core` is a runtime dependency of the composer/signature/rich-text editors. The affected `mergeAttributes` path is therefore present in the shipped UI. The reviewed editor configurations use fixed `HTMLAttributes`; remote email rendering is not passed through TipTap. No app-controlled path supplying an attacker-owned `__proto__` attributes object was found in this validation pass. The available fix is `3.30.4`, a major upgrade outside this release's compatibility scope. Track the upgrade and re-test editor behavior for a future release; do not treat the advisory as absent.
2. `esbuild` is transitive through `svelte-i18n` and applies to the Vite development server. The packaged desktop application does not expose that development server. The fix proposed by the audit would require dependency changes beyond the existing compatibility constraints.

No compatible dependency update was applied and `npm audit fix` was not run.

## Release metadata and Flatpak

`VERSION`, `CHANGELOG.md`, `frontend/package.json`, `wails.json`, and Flatpak metainfo all declare 0.3.6. `README.md` does not declare a standalone release version, so it has no conflicting value. The Flathub manifest declares `tag: v0.3.6`, but its `commit: 4c33a9821c86778e33003aa2d9e18e325b3c1e93` predates this uncommitted workspace.

Before publishing, update only the following manifest fields after creating the actual release commit and tag:

- `build/flatpak/flathub/io.github.wesleiaqui.eternomail.yml`: `tag` and `commit` in the application Git source.

Do not retain the current source SHA for the final release and do not substitute a made-up SHA. The local Flatpak packaging build was not run: `flatpak` and required runtimes exist, but the host lacks the `flatpak-builder` CLI; installing tools/runtimes or modifying the user's Flatpak installation was outside this validation pass.

## Build and test results

| Command | Result |
|---|---|
| Production-tagged smoke build and `--version` | PASS — reports 0.3.6 |
| `GOFLAGS=-tags=webkit2_41 go test ./... -race -count=1` | PASS — exit 0 |
| `GOFLAGS=-tags=webkit2_41 go vet ./...` | PASS — exit 0 |
| `GOFLAGS=-tags=webkit2_41 go build ./...` | PASS — exit 0 |
| `npm run check` | PASS — zero errors and warnings |
| `node --test tests/*.test.mjs` | PASS — 5/5 |
| `npm run build` | PASS — existing bundle-size warnings only |
| `npm audit --json` | 34 moderate, 0 high, 0 critical; documented above |
| `git diff --check` | PASS — exit 0 |

Go validation ran in the `anclo-dev` container with GTK/WebKit development dependencies and the `webkit2_41` tag. The frontend uses the installed Node 24 Flatpak SDK runtime.

## Limitations

- No graphical session is available, so interactive desktop E2E, normal GUI shutdown, multiple visible accounts, configured mailboxes, live OAuth, keyring, PGP/S/MIME, contacts, and calendar sources could not be exercised.
- The headless GTK initialization panic is environmental and must be rechecked on a release-like desktop session before distribution.
- The Flatpak manifest must receive the real final commit/tag; local Flatpak packaging was not available from the host CLI.
- The two npm advisories described above remain. The TipTap issue is runtime-present but no attacker-controlled attribute-object path was found; the esbuild issue is development-server-only.

## Files altered in this stage

- `docs/audit-2026-09-07/release-0.3.6-readiness.md`

No production code or release metadata was changed in this stage.

## Git status

`git status --short` was reviewed after the validation. The workspace remains intentionally dirty from the prior rounds; this stage adds the readiness report only.

```text
M CHANGELOG.md; README.md; VERSION
M app/account.go; app/app.go; app/attachment.go; app/oauth.go; app/search.go; app/settings.go
M build/flatpak/flathub/go.mod.yml; build/flatpak/flathub/io.github.wesleiaqui.eternomail.yml; build/flatpak/flathub/modules.txt; build/flatpak/flathub/node-sources.json; build/flatpak/io.github.wesleiaqui.eternomail.metainfo.xml
M extensions/calendar/backend/freebusy_aggregator.go; extensions/calendar/backend/provider_local_freebusy.go
M frontend/package-lock.json; frontend/package.json; frontend/package.json.md5; frontend/src/App.svelte; frontend/src/lib/components/WhatsNewDialog.svelte; frontend/src/lib/components/viewer/AttachmentList.svelte; frontend/src/lib/components/viewer/ConversationViewer.svelte; frontend/src/lib/components/viewer/EmailBody.svelte; frontend/src/lib/i18n/locales/en.json; frontend/src/lib/i18n/locales/pt-BR.json; frontend/src/lib/stores/accounts.svelte.ts; frontend/src/lib/stores/contactPhotos.svelte.ts; frontend/src/lib/stores/inlineAttachmentCache.ts
M go.mod; go.sum
M internal/carddav/client.go; internal/carddav/scheduler.go; internal/carddav/sync.go
M internal/contact/google_sync.go; internal/contact/microsoft_sync.go
M internal/credentials/oauth.go; internal/credentials/oauth_clientconfig.go; internal/credentials/oauth_custom_provider.go; internal/credentials/oauth_slot_alias.go; internal/credentials/oauth_user_creds.go; internal/credentials/store.go
M internal/crypto/crypto.go; internal/crypto/crypto_test.go
M internal/database/database.go
M internal/email/attachment.go; internal/email/download.go
M internal/extensions/auth/transport.go
M internal/folder/store.go
M internal/imap/client.go; internal/imap/idle.go; internal/imap/pool.go
M internal/ipc/client.go; internal/ipc/server.go; internal/ipc/server_unix.go; internal/ipc/token.go
M internal/keyring/keyring.go
M internal/oauth2/flow.go; internal/oauth2/server.go; internal/oauth2/server_test.go
M internal/pgp/store.go
M internal/smime/store.go; internal/smime/verifier.go
M internal/smtp/client.go; internal/smtp/client_test.go
M internal/sync/charset.go; internal/sync/header_recovery.go; internal/sync/helpers.go; internal/sync/messages.go; internal/sync/messages_integrity_test.go; internal/sync/parse.go; internal/sync/scheduler.go; internal/sync/search.go
M wails.json
?? app/round3_account_scope_test.go; app/round3_preflight_linux_test.go; app/round3_test.go
?? docs/audit-2026-09-07/
?? extensions/calendar/backend/round3_test.go
?? frontend/src/lib/utils/emailLinks.ts; frontend/tests/
?? internal/carddav/round3_test.go; internal/contact/round3_test.go
?? internal/credentials/audit_test.go; internal/credentials/migration.go; internal/credentials/migration_test.go; internal/credentials/round3_test.go
?? internal/crypto/audit_test.go; internal/crypto/keyfile_unix.go; internal/crypto/keyfile_windows.go; internal/crypto/lock.go; internal/crypto/lock_unix.go; internal/crypto/lock_windows.go; internal/crypto/migration_test.go
?? internal/database/audit_test.go
?? internal/email/download_test.go; internal/email/limits.go; internal/email/round3_test.go
?? internal/extensions/auth/round3_test.go
?? internal/folder/audit_test.go
?? internal/imap/audit_test.go
?? internal/ipc/audit_test.go; internal/ipc/audit_unix_test.go; internal/ipc/reader.go
?? internal/keyring/audit_test.go
?? internal/oauth2/flow_test.go; internal/oauth2/token_clear.go; internal/oauth2/token_clear_test.go
?? internal/pgp/round3_test.go
?? internal/smime/round3_test.go; internal/smime/verifier_test.go
?? internal/sync/audit_test.go
```

## Classification

RELEASE READY WITH NON-BLOCKING LIMITATIONS
