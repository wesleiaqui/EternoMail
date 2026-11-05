<script lang="ts">
  import Icon from '@iconify/svelte'
  import { replyLaterQueue } from '$lib/stores/replyLater.svelte'
  import { readLaterQueue } from '$lib/stores/readLater.svelte'
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
    ResolveMailActionTarget,
    UndoOperation,
    SnoozeConversation,
    UnsnoozeConversation,
  } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { account, operations } from '../../../../wailsjs/go/models'
  import { toasts } from '$lib/stores/toast'
  import { ConfirmDialog } from '$lib/components/ui/confirm-dialog'
  import FolderPickerDialog from './FolderPickerDialog.svelte'
  import type { Snippet } from 'svelte'
  import { _ } from '$lib/i18n'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  import { runMessageMutation } from '$lib/components/list/mutationInFlight'
  import ShortcutHint from '$lib/components/shortcuts/ShortcutHint.svelte'
  import { publishUndoOperationCompleted, publishUndoOperationCreated } from '$lib/components/list/undoMutationEvents'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'

  interface Props {
    conversationId?: string
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
    conversationId,
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
  let showSnoozeDialog = $state(false)
  let snoozeDays = $state('1')

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

  async function resolveActionMessages(): Promise<string[]> {
    if (!messageIds.length) return []
    if (!accountId) throw new Error('Cannot validate a message action without its account')
    const messages = messageIds.map((messageId) => ({ accountId, messageId }))
    return ResolveMailActionTarget(operations.Target.createFrom({
      kind: messages.length === 1 ? 'message' : 'matched_messages',
      messages,
    }))
  }

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
      void replyLaterQueue.load().catch(() => {})
      void readLaterQueue.load().catch(() => {})
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

  async function handleReplyLater() {
    if (!conversationId) return
    const inQueue = !replyLaterQueue.has(accountId, conversationId)
    try {
      const result = await replyLaterQueue.set(accountId, conversationId, inQueue)
      if (!result?.changed) return
      toasts.success($_(inQueue ? 'replyLater.added' : 'replyLater.removed'), [{
        label: $_('common.undo'), onClick: () => { void UndoOperation(result.operationId).catch(() => toasts.error($_('toast.undoFailed'))) },
      }])
    } catch (err) { toasts.error(`${$_('replyLater.failed')}: ${String(err)}`) }
  }

  async function handleReadLater() {
    if (!conversationId) return
    const inQueue = !readLaterQueue.has(accountId, conversationId)
    try {
      const changed = await readLaterQueue.set(accountId, conversationId, inQueue)
      if (changed) toasts.success($_(inQueue ? 'readLater.added' : 'readLater.removed'))
    } catch (err) { toasts.error(`${$_('readLater.failed')}: ${String(err)}`) }
  }

  async function confirmSnooze() {
    if (!conversationId) return
    const days = Number(snoozeDays)
    if (!Number.isFinite(days) || days < 1 || days > 3650) return
    const wakeAt = new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString()
    try {
      await SnoozeConversation(accountId, conversationId, wakeAt)
      showSnoozeDialog = false
      toasts.success($_('snooze.done'))
      onActionComplete?.(true)
    } catch (err) { toasts.error(`${$_('snooze.failed')}: ${String(err)}`) }
  }

  async function handleUnsnooze() {
    if (!conversationId) return
    try {
      await UnsnoozeConversation(accountId, conversationId)
      toasts.success($_('snooze.restored'))
      onActionComplete?.(true)
    } catch (err) { toasts.error(`${$_('snooze.failed')}: ${String(err)}`) }
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
      const targetIds = await resolveActionMessages()
      await runMessageMutation(targetIds, async () => {
        const result = await MoveToInboxWithUndo(targetIds)
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
      const targetIds = await resolveActionMessages()
      await runMessageMutation(targetIds, async () => {
        const result = await ArchiveWithUndo(targetIds)
        if (result.coalesced) return
        showTargetedUndoToast($_('toast.archived'), result.operationId, 'archive')
        onActionComplete?.(true)
      })
    } catch (err) {
      console.error('Archive failed:', err)
      toasts.error($_('toast.failedToArchive'))
    }
  }

  async function handleArchiveAndMarkRead() {
    let archived = false
    try {
      const targetIds = await resolveActionMessages()
      await runMessageMutation(targetIds, async () => {
        const result = await ArchiveWithUndo(targetIds)
        if (result.coalesced) return
        archived = true

        try {
          await MarkAsRead(targetIds)
        } catch (err) {
          showTargetedUndoToast($_('toast.archived'), result.operationId, 'archive')
          onActionComplete?.(true)
          throw err
        }

        showTargetedUndoToast($_('toast.archived'), result.operationId, 'archive')
        onActionComplete?.(true)
      })
    } catch (err) {
      console.error('Archive and mark read failed:', err)
      toasts.error($_(archived ? 'toast.failedToUpdateReadStatus' : 'toast.failedToArchive'))
    }
  }

  async function handleDelete() {
    if (isTrashFolder) {
      showDeleteConfirm = true
    } else {
      try {
        const targetIds = await resolveActionMessages()
        await runMessageMutation(targetIds, async () => {
          const result = await TrashWithUndo(targetIds)
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
      const targetIds = await resolveActionMessages()
      await runMessageMutation(targetIds, async () => {
        await DeletePermanently(targetIds)
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
      const targetIds = await resolveActionMessages()
      await runMessageMutation(targetIds, async () => {
        if (isSpamFolder) {
          // If we're in spam folder, mark as NOT spam
          const result = await MarkAsNotSpamWithUndo(targetIds)
          if (result.coalesced) return
          showTargetedUndoToast($_('toast.markedAsNotSpam'), result.operationId, 'mark-as-not-spam')
          onActionComplete?.(true)
          return
        }
        // Otherwise, mark as spam
        const result = await MarkAsSpamWithUndo(targetIds)
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
      const targetIds = await resolveActionMessages()
      await runMessageMutation(targetIds, async () => {
        if (isStarred) {
          await Unstar(targetIds)
          toasts.success($_('toast.starRemoved'))
        } else {
          await Star(targetIds)
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
      const targetIds = await resolveActionMessages()
      await runMessageMutation(targetIds, async () => {
        if (isRead) {
          await MarkAsUnread(targetIds)
          toasts.success($_('toast.markedAsUnread'))
        } else {
          await MarkAsRead(targetIds)
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
      const targetIds = await resolveActionMessages()
      await runMessageMutation(targetIds, async () => {
        const result = await MoveToFolderWithUndo(targetIds, destFolderId)
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
      const targetIds = await resolveActionMessages()
      await runMessageMutation(targetIds, async () => {
        await CopyToFolder(targetIds, destFolderId)
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
        <ShortcutHint id="mail.reply" />
      </ContextMenuItem>
      <ContextMenuItem onSelect={handleReplyAll}>
        <Icon icon="mdi:reply-all" class="mr-2 h-4 w-4" />
        {$_('contextMenu.replyAll')}
        <ShortcutHint id="mail.replyAll" />
      </ContextMenuItem>
      <ContextMenuItem onSelect={handleForward}>
        <Icon icon="mdi:share" class="mr-2 h-4 w-4" />
        {$_('contextMenu.forward')}
        <ShortcutHint id="mail.forward" />
      </ContextMenuItem>
      <ContextMenuSeparator />
    {/if}

    {#if conversationId}
      {#if folderType === 'snoozed'}
        <ContextMenuItem onSelect={handleUnsnooze}>
          <Icon icon="mdi:clock-check-outline" class="mr-2 h-4 w-4" />
          {$_('snooze.unsnooze')}
        </ContextMenuItem>
      {:else}
      <ContextMenuItem onSelect={handleReadLater}>
        <Icon icon="mdi:book-clock-outline" class="mr-2 h-4 w-4" />
        {$_(readLaterQueue.has(accountId, conversationId) ? 'readLater.remove' : 'readLater.add')}
      </ContextMenuItem>
      <ContextMenuItem onSelect={handleReplyLater}>
        <Icon icon="mdi:clock-outline" class="mr-2 h-4 w-4" />
        {$_(replyLaterQueue.has(accountId, conversationId) ? 'replyLater.remove' : 'replyLater.add')}
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => { snoozeDays = '1'; showSnoozeDialog = true }}>
        <Icon icon="mdi:clock-time-four-outline" class="mr-2 h-4 w-4" />
        {$_('snooze.action')}
      </ContextMenuItem>
      {/if}
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
      <ShortcutHint id="mail.archive" />
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleArchiveAndMarkRead}>
      <Icon icon="mdi:archive-check-outline" class="mr-2 h-4 w-4" />
      {$_('contextMenu.archive')} + {$_('contextMenu.markAsRead')}
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleDelete}>
      <Icon icon={isTrashFolder ? 'mdi:delete-forever' : 'mdi:delete-outline'} class="mr-2 h-4 w-4" />
      {$_(isTrashFolder ? 'contextMenu.deletePermanently' : 'contextMenu.delete')}
      <ShortcutHint id={isTrashFolder ? 'mail.deletePermanently' : 'mail.trash'} />
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleSpam}>
      <Icon icon={isSpamFolder ? 'mdi:email-check-outline' : 'mdi:alert-octagon-outline'} class="mr-2 h-4 w-4" />
      {$_(isSpamFolder ? 'contextMenu.markAsNotSpam' : 'contextMenu.markAsSpam')}
      {#if !isSpamFolder}<ShortcutHint id="mail.spam" />{/if}
    </ContextMenuItem>

    <ContextMenuSeparator />

    <!-- Move to folder picker -->
    <ContextMenuItem onSelect={openMoveTo}>
      <Icon icon="mdi:folder-move-outline" class="mr-2 h-4 w-4" />
      {$_('contextMenu.moveTo')}
      <ShortcutHint id="mail.moveTo" />
    </ContextMenuItem>

    <!-- Copy to folder picker -->
    <ContextMenuItem onSelect={openCopyTo}>
      <Icon icon="mdi:content-copy" class="mr-2 h-4 w-4" />
      {$_('contextMenu.copyTo')}
      <ShortcutHint id="mail.copyTo" />
    </ContextMenuItem>

    <ContextMenuSeparator />

    <!-- Flag actions -->
    <ContextMenuItem onSelect={handleToggleStar}>
      <Icon
        icon={isStarred ? 'mdi:star' : 'mdi:star-outline'}
        class="mr-2 h-4 w-4 {isStarred ? 'text-yellow-500' : ''}"
      />
      {$_(isStarred ? 'contextMenu.removeStar' : 'contextMenu.star')}
      <ShortcutHint id="mail.star" />
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleToggleRead}>
      <Icon
        icon={isRead ? 'mdi:email-outline' : 'mdi:email-open-outline'}
        class="mr-2 h-4 w-4"
      />
      {$_(isRead ? 'contextMenu.markAsUnread' : 'contextMenu.markAsRead')}
      <ShortcutHint id={isRead ? 'mail.markUnread' : 'mail.markRead'} />
    </ContextMenuItem>
  </ContextMenuContent>
</ContextMenuPrimitive.Root>

<Dialog.Root bind:open={showSnoozeDialog}>
  <Dialog.Content class="max-w-sm">
    <Dialog.Header>
      <Dialog.Title>{$_('snooze.title')}</Dialog.Title>
      <Dialog.Description>{$_('snooze.description')}</Dialog.Description>
    </Dialog.Header>
    <label class="grid gap-2 text-sm" for="snooze-days">
      {$_('snooze.days')}
      <input id="snooze-days" type="number" min="1" max="3650" step="1" bind:value={snoozeDays} class="rounded-md border border-input bg-background px-3 py-2" />
    </label>
    <Dialog.Footer>
      <Button variant="outline" onclick={() => showSnoozeDialog = false}>{$_('common.cancel')}</Button>
      <Button onclick={confirmSnooze}>{$_('snooze.confirm')}</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

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
