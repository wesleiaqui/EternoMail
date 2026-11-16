# Message attachment layout

The reader uses one line until the user expands attachments. One to three files appear directly as file chips. Four or more show an attachment count, expansion button and Save All action. Expansion lists every file with its full name, size, Open and Download actions. The expansion state is account/message scoped.

The chip strip scrolls horizontally in a narrow reader rather than wrapping or compressing names to a character. Every chip stays keyboard reachable. Expand also reveals all full filenames without horizontal scrolling. Download/Open labels include filenames, expansion reports its state, and the existing Save All dialog remains unchanged.

This is presentation only. Ordinary attachment entries keep their attachment IDs, including same-name files. Encrypted entries are rendered by original position so same-name metadata does not collide in the list. Their existing filename-based backend action APIs are unchanged. There is no filename-based filtering, database migration, cache repair or production-data write.

Verification: a real browser mounts the production Svelte component with a deterministic backend and checks 0, 1, 3, 4 and 19 files, identical names with distinct part IDs, one-line collapse, expand/collapse, exact-account/part Open and Download calls, Save All, encrypted duplicates/actions, and 360px overflow. It runs explicitly in CI, as well as local frontend type, lint, full regression and production-build checks. Screenshots are browser component captures, not installed macOS captures. Real save dialogs and OS file opening require installed-app acceptance.
