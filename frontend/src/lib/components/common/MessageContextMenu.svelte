<script lang="ts">
  import Icon from '@iconify/svelte'
  import { ContextMenu as ContextMenuPrimitive } from 'bits-ui'
  import {
    ContextMenuContent,
    ContextMenuItem,
    ContextMenuSeparator,
  } from '$lib/components/ui/context-menu'
  import {
    GetAccounts,
    MarkAsRead,
    MarkAsUnread,
    Star,
    Unstar,
    MoveToInboxWithUndo,
    ArchiveWithUndo,
    TrashWithUndo,
    MarkAsSpamWithUndo,
    MarkAsNotSpamWithUndo,
    DeletePermanently,
    MoveToFolderWithUndo,
    CopyToFolder,
    UndoOperation,
  } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { account } from '../../../../wailsjs/go/models'
  import { toasts } from '$lib/stores/toast'
  import { ConfirmDialog } from '$lib/components/ui/confirm-dialog'
  import FolderPickerDialog from './FolderPickerDialog.svelte'
  import type { Snippet } from 'svelte'
  import { _ } from '$lib/i18n'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  import { runMessageMutation } from '$lib/components/list/mutationInFlight'
  import { publishUndoOperationCompleted, publishUndoOperationCreated } from '$lib/components/list/undoMutationEvents'

  interface Props {
    messageIds: string[]
    accountId: string
    currentFolderId: string
    folderType: string
    isStarred: boolean
    isRead: boolean
    onActionComplete?: (autoSelectNext?: boolean) => void
    onReply?: (mode: 'reply' | 'reply-all' | 'forward', messageId: string) => void
    onOpenChange?: (open: boolean) => void
    children?: Snippet
  }

  let {
    messageIds,
    accountId,
    currentFolderId,
    folderType,
    isStarred,
    isRead,
    onActionComplete,
    onReply,
    onOpenChange,
    children,
  }: Props = $props()

  // Accounts list — passed to FolderPickerDialog for its account dropdown.
  // Loaded lazily when the context menu opens.
  let accounts = $state<account.Account[]>([])
  let accountsLoaded = $state(false)
  let accountsLoading = $state(false)

  // Permanent delete confirmation
  let showDeleteConfirm = $state(false)

  // Folder picker dialog state
  let showFolderPicker = $state(false)
  let folderPickerMode = $state<'move' | 'copy'>('move')

  // Track dialog open/close to prevent background reloads from dismissing dialogs
  $effect(() => {
    if (showFolderPicker) {
      dialogGuardOpen()
      return () => dialogGuardClose()
    }
  })

  $effect(() => {
    if (showDeleteConfirm) {
      dialogGuardOpen()
      return () => dialogGuardClose()
    }
  })

  // Computed values
  const isTrashFolder = $derived(folderType === 'trash')
  const isSpamFolder = $derived(folderType === 'spam')
  const isSingleMessage = $derived(messageIds.length === 1)
  const undoInFlight = new Set<string>()

  // Load accounts when context menu opens (used by FolderPickerDialog's dropdown)
  async function loadAccounts() {
    if (accountsLoaded || accountsLoading) return

    accountsLoading = true
    try {
      const result = await GetAccounts()
      accounts = result || []
      accountsLoaded = true
    } catch (err) {
      console.error('Failed to load accounts:', err)
    } finally {
      accountsLoading = false
    }
  }

  // Handle menu open
  function handleOpenChange(open: boolean) {
    if (open) {
      loadAccounts()
    }
    onOpenChange?.(open)
  }

  function showTargetedUndoToast(message: string, operationID: string, action: string, undoable = true) {
    const canUndo = undoable && typeof operationID === 'string' && operationID.length > 0
    if (canUndo) publishUndoOperationCreated(operationID, messageIds, action)
    let toastId = ''
    const actions = canUndo
      ? [{ label: $_('common.undo'), onClick: () => { void handleTargetedUndo(toastId, operationID) } }]
      : []
    toastId = toasts.success(message, actions)
  }

  async function handleTargetedUndo(toastId: string, operationID: string) {
    if (!operationID || undoInFlight.has(operationID)) return
    undoInFlight.add(operationID)
    toasts.replace(toastId, { message: 'Undoing...', actions: [], duration: 20_000 })
    try {
      const description = await UndoOperation(operationID)
      publishUndoOperationCompleted(operationID)
      toasts.replace(toastId, {
        message: $_('toast.undone', { values: { description } }),
        type: 'success',
        actions: [],
        duration: 4000,
      })
    } catch (err) {
      console.error('Undo failed:', err)
      toasts.replace(toastId, {
        message: $_('toast.undoFailed'),
        type: 'error',
        actions: [],
        duration: 6000,
      })
    } finally {
      undoInFlight.delete(operationID)
    }
  }

  // Action handlers
  async function handleReply() {
    if (isSingleMessage && onReply) {
      onReply('reply', messageIds[0])
    }
  }

  async function handleReplyAll() {
    if (isSingleMessage && onReply) {
      onReply('reply-all', messageIds[0])
    }
  }

  async function handleForward() {
    if (isSingleMessage && onReply) {
      onReply('forward', messageIds[0])
    }
  }

  async function handleMoveToInbox() {
    try {
      await runMessageMutation(messageIds, async () => {
        const result = await MoveToInboxWithUndo(messageIds)
        if (result.coalesced) return
        showTargetedUndoToast($_('toast.movedTo', { values: { folder: $_('sidebar.inbox') } }), result.operationId, 'move-to-inbox')
        onActionComplete?.(true)
      })
    } catch (err) {
      console.error('Move to inbox failed:', err)
      toasts.error($_('toast.failedToMove'))
    }
  }

  async function handleArchive() {
    try {
      await runMessageMutation(messageIds, async () => {
        const result = await ArchiveWithUndo(messageIds)
        if (result.coalesced) return
        showTargetedUndoToast($_('toast.archived'), result.operationId, 'archive')
        onActionComplete?.(true)
      })
    } catch (err) {
      console.error('Archive failed:', err)
      toasts.error($_('toast.failedToArchive'))
    }
  }

  async function handleDelete() {
    if (isTrashFolder) {
      showDeleteConfirm = true
    } else {
      try {
        await runMessageMutation(messageIds, async () => {
          const result = await TrashWithUndo(messageIds)
          if (result.coalesced) return
          const toastMsg = result.movedToTrash ? $_('toast.movedToTrash') : $_('toast.deletedFromFolder')
          showTargetedUndoToast(toastMsg, result.operationId, 'trash', result.movedToTrash)
          onActionComplete?.(true)
        })
      } catch (err) {
        console.error('Delete failed:', err)
        toasts.error($_('toast.failedToDelete'))
      }
    }
  }

  async function handleConfirmPermanentDelete() {
    try {
      await runMessageMutation(messageIds, async () => {
        await DeletePermanently(messageIds)
        toasts.success($_('toast.permanentlyDeleted'))
        showDeleteConfirm = false
        onActionComplete?.(true)
      })
    } catch (err) {
      console.error('Permanent delete failed:', err)
      toasts.error($_('toast.failedToDelete'))
      showDeleteConfirm = false
    }
  }

  async function handleSpam() {
    try {
      await runMessageMutation(messageIds, async () => {
        if (isSpamFolder) {
          // If we're in spam folder, mark as NOT spam
          const result = await MarkAsNotSpamWithUndo(messageIds)
          if (result.coalesced) return
          showTargetedUndoToast($_('toast.markedAsNotSpam'), result.operationId, 'mark-as-not-spam')
          onActionComplete?.(true)
          return
        }
        // Otherwise, mark as spam
        const result = await MarkAsSpamWithUndo(messageIds)
        if (result.coalesced) return
        const toastMsg = result.movedToSpam ? $_('toast.markedAsSpam') : $_('toast.deletedFromFolder')
        showTargetedUndoToast(toastMsg, result.operationId, 'mark-as-spam', result.movedToSpam)
        onActionComplete?.(true)
      })
    } catch (err) {
      console.error('Spam toggle failed:', err)
      toasts.error($_(isSpamFolder ? 'toast.failedToMarkAsNotSpam' : 'toast.failedToMarkAsSpam'))
    }
  }

  async function handleToggleStar() {
    try {
      await runMessageMutation(messageIds, async () => {
        if (isStarred) {
          await Unstar(messageIds)
          toasts.success($_('toast.starRemoved'))
        } else {
          await Star(messageIds)
          toasts.success($_('toast.starred'))
        }
        onActionComplete?.()
      })
    } catch (err) {
      console.error('Star toggle failed:', err)
      toasts.error($_('toast.failedToUpdateStar'))
    }
  }

  async function handleToggleRead() {
    try {
      await runMessageMutation(messageIds, async () => {
        if (isRead) {
          await MarkAsUnread(messageIds)
          toasts.success($_('toast.markedAsUnread'))
        } else {
          await MarkAsRead(messageIds)
          toasts.success($_('toast.markedAsRead'))
        }
        onActionComplete?.()
      })
    } catch (err) {
      console.error('Read status toggle failed:', err)
      toasts.error($_('toast.failedToUpdateReadStatus'))
    }
  }

  function openMoveTo() {
    folderPickerMode = 'move'
    showFolderPicker = true
  }

  function openCopyTo() {
    folderPickerMode = 'copy'
    showFolderPicker = true
  }

  // Exported for keyboard access (Alt+M / Alt+C): toggles the picker without
  // opening the context menu. Same-mode press closes; other-mode press
  // switches the dialog in place (title is reactive to folderPickerMode).
  export function isFolderPickerOpen(): boolean {
    return showFolderPicker
  }

  export function toggleFolderPicker(mode: 'move' | 'copy') {
    if (showFolderPicker && folderPickerMode === mode) {
      showFolderPicker = false
      return
    }
    if (showFolderPicker) {
      folderPickerMode = mode
      return
    }
    loadAccounts() // normally triggered by menu open; needed when bypassing the menu
    folderPickerMode = mode
    showFolderPicker = true
  }

  function handleFolderSelected(folderId: string, folderName: string, _destAccountId: string) {
    showFolderPicker = false
    switch (folderPickerMode) {
      case 'move':
        handleMoveTo(folderId, folderName)
        break
      case 'copy':
        handleCopyTo(folderId, folderName)
        break
    }
  }

  async function handleMoveTo(destFolderId: string, folderName: string) {
    try {
      await runMessageMutation(messageIds, async () => {
        const result = await MoveToFolderWithUndo(messageIds, destFolderId)
        if (result.coalesced) return
        showTargetedUndoToast($_('toast.movedTo', { values: { folder: folderName } }), result.operationId, 'move-to-folder')
        onActionComplete?.(true)
      })
    } catch (err) {
      console.error('Move failed:', err)
      toasts.error($_('toast.failedToMove'))
    }
  }

  async function handleCopyTo(destFolderId: string, folderName: string) {
    try {
      await runMessageMutation(messageIds, async () => {
        await CopyToFolder(messageIds, destFolderId)
        toasts.success($_('toast.copyingTo', { values: { folder: folderName } }))
        // CopyToFolder syncs in background; sidebar count + dest folder list refresh ride on folder:synced.
      })
    } catch (err) {
      console.error('Copy failed:', err)
      toasts.error($_('toast.failedToCopy'))
    }
  }
</script>

<ContextMenuPrimitive.Root onOpenChange={handleOpenChange}>
  <ContextMenuPrimitive.Trigger>
    {#if children}
      {@render children()}
    {/if}
  </ContextMenuPrimitive.Trigger>

  <ContextMenuContent>
    <!-- Reply actions (single message only) -->
    {#if isSingleMessage}
      <ContextMenuItem onSelect={handleReply}>
        <Icon icon="mdi:reply" class="mr-2 h-4 w-4" />
        {$_('contextMenu.reply')}
      </ContextMenuItem>
      <ContextMenuItem onSelect={handleReplyAll}>
        <Icon icon="mdi:reply-all" class="mr-2 h-4 w-4" />
        {$_('contextMenu.replyAll')}
      </ContextMenuItem>
      <ContextMenuItem onSelect={handleForward}>
        <Icon icon="mdi:share" class="mr-2 h-4 w-4" />
        {$_('contextMenu.forward')}
      </ContextMenuItem>
      <ContextMenuSeparator />
    {/if}

    <!-- Move/Delete actions -->
    {#if isTrashFolder}
      <ContextMenuItem onSelect={handleMoveToInbox}>
        <Icon icon="mdi:inbox-arrow-down-outline" class="mr-2 h-4 w-4" />
        {$_('viewer.moveToInbox')}
      </ContextMenuItem>
    {/if}
    <ContextMenuItem onSelect={handleArchive}>
      <Icon icon="mdi:archive-outline" class="mr-2 h-4 w-4" />
      {$_('contextMenu.archive')}
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleDelete}>
      <Icon icon={isTrashFolder ? 'mdi:delete-forever' : 'mdi:delete-outline'} class="mr-2 h-4 w-4" />
      {$_(isTrashFolder ? 'contextMenu.deletePermanently' : 'contextMenu.delete')}
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleSpam}>
      <Icon icon={isSpamFolder ? 'mdi:email-check-outline' : 'mdi:alert-octagon-outline'} class="mr-2 h-4 w-4" />
      {$_(isSpamFolder ? 'contextMenu.markAsNotSpam' : 'contextMenu.markAsSpam')}
    </ContextMenuItem>

    <ContextMenuSeparator />

    <!-- Move to folder picker -->
    <ContextMenuItem onSelect={openMoveTo}>
      <Icon icon="mdi:folder-move-outline" class="mr-2 h-4 w-4" />
      {$_('contextMenu.moveTo')}
    </ContextMenuItem>

    <!-- Copy to folder picker -->
    <ContextMenuItem onSelect={openCopyTo}>
      <Icon icon="mdi:content-copy" class="mr-2 h-4 w-4" />
      {$_('contextMenu.copyTo')}
    </ContextMenuItem>

    <ContextMenuSeparator />

    <!-- Flag actions -->
    <ContextMenuItem onSelect={handleToggleStar}>
      <Icon
        icon={isStarred ? 'mdi:star' : 'mdi:star-outline'}
        class="mr-2 h-4 w-4 {isStarred ? 'text-yellow-500' : ''}"
      />
      {$_(isStarred ? 'contextMenu.removeStar' : 'contextMenu.star')}
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleToggleRead}>
      <Icon
        icon={isRead ? 'mdi:email-outline' : 'mdi:email-open-outline'}
        class="mr-2 h-4 w-4"
      />
      {$_(isRead ? 'contextMenu.markAsUnread' : 'contextMenu.markAsRead')}
    </ContextMenuItem>
  </ContextMenuContent>
</ContextMenuPrimitive.Root>

<!-- Permanent Delete Confirmation Dialog -->
<ConfirmDialog
  bind:open={showDeleteConfirm}
  title={$_('dialog.deletePermanently')}
  description={$_('dialog.deleteDescription')}
  confirmLabel={$_('dialog.confirmDeletePermanently')}
  variant="destructive"
  onConfirm={handleConfirmPermanentDelete}
  onCancel={() => (showDeleteConfirm = false)}
/>

<!-- Folder Picker Dialog -->
<FolderPickerDialog
  bind:open={showFolderPicker}
  title={$_(folderPickerMode === 'move' ? 'contextMenu.moveTo' : 'contextMenu.copyTo')}
  initialAccountId={accountId}
  {accounts}
  excludeFolderId={currentFolderId}
  onSelect={handleFolderSelected}
/>
