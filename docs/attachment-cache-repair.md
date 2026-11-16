# Attachment cache repair

This repairs the local mailbox cache, not the original messages on the server.
It is not available on a remote-only desktop or through the helper's network API.

When the local app opens a message whose attachment metadata repeats, it tries
one repair per message/account per app session. It fetches the original IMAP
message with BODY.PEEK, requires matching known UIDVALIDITY, the exact UID and RFC822.SIZE and successful
FETCH completion, checks the original Message-ID, and strictly parses the whole
MIME tree. A failure leaves all old cache rows in place. Signed, encrypted, TNEF,
nested-message, missing-type, non-UTF8 text attachments, attached multipart entities and damaged MIME are currently left untouched.

A successful repair keeps every real attachment occurrence, even when filenames,
sizes or payloads match. It saves the old rows, inline bytes and download paths
in the same database transaction before installing the complete replacement.
Exact decoded bytes are retained for each repaired part so downloading two files
with the same name cannot accidentally choose the first file twice. Cache paths
are reused only when a bounded read under the app's attachment cache matches the
actual payload. No existing file is deleted. Later body refetches and force-sync
attachment cleanup cannot overwrite the repaired set. Automatic moved-row collision merges are refused when either row has a repair, preserving both messages and undo history. These are terminal move conflicts, not signals for fallback sync. Fallback temporary-row cleanup also refuses to delete repaired rows, including moves without COPYUID. A source-folder/UID/Message-ID change during fetching also aborts the replacement.

This stores decoded attachment payloads and undo snapshots in the local database,
so the database can grow. A repair snapshot is limited to 50 MiB and 10,000 MIME
parts. Existing backups remain until their parent message is deleted. The normal
mailbox database protections apply; this is not an external backup of the whole
mailbox. Back up the full data directory before testing a prerelease on real mail.

## Inspect or undo a repair

Quit the mailbox-owning app first. From this repository's root, use the local
maintenance command. Replace the path and IDs with those of the affected message;
do not guess which database belongs to the account.

```sh
go run ./cmd/postilaatikko-cache --db '/exact/path/to/aerion.db' --account ACCOUNT_ID --message MESSAGE_ID
```

It prints the message and current `Backup` ID. No attachment contents are printed.
To restore that backup:

```sh
go run ./cmd/postilaatikko-cache --db '/exact/path/to/aerion.db' --account ACCOUNT_ID --message MESSAGE_ID --restore BACKUP_ID
```

The command refuses a nonexistent database, account mismatch or stale backup ID.
It first saves the current rows and exact payloads as a new undo generation, then
restores the previous rows. The new `Backup` ID can undo the restore. Cache files
are never removed. Background insertion remains blocked for a restored message,
so opening it does not immediately recreate another set of duplicate rows.

The maintenance command is a source-build tool, not yet a bundled DMG executable.
Native macOS, real IMAP and two-machine Studio operation have not been verified.
