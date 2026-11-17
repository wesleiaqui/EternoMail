# Agent Instructions

Before starting work, read `ROADMAP.md` for product direction and `TASKS.md` for current claims, dependencies, and acceptance criteria.

## Coordination and worktrees

- Use a separate Git worktree and branch for each agent/task. Do not edit the shared primary checkout when other agents or the user may be working in it.
- Start from the latest `main`. Use a task-specific branch such as `agent/P-006-virtual-folder-criteria` and a task-specific worktree outside the primary checkout.
- Claim one **Unclaimed** task in `TASKS.md` before editing. Record the owner, branch, and worktree in the task row; change its status to **In progress**. Do not start a task already marked In progress or Review.
- If the required worktree cannot be created, stop before editing and tell the coordinator what is blocking it. Do not fall back to editing another agent's checkout.
- Keep one task per branch. Avoid overlapping files with other active tasks; coordinate sequencing when overlap is unavoidable.
- Do not reset, overwrite, stage, or commit another agent's changes. Inspect the worktree status before editing and before committing.
- When implementation is ready, run only the checks appropriate to the task and project instructions, commit on the task branch, and update `TASKS.md` with the commit and review status. Do not merge or push another agent's branch; integration belongs to the coordinator.
- Update `ROADMAP.md` only when scope, priority, or a durable product decision changes. Keep `TASKS.md` as the execution/claim ledger.

## Project guidance

- Preserve mail data. A cross-account move must confirm the destination copy before deleting the source.
- Keep routine sync lightweight: fetch headers and flags during sync; fetch full message content when requested by the user.
- Read `CONTRIBUTING.md` for project conventions. Keep changes focused and describe verification and known limitations in the task update.

## Repo facts (verified 2026-11-17)

Checks (mirroring `.github/workflows/ci.yml`):

- Version consistency: `./scripts/version.sh check`
- Backend: Go 1.26. Tests `make test`; lint `golangci-lint run --build-tags=webkit2_41` (CI pins golangci-lint v2.12.2).
- Frontend: Node 24. `npm ci --prefix frontend`, then `npm run lint`, `npm run check`, `npm test` and `npm run build`, each `--prefix frontend`.
- Go tests compile against an embedded `frontend/dist`. CI creates a placeholder `frontend/dist/index.html` for this; if `go test` fails on the embed, create that fixture first.

CI quirks:

- CI is change-scoped by `scripts/ci_scope.py`: documentation-only PRs skip the frontend/backend jobs, but the workflow still runs so required checks conclude.
- UI browser regressions run under Playwright with a Chromium installed into `$RUNNER_TEMP`; they do not use a system browser.

Known flaky tests: none documented as of 2026-11-17. If a test fails on an unrelated diff, check whether main passes it before changing code.

Merge gate: a PR merges only when Devin review shows no open findings on the current head and the required CI checks are green (owner's standing rule). If Devin has not reviewed the current head, wait for it instead of merging on CI alone.

Loop guard: if the same check fails twice with the same error, stop. Report the command, the error and what you tried instead of retrying a third time or patching blind.
