# Sentinel Security Journal

## 2026-10-31 - URL Scheme Validation and Fallback Bypasses
**Vulnerability:** External URL opening in `App.OpenURL` relied on naive string prefix matching (`allowedPrefixes = []string{"http://", "https://", "mailto:"}`), while frontend `EmailBody.svelte` caught failures from `OpenURL` and fell back to unvalidated `BrowserOpenURL(url)`.
**Learning:** Naive prefix checks can fail on case sensitivity (`HTTP://`), malformed URI structures, or scheme confusion, while UI fallback handlers can inadvertently bypass backend security controls when errors occur.
**Prevention:** Always parse URLs with standard libraries (`net/url.Parse`), validate scheme and host strictly, and never provide permissive client-side fallbacks that bypass centralized backend security policies.

## 2026-11-01 - Frontend URL Validation Parity and CLI Argument Injection Guard
**Vulnerability:** Frontend link validation (`isAllowedEmailURL`) used simple protocol matching (`new URL(val).protocol`), permitting malformed URLs without hostnames, URLs with control characters, and leading dashes in hostnames that could trigger option/flag injection when passed to URL openers.
**Learning:** In desktop applications wrapping OS URL openers (`xdg-open` or `open`), validating protocol alone is insufficient; hostnames must be non-empty and must not start with dash (`-`) characters to prevent CLI argument confusion.
**Prevention:** Enforce defense-in-depth on the frontend matching backend URL security constraints, ensuring non-empty hostnames and disallowing leading dashes and control characters.

## 2026-11-04 - Backend URL Validation Parity and CLI Flag Injection Prevention
**Vulnerability:** Backend URL protocol validation in `app/app.go` (`isAllowedProtocol`) only validated that `parsed.Host != ""` for http/https URLs, allowing URLs with leading dashes in the hostname or mailto target, as well as unstripped control characters (`\r`, `\n`, `\0`), which could lead to CLI argument injection in OS URL handlers (`xdg-open` or `open`).
**Learning:** When defense-in-depth checks are placed on the client UI, corresponding backend validation must mirror those checks to prevent direct or bypass API invocations from injecting CLI flags into system command execution.
**Prevention:** Enforce strict hostname and target validation on the backend by disallowing leading dash (`-`) characters and control characters across all accepted URI schemes.

## 2026-11-04 - Frontend Mailto Validation Parity Against CLI Option Injection
**Vulnerability:** Frontend link validation (`isAllowedEmailURL`) rejected leading dash characters on `http:` and `https:` hostnames but accepted `mailto:` URLs starting with a dash (e.g. `mailto:-flag@example.com` or `mailto:--help`), diverging from backend guards.
**Learning:** Defense-in-depth URL validators across frontend and backend boundaries must keep scheme-specific argument injection guards in sync, including `mailto` target paths.
**Prevention:** Mirror all backend target/flag filters in frontend URL gatekeepers before passing URLs to external system openers or client dispatchers.

## 2026-11-05 - Centralized Path Validation and CLI Option Injection Prevention for File Openers
**Vulnerability:** `OpenAttachment` and `OpenEncryptedAttachment` called `openFile` directly without passing through `validateOpenPath`, while `validateOpenPath` itself did not guard against leading dash (`-`) characters or control characters in file paths passed to system handlers (`xdg-open` or `open`).
**Learning:** File opening functions wrapping OS launchers must enforce directory boundary checks and CLI option injection defenses centrally in the internal dispatcher (`openFile`), rather than relying on individual caller methods.
**Prevention:** Route all file execution/opening through a centralized helper that enforces strict directory containment and rejects paths beginning with dashes or containing control characters.

## 2026-11-06 - Directory Component Dash Validation and OpenFolder CLI Option Defense
**Vulnerability:** In `OpenFolder`, the directory target was resolved via `filepath.Dir(path)` without checking if path components or the directory itself began with a dash (`-`). If a subfolder name started with a dash, `filepath.Dir` could pass an option flag argument to `xdg-open` or `open`.
**Learning:** Path validation must inspect every component of the path relative to the allowed root, not merely the outermost path prefix, because directory resolution functions like `filepath.Dir` can extract internal path segments into argument positions.
**Prevention:** Ensure path validation rejects any segment starting with a dash, and validate resolved directory targets in folder opening helpers prior to invoking OS handlers.
