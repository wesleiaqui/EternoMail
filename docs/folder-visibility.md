# Local folder visibility

Right-click a folder and choose **Hide from Postilaatikko**, or open **Settings > Accounts > Edit account > Folders**. The folder list supports search, All/Hidden filtering, checkboxes, bulk Hide/Show and per-folder **Show + refresh**. Selecting a parent does not silently hide descendants: the confirmation offers that separately.

Hiding is local to the mailbox-owning installation. It does not delete cached messages or the server folder, unsubscribe IMAP folders, or alter Gmail label visibility. Existing pins, content-policy overrides, saved-view members and filter rules remain stored. Hidden folders leave navigation and destination pickers. Showing restores them, including their prior pins and policy overrides.

Explicit folder header and flag refresh, scheduler/bulk refresh, and automatic body prefetch skip hidden folders. A request already running can finish before the change settles. Visibility changes wait for active header/flag reconciliation; queued work checks the persisted flag. Already-started body downloads may finish. Opening cached mail or explicitly requesting a message body is not prohibited.

Gmail account-wide history and all-mail search can still include messages carrying a hidden label through other labels. Hiding a label is not a zero-network guarantee.

Saved views retain their folder links. If an account's saved-view scope includes a hidden folder, that account's part of the view serves retained cached conversations with a stale-source warning instead of querying the provider. Other accounts in the view continue normally. Cached pages may be incomplete until the folder is shown and refreshed. Saved provider searches retain up to 200 provider-resolved conversations as successful pages are opened, rather than replaying provider operators as local FTS. A view never opened online has no retained results; incomplete paging retains only the pages opened. Typed queries and temporary filters cannot overwrite the saved membership; a new typed query requires showing the sources first. Gmail replied/unreplied membership remains the last provider decision while paused, rather than being reinterpreted as an IMAP Answered flag; cached read/star/date criteria still apply across the entire Gmail thread, including messages outside the selected labels. Automatic filters whose destination is hidden are skipped dynamically; their Enabled preference is not rewritten, and showing the destination restores routing.

## Storage and rollout

Migration 81 adds `folders.hidden INTEGER NOT NULL DEFAULT 0`. Existing folders remain visible. Legacy saved-query indexes are invalidated because older cache writers interpreted provider operators as local FTS; messages and saved-view definitions remain untouched. Open the saved search online once to rebuild its retained results. Discovery updates do not overwrite the preference. Batches validate that every folder belongs to the stated account before committing atomically. The desktop forwards changes to the owning helper, with mail-write authorization, rather than silently mutating a remote desktop cache.

Update the desktop and helper together. Older versions may refuse a database with a newer schema. Schema migration 81 has explicit owner clearance. The concurrent filter change must use migration 82; the migrator skips later-added lower numbers. This change does not publish a DMG.

## Verification

Automated coverage includes cross-account batch rejection with no partial update, mail/subscription retention through folder rediscovery, header/flag provider exclusion, active-reconcile serialization, scheduler exclusion, cached saved-view paging and preserved membership, dynamic filter pause/resume, API validation and capability denial, and mounted Svelte folder controls. Native macOS, packaged behavior and live-mailbox traffic have not been tested.
