<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte'
  import Icon from '@iconify/svelte'
  // @ts-ignore - wailsjs bindings
  import { GetReaderIdentityEmails, GetConversation, GetReadReceiptResponsePolicy, SendReadReceipt, IgnoreReadReceipt, GetMessageSource, ProcessSMIMEMessage, ProcessPGPMessage, FetchMessageBody } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs bindings
  import { MoveToInboxWithUndo, GetAccounts, MoveToFolderWithUndo, CopyToFolder, MarkAsRead, MarkAsUnread, Star, Unstar, ArchiveWithUndo, RemoveFromInboxWithUndo, TrashWithUndo, MarkAsSpamWithUndo, MarkAsNotSpamWithUndo, DeletePermanently, ResolveMailActionTarget, UndoOperation } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { EventsOn } from '../../../../wailsjs/runtime/runtime'
  // @ts-ignore - wailsjs path
  import { message as messageModels, account, operations } from '../../../../wailsjs/go/models'
  import AttachmentList from './AttachmentList.svelte'
  import ConversationMessage from './ConversationMessage.svelte'
  import InlineReply from './InlineReply.svelte'
  import { reactionLayout } from './reactionLayout'
  import { bubbleLayout, senderAddress } from './bubbleLayout'
  import { isConfiguredAccountEmail } from '$lib/stores/accounts.svelte'
  import ReaderModeToggle from './ReaderModeToggle.svelte'
  import { canExtract } from './replyContent'
  import { openTimelineAtTarget, captureScrollAnchor, holdScrollAnchor } from './timelineScroll'
  import type { ReaderMode } from './readerMode'
  import { getReaderMode, setReaderMode } from '$lib/stores/uiState.svelte'
  import Avatar from '$lib/components/kit/Avatar.svelte'
  import { toasts } from '$lib/stores/toast'
  // @ts-ignore - wailsjs bindings
  import { Calendar_DetectDateTimes, Calendar_CreateICSFromDetails } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { backend as backendModels } from '../../../../wailsjs/go/models'
  import { setFocusedPane, isInputElement, isComposerOpen } from '$lib/stores/keyboard.svelte'
  import { ConfirmDialog } from '$lib/components/ui/confirm-dialog'
  import MessageContextMenu from '$lib/components/common/MessageContextMenu.svelte'
  import FolderPickerDialog from '$lib/components/common/FolderPickerDialog.svelte'
  import { withShortcut, ariaKeyshortcuts } from '$lib/keyboard/format'
  import { _ } from '$lib/i18n'
  import { isDialogGuardActive, onDialogGuardChange, dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  import { getShowViewerCircles, getDarkMailContent, getMarkAsReadDelay } from '$lib/stores/settings.svelte'
  import { getIsDarkActive } from '$lib/stores/theme.svelte'
  import { contactPhotos } from '$lib/stores/contactPhotos.svelte'
  import { PendingReadOnLeave } from './pendingReadOnLeave'
  import { publishUndoOperationCompleted, publishUndoOperationCreated } from '../list/undoMutationEvents'
  import { runMessageMutation } from '../list/mutationInFlight'

  interface Props {
    threadId?: string | null
    folderId?: string | null
    folderType?: string | null
    accountId?: string | null
    onReply?: (mode: 'reply' | 'reply-all' | 'forward', messageId: string, imagesLoaded?: boolean) => void
    onComposeToAddress?: (toAddress: string) => void
    onEditDraft?: (draftId: string) => void
    onActionComplete?: (autoSelectNext?: boolean) => void
    isFocused?: boolean
    isFlashing?: boolean
    showBackButton?: boolean
    onBack?: () => void
    /** True while the mail viewer is actually visible, including responsive layouts. */
    isViewerVisible?: boolean
    /** Scroll position to restore after this conversation has rendered. */
    restoreScrollTop?: number
    /** Called before the viewer is destroyed so its workspace can restore its position. */
    onSaveScrollPosition?: (threadId: string, folderId: string, accountId: string, scrollTop: number) => void
    /** Defer automatic read changes while an unread-only list owns the selection. */
    deferMarkAsReadUntilLeave?: boolean
    // Focus mode (whole thread or single message takes full window)
    inFocusMode?: boolean
    focusModeKind?: 'thread' | 'message' | null
    focusedMessageIdInFocus?: string | null
    onToggleThreadFocus?: () => void
    onToggleMessageFocus?: (messageId: string) => void
  }

  let {
    threadId = null,
    folderId = null,
    folderType = null,
    accountId = null,
    onReply,
    onComposeToAddress,
    onEditDraft,
    onActionComplete,
    isFocused = false,
    isFlashing = false,
    showBackButton = false,
    onBack,
    isViewerVisible = true,
    restoreScrollTop,
    onSaveScrollPosition,
    deferMarkAsReadUntilLeave = false,
    inFocusMode = false,
    focusModeKind = null,
    focusedMessageIdInFocus = null,
    onToggleThreadFocus,
    onToggleMessageFocus,
  }: Props = $props()

  // Track which messages have had their remote images loaded by the user
  const messagesWithImagesLoaded = new Set<string>()
  let moveMutationInFlight = $state(false)

  // Decrypted attachment metadata
  interface DecryptedAttachment {
    filename: string
    contentType: string
    size: number
    isInline: boolean
    contentId: string
  }

  // S/MIME on-view processing result type
  interface SMIMEViewResult {
    bodyHtml: string
    bodyText: string
    smimeStatus: string
    smimeSignerEmail: string
    smimeSignerSubject: string
    smimeEncrypted: boolean
    inlineAttachments?: Record<string, string>
    attachments?: DecryptedAttachment[]
  }

  // PGP on-view processing result type
  interface PGPViewResult {
    bodyHtml: string
    bodyText: string
    pgpStatus: string
    pgpSignerEmail: string
    pgpSignerKeyId: string
    pgpEncrypted: boolean
    inlineAttachments?: Record<string, string>
    attachments?: DecryptedAttachment[]
  }

  // State
  let conversation = $state<messageModels.Conversation | null>(null)
  let inlineReplyTarget = $state<{
    threadId: string
    accountId: string
    messageId: string
    mode: 'reply' | 'reply-all'
    imagesLoaded: boolean
  } | null>(null)
  // Per-message EmailBody refs, used to pull each rendered body for printing.
  let emailBodyRefs = $state<Record<string, { getPrintableHtml(): Promise<string> }>>({})
  let loading = $state(false)
  let error = $state<string | null>(null)

  // Folder-picker state for moving/copying the open conversation from the
  // keyboard (`v` / Alt+M / Alt+C) while the viewer is focused.
  let accounts = $state<account.Account[]>([])
  let accountsLoaded = $state(false)
  let accountsLoading = $state(false)
  let showFolderPicker = $state(false)
  let folderPickerMode = $state<'move' | 'copy'>('move')

  // Keep the folder-picker dialog behind the modal guard so its keys are not
  // swallowed by the global mail handler while it is open.
  $effect(() => {
    if (showFolderPicker) {
      dialogGuardOpen()
      return () => dialogGuardClose()
    }
  })

  async function resolveDisplayedMessageTarget(messageIds: string[]): Promise<string[]> {
    if (!messageIds.length) return []
    const owningAccountId = accountId || conversation?.messages?.[0]?.accountId
    if (!owningAccountId) throw new Error('Cannot validate a message action without its account')
    const displayedIds = new Set(conversation?.messages?.map((item) => item.id) ?? [])
    if (messageIds.some((id) => !displayedIds.has(id))) {
      throw new Error('Message action target is no longer in the displayed conversation')
    }
    return ResolveMailActionTarget(operations.Target.createFrom({
      kind: 'matched_messages',
      messages: messageIds.map((messageId) => ({ accountId: owningAccountId, messageId })),
    }))
  }

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

  // Open the move/copy folder picker for the open conversation. Used by the
  // viewer-focused `v` / Alt+M / Alt+C shortcuts routed from App.svelte.
  export function toggleFolderPicker(mode: 'move' | 'copy') {
    if (showFolderPicker && folderPickerMode === mode) {
      showFolderPicker = false
      return
    }
    if (showFolderPicker) {
      folderPickerMode = mode
      return
    }
    loadAccounts()
    folderPickerMode = mode
    showFolderPicker = true
  }

  async function handleFolderSelected(destFolderId: string, _folderName: string, _destAccountId: string) {
    showFolderPicker = false
    if (!conversation?.messages) return
    let messageIds = conversation.messages.map((m) => m.id)
    try {
      messageIds = await resolveDisplayedMessageTarget(messageIds)
      if (folderPickerMode === 'move') {
        await runMessageMutation(messageIds, async () => {
          moveMutationInFlight = true
          try {
            const result = await MoveToFolderWithUndo(messageIds, destFolderId)
            if (result.coalesced) return
            publishUndoOperationCreated(result.operationId, messageIds, 'move-to-folder')
            showUndoableSuccess($_('toast.movedTo', { values: { folder: _folderName } }), result.operationId)
            onActionComplete?.(true)
          } finally {
            moveMutationInFlight = false
          }
        })
      } else {
        await runMessageMutation(messageIds, async () => {
          await CopyToFolder(messageIds, destFolderId)
          toasts.success($_('toast.copyingTo', { values: { folder: _folderName } }))
        })
      }
    } catch (err) {
      console.error('Move failed:', err)
      toasts.error($_('toast.failedToMove'))
    }
  }

  // S/MIME on-view processing results per message
  let smimeResults = $state<Record<string, SMIMEViewResult>>({})
  let smimeLoading = $state<Set<string>>(new Set())

  // PGP on-view processing results per message
  let pgpResults = $state<Record<string, PGPViewResult>>({})
  let pgpLoading = $state<Set<string>>(new Set())

  // Track which messages are expanded (unread messages auto-expand)
  let expandedMessages = $state<Set<string>>(new Set())

  // Date/time mentions recognized for the "Add to Calendar" handoff, deduped
  // across the conversation's messages and rendered as chips under the subject.
  type CalendarChip = backendModels.DateTimeMention & { formatted: string }
  let calendarMentions = $state<CalendarChip[]>([])

  // Per-message override for the dark-mail-content filter. Runtime-only,
  // resets when the conversation changes. Truthy value = user explicitly
  // disabled the filter for this message.
  let darkMailOverrides = $state<Record<string, boolean>>({})

  // The message list already retrieves local contact photos in batches. Do the
  // same in the reader so a saved/synced profile photo does not disappear as
  // soon as its conversation is opened.
  $effect(() => {
    const messages = conversation?.messages ?? []
    const emails = messages.map(item => item.fromEmail).filter((email): email is string => !!email)
    if (emails.length) void contactPhotos.ensure(emails)
  })

  function shouldDarkenMessage(msgId: string): boolean {
    if (!getDarkMailContent()) return false
    if (!getIsDarkActive()) return false
    return !darkMailOverrides[msgId]
  }

  function toggleDarkMailOverride(msgId: string) {
    darkMailOverrides = { ...darkMailOverrides, [msgId]: !darkMailOverrides[msgId] }
  }

  // Track focused message for keyboard deletion
  let focusedMessageId = $state<string | null>(null)

  // Read receipt policy and tracking
  let readReceiptPolicy = $state<'never' | 'ask' | 'always'>('ask')
  let handledReadReceipts = $state<Set<string>>(new Set()) // Track locally handled receipts
  let sendingReadReceipt = $state<Set<string>>(new Set()) // Track in-flight sends

  // Delete confirmation state
  let showDeleteConfirm = $state(false)

  // Messages that were unread when this conversation was first displayed.
  // They are marked read either immediately, after a delay, or when leaving the viewer.
  const pendingReadOnLeave = new PendingReadOnLeave()
  let markAsReadTimer: ReturnType<typeof setTimeout> | null = null

  function clearMarkAsReadTimer() {
    if (markAsReadTimer) {
      clearTimeout(markAsReadTimer)
      markAsReadTimer = null
    }
  }

  function markCurrentPendingAsRead() {
    clearMarkAsReadTimer()
    const messageIds = pendingReadOnLeave.take()
    if (!messageIds.length) return
    MarkAsRead(messageIds).catch(err => {
      console.error('Failed to mark messages as read:', err)
    })
  }

  function scheduleMarkAsRead() {
    clearMarkAsReadTimer()
    if (deferMarkAsReadUntilLeave) return
    const delay = getMarkAsReadDelay()
    if (delay === 0) {
      markCurrentPendingAsRead()
    } else if (delay > 0) {
      markAsReadTimer = setTimeout(() => {
        markAsReadTimer = null
        markCurrentPendingAsRead()
      }, delay)
    }
    // delay === -2: handled by finalizeReadOnLeave on leave
    // delay === -1: manual only, never auto-mark
  }

  function handleReadOnLeave() {
    clearMarkAsReadTimer()
    const delay = getMarkAsReadDelay()
    if (deferMarkAsReadUntilLeave || delay === -2) {
      finalizeReadOnLeave()
    } else {
      pendingReadOnLeave.clear()
    }
  }

  export function prepareForSuspend() {
    handleReadOnLeave()
  }

  // Debounce timer for refreshConversation (coalesces rapid sync events)
  let refreshTimer: ReturnType<typeof setTimeout> | null = null
  let pendingRefresh: { tid: string; fid: string } | null = null
  let stopDialogGuardListener: (() => void) | null = null

  // Event listener cleanup functions
  let cleanupFunctions: (() => void)[] = []

  // Load settings and set up event listeners on mount
  onMount(async () => {
    GetReaderIdentityEmails().then(emails => {
      ownEmails = new Set((emails || []).map(senderAddress).filter(Boolean))
    }).catch(error => console.warn('[viewer] sender identities unavailable', error))
    try {
      const policy = await GetReadReceiptResponsePolicy()
      readReceiptPolicy = policy as 'never' | 'ask' | 'always'
    } catch (err) {
      console.error('Failed to load settings:', err)
    }

    // Listen for message changes from backend
    cleanupFunctions.push(
      EventsOn('messages:readChanged', (data: { messageIds: string[], isRead: boolean }) => {
        if (!conversation?.messages?.some(m => data.messageIds.includes(m.id))) return
        // A manual or external state change wins over an old snapshot. This
        // includes explicit Mark as unread while the viewer stays open.
        pendingReadOnLeave.cancel(data.messageIds)
        if (!data.isRead) clearMarkAsReadTimer()
        const changed = new Set(data.messageIds)
        for (const message of conversation.messages) {
          if (changed.has(message.id)) message.isRead = data.isRead
        }
        conversation = conversation
      })
    )

    cleanupFunctions.push(
      EventsOn('messages:moved', (data: { messageIds: string[], destFolderId: string }) => {
        if (!conversation?.messages?.some(m => data.messageIds.includes(m.id))) return
        // A destructive/move action owns these messages. Leaving the viewer
        // must not launch a second MarkAsRead mutation against temporary UIDs.
        pendingReadOnLeave.cancel(data.messageIds)

        const movedCount = conversation.messages.filter(m => data.messageIds.includes(m.id)).length
        const remainingCount = conversation.messages.length - movedCount

        if (remainingCount === 0) {
          // The mutation Promise owns auto-selection. The backend event only
          // invalidates the viewer, preventing event + Promise double-complete.
          dismissConversation(false)
          return
        }
        // Some messages remain — reload conversation
        if (threadId && folderId) {
          loadConversation(threadId, folderId)
        }
      })
    )

    cleanupFunctions.push(
      EventsOn('messages:deleted', async (messageIds: string[]) => {
        if (conversation?.messages?.some(m => messageIds.includes(m.id))) {
          pendingReadOnLeave.cancel(messageIds)
          // Check how many messages were deleted
          const deletedCount = conversation.messages.filter(m => messageIds.includes(m.id)).length
          const remainingCount = conversation.messages.length - deletedCount

          if (remainingCount === 0) {
            // The mutation Promise owns auto-selection (same rule as moved).
            dismissConversation(false)
          } else {
            // Some messages remain - reload conversation
            if (threadId && folderId) {
              await loadConversation(threadId, folderId)
            }
          }
        }
      })
    )

    // Targeted undo updates the list through normal message events. Do not
    // reload here: an undo of a previously viewed conversation must not steal
    // focus from the conversation currently open in the viewer.

    cleanupFunctions.push(
      EventsOn('messages:updated', (data: { accountId: string; folderId: string }) => {
        if (threadId && folderId && accountId && data.accountId === accountId && data.folderId === folderId) {
          scheduleRefresh(threadId, folderId)
        }
      })
    )

    cleanupFunctions.push(
      EventsOn('folder:synced', (data: { accountId: string; folderId: string }) => {
        if (threadId && folderId && accountId && data.accountId === accountId && data.folderId === folderId) {
          // If conversation hasn't loaded yet or errored, do a full load
          if (!conversation || error) {
            loadConversation(threadId, folderId)
            return
          }
          // Otherwise, smart refresh: only update DOM if messages actually changed.
          scheduleRefresh(threadId, folderId)
        }
      })
    )

    cleanupFunctions.push(
      EventsOn('sent:synced', (data: { accountId: string }) => {
        if (threadId && folderId && accountId && data.accountId === accountId) {
          scheduleRefresh(threadId, folderId)
        }
      })
    )

    // Keyboard handler for message navigation and deletion
    const handleKeyDown = (e: KeyboardEvent) => {
      // Only handle if viewer pane is focused
      if (!isFocused) return

      // Handle Tab for message navigation. preventDefault is called only when
      // we actually navigate, so at the first/last boundary native Tab passes
      // through and the user can leave the viewer normally.
      if (e.key === 'Tab' && conversation?.messages) {
        const messageIds = visibleMessages.map(m => m.id)
        const currentIndex = focusedMessageId ? messageIds.indexOf(focusedMessageId) : -1

        if (e.shiftKey) {
          if (currentIndex > 0) {
            e.preventDefault()
            focusedMessageId = messageIds[currentIndex - 1]
            ;(document.querySelector(`[data-message-id="${focusedMessageId}"]`) as HTMLElement)?.focus()
          }
          // currentIndex <= 0: let native Shift+Tab navigate out of the viewer
          return
        }

        if (currentIndex >= 0 && currentIndex < messageIds.length - 1) {
          e.preventDefault()
          focusedMessageId = messageIds[currentIndex + 1]
          ;(document.querySelector(`[data-message-id="${focusedMessageId}"]`) as HTMLElement)?.focus()
          return
        }
        if (currentIndex === -1 && messageIds.length > 0) {
          e.preventDefault()
          focusedMessageId = messageIds[0]
          ;(document.querySelector(`[data-message-id="${focusedMessageId}"]`) as HTMLElement)?.focus()
        }
        // At last message (currentIndex === messageIds.length - 1): let native Tab navigate out
        return
      }

      // Handle delete for focused message. Guard against the composer-mount
      // focus race (a keystroke fired between Reply click and TipTap focus)
      // and against any input/contenteditable having focus.
      if (focusedMessageId && (e.key === 'Delete' || e.key === 'Backspace')) {
        if (isComposerOpen()) return
        if (isInputElement(e.target) || isInputElement(document.activeElement)) return
        e.preventDefault()
        handleDeleteFocusedMessage()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    cleanupFunctions.push(() => {
      window.removeEventListener('keydown', handleKeyDown)
    })

    // Flush deferred refreshes as soon as the last blocking dialog closes.
    stopDialogGuardListener = onDialogGuardChange(active => {
      if (!active && pendingRefresh) {
        const { tid, fid } = pendingRefresh
        pendingRefresh = null
        scheduleRefresh(tid, fid)
      }
    })
  })

  onDestroy(() => {
    // Inactive workspace tabs explicitly unmount their viewer to release the
    // rendered message bodies. Treat that as leaving the message, but do not
    // mutate read state when the whole app is simply shutting down.
    if (!isViewerVisible) handleReadOnLeave()
    if (threadId && folderId && accountId) {
      onSaveScrollPosition?.(threadId, folderId, accountId, contentContainerRef?.scrollTop ?? 0)
    }
    // Ignore any body, signature, or security work that completes after this
    // viewer is released; also drop the local conversation references now.
    viewGeneration++
    conversation = null
    emailBodyRefs = {}
    // Do not issue an asynchronous read-state mutation during process shutdown.
    // Normal navigation finalizes the snapshot before this component unmounts.
    clearMarkAsReadTimer()
    pendingReadOnLeave.clear()
    if (refreshTimer) {
      clearTimeout(refreshTimer)
      refreshTimer = null
    }
    stopDialogGuardListener?.()
    // Clean up all event listeners
    cleanupFunctions.forEach(cleanup => cleanup())
  })

  let viewGeneration = 0

  // Load conversation when threadId changes
  $effect(() => {
    // Props have already changed by the time this effect runs, so finalizing
    // here cannot reorder the list before the next conversation is selected.
    handleReadOnLeave()
    void accountId
    viewGeneration++
    conversation = null
    smimeResults = {}
    pgpResults = {}
    smimeLoading = new Set()
    pgpLoading = new Set()
    emailBodyRefs = {}
    pendingRefresh = null
    // Clear pending refresh timer on any threadId change (full load supersedes refresh)
    if (refreshTimer) {
      clearTimeout(refreshTimer)
      refreshTimer = null
    }
    messagesWithImagesLoaded.clear()
    darkMailOverrides = {}
    // Reset focused message on thread change so opening a thread starts fresh.
    // Same-thread refreshes (handled via scheduleRefresh) preserve focus.
    focusedMessageId = null
    revealedReactions = new Set()

    if (threadId && folderId) {
      loadConversation(threadId, folderId)
    }

    if (!threadId || !folderId) {
      conversation = null
      expandedMessages = new Set()
    }
  })

  $effect(() => {
    if (!isViewerVisible) handleReadOnLeave()
  })

  let wasDeferringReadForUnreadView = false
  $effect(() => {
    const shouldDefer = deferMarkAsReadUntilLeave
    if (wasDeferringReadForUnreadView && !shouldDefer) handleReadOnLeave()
    wasDeferringReadForUnreadView = shouldDefer
  })

  // Debounced refresh: coalesces rapid sync events (e.g. folder:synced + messages:updated)
  // into a single refreshConversation call, reducing Wails bridge pressure.
  // Defers if a dialog guard is active (e.g. folder picker open).
  function scheduleRefresh(tid: string, fid: string) {
    if (isDialogGuardActive()) {
      pendingRefresh = { tid, fid }
      return
    }
    if (refreshTimer) clearTimeout(refreshTimer)
    refreshTimer = setTimeout(() => {
      refreshTimer = null
      refreshConversation(tid, fid)
    }, 300)
  }

  // Lightweight refresh: fetches the conversation but only updates the DOM
  // if something actually changed (new messages, different count, etc.).
  // This avoids the visible flash/re-render when a sync completes with no changes.
  async function refreshConversation(tid: string, fid: string) {
    const generation = viewGeneration
    try {
      const updated = await GetConversation(tid, fid)

      // Stale guard: user navigated away while we were fetching
      if (threadId !== tid || folderId !== fid || generation !== viewGeneration) return

      if (!updated?.messages || updated.messages.length === 0) {
        dismissConversation(true)
        return
      }
      if (!conversation?.messages) return

      // Compare message count and latest message ID to detect actual changes
      if (updated.messages.length === conversation.messages.length) {
        const currentLatestId = conversation.messages[conversation.messages.length - 1]?.id
        const updatedLatestId = updated.messages[updated.messages.length - 1]?.id
        if (currentLatestId === updatedLatestId) return
      }

      // Something changed — update conversation, preserving scroll position
      const scrollTop = contentContainerRef?.scrollTop ?? 0
      conversation = updated

      // Expand any new unread messages
      if (conversation.messages) {
        const newExpanded = new Set(expandedMessages)
        conversation.messages.forEach((m, i) => {
          if (!m.isRead || i === conversation!.messages!.length - 1) {
            newExpanded.add(m.id)
          }
        })
        expandedMessages = newExpanded
        processSMIMEMessages(conversation.messages)
        processPGPMessages(conversation.messages)
      }

      await tick()
      if (contentContainerRef) {
        contentContainerRef.scrollTop = scrollTop
      }
    } catch (err) {
      console.error('Failed to refresh conversation:', err)
    }
  }

  async function loadConversation(tid: string, fid: string) {
    const generation = viewGeneration
    loading = true
    error = null

    try {
      const result = await GetConversation(tid, fid)

      // Stale guard: user navigated away while we were fetching
      if (threadId !== tid || folderId !== fid || generation !== viewGeneration) return

      conversation = result

      // Auto-expand unread messages and the last message
      if (conversation?.messages) {
        const newExpanded = new Set<string>()
        conversation.messages.forEach((m, i) => {
          // Expand if unread or if it's the last message
          if (!m.isRead || i === conversation!.messages!.length - 1) {
            newExpanded.add(m.id)
          }
        })
        expandedMessages = newExpanded

        // Process S/MIME messages on-view
        processSMIMEMessages(conversation.messages)

        // Process PGP messages on-view
        processPGPMessages(conversation.messages)

        // Fetch bodies for messages that don't have them yet (on-demand)
        fetchUnfetchedBodies(conversation.messages)
      }
    } catch (err) {
      console.error('Failed to load conversation:', err)
      if (generation === viewGeneration) error = $_('viewer.failedToLoad')
    } finally {
      // A newer load owns the loading state; leave it alone.
      if (generation === viewGeneration) {
        loading = false
        // Open at the first unread message, else the newest, keeping a peek of
        // the message before it so the thread still reads as a thread.
        await tick()
        if (contentContainerRef) {
          if (restoreScrollTop !== undefined) {
            contentContainerRef.scrollTop = restoreScrollTop
          } else {
            openTimelineAtTarget(
              contentContainerRef,
              readerMessages,
              () => generation === viewGeneration && threadId === tid && folderId === fid,
            )
          }
        }
        // Capture only after the conversation is actually rendered and still
        // visible. A restored selection that is replaced during startup never
        // becomes a pending read operation merely because it mounted briefly.
        if (threadId === tid && folderId === fid && isViewerVisible && conversation?.messages) {
          pendingReadOnLeave.capture(conversation.messages)
          scheduleMarkAsRead()
        }
      }
    }
  }

  let ownEmails = $state<Set<string>>(new Set())
  let expandedHeaders = $state<Set<string>>(new Set())
  function toggleHeader(messageId: string) {
    const next = new Set(expandedHeaders)
    if (!next.delete(messageId)) next.add(messageId)
    expandedHeaders = next
  }
  function toggleCard(messageId: string) {
    if (readerMode === 'conversation') toggleHeader(messageId)
    else toggleMessage(messageId)
  }

  const readerMode = $derived(getReaderMode())

  /**
   * Switches how bodies are drawn without changing the thread, the selected
   * message or what the reader is looking at: the card nearest the top keeps its
   * place while the bodies around it change height.
   */
  async function switchReaderMode(next: ReaderMode) {
    if (next === readerMode) return
    const container = contentContainerRef
    const ids = conversation?.messages?.map((m) => m.id) ?? []
    const anchor = container ? captureScrollAnchor(container, ids) : null
    const tid = threadId
    const fid = folderId
    setReaderMode(next)
    if (!container || !anchor) return
    await tick()
    holdScrollAnchor(container, anchor, () => threadId === tid && folderId === fid)
  }

  /** Dismiss the current conversation from the viewer.
   *  Finalizes the captured unread set, clears the conversation state,
   *  and optionally tells the message list to auto-select the next item. */
  function dismissConversation(autoSelectNext: boolean) {
    viewGeneration++
    pendingRefresh = null
    if (refreshTimer) {
      clearTimeout(refreshTimer)
      refreshTimer = null
    }
    handleReadOnLeave()
    conversation = null
    if (autoSelectNext) {
      onActionComplete?.(true)
    }
  }

  // Process S/MIME messages on-view (verify/decrypt fresh each time)
  // Fetch bodies on-demand for messages that don't have them yet
  let bodyFetchErrors = $state<Record<string, string>>({})

  async function fetchUnfetchedBodies(messages: messageModels.Message[]) {
    const generation = viewGeneration
    for (const msg of messages) {
      if (generation !== viewGeneration) return
      if ((msg as any).bodyFetched === false && !msg.bodyHtml && !msg.bodyText) {
        bodyFetchErrors = { ...bodyFetchErrors, [msg.id]: '' }
        try {
          const updated = await FetchMessageBody(msg.id)
          if (generation !== viewGeneration) {
            continue
          }
          // Update the message in the conversation if still viewing
          if (conversation?.messages) {
            const idx = conversation.messages.findIndex(m => m.id === msg.id)
            if (idx >= 0 && updated) {
              conversation.messages[idx] = updated
              conversation = conversation // trigger reactivity
            }
          }
        } catch (err) {
          if (generation !== viewGeneration) {
            continue
          }
          console.error('Failed to fetch body for message:', msg.id, err)
          // Rate limits, network failures and authorization errors do not mean
          // this message was deleted. Keep its header and offer an explicit retry.
          bodyFetchErrors = { ...bodyFetchErrors, [msg.id]: String(err) }
        }
      }
    }
  }

  function processSMIMEMessages(messages: messageModels.Message[]) {
    const generation = viewGeneration
    // Clear previous results
    smimeResults = {}
    smimeLoading = new Set()

    for (const msg of messages) {
      if (!msg.hasSMIME) continue
      smimeLoading = new Set([...smimeLoading, msg.id])

      ProcessSMIMEMessage(msg.id).then(result => {
        if (generation !== viewGeneration) return
        smimeResults = { ...smimeResults, [msg.id]: result }
        const next = new Set(smimeLoading)
        next.delete(msg.id)
        smimeLoading = next
      }).catch(err => {
        if (generation !== viewGeneration) return
        console.error('Failed to process S/MIME message:', msg.id, err)
        const next = new Set(smimeLoading)
        next.delete(msg.id)
        smimeLoading = next
      })
    }
  }

  // Process PGP messages on-view (verify/decrypt fresh each time)
  function processPGPMessages(messages: messageModels.Message[]) {
    const generation = viewGeneration
    pgpResults = {}
    pgpLoading = new Set()

    for (const msg of messages) {
      if (!msg.hasPGP) continue
      pgpLoading = new Set([...pgpLoading, msg.id])

      ProcessPGPMessage(msg.id).then(result => {
        if (generation !== viewGeneration) return
        pgpResults = { ...pgpResults, [msg.id]: result }
        const next = new Set(pgpLoading)
        next.delete(msg.id)
        pgpLoading = next
      }).catch(err => {
        if (generation !== viewGeneration) return
        console.error('Failed to process PGP message:', msg.id, err)
        const next = new Set(pgpLoading)
        next.delete(msg.id)
        pgpLoading = next
      })
    }
  }

  function finalizeReadOnLeave() {
    const messageIds = pendingReadOnLeave.take()
    if (!messageIds.length) return
    // This lifecycle write deliberately does not use the reject-on-conflict
    // interaction guard: take() is single-consumer, and dropping the write
    // while a non-destructive action (such as Star) is active would violate
    // read-on-leave. Move/delete paths cancel the pending IDs before this runs.
    MarkAsRead(messageIds).catch(err => {
      console.error('Failed to mark messages as read after leaving conversation:', err)
    })
  }

  function toggleMessage(messageId: string) {
    const newSet = new Set(expandedMessages)
    const wasExpanded = newSet.has(messageId)

    if (wasExpanded) {
      newSet.delete(messageId)
    } else {
      newSet.add(messageId)

      // Check for auto-send read receipt on expand
      if (readReceiptPolicy === 'always' && conversation?.messages) {
        const msg = conversation.messages.find(m => m.id === messageId)
        if (msg) {
          handleMessageExpanded(msg)
        }
      }
    }
    expandedMessages = newSet
  }

  function expandAll() {
    if (conversation?.messages) {
      expandedMessages = new Set(conversation.messages.map(m => m.id))
    }
  }

  function collapseAll() {
    // Keep only the last message expanded
    if (conversation?.messages && conversation.messages.length > 0) {
      expandedMessages = new Set([conversation.messages[conversation.messages.length - 1].id])
    }
  }

  function formatDate(dateStr: any): string {
    const date = new Date(dateStr)
    return `${date.toLocaleDateString()} at ${date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
  }

  function getFaviconUrl(email: string): string {
    const domain = email.trim().split('@')[1]?.toLowerCase()
    return domain ? `https://www.google.com/s2/favicons?domain=${encodeURIComponent(domain)}&sz=64` : ''
  }

  // Parse recipient list (JSON array format from backend)
  function parseRecipients(recipientStr: string | undefined): Array<{ name: string; email: string }> {
    if (!recipientStr) return []
    try {
      const parsed = JSON.parse(recipientStr)
      if (Array.isArray(parsed)) {
        return parsed.map((r: any) => ({
          name: r.name || '',
          email: r.email || ''
        }))
      }
      return []
    } catch {
      return []
    }
  }

  // Get the last message ID in the conversation (for reply actions)
  // Exported for keyboard shortcut use from App.svelte
  export function hasFocusedMessage(): boolean {
    return focusedMessageId !== null
  }

  export function getFocusedMessageId(): string | null {
    return focusedMessageId
  }

  export function selectAllText() {
    const targetId = focusedMessageId ?? getLastMessageId()
    if (!targetId || !expandedMessages.has(targetId)) return
    const messageEl = document.querySelector(`[data-message-id="${targetId}"]`)
    if (!messageEl) return
    const iframe = messageEl.querySelector('iframe') as HTMLIFrameElement | null
    if (!iframe?.contentWindow) return
    iframe.contentWindow.postMessage({ type: 'select-all' }, '*')
  }

  export function getLastMessageId(): string | null {
    const messages = readerMode === 'traditional' ? (conversation?.messages || []) : reactions.messages
    return messages[messages.length - 1]?.id || null
  }

  // Re-fetch conversation to pick up flag changes (star, read) from external actions
  export async function refreshFlags() {
    if (!threadId || !folderId) return
    try {
      conversation = await GetConversation(threadId, folderId)
    } catch {
      // Silent — flag refresh is best-effort
    }
  }

  // Get the target message ID for actions:
  // Use focused message if one is focused, otherwise fall back to last message
  function getTargetMessageId(): string | null {
    return focusedMessageId ?? getLastMessageId()
  }

  // Archive/Done actions fired in a short burst share one visual toast.
  // Each backend move remains an independent undo command; this only groups
  // their presentation so rapid triage doesn't flood the screen with cards.
  const ARCHIVE_TOAST_BATCH_MS = 1800

  const archiveUndoOperations = new Map<string, string[]>()
  const undoInFlight = new Set<string>()
  const archiveToastCleanupTimers =
    new Map<string, ReturnType<typeof setTimeout>>()

  let activeArchiveToastId: string | null = null
  let activeArchiveToastAt = 0

  function scheduleArchiveToastCleanup(toastId: string) {
    const existing = archiveToastCleanupTimers.get(toastId)
    if (existing) clearTimeout(existing)

    archiveToastCleanupTimers.set(
      toastId,
      setTimeout(() => {
        archiveUndoOperations.delete(toastId)
        archiveToastCleanupTimers.delete(toastId)

        if (activeArchiveToastId === toastId) {
          activeArchiveToastId = null
          activeArchiveToastAt = 0
        }
      }, 10000)
    )
  }

  function clearArchiveToastTracking(toastId: string) {
    archiveUndoOperations.delete(toastId)

    const timer = archiveToastCleanupTimers.get(toastId)
    if (timer) {
      clearTimeout(timer)
      archiveToastCleanupTimers.delete(toastId)
    }

    if (activeArchiveToastId === toastId) {
      activeArchiveToastId = null
      activeArchiveToastAt = 0
    }
  }

  function showArchiveUndoToast(operationID: string) {
    if (!operationID) {
      toasts.success($_('toast.conversationArchived'))
      return
    }
    const now = Date.now()
    const baseMessage = $_('toast.conversationArchived')

    if (
      activeArchiveToastId &&
      archiveUndoOperations.has(activeArchiveToastId) &&
      now - activeArchiveToastAt <= ARCHIVE_TOAST_BATCH_MS
    ) {
      const toastId = activeArchiveToastId
      const operations = archiveUndoOperations.get(toastId) ?? []
      operations.push(operationID)
      const count = operations.length

      archiveUndoOperations.set(toastId, operations)
      activeArchiveToastAt = now

      toasts.replace(
        toastId,
        {
          message: `${baseMessage} (${count})`,
          type: 'success',
          actions: [
            {
              label: $_('common.undo'),
              onClick: () => handleArchiveBatchUndo(toastId)
            }
          ],
          duration: 8000
        })

      scheduleArchiveToastCleanup(toastId)
      return
    }

    let toastId = ''

    toastId = toasts.success(baseMessage, [
      {
        label: $_('common.undo'),
        onClick: () => handleArchiveBatchUndo(toastId)
      }
    ])

    archiveUndoOperations.set(toastId, [operationID])
    activeArchiveToastId = toastId
    activeArchiveToastAt = now
    scheduleArchiveToastCleanup(toastId)
  }

  async function handleArchiveBatchUndo(toastId: string) {
    const operations = archiveUndoOperations.get(toastId) ?? []

    // This burst is closed as soon as Undo is clicked. Any subsequent Done
    // starts a fresh toast instead of joining an Undo already in progress.
    clearArchiveToastTracking(toastId)

    // Reuse the card that the user clicked and remove its button while the
    // backend waits for pending IMAP reconciliation.
    toasts.replace(
      toastId,
      {
        message: 'Undoing...',
        actions: [],
        duration: 20000
      })

    if (operations.length === 0 || undoInFlight.has(toastId)) return
    undoInFlight.add(toastId)
    let description = ''
    let completed = 0

    try {
      for (const operationID of [...operations].reverse()) {
        description = await UndoOperation(operationID)
        publishUndoOperationCompleted(operationID)
        completed++
      }

      const displayDescription =
        operations.length > 1
          ? `${operations.length} × ${description}`
          : description

      // Important: update the existing Conversation archived notification.
      // Do NOT create another toast for the Undo result.
      toasts.replace(
        toastId,
        {
          message: $_('toast.undone', {
            values: { description: displayDescription }
          }),
          type: 'success',
          actions: [],
          duration: 4000
        })

      onActionComplete?.()
    } catch (err) {
      console.error('Archive batch undo failed:', err)

      // The same card also becomes the error notification.
      toasts.replace(
        toastId,
        {
          message: completed > 0 ? `${$_('toast.undoFailed')} (${completed}/${operations.length})` : $_('toast.undoFailed'),
          type: 'error',
          actions: [],
          duration: 5000
        })

      if (completed > 0) {
        onActionComplete?.()
      }
    } finally {
      undoInFlight.delete(toastId)
    }
  }

  // Action button handlers
  function handleReply() {
    const messageId = getTargetMessageId()
    if (messageId) void openReply('reply', messageId, messagesWithImagesLoaded.has(messageId))
  }

  function handleReplyAll() {
    const messageId = getTargetMessageId()
    if (messageId) void openReply('reply-all', messageId, messagesWithImagesLoaded.has(messageId))
  }

  function handleForward() {
    const messageId = getTargetMessageId()
    if (messageId && onReply) {
      onReply('forward', messageId, messagesWithImagesLoaded.has(messageId))
    }
  }

  async function openReply(mode: 'reply' | 'reply-all' | 'forward', messageId: string, imagesLoaded = false) {
    const targetImagesLoaded = imagesLoaded || messagesWithImagesLoaded.has(messageId)
    if (mode === 'forward' || readerMode !== 'conversation' || inFocusMode) {
      onReply?.(mode, messageId, targetImagesLoaded)
      return
    }
    const sourceMessage = conversation?.messages?.find(message => message.id === messageId)
    const sourceAccountId = sourceMessage?.accountId || accountId
    if (!sourceAccountId) {
      onReply?.(mode, messageId, targetImagesLoaded)
      return
    }
    inlineReplyTarget = { threadId: threadId || '', accountId: sourceAccountId, messageId, mode, imagesLoaded: targetImagesLoaded }
    await tick()
    document.querySelector('[data-inline-reply]')?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  }

  function handleInlineReplySent() {
    const currentThreadId = threadId
    const currentFolderId = folderId
    inlineReplyTarget = null
    onActionComplete?.(false)
    if (currentThreadId && currentFolderId) void refreshConversation(currentThreadId, currentFolderId)
  }

  function fallbackInlineReply(target: NonNullable<typeof inlineReplyTarget>) {
    inlineReplyTarget = null
    onReply?.(target.mode, target.messageId, target.imagesLoaded)
  }

  function showUndoableSuccess(message: string, operationID: string) {
    if (!operationID) {
      toasts.success(message)
      return
    }
    let toastId = ''

    toastId = toasts.success(message, [
      {
        label: $_('common.undo'),
        onClick: () => {
          void handleUndo(toastId, operationID)
        }
      }
    ])

    return toastId
  }

  async function handleMoveToInbox() {
    if (!conversation?.messages) return
    let messageIds = conversation.messages.map(m => m.id)
    try {
      messageIds = await resolveDisplayedMessageTarget(messageIds)
      await runMessageMutation(messageIds, async () => {
        moveMutationInFlight = true
        try {
          pendingReadOnLeave.cancel(messageIds)
          const result = await MoveToInboxWithUndo(messageIds)
          if (result.coalesced) return
          publishUndoOperationCreated(result.operationId, messageIds, 'move-to-inbox')
          showUndoableSuccess($_('toast.movedTo', { values: { folder: $_('sidebar.inbox') } }), result.operationId)
          onActionComplete?.(true)
        } finally {
          moveMutationInFlight = false
        }
      })
    } catch (err) {
      console.error('Move to inbox failed:', err)
      toasts.error($_('toast.failedToMove'))
    }
  }

  async function handleArchive() {
    if (!conversation?.messages) return
    let messageIds = conversation.messages.map(m => m.id)
    try {
      messageIds = await resolveDisplayedMessageTarget(messageIds)
      await runMessageMutation(messageIds, async () => {
        moveMutationInFlight = true
        try {
          const result = await ArchiveWithUndo(messageIds)
          if (result.coalesced) return
          publishUndoOperationCreated(result.operationId, messageIds, 'archive')
          showArchiveUndoToast(result.operationId)
          onActionComplete?.(true)
        } finally {
          moveMutationInFlight = false
        }
      })
    } catch (err) {
      console.error('Archive failed:', err)
      toasts.error($_('toast.failedToArchive'))
    }
  }

  async function handleDone() {
    if (!conversation?.messages) return
    let messageIds = conversation.messages.map(m => m.id)
    try {
      messageIds = await resolveDisplayedMessageTarget(messageIds)
      await runMessageMutation(messageIds, async () => {
        moveMutationInFlight = true
        try {
          pendingReadOnLeave.cancel(messageIds)
          const result = await RemoveFromInboxWithUndo(messageIds)
          if (result.coalesced) return
          publishUndoOperationCreated(result.operationId, messageIds, 'remove-from-inbox')
          showArchiveUndoToast(result.operationId)
          onActionComplete?.(true)
        } finally {
          moveMutationInFlight = false
        }
      })
    } catch (err) {
      console.error('Done failed:', err)
      toasts.error($_('toast.failedToArchive'))
    }
  }

  async function handleDelete() {
    if (!conversation?.messages) return

    if (isTrashFolder) {
      // Show confirmation dialog for permanent delete
      showDeleteConfirm = true
    } else {
      // Move to trash (undoable)
      let messageIds = conversation.messages.map(m => m.id)
      try {
        messageIds = await resolveDisplayedMessageTarget(messageIds)
        await runMessageMutation(messageIds, async () => {
          moveMutationInFlight = true
          try {
            pendingReadOnLeave.cancel(messageIds)
            const result = await TrashWithUndo(messageIds)
            if (result.coalesced) return
            publishUndoOperationCreated(result.operationId, messageIds, 'trash')
            const toastMsg = result.movedToTrash ? $_('toast.movedToTrash') : $_('toast.deletedFromFolder')
            showUndoableSuccess(toastMsg, result.operationId)
            onActionComplete?.(true)
          } finally {
            moveMutationInFlight = false
          }
        })
      } catch (err) {
        console.error('Delete failed:', err)
        toasts.error($_('toast.failedToDelete'))
      }
    }
  }

  async function handleConfirmPermanentDelete() {
    if (!conversation?.messages) return
    let messageIds = conversation.messages.map(m => m.id)

    try {
      messageIds = await resolveDisplayedMessageTarget(messageIds)
      await runMessageMutation(messageIds, async () => {
        moveMutationInFlight = true
        try {
          await DeletePermanently(messageIds)
          toasts.success($_('toast.permanentlyDeleted'))
          showDeleteConfirm = false
          onActionComplete?.(true)
        } finally {
          moveMutationInFlight = false
        }
      })
    } catch (err) {
      console.error('Permanent delete failed:', err)
      toasts.error($_('toast.failedToDelete'))
      showDeleteConfirm = false
    }
  }

  // Delete the currently focused message (via keyboard)
  async function handleDeleteFocusedMessage() {
    if (!focusedMessageId) return
    const messageID = focusedMessageId

    if (isTrashFolder) {
      // Permanent delete from trash
      try {
        await runMessageMutation([messageID], async () => {
          moveMutationInFlight = true
          try {
            await DeletePermanently([messageID])
            toasts.success($_('toast.permanentlyDeleted'))
            focusedMessageId = null
            // Will auto-reload via messages:deleted event
          } finally {
            moveMutationInFlight = false
          }
        })
      } catch (err) {
        console.error('Permanent delete failed:', err)
        toasts.error($_('toast.failedToDelete'))
      }
    } else {
      // Move to trash (undoable)
      try {
        await runMessageMutation([messageID], async () => {
          moveMutationInFlight = true
          try {
            pendingReadOnLeave.cancel([messageID])
            const result = await TrashWithUndo([messageID])
            if (result.coalesced) return
            publishUndoOperationCreated(result.operationId, [messageID], 'trash')
            const toastMsg = result.movedToTrash ? $_('toast.movedToTrash') : $_('toast.deletedFromFolder')
            showUndoableSuccess(toastMsg, result.operationId)
            focusedMessageId = null
            // Will auto-reload via messages:deleted event
          } finally {
            moveMutationInFlight = false
          }
        })
      } catch (err) {
        console.error('Delete failed:', err)
        toasts.error($_('toast.failedToDelete'))
      }
    }
  }

  async function handleSpam() {
    if (!conversation?.messages) return
    let messageIds = conversation.messages.map(m => m.id)
    try {
      messageIds = await resolveDisplayedMessageTarget(messageIds)
      await runMessageMutation(messageIds, async () => {
        moveMutationInFlight = true
        try {
          pendingReadOnLeave.cancel(messageIds)
          if (isSpamFolder) {
            // If we're in spam folder, mark as NOT spam
            const result = await MarkAsNotSpamWithUndo(messageIds)
            if (result.coalesced) return
            publishUndoOperationCreated(result.operationId, messageIds, 'mark-as-not-spam')
            showUndoableSuccess($_('toast.markedAsNotSpam'), result.operationId)
            onActionComplete?.(true)
            return
          }
          // Otherwise, mark as spam
          const result = await MarkAsSpamWithUndo(messageIds)
          if (result.coalesced) return
          publishUndoOperationCreated(result.operationId, messageIds, 'mark-as-spam')
          const toastMsg = result.movedToSpam ? $_('toast.markedAsSpam') : $_('toast.deletedFromFolder')
          showUndoableSuccess(toastMsg, result.operationId)
          onActionComplete?.(true)
        } finally {
          moveMutationInFlight = false
        }
      })
    } catch (err) {
      console.error('Spam toggle failed:', err)
      toasts.error($_(isSpamFolder ? 'toast.failedToMarkAsNotSpam' : 'toast.failedToMarkAsSpam'))
    }
  }

  async function handleStar() {
    if (!conversation?.messages || !threadId || !folderId) return

    // Toggle based on current state - star if any unstarred, unstar if all starred
    const wasAllStarred = conversation.messages.every(m => m.isStarred)
    let messageIds = conversation.messages.map(m => m.id)

    try {
      messageIds = await resolveDisplayedMessageTarget(messageIds)
      await runMessageMutation(messageIds, async () => {
        if (wasAllStarred) {
          await Unstar(messageIds)
          toasts.success($_('toast.removedStar'))
        }
        if (!wasAllStarred) {
          await Star(messageIds)
          toasts.success($_('toast.starred'))
        }
        conversation = await GetConversation(threadId, folderId)
        onActionComplete?.()
      })
    } catch (err) {
      console.error('Star toggle failed:', err)
      toasts.error($_('toast.failedToUpdateStar'))
    }
  }

  async function handleMarkRead() {
    if (!conversation?.messages) return

    // Toggle based on current state
    const allRead = conversation.messages.every(m => m.isRead)
    let messageIds = conversation.messages.map(m => m.id)

    try {
      messageIds = await resolveDisplayedMessageTarget(messageIds)
      await runMessageMutation(messageIds, async () => {
        // Explicit actions supersede the leave snapshot and remain immediate.
        pendingReadOnLeave.cancel(messageIds)
        if (allRead) {
          await MarkAsUnread(messageIds)
          toasts.success($_('toast.markedAsUnread'))
        }
        if (!allRead) {
          await MarkAsRead(messageIds)
          toasts.success($_('toast.markedAsRead'))
        }
      })
    } catch (err) {
      console.error('Read status toggle failed:', err)
      toasts.error($_('toast.failedToUpdateReadStatus'))
    }
  }

  async function handleUndo(toastId: string, operationID: string) {
    if (undoInFlight.has(operationID)) return
    undoInFlight.add(operationID)
    toasts.replace(toastId, { message: 'Undoing...', actions: [], duration: 20000 })
    try {
      const description = await UndoOperation(operationID)
      publishUndoOperationCompleted(operationID)
      const message = $_('toast.undone', { values: { description } })

      // Undo triggered from a toast should transform that same toast instead
      // of adding another card to the notification stack.
      {
        toasts.replace(toastId, {
          message,
          type: 'success'
        })
      }
      onActionComplete?.()
    } catch (err) {
      console.error('Undo failed:', err)

      {
        toasts.replace(toastId, {
          message: $_('toast.undoFailed'),
          type: 'error'
        })
      }
    } finally {
      undoInFlight.delete(operationID)
    }
  }

  function escapeHtmlText(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  }

  function printRecipientsRow(label: string, raw: string | undefined): string {
    const list = parseRecipients(raw)
    if (list.length === 0) return ''
    const people = list.map((r) => escapeHtmlText(formatEmailForCopy(r.name, r.email))).join(', ')
    return `<tr><td class="lbl">${label}</td><td>${people}</td></tr>`
  }

  function buildPrintBlock(msg: messageModels.Message, bodyHtml: string): string {
    const header = `<table class="hdr">
      <tr><td class="lbl">From</td><td>${escapeHtmlText(formatEmailForCopy(msg.fromName, msg.fromEmail))}</td></tr>
      ${printRecipientsRow('To', msg.toList)}
      ${printRecipientsRow('Cc', msg.ccList)}
      <tr><td class="lbl">Date</td><td>${escapeHtmlText(formatDate(msg.date))}</td></tr>
    </table>`
    return `<section class="msg">${header}<div class="body">${bodyHtml}</div></section>`
  }

  // --- Add to Calendar handoff (P-022) -----------------------------------------

  // Race token: only the latest detection run may publish mentions.
  let calendarMentionsToken = 0

  async function refreshCalendarMentions() {
    const token = ++calendarMentionsToken
    const convo = conversation
    calendarMentions = []
    if (!convo?.messages) {
      return
    }
    const merged = new Map<string, backendModels.DateTimeMention>()
    for (const msg of convo.messages) {
      const text = (msg.bodyText || '').trim()
      if (!text) {
        continue
      }
      try {
        const found = await Calendar_DetectDateTimes(text.slice(0, 20000))
        for (const mention of found ?? []) {
          const key = `${mention.startUnix}:${mention.endUnix}`
          if (!merged.has(key)) {
            merged.set(key, mention)
          }
        }
      } catch (err) {
        console.warn('calendar date detection failed for a message', err)
      }
    }
    if (token !== calendarMentionsToken) {
      return
    }
    const dateFmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' })
    const dateTimeFmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
    calendarMentions = Array.from(merged.values()).slice(0, 8).map((mention) => ({
      ...mention,
      formatted: mention.isAllDay
        ? dateFmt.format(new Date(mention.startUnix * 1000))
        : dateTimeFmt.format(new Date(mention.startUnix * 1000)),
    }))
  }

  $effect(() => {
    // Re-run when the conversation or its message bodies change.
    void conversation?.messages?.map((m) => m.bodyText ?? '').join('')
    void refreshCalendarMentions()
  })

  async function addToCalendar(mention: CalendarChip) {
    if (!conversation) {
      return
    }
    try {
      await Calendar_CreateICSFromDetails({
        calendarId: '',
        summary: conversation.subject || mention.rawText,
        dtstartUnix: mention.startUnix,
        dtendUnix: mention.endUnix,
        isAllDay: mention.isAllDay,
      } as backendModels.EventInput)
    } catch (err) {
      console.warn('calendar event handoff failed', err)
      toasts.error($_('viewer.addToCalendarFailed'))
    }
  }

  function printDocument(subject: string, blocks: string[]) {
    const doc = `<!DOCTYPE html><html><head><meta charset="utf-8">
      <style>
        body { font-family: -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; color: #000; background: #fff; margin: 0; padding: 0; font-size: 12px; line-height: 1.45; }
        h1 { font-size: 15px; margin: 0 0 14px; }
        .msg { margin-bottom: 20px; }
        .msg + .msg { border-top: 2px solid #ddd; padding-top: 14px; }
        .hdr { border-collapse: collapse; margin-bottom: 10px; font-size: 11px; }
        .hdr td { vertical-align: top; padding: 1px 0; }
        .hdr td.lbl { color: #555; font-weight: 600; padding-right: 12px; white-space: nowrap; }
        .body { font-size: 12px; }
        .body img { max-width: 100%; height: auto; }
        @page { margin: 14mm; }

</style></head>
      <body><h1>${escapeHtmlText(subject)}</h1>${blocks.join('')}</body></html>`

    const frame = document.createElement('iframe')
    frame.setAttribute('aria-hidden', 'true')
    frame.style.cssText = 'position:fixed;right:0;bottom:0;width:0;height:0;border:0;'
    frame.srcdoc = doc
    frame.onload = () => {
      const win = frame.contentWindow
      if (!win) {
        frame.remove()
        return
      }
      win.addEventListener('afterprint', () => setTimeout(() => frame.remove(), 500))
      win.focus()
      win.print()
      // Fallback cleanup if afterprint never fires (some webviews)
      setTimeout(() => frame.remove(), 60000)
    }
    document.body.appendChild(frame)
  }

  async function handlePrint() {
    // Print only the messages currently rendered (those with an EmailBody).
    const msgs = visibleMessages.filter((m) => emailBodyRefs[m.id])
    if (msgs.length === 0) {
      window.print()
      return
    }
    const blocks: string[] = []
    for (const m of msgs) {
      let body: string
      try {
        body = await emailBodyRefs[m.id].getPrintableHtml()
      } catch {
        body = ''
      }
      blocks.push(buildPrintBlock(m, body))
    }
    printDocument(conversation?.subject ?? '', blocks)
  }

  // Read receipt handling
  async function handleSendReadReceipt(messageId: string, accountId: string) {
    if (sendingReadReceipt.has(messageId)) return

    sendingReadReceipt = new Set([...sendingReadReceipt, messageId])

    try {
      await SendReadReceipt(accountId, messageId)
      handledReadReceipts = new Set([...handledReadReceipts, messageId])
      toasts.success($_('viewer.readReceiptSent'))
    } catch (err) {
      console.error('Failed to send read receipt:', err)
      toasts.error($_('viewer.failedToSendReceipt'))
    } finally {
      const newSet = new Set(sendingReadReceipt)
      newSet.delete(messageId)
      sendingReadReceipt = newSet
    }
  }

  async function handleIgnoreReadReceipt(messageId: string, accountId: string) {
    try {
      await IgnoreReadReceipt(accountId, messageId)
      handledReadReceipts = new Set([...handledReadReceipts, messageId])
    } catch (err) {
      console.error('Failed to ignore read receipt:', err)
    }
  }

  // Check if message should show read receipt banner
  function shouldShowReadReceiptBanner(msg: messageModels.Message): boolean {
    // Don't show if policy is 'never'
    if (readReceiptPolicy === 'never') return false

    // Don't show if no read receipt requested
    if (!msg.readReceiptTo) return false

    // Don't show if already handled (from server or locally)
    if (msg.readReceiptHandled || handledReadReceipts.has(msg.id)) return false

    return true
  }

  // Auto-send read receipt when message is expanded (for 'always' policy)
  function handleMessageExpanded(msg: messageModels.Message) {
    if (readReceiptPolicy === 'always' && shouldShowReadReceiptBanner(msg)) {
      handleSendReadReceipt(msg.id, msg.accountId)
    }
  }

  // Computed: are all messages in the conversation starred?
  const allStarred = $derived(
    conversation?.messages?.every(m => m.isStarred) ?? false
  )

  // Computed: are all messages in the conversation read?
  const allRead = $derived(
    conversation?.messages?.every(m => m.isRead) ?? false
  )

  // Computed: is this the Trash folder?
  const isTrashFolder = $derived(folderType === 'trash')

  // Computed: is this the Spam folder?
  const isSpamFolder = $derived(folderType === 'spam')

  // Computed: messages visible in the viewer.
  // In message-focus mode, narrow to the single targeted message.
  // Otherwise show the whole thread.
  let revealedReactions = $state<Set<string>>(new Set())
  const reactions = $derived(reactionLayout(conversation?.messages || []))
  const readerMessages = $derived(readerMode === 'traditional' ? (conversation?.messages || []) : (conversation?.messages || []).filter(message => !reactions.hidden.has(message.id) || revealedReactions.has(message.id)))
  function toggleReactionMessages(ids: string[]) {
    const next = new Set(revealedReactions)
    const hide = ids.every(id => next.has(id))
    for (const id of ids) { if (hide) next.delete(id); else next.add(id) }
    revealedReactions = next
  }
  const visibleMessages = $derived(
    inFocusMode && focusModeKind === 'message' && focusedMessageIdInFocus
      ? readerMessages.filter(m => m.id === focusedMessageIdInFocus)
      : readerMessages
  )

  const bubbles = $derived(bubbleLayout(readerMessages, email => isConfiguredAccountEmail(email) || ownEmails.has(email)))

  // Reference to the scrollable content area
  let contentContainerRef = $state<HTMLDivElement | null>(null)
  const SCROLL_AMOUNT = 100 // pixels to scroll per keypress

  // Scroll the viewer up (exposed for keyboard navigation)
  export function scrollUp() {
    if (contentContainerRef) {
      contentContainerRef.scrollBy({ top: -SCROLL_AMOUNT, behavior: 'smooth' })
    }
  }

  // Scroll the viewer down (exposed for keyboard navigation)
  export function scrollDown() {
    if (contentContainerRef) {
      contentContainerRef.scrollBy({ top: SCROLL_AMOUNT, behavior: 'smooth' })
    }
  }

  // Expose action functions for keyboard shortcuts
  export function toggleStar() {
    handleStar()
  }

  export function markRead() {
    handleMarkRead()
  }

  export function markUnread() {
    // Invert the read state
    if (allRead) return // Already handled by handleMarkRead toggle
    handleMarkRead()
  }

  export function archive() {
    handleArchive()
  }

  export function spam() {
    handleSpam()
  }

  export function trash() {
    if (focusedMessageId) {
      handleDeleteFocusedMessage()
      return
    }
    handleDelete()
  }

  export function deletePermanently() {
    if (focusedMessageId) {
      handleDeleteFocusedMessage()
      return
    }
    handleConfirmPermanentDelete()
  }

  export function reply() {
    handleReply()
  }

  export function replyAll() {
    handleReplyAll()
  }

  export function forward() {
    handleForward()
  }

  export function isImagesLoaded(messageId: string): boolean {
    return messagesWithImagesLoaded.has(messageId)
  }

  export function loadImages() {
    // Dispatch custom event that EmailBody components listen to
    window.dispatchEvent(new CustomEvent('load-remote-images'))
  }

  export function openAlwaysLoadDropdown() {
    // Dispatch custom event that EmailBody components listen to
    window.dispatchEvent(new CustomEvent('open-always-load-dropdown'))
  }

  // Open context menu for the focused (or last) message
  export function openContextMenu() {
    const targetId = focusedMessageId ?? getLastMessageId()
    if (!targetId) return
    const messageEl = document.querySelector(`[data-message-id="${targetId}"]`) as HTMLElement | null
    if (!messageEl) return
    const rect = messageEl.getBoundingClientRect()
    messageEl.dispatchEvent(new MouseEvent('contextmenu', {
      bubbles: true,
      clientX: rect.right,
      clientY: rect.top + rect.height / 2,
    }))
  }

  // Handle action completion from context menu (per-message)
  async function handleContextMenuActionComplete() {
    // Reload conversation after context menu action
    if (threadId && folderId) {
      try {
        await loadConversation(threadId, folderId)

        // If conversation no longer exists or has no messages, navigate away
        if (!conversation || !conversation.messages || conversation.messages.length === 0) {
          onActionComplete?.(true) // Auto-select next conversation
        }
      } catch {
        // Conversation deleted or error loading - navigate away
        onActionComplete?.(true)
      }
    }
  }

  // Copy text to clipboard with toast feedback
  async function copyToClipboard(text: string, label: string = 'Text') {
    try {
      await navigator.clipboard.writeText(text)
      toasts.success($_('viewer.copiedToClipboard', { values: { label } }))
    } catch {
      toasts.error($_('viewer.failedToCopy'))
    }
  }

  // Format email for display/copy: "Name <email>" or just "email"
  function formatEmailForCopy(name: string | undefined, email: string): string {
    if (name && name.trim()) {
      return `${name} <${email}>`
    }
    return email
  }

  // View source state
  let viewingSourceMessageId = $state<string | null>(null)
  let messageSource = $state<string | null>(null)
  let loadingSource = $state(false)

  // Toggle view source for a message
  async function toggleViewSource(msgId: string) {
    if (viewingSourceMessageId === msgId) {
      // Close source view
      viewingSourceMessageId = null
      messageSource = null
      return
    }

    viewingSourceMessageId = msgId
    loadingSource = true
    messageSource = null

    try {
      const source = await GetMessageSource(msgId)
      messageSource = source
    } catch {
      toasts.error($_('viewer.failedToLoadSource'))
      viewingSourceMessageId = null
    } finally {
      loadingSource = false
    }
  }

</script>

<div class="flex flex-col h-full {isFlashing ? 'pane-focus-flash' : ''}">
  {#if !threadId}
    <!-- No conversation selected -->
    <div class="flex flex-col items-center justify-center h-full text-muted-foreground">
      <Icon icon="mdi:email-open-outline" class="w-16 h-16 mb-4" />
      <p class="text-lg">{$_('viewer.selectConversation')}</p>
    </div>
  {:else if loading}
    <!-- Loading -->
    <div class="flex items-center justify-center h-full">
      <Icon icon="mdi:loading" class="w-8 h-8 animate-spin text-muted-foreground" />
    </div>
  {:else if error}
    <!-- Error -->
    <div class="flex flex-col items-center justify-center h-full text-center px-4">
      <Icon icon="mdi:alert-circle-outline" class="w-12 h-12 text-destructive mb-3" />
      <p class="text-destructive mb-2">{$_('viewer.failedToLoad')}</p>
      <p class="text-sm text-muted-foreground">{error}</p>
      <button
        class="mt-4 text-sm text-primary hover:underline"
        onclick={() => loadConversation(threadId!, folderId!)}
      >
        {$_('viewer.tryAgain')}
      </button>
    </div>
  {:else if conversation}
    <div class="conversation-viewer-content flex flex-col h-full">
    <!-- Header with Actions -->
    <div class="spark-viewer-toolbar flex flex-wrap gap-2 items-center justify-between px-4 py-3 border-b border-border">
      <div class="flex flex-wrap items-center gap-2">
        {#if showBackButton}
          <button
            class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95 mr-1"
            data-tooltip={$_('responsive.back')}
            aria-label={$_('aria.backToList')}
            onclick={onBack}
          >
            <Icon icon="mdi:arrow-left" class="w-5 h-5 text-muted-foreground" />
          </button>
          <div class="w-px h-5 bg-border mx-1"></div>
        {/if}
        <button
          class="viewer-toolbar-action p-2 rounded-md bg-muted/70 hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={$_('common.done')}
          aria-label={$_('common.done')}
          onclick={handleDone}
          disabled={moveMutationInFlight}
          aria-busy={moveMutationInFlight}
        >
          <Icon icon="mdi:check" class="w-5 h-5 text-muted-foreground" />
        </button>
        <button
          class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={withShortcut($_('viewer.reply'), 'mail.reply')}
          aria-label={$_('viewer.reply')}
          aria-keyshortcuts={ariaKeyshortcuts('mail.reply')}
          onclick={handleReply}
        >
          <Icon icon="mdi:reply" class="w-5 h-5 text-muted-foreground" />
        </button>
        <button
          class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={withShortcut($_('viewer.replyAll'), 'mail.replyAll')}
          aria-label={$_('viewer.replyAll')}
          aria-keyshortcuts={ariaKeyshortcuts('mail.replyAll')}
          onclick={handleReplyAll}
        >
          <Icon icon="mdi:reply-all" class="w-5 h-5 text-muted-foreground" />
        </button>
        <button
          class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={withShortcut($_('viewer.forward'), 'mail.forward')}
          aria-label={$_('viewer.forward')}
          aria-keyshortcuts={ariaKeyshortcuts('mail.forward')}
          onclick={handleForward}
        >
          <Icon icon="mdi:share" class="w-5 h-5 text-muted-foreground" />
        </button>

        <div class="w-px h-5 bg-border mx-1"></div>
        {#if isTrashFolder}
          <button class="inline-flex items-center gap-2 rounded-md px-3 py-2 text-sm hover:bg-muted" onclick={handleMoveToInbox} disabled={moveMutationInFlight}>
            <Icon icon="mdi:inbox-arrow-down-outline" class="w-5 h-5" />
            {$_('viewer.moveToInbox')}
          </button>
        {/if}
        <button
          class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={withShortcut($_('viewer.archive'), 'mail.archive')}
          aria-label={$_('viewer.archive')}
          aria-keyshortcuts={ariaKeyshortcuts('mail.archive')}
          onclick={handleArchive}
          disabled={moveMutationInFlight}
        >
          <Icon icon="mdi:archive-outline" class="w-5 h-5 text-muted-foreground" />
        </button>
        <button
          class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={withShortcut($_(isTrashFolder ? 'viewer.deletePermanently' : 'viewer.delete'), isTrashFolder ? 'mail.deletePermanently' : 'mail.trash')}
          aria-label={$_(isTrashFolder ? 'viewer.deletePermanently' : 'viewer.delete')}
          onclick={handleDelete}
          disabled={moveMutationInFlight}
        >
          <Icon icon={isTrashFolder ? 'mdi:delete-forever' : 'mdi:delete-outline'} class="w-5 h-5 text-muted-foreground" />
        </button>
        <button
          class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={isSpamFolder ? $_('viewer.markAsNotSpam') : withShortcut($_('viewer.markAsSpam'), 'mail.spam')}
          aria-label={$_(isSpamFolder ? 'viewer.markAsNotSpam' : 'viewer.markAsSpam')}
          onclick={handleSpam}
          disabled={moveMutationInFlight}
        >
          <Icon icon={isSpamFolder ? 'mdi:email-check-outline' : 'mdi:alert-octagon-outline'} class="w-5 h-5 text-muted-foreground" />
        </button>

        <div class="w-px h-5 bg-border mx-1"></div>

        <button
          class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={withShortcut($_(allStarred ? 'viewer.removeStar' : 'viewer.star'), 'mail.star')}
          aria-label={$_(allStarred ? 'viewer.removeStar' : 'viewer.star')}
          onclick={handleStar}
        >
          <Icon icon={allStarred ? 'mdi:star' : 'mdi:star-outline'} class="w-5 h-5 {allStarred ? 'text-yellow-500' : 'text-muted-foreground'}" />
        </button>
        <button
          class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={withShortcut($_(allRead ? 'viewer.markAsUnread' : 'viewer.markAsRead'), allRead ? 'mail.markUnread' : 'mail.markRead')}
          aria-label={$_(allRead ? 'viewer.markAsUnread' : 'viewer.markAsRead')}
          onclick={handleMarkRead}
        >
          <Icon icon={allRead ? 'mdi:email-open-outline' : 'mdi:email-outline'} class="w-5 h-5 text-muted-foreground" />
        </button>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <ReaderModeToggle mode={readerMode} onChange={switchReaderMode} />
        {#if readerMode === 'traditional' && conversation.messages && conversation.messages.length > 1}
          <button
            class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
            data-tooltip={$_('viewer.expandAll')}
            aria-label={$_('viewer.expandAll')}
            onclick={expandAll}
          >
            <Icon icon="mdi:unfold-more-horizontal" class="w-5 h-5 text-muted-foreground" />
          </button>
          <button
            class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
            data-tooltip={$_('viewer.collapseAll')}
            aria-label={$_('viewer.collapseAll')}
            onclick={collapseAll}
          >
            <Icon icon="mdi:unfold-less-horizontal" class="w-5 h-5 text-muted-foreground" />
          </button>
        {/if}
        <button
          class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={withShortcut(inFocusMode && focusModeKind === 'thread' ? $_('viewer.exitFocus') : $_('viewer.focusThread'), 'viewer.threadFocus')}
          aria-label={inFocusMode && focusModeKind === 'thread' ? $_('viewer.exitFocus') : $_('viewer.focusThread')}
          onclick={onToggleThreadFocus}
        >
          <Icon icon={inFocusMode && focusModeKind === 'thread' ? 'mdi:fullscreen-exit' : 'mdi:fullscreen'} class="w-5 h-5 text-muted-foreground" />
        </button>
        <button
          class="viewer-toolbar-action p-2 rounded-md hover:bg-muted transition-all duration-150 hover:-translate-y-0.5 hover:scale-105 hover:shadow-sm active:translate-y-0 active:scale-95"
          data-tooltip={$_('viewer.print')}
          aria-label={$_('viewer.print')}
          onclick={handlePrint}
        >
          <Icon icon="mdi:printer-outline" class="w-5 h-5 text-muted-foreground" />
        </button>
      </div>
    </div>

    <!-- Conversation Content -->
    <div bind:this={contentContainerRef} class="spark-viewer-scroll flex-1 min-h-0 overflow-y-auto scrollbar-thin" onfocusin={() => setFocusedPane('viewer')}>
      <div class="p-6">
        <!-- Subject -->
        <h1 class="spark-conversation-subject text-xl font-semibold text-foreground mb-4">
          {conversation.subject || $_('viewer.noSubject')}
        </h1>

        <!-- Add to Calendar chips for recognized date/time mentions -->
        {#if calendarMentions.length > 0}
          <div class="mb-4 flex flex-wrap gap-2">
            {#each calendarMentions as mention (mention.startUnix + ':' + mention.endUnix)}
              <button
                class="inline-flex items-center gap-1.5 rounded-full border border-border bg-muted/40 px-3 py-1 text-sm text-foreground hover:bg-muted transition-colors"
                data-tooltip={$_('viewer.addToCalendar')}
                aria-label={`${$_('viewer.addToCalendar')}: ${mention.formatted}`}
                onclick={() => addToCalendar(mention)}
              >
                <Icon icon="mdi:calendar-plus" class="w-4 h-4 text-muted-foreground" />
                {mention.formatted}
              </button>
            {/each}
          </div>
        {/if}

        <!-- Message Count Badge -->
        {#if conversation.messages && conversation.messages.length > 1}
          <div class="mb-4 text-sm text-muted-foreground">
            {$_('viewer.messagesInConversation', { values: { count: readerMessages.length } })}
          </div>
        {/if}

        <!-- Stacked Messages -->
        {#if conversation.messages}
          <div class="space-y-4">
            {#each visibleMessages as msg, _index (msg.id)}
              {@const isExpanded = expandedMessages.has(msg.id)}
              {@const bubble = bubbles.get(msg.id)}
              {@const isFocusedMsg = inFocusMode && focusModeKind === 'message' && focusedMessageIdInFocus === msg.id}

              <!-- Wrap each message in its own context menu -->
              <MessageContextMenu
                messageIds={[msg.id]}
                accountId={accountId || ''}
                currentFolderId={folderId || ''}
                folderType={folderType || 'inbox'}
                isStarred={msg.isStarred}
                isRead={msg.isRead}
                onActionComplete={handleContextMenuActionComplete}
                onReply={openReply}
              >
                <div
                  class="spark-message-card border rounded-lg overflow-hidden transition-all {readerMode === 'conversation' ? 'conversation-bubble ' + (bubble?.side === 'right' ? 'bubble-right' : 'bubble-left') : ''} {focusedMessageId === msg.id ? 'border-primary ring-2 ring-primary/20' : 'border-border'}"
                  data-message-id={msg.id}
                  tabindex="-1"
                  role="button"
                  aria-expanded={readerMode === 'conversation' || isExpanded}
                  onfocus={() => focusedMessageId = msg.id}
                  onblur={() => { if (focusedMessageId === msg.id) focusedMessageId = null }}
                  onkeydown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      toggleCard(msg.id)
                    }
                  }}
                >
                {#if readerMode === 'conversation'}
                  <div class="bubble-heading flex items-center gap-2 px-4 pt-2 pb-1 text-xs text-muted-foreground">
                    <button type="button" class="flex min-w-0 flex-1 items-center justify-between gap-3 text-left" aria-expanded={expandedHeaders.has(msg.id)} title={$_('viewer.messageDetails')} onkeydown={(event) => event.stopPropagation()} onclick={() => toggleHeader(msg.id)}>
                      <span class="truncate font-medium text-foreground">{bubble?.showSender ? (msg.fromName || msg.fromEmail) : ''}</span>
                      <span class="whitespace-nowrap">{formatDate(msg.date)} <span aria-hidden="true">⌄</span></span>
                    </button>
                    {#if msg.isDraft}
                      <button type="button" class="inline-flex shrink-0 items-center gap-1 rounded px-2 py-1 text-primary hover:bg-muted" title={$_('viewer.editDraft')} aria-label={$_('viewer.editDraft')} onkeydown={(event) => event.stopPropagation()} onclick={(event) => { event.stopPropagation(); onEditDraft?.(msg.id) }}>
                        <Icon icon="mdi:pencil" class="h-3.5 w-3.5" />{$_('viewer.editDraft')}
                      </button>
                    {/if}
                  </div>
                {/if}
                {#if readerMode === 'traditional' || expandedHeaders.has(msg.id)}
                <!-- Full message details remain available on request. -->
                <div
                  class="spark-message-card-header w-full flex items-start gap-3 p-4 text-left hover:bg-muted/50 transition-colors cursor-pointer {!isExpanded ? 'bg-muted/30' : ''}"
                  onclick={() => toggleCard(msg.id)}
                  onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') toggleCard(msg.id) }}
                  onfocus={() => focusedMessageId = msg.id}
                  role="button"
                  tabindex="0"
                >
                  <!-- Sender circle (colored, with initials) -->
                  {#if getShowViewerCircles()}
                    {@const avatarPhoto = contactPhotos.get(msg.fromEmail)}
                    <Avatar
                      email={msg.fromEmail}
                      name={msg.fromName}
                      size={40}
                      photoData={avatarPhoto?.data}
                      photoMediaType={avatarPhoto?.mediaType}
                      faviconUrl={getFaviconUrl(msg.fromEmail)}
                    />
                  {/if}

                  <!-- Header Info -->
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 flex-wrap">
                      <span class="font-medium text-foreground">{msg.fromName || $_('viewer.unknown')}</span>
                      <span
                        role="button"
                        tabindex="0"
                        class="text-sm text-muted-foreground hover:text-primary hover:underline cursor-pointer"
                        title={$_('viewer.copyEmail')}
                        onclick={(e) => { e.stopPropagation(); copyToClipboard(formatEmailForCopy(msg.fromName, msg.fromEmail), $_('viewer.from')) }}
                        onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); copyToClipboard(formatEmailForCopy(msg.fromName, msg.fromEmail), $_('viewer.from')) }}}
                      >&lt;{msg.fromEmail}&gt;</span>

                      <!-- Unread indicator -->
                      {#if !msg.isRead}
                        <span class="w-2 h-2 rounded-full bg-primary flex-shrink-0"></span>
                      {/if}
                    </div>

                    {#if msg.replyTo && msg.replyTo.toLowerCase() !== msg.fromEmail.toLowerCase()}
                      <div class="text-sm text-muted-foreground flex flex-wrap items-center gap-1">
                        <span class="opacity-60">{$_('viewer.replyTo')}</span>
                        <span
                          role="button"
                          tabindex="0"
                          class="hover:text-primary hover:underline cursor-pointer text-muted-foreground"
                          title={$_('viewer.copyEmail')}
                          onclick={(e) => { e.stopPropagation(); copyToClipboard(msg.replyTo!, $_('viewer.replyTo')) }}
                          onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); copyToClipboard(msg.replyTo!, $_('viewer.replyTo')) }}}
                        >{msg.replyTo}</span>
                      </div>
                    {/if}

                    {#if msg.toList}
                      {@const recipients = parseRecipients(msg.toList)}
                      <div class="text-sm text-muted-foreground flex flex-wrap items-center gap-1">
                        <span class="opacity-60">{$_('viewer.to')}</span>
                        {#each recipients as recipient, i (recipient.email + ':' + i)}
                          <span
                            role="button"
                            tabindex="0"
                            class="hover:text-primary hover:underline cursor-pointer text-muted-foreground"
                            title={$_('viewer.copyEmail')}
                            onclick={(e) => { e.stopPropagation(); copyToClipboard(formatEmailForCopy(recipient.name, recipient.email), $_('viewer.to')) }}
                            onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); copyToClipboard(formatEmailForCopy(recipient.name, recipient.email), $_('viewer.to')) }}}
                          >{recipient.name || recipient.email}{i < recipients.length - 1 ? ',' : ''}</span>
                        {/each}
                      </div>
                    {/if}

                    {#if msg.ccList}
                      {@const ccRecipients = parseRecipients(msg.ccList)}
                      {#if ccRecipients.length > 0}
                        <div class="text-sm text-muted-foreground flex flex-wrap items-center gap-1">
                          <span class="opacity-60">{$_('viewer.cc')}</span>
                          {#each ccRecipients as recipient, i (recipient.email + ':' + i)}
                            <span
                              role="button"
                              tabindex="0"
                              class="hover:text-primary hover:underline cursor-pointer text-muted-foreground"
                              title={$_('viewer.copyEmail')}
                              onclick={(e) => { e.stopPropagation(); copyToClipboard(formatEmailForCopy(recipient.name, recipient.email), $_('viewer.cc')) }}
                              onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); copyToClipboard(formatEmailForCopy(recipient.name, recipient.email), $_('viewer.cc')) }}}
                            >{recipient.name || recipient.email}{i < ccRecipients.length - 1 ? ',' : ''}</span>
                          {/each}
                        </div>
                      {/if}
                    {/if}

                    {#if msg.bccList}
                      {@const bccRecipients = parseRecipients(msg.bccList)}
                      {#if bccRecipients.length > 0}
                        <div class="text-sm text-muted-foreground flex flex-wrap items-center gap-1">
                          <span class="opacity-60">{$_('viewer.bcc')}</span>
                          {#each bccRecipients as recipient, i (recipient.email + ':' + i)}
                            <span
                              role="button"
                              tabindex="0"
                              class="hover:text-primary hover:underline cursor-pointer text-muted-foreground"
                              title={$_('viewer.copyEmail')}
                              onclick={(e) => { e.stopPropagation(); copyToClipboard(formatEmailForCopy(recipient.name, recipient.email), $_('viewer.bcc')) }}
                              onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); copyToClipboard(formatEmailForCopy(recipient.name, recipient.email), $_('viewer.bcc')) }}}
                            >{recipient.name || recipient.email}{i < bccRecipients.length - 1 ? ',' : ''}</span>
                          {/each}
                        </div>
                      {/if}
                    {/if}

                    {#if !isExpanded}
                      <!-- Show snippet when collapsed -->
                      <p class="text-sm text-muted-foreground truncate mt-1">
                        {msg.snippet || ''}
                      </p>
                    {/if}
                  </div>

                  <!-- Date, edit button (drafts), and expand icon -->
                  <div class="flex flex-col items-end gap-1 flex-shrink-0">
                    <div class="flex items-center gap-2">
                      <span class="text-sm text-muted-foreground">
                        {formatDate(msg.date)}
                      </span>
                      {#if msg.isDraft}
                        <button
                          class="p-1 rounded hover:bg-muted transition-colors"
                          title={$_('viewer.editDraft')}
                          onclick={(e) => { e.stopPropagation(); onEditDraft?.(msg.id) }}
                        >
                          <Icon icon="mdi:pencil" class="w-4 h-4 text-muted-foreground" />
                        </button>
                      {/if}
                      <button
                        class="p-1 rounded hover:bg-muted transition-colors"
                        title={isFocusedMsg ? $_('viewer.exitFocus') : $_('viewer.focusMessage')}
                        onclick={(e) => { e.stopPropagation(); onToggleMessageFocus?.(msg.id) }}
                      >
                        <Icon icon={isFocusedMsg ? 'mdi:fullscreen-exit' : 'mdi:fullscreen'} class="w-4 h-4 text-muted-foreground" />
                      </button>
                      <Icon
                        icon={isExpanded ? 'mdi:chevron-up' : 'mdi:chevron-down'}
                        class="w-5 h-5 text-muted-foreground"
                      />
                    </div>
                    {#if getDarkMailContent() && getIsDarkActive()}
                      <button
                        class="text-xs leading-none px-1.5 py-0.5 rounded hover:bg-muted transition-colors"
                        title={shouldDarkenMessage(msg.id) ? $_('viewer.darkMailToLight') : $_('viewer.darkMailToDark')}
                        onclick={(e) => { e.stopPropagation(); toggleDarkMailOverride(msg.id) }}
                      >
                        {shouldDarkenMessage(msg.id) ? '☀️' : '🌛'}
                      </button>
                    {/if}
                  </div>
                </div>

                {/if}
                <!-- Message Body (visible when expanded) -->
                {#if isExpanded || readerMode === 'conversation'}
                  <div class="px-4 pb-4 pt-0">
                    <div class={readerMode === 'traditional' ? 'ml-13 pl-3 border-l-2 border-border' : ''}>
                      <!-- Read Receipt Banner -->
                      {#if shouldShowReadReceiptBanner(msg) && readReceiptPolicy === 'ask'}
                        <div class="flex items-center justify-between gap-3 px-3 py-2 mb-4 bg-blue-50 dark:bg-blue-950/30 border border-blue-200 dark:border-blue-800 rounded-md">
                          <div class="flex items-center gap-2 text-sm text-blue-700 dark:text-blue-300">
                            <Icon icon="mdi:email-check-outline" class="w-4 h-4 flex-shrink-0" />
                            <span>{$_('viewer.readReceiptRequested')}</span>
                          </div>
                          <div class="flex items-center gap-2">
                            <button
                              onclick={() => handleSendReadReceipt(msg.id, msg.accountId)}
                              disabled={sendingReadReceipt.has(msg.id)}
                              class="px-3 py-1 text-xs font-medium text-white bg-blue-600 hover:bg-blue-700 rounded transition-colors disabled:opacity-50"
                            >
                              {#if sendingReadReceipt.has(msg.id)}
                                <Icon icon="mdi:loading" class="w-3 h-3 animate-spin" />
                              {:else}
                                {$_('viewer.sendReceipt')}
                              {/if}
                            </button>
                            <button
                              onclick={() => handleIgnoreReadReceipt(msg.id, msg.accountId)}
                              class="px-3 py-1 text-xs font-medium text-blue-700 dark:text-blue-300 hover:bg-blue-100 dark:hover:bg-blue-900/50 rounded transition-colors"
                            >
                              {$_('viewer.ignoreReceipt')}
                            </button>
                          </div>
                        </div>
                      {:else if shouldShowReadReceiptBanner(msg) && readReceiptPolicy === 'always' && sendingReadReceipt.has(msg.id)}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-green-50 dark:bg-green-950/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-300">
                          <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
                          <span>{$_('viewer.sendingReadReceipt')}</span>
                        </div>
                      {/if}

                      <!-- S/MIME Loading Spinner (on-view processing) -->
                      {#if smimeLoading.has(msg.id)}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-muted/50 border border-border rounded-md text-sm text-muted-foreground">
                          <Icon icon="mdi:loading" class="w-4 h-4 animate-spin flex-shrink-0" />
                          <span>{$_('viewer.processingSMIME')}</span>
                        </div>
                      {/if}

                      <!-- S/MIME Encryption Banner -->
                      {#if smimeResults[msg.id]?.smimeEncrypted}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-blue-50 dark:bg-blue-950/30 border border-blue-200 dark:border-blue-800 rounded-md text-sm text-blue-700 dark:text-blue-300">
                          <Icon icon="mdi:lock-check" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.smimeEncryptedWith')}</span>
                        </div>
                      {/if}

                      <!-- S/MIME Signature Banner (on-view result for S/MIME messages, cached for non-S/MIME) -->
                      {#if (msg.hasSMIME ? smimeResults[msg.id]?.smimeStatus : msg.smimeStatus) === 'signed'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-green-50 dark:bg-green-950/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-300">
                          <Icon icon="mdi:shield-check" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.smimeSignedBy', { values: { email: (msg.hasSMIME ? smimeResults[msg.id]?.smimeSignerEmail : msg.smimeSignerEmail) || (msg.hasSMIME ? smimeResults[msg.id]?.smimeSignerSubject : msg.smimeSignerSubject) || $_('viewer.unknown').toLowerCase() } })}</span>
                        </div>
                      {:else if (msg.hasSMIME ? smimeResults[msg.id]?.smimeStatus : msg.smimeStatus) === 'unknown_signer'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800 rounded-md text-sm text-amber-700 dark:text-amber-300">
                          <Icon icon="mdi:shield-alert" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.smimeUnknownSigner', { values: { email: (msg.hasSMIME ? smimeResults[msg.id]?.smimeSignerEmail : msg.smimeSignerEmail) || (msg.hasSMIME ? smimeResults[msg.id]?.smimeSignerSubject : msg.smimeSignerSubject) || $_('viewer.unknown').toLowerCase() } })}</span>
                        </div>
                      {:else if (msg.hasSMIME ? smimeResults[msg.id]?.smimeStatus : msg.smimeStatus) === 'self_signed'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800 rounded-md text-sm text-amber-700 dark:text-amber-300">
                          <Icon icon="mdi:shield-alert" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.smimeSelfSigned', { values: { email: (msg.hasSMIME ? smimeResults[msg.id]?.smimeSignerEmail : msg.smimeSignerEmail) || (msg.hasSMIME ? smimeResults[msg.id]?.smimeSignerSubject : msg.smimeSignerSubject) || $_('viewer.unknown').toLowerCase() } })}</span>
                        </div>
                      {:else if (msg.hasSMIME ? smimeResults[msg.id]?.smimeStatus : msg.smimeStatus) === 'expired_cert'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">
                          <Icon icon="mdi:shield-off" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.smimeExpiredCert', { values: { email: (msg.hasSMIME ? smimeResults[msg.id]?.smimeSignerEmail : msg.smimeSignerEmail) || (msg.hasSMIME ? smimeResults[msg.id]?.smimeSignerSubject : msg.smimeSignerSubject) || $_('viewer.unknown').toLowerCase() } })}</span>
                        </div>
                      {:else if (msg.hasSMIME ? smimeResults[msg.id]?.smimeStatus : msg.smimeStatus) === 'invalid'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">
                          <Icon icon="mdi:shield-off" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.smimeInvalid')}</span>
                        </div>
                      {:else if (msg.hasSMIME ? smimeResults[msg.id]?.smimeStatus : msg.smimeStatus) === 'decrypt_failed'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">
                          <Icon icon="mdi:lock-off" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.smimeDecryptFailed')}</span>
                        </div>
                      {/if}

                      <!-- PGP Loading Spinner (on-view processing) -->
                      {#if pgpLoading.has(msg.id)}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-muted/50 border border-border rounded-md text-sm text-muted-foreground">
                          <Icon icon="mdi:loading" class="w-4 h-4 animate-spin flex-shrink-0" />
                          <span>{$_('viewer.processingPGP')}</span>
                        </div>
                      {/if}

                      <!-- PGP Encryption Banner -->
                      {#if pgpResults[msg.id]?.pgpEncrypted}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-blue-50 dark:bg-blue-950/30 border border-blue-200 dark:border-blue-800 rounded-md text-sm text-blue-700 dark:text-blue-300">
                          <Icon icon="mdi:lock-check" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.pgpEncryptedWith')}</span>
                        </div>
                      {/if}

                      <!-- PGP Signature Banner -->
                      {#if pgpResults[msg.id]?.pgpStatus === 'signed'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-green-50 dark:bg-green-950/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-300">
                          <Icon icon="mdi:key-outline" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.pgpSignedBy', { values: { email: pgpResults[msg.id]?.pgpSignerEmail || $_('viewer.unknown').toLowerCase() } })}</span>
                        </div>
                      {:else if pgpResults[msg.id]?.pgpStatus === 'unknown_key'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800 rounded-md text-sm text-amber-700 dark:text-amber-300">
                          <Icon icon="mdi:key-alert" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.pgpUnknownKey', { values: { keyId: pgpResults[msg.id]?.pgpSignerKeyId || '' } })}</span>
                        </div>
                      {:else if pgpResults[msg.id]?.pgpStatus === 'expired_key'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800 rounded-md text-sm text-amber-700 dark:text-amber-300">
                          <Icon icon="mdi:key-alert" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.pgpExpiredKey', { values: { email: pgpResults[msg.id]?.pgpSignerEmail || $_('viewer.unknown').toLowerCase() } })}</span>
                        </div>
                      {:else if pgpResults[msg.id]?.pgpStatus === 'revoked_key'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">
                          <Icon icon="mdi:key-remove" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.pgpRevokedKey', { values: { email: pgpResults[msg.id]?.pgpSignerEmail || $_('viewer.unknown').toLowerCase() } })}</span>
                        </div>
                      {:else if pgpResults[msg.id]?.pgpStatus === 'invalid'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">
                          <Icon icon="mdi:key-remove" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.pgpInvalid')}</span>
                        </div>
                      {:else if pgpResults[msg.id]?.pgpStatus === 'decrypt_failed'}
                        <div class="flex items-center gap-2 px-3 py-2 mb-4 bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">
                          <Icon icon="mdi:lock-off" class="w-4 h-4 flex-shrink-0" />
                          <span>{$_('viewer.pgpDecryptFailed')}</span>
                        </div>
                      {/if}

                      <!-- Body (use on-view result for S/MIME or PGP messages) -->
                      <div class="mb-4">
                        {#if (msg as any).bodyFetched === false && !msg.bodyHtml && !msg.bodyText}
                          {#if bodyFetchErrors[msg.id]}
                            <div class="py-4 text-sm" role="status">
                              <p class="text-muted-foreground">{$_('viewer.contentDownloadFailed')}</p>
                              <button type="button" class="mt-2 text-primary hover:underline" onclick={() => void fetchUnfetchedBodies([msg])}>{$_('common.retry')}</button>
                            </div>
                          {:else}
                            <div class="flex items-center gap-2 text-muted-foreground text-sm italic py-4">
                              <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
                              {$_('viewer.downloadingContent')}
                            </div>
                          {/if}
                        {:else if (msg.hasSMIME && !smimeResults[msg.id] && smimeLoading.has(msg.id)) || (msg.hasPGP && !pgpResults[msg.id] && pgpLoading.has(msg.id))}
                          <!-- Show placeholder while processing -->
                          <div class="text-muted-foreground text-sm italic py-4">{$_('viewer.decryptingMessage')}</div>
                        {:else}
                          <ConversationMessage
                            bind:this={emailBodyRefs[msg.id]}
                            messageId={msg.id}
                            accountId={msg.accountId}
                            extractable={canExtract(msg)}
                            mode={readerMode}
                            bodyHtml={msg.hasPGP && pgpResults[msg.id] ? pgpResults[msg.id].bodyHtml : msg.hasSMIME && smimeResults[msg.id] ? smimeResults[msg.id].bodyHtml : msg.bodyHtml}
                            bodyText={msg.hasPGP && pgpResults[msg.id] ? pgpResults[msg.id].bodyText : msg.hasSMIME && smimeResults[msg.id] ? smimeResults[msg.id].bodyText : msg.bodyText}
                            fromEmail={msg.fromEmail}
                            onCompose={onComposeToAddress}
                            onImagesLoaded={() => messagesWithImagesLoaded.add(msg.id)}
                            encryptedInlineAttachments={pgpResults[msg.id]?.inlineAttachments ?? smimeResults[msg.id]?.inlineAttachments}
                            darken={shouldDarkenMessage(msg.id)}
                          />
                        {/if}
                      </div>

                      {#if readerMode === 'conversation' && reactions.groups.has(msg.id)}
                        <div class="flex flex-wrap gap-2 mt-2" aria-label={$_('viewer.reactions')}>
                          {#each reactions.groups.get(msg.id) || [] as group (group.emoji)}
                            <button type="button" class="inline-flex items-center gap-1 rounded-full border border-border bg-background/60 px-2 py-1 text-sm hover:bg-muted"
                              data-reaction-chip aria-expanded={group.messages.every(message => revealedReactions.has(message.id))}
                              aria-label={`${$_('viewer.showReactionMessages')}: ${group.emoji} ${group.messages.map(message => message.fromName || message.fromEmail).join(', ')}`}
                              title={group.messages.map(message => message.fromName || message.fromEmail).join(', ')}
                              onkeydown={(event) => event.stopPropagation()}
                              onclick={() => toggleReactionMessages(group.messages.map(message => message.id))}>
                              <span>{group.emoji}</span><span>{group.messages.length}</span>
                            </button>
                          {/each}
                        </div>
                      {/if}

                      <!-- Attachments -->
                      {#if msg.hasAttachments || (pgpResults[msg.id]?.attachments?.length ?? 0) > 0 || (smimeResults[msg.id]?.attachments?.length ?? 0) > 0}
                        <div class="border-t border-border pt-4 mt-4">
                          <h3 class="text-sm font-medium text-foreground mb-3 flex items-center gap-2">
                            <Icon icon="mdi:paperclip" class="w-4 h-4" />
                            {$_('viewer.attachments')}
                          </h3>
                          <AttachmentList
                            messageId={msg.id}
                            encryptedAttachments={pgpResults[msg.id]?.attachments ?? smimeResults[msg.id]?.attachments}
                          />
                        </div>
                      {/if}

                      {#if readerMode === 'traditional'}
                      <!-- View Source Button -->
                      <div class="border-t border-border pt-4 mt-4">
                        <button
                          onclick={() => toggleViewSource(msg.id)}
                          class="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
                        >
                          <Icon icon={viewingSourceMessageId === msg.id ? 'mdi:code-tags' : 'mdi:code-tags'} class="w-4 h-4" />
                          {viewingSourceMessageId === msg.id ? $_('viewer.hideSource') : $_('viewer.viewSource')}
                        </button>

                        {#if viewingSourceMessageId === msg.id}
                          <div class="mt-3">
                            {#if loadingSource}
                              <div class="flex items-center gap-2 text-sm text-muted-foreground">
                                <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
                                {$_('viewer.loadingSource')}
                              </div>
                            {:else if messageSource}
                              <div class="relative">
                                <button
                                  onclick={() => copyToClipboard(messageSource || '', $_('viewer.viewSource'))}
                                  class="absolute top-2 right-2 p-1.5 rounded bg-muted hover:bg-muted/80 transition-colors"
                                  title={$_('viewer.copySource')}
                                >
                                  <Icon icon="mdi:content-copy" class="w-4 h-4" />
                                </button>
                                <pre class="text-xs bg-muted/50 p-4 rounded-md overflow-x-auto max-h-96 overflow-y-auto whitespace-pre-wrap break-all font-mono">{messageSource}</pre>
                              </div>
                            {/if}
                          </div>
                        {/if}
                      </div>
                      {/if}
                    </div>
                  </div>
                {/if}
                </div>
              </MessageContextMenu>
            {/each}
          </div>
        {/if}
        {#if inlineReplyTarget && inlineReplyTarget.threadId === (threadId || '')}
          <div data-inline-reply>
            {#key `${inlineReplyTarget.threadId}:${inlineReplyTarget.messageId}:${inlineReplyTarget.mode}`}
              <InlineReply
                accountId={inlineReplyTarget.accountId}
                messageId={inlineReplyTarget.messageId}
                mode={inlineReplyTarget.mode}
                imagesLoaded={inlineReplyTarget.imagesLoaded}
                onClose={() => (inlineReplyTarget = null)}
                onSent={handleInlineReplySent}
                onFallback={() => { if (inlineReplyTarget) fallbackInlineReply(inlineReplyTarget) }}
              />
            {/key}
          </div>
        {/if}
      </div>
    </div>
    </div>
  {/if}
</div>

<!-- Move / copy folder picker for the open conversation (`v` / Alt+M / Alt+C) -->
<FolderPickerDialog
  bind:open={showFolderPicker}
  title={$_(folderPickerMode === 'move' ? 'contextMenu.moveTo' : 'contextMenu.copyTo')}
  initialAccountId={accountId || ''}
  {accounts}
  excludeFolderId={folderId || ''}
  onSelect={handleFolderSelected}
/>

<!-- Permanent Delete Confirmation Dialog -->
<ConfirmDialog
  bind:open={showDeleteConfirm}
  title={$_('viewer.deleteConversationTitle')}
  description={$_('viewer.deleteConversationDescription')}
  confirmLabel={$_('viewer.deletePermanently')}
  variant="destructive"
  onConfirm={handleConfirmPermanentDelete}
  onCancel={() => showDeleteConfirm = false}
/>


<style>
  .conversation-bubble {
    --bubble-fill: color-mix(in srgb, hsl(var(--muted)) 55%, hsl(var(--background)));
    position: relative;
    overflow: visible;
    width: 100%;
    border-radius: 1rem;
    background: var(--bubble-fill) !important;
  }
  .conversation-bubble.bubble-right {
    --bubble-fill: color-mix(in srgb, hsl(var(--primary)) 14%, hsl(var(--background)));
    margin-left: auto;
  }
  .conversation-bubble.bubble-left { margin-right: auto; }
  .conversation-bubble::after {
    content: '';
    position: absolute;
    bottom: 16px;
    width: 16px;
    height: 16px;
    background: var(--bubble-fill);
    transform: rotate(45deg);
    pointer-events: none;
  }
  .conversation-bubble.bubble-left::after {
    left: -9px;
    border-left: 1px solid hsl(var(--border));
    border-bottom: 1px solid hsl(var(--border));
  }
  .conversation-bubble.bubble-right::after {
    right: -9px;
    border-top: 1px solid hsl(var(--border));
    border-right: 1px solid hsl(var(--border));
  }
  @media (max-width: 600px) { .conversation-bubble { width: 94%; } }
  .viewer-toolbar-action :global(svg) {
    transition: color 150ms ease, transform 150ms ease;
  }

  .viewer-toolbar-action:hover :global(svg),
  .viewer-toolbar-action:focus-visible :global(svg) {
    color: hsl(var(--foreground));
  }

  /* Viewer toolbar tooltips */
  .viewer-toolbar-action {
    position: relative;
  }

  .viewer-toolbar-action::after {
    content: attr(data-tooltip);
    position: absolute;
    left: 50%;
    top: calc(100% + 0.5rem);
    z-index: 100;
    transform: translate(-50%, -3px);
    width: max-content;
    max-width: 14rem;
    padding: 0.35rem 0.55rem;
    border: 1px solid hsl(var(--border));
    border-radius: 0.4rem;
    background: hsl(var(--popover));
    color: hsl(var(--popover-foreground));
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.3);
    font-size: 0.72rem;
    font-weight: 500;
    line-height: 1rem;
    white-space: nowrap;
    opacity: 0;
    visibility: hidden;
    pointer-events: none;
    transition:
      opacity 120ms ease,
      transform 120ms ease,
      visibility 120ms ease;
  }

  .viewer-toolbar-action:hover::after,
  .viewer-toolbar-action:focus-visible::after {
    opacity: 1;
    visibility: visible;
    transform: translate(-50%, 0);
  }

</style>
