# CPU and memory report — 2026-11-02

## Source findings

The app refreshed `pendingNow` every 250 ms for its pending-send countdown,
including when the pending-send list was empty. Since this state lives in the
root app, the timer woke the UI four times per second during otherwise idle
use.

Automatic body prefetch also accepted any message size. It could queue up to 50
messages from a visible page (with a 128-message queue per folder) and fetched
messages up to the existing 50 MiB raw-message limit. Two prefetch workers can
run concurrently. This is bounded by count and concurrency, but not by message
size; parsing large MIME messages can allocate substantially more than their
wire size.

## Changes

The pending-send clock now runs only while the pending-send list is non-empty,
and updates once per second. Automatic body prefetch skips messages reported
larger than 5 MiB. Such messages remain available through the existing
on-demand body fetch path. Unknown or non-positive sizes are still prefetched.

## Verification and limits

Frontend ESLint passed and `git diff --check` passed. Svelte diagnostics on the
`main` base report 12 existing errors and two existing accessibility warnings,
including diagnostics addressed separately in PR #95. A Go build could not
start because the sandbox could not create the Go module cache outside the
writable worktree. No tests were run.

Live macOS CPU and RSS measurements are unavailable in this environment:
process inspection is denied. These changes bound known idle work and the size
of automatic body-prefetch inputs, but do not establish the cause or measured
reduction behind the reported 150% CPU and 1.2 GiB RSS. A before/after profile
with the user's Gmail mailbox is still needed.

## Collect a profile on macOS

Build/run the app with `ETERNOMAIL_PPROF=1`. It prints a loopback-only,
ephemeral pprof URL on startup. While reproducing the high-CPU state, capture a
30-second Go CPU profile and a heap snapshot:

```sh
curl -o /tmp/eternomail-cpu.pprof 'http://127.0.0.1:PORT/debug/pprof/profile?seconds=30'
curl -o /tmp/eternomail-heap.pprof 'http://127.0.0.1:PORT/debug/pprof/heap'
go tool pprof -top ./EternoMail /tmp/eternomail-cpu.pprof
go tool pprof -top -inuse_space ./EternoMail /tmp/eternomail-heap.pprof
```

Replace `PORT` with the logged port and `./EternoMail` with the exact executable
used to launch the app. Profiles may contain message content or account data;
keep them local and review before sharing. The server is disabled by default
and binds only to `127.0.0.1` when enabled.

In another terminal, run `scripts/profile-macos-processes.sh` to sample the app
and WebKit process CPU/RSS once per second. This distinguishes growth in the Go
heap from WebKit or other native process memory. Stop it with Ctrl-C after
capturing idle baseline, reproducing the spike, and allowing the app to settle.
