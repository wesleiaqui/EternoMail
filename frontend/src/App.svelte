<script lang="ts">
  import Icon from '@iconify/svelte'
  import MailWindowApp from './MailWindowApp.svelte'
  import type { MailScope } from './lib/windows/mailWindow'
  import type { NavigationEntry } from './lib/sidebar/cards'
  // Load offline icon data before anything else
  import { LAYOUT_SYMBOL, PERMANENT_DELETE } from '$lib/keyboard/shortcuts'
  import './lib/iconify-offline'
  import { clearCache as clearInlineAttachmentCache } from './lib/stores/inlineAttachmentCache'

  import { onDestroy, onMount, tick, untrack } from 'svelte'
  import TitleBar from './lib/components/common/TitleBar.svelte'
  import Sidebar from './lib/components/sidebar/Sidebar.svelte'
  import GlobalSearch from './lib/components/common/GlobalSearch.svelte'
  let showGlobalSearch = $state(false)
  let showUndoHistory = $state(false)
  import ScheduledPane from './lib/components/scheduled/ScheduledPane.svelte'
  import MessageList from './lib/components/list/MessageList.svelte'
  import ConversationViewer from './lib/components/viewer/ConversationViewer.svelte'
  import Composer from './lib/components/composer/Composer.svelte'
  import ToastContainer from './lib/components/ui/toast/ToastContainer.svelte'
  import SpellSuggestionMenu from './lib/spellcheck/SpellSuggestionMenu.svelte'
  import TermsDialog from './lib/components/TermsDialog.svelte'
  import OAuthMissingDialog from './lib/components/OAuthMissingDialog.svelte'
  import WhatsNewDialog from './lib/components/WhatsNewDialog.svelte'
  import { shouldShowAfterLaunch } from './lib/utils/whatsNew'
  import UpdateAvailableNotice from './lib/components/UpdateAvailableNotice.svelte'
  import CertificateDialog from './lib/components/settings/CertificateDialog.svelte'
  import ExtensionSettingsDialog from './lib/components/settings/ExtensionSettingsDialog.svelte'
  import ExtensionRail from './lib/components/rail/ExtensionRail.svelte'
  import ShortcutsDialog from './lib/components/shortcuts/ShortcutsDialog.svelte'
  import UndoHistoryPanel from './lib/components/common/UndoHistoryPanel.svelte'
  import CommandPalette from './lib/components/command/CommandPalette.svelte'
  import ContactsPane from '$extensions/contacts/frontend/components/ContactsPane.svelte'
  import CalendarPane from '$extensions/calendar/frontend/components/CalendarPane.svelte'
  import { refreshExtensionRegistry, getRailTabs } from '$lib/stores/extensionRegistry.svelte'
  import { KEY } from '$lib/keyboard/shortcuts'
  import * as AlertDialog from '$lib/components/ui/alert-dialog'
  import { accountStore } from '$lib/stores/accounts.svelte'
  import { virtualFoldersStore } from '$lib/stores/virtualFolders.svelte'
  import { addToast, toasts } from '$lib/stores/toast'
  import { loadSettings, getThemeMode, getShowTitleBar, getNativeTitleBar, getComposerMode, getMailtoMode } from '$lib/stores/settings.svelte'
  import { loadImageAllowlist } from '$lib/stores/imageAllowlist.svelte'
  import { initializeUpdateChecker } from '$lib/stores/updateChecker.svelte'
  import { initTheme, applyTheme, applyThemeFromMode, getIsDarkActive, handleSystemThemeEvent, handleMediaQueryChange } from '$lib/stores/theme.svelte'
  import { loadUIState, saveUIState, paneConstraints, getActiveExtension, setActiveExtension } from '$lib/stores/uiState.svelte'
  import { setPendingDeepLink } from '$lib/stores/extensionDeepLink.svelte'
  import {
    type FocusablePane,
    getFocusedPane,
    setFocusedPane,
    focusPreviousPane,
    focusNextPane,
    isPaneFlashing,
    isInputElement,
    setComposerOpen,
    getPaneNav
  } from '$lib/stores/keyboard.svelte'
  import { isDialogGuardActive } from '$lib/stores/dialogGuard'
  import { dispatchExtensionShortcut } from '$lib/stores/extensionShortcuts.svelte'
  import { shortcutsOverlay, openShortcutsOverlay } from '$lib/stores/shortcutsOverlay.svelte'
  import { openCommandPalette, registerCommandAvailability, registerCommandHandler } from '$lib/stores/commandPalette.svelte'
  import { LIST_ACTIONS, getListAction, runListAction as runActionOnTarget } from '$lib/actions/listActions'
  import { resolveListTarget } from '$lib/actions/target'
  import type { ActionTarget, ListTarget, MessageActionExecutor, RunOptions } from '$lib/actions/types'
  import { initLayout, getLayoutMode, getResponsiveView, showViewer, hideViewer, showSidebar, hideSidebar, isResponsive } from '$lib/stores/layout.svelte'
  import { publishUndoOperationCompleted, publishUndoOperationCreated } from '$lib/components/list/undoMutationEvents'
  import { runMessageMutation } from '$lib/components/list/mutationInFlight'
  // @ts-ignore - wailsjs path
  import { PrepareReply, GetPendingMailto, GetDraftForEdit, DeleteDraft, ClearDraftHistory, SendMessageWithDraftGuard, CancelPendingSend, CompletePendingSend, ListPendingSends, MarkAsRead, MarkAsUnread, Star, Unstar, ArchiveWithUndo, MarkAsSpamWithUndo, MarkAsNotSpamWithUndo, ResolveMailActionTarget, UndoOperation, UndoLatestWithResult, GetTermsAccepted, SetTermsAccepted, RefreshWindowConstraints, AcceptCertificate, GetStartHiddenActive, CloseWindow, QuitApp, RequestQuitApp, CancelQuitAppRequest, OpenComposerWindow, OpenMailWindow, OpenReaderWindow, GetSystemTheme, NotifyStartupComplete, GetOAuthBuildStatus, GetOAuthWarningDisabled, SetOAuthWarningDisabled, GetLastSeenVersion, SetLastSeenVersion, GetAppInfo, GetWindowDecorationStatus } from '../wailsjs/go/app/App.js'
  // @ts-ignore - wailsjs path
  import { smtp, folder, certificate, operations } from '../wailsjs/go/models'
  // @ts-ignore - wailsjs runtime
  import { WindowShow, EventsOn } from '../wailsjs/runtime/runtime'
  import { _ } from '$lib/i18n'

  // Component refs for keyboard navigation. Plain `let` (not $state) is
  // intentional: svelte-check warns "Changing its value will not correctly
  // trigger updates" but nothing here actually reads these refs in a reactive
  // context — they're only used inside event handlers. Making them $state
  // added bookkeeping cost (visible in idle-CPU profiling) without any benefit.
  let sidebarRef: Sidebar | null = null
  let messageListRef: MessageList | null = null
  let viewerRef: ConversationViewer | null = null
  let messageListContainerRef: HTMLElement | null = null
  let effectiveTitleBarMode = $state('native')

  // React to theme mode changes from settings store
  $effect(() => {
    const mode = getThemeMode()
    applyThemeFromMode(mode)
  })

  // Selected folder state
  let selectedAccountId = $state<string | null>(null)
  let selectedFolderId = $state<string | null>(null)
  let selectedFolderName = $state('Inbox')
  let selectedFolderType = $state<string | null>(null)
  let contextPaneMode = $state<'folders' | 'messages'>('messages')
  let contextPaneAutoHide = $state(false)
  let contextPaneRevealed = $state(true)
  let contextPaneHideTimer: ReturnType<typeof setTimeout> | null = null
  const CONTEXT_PANE_AUTO_HIDE_KEY = 'postilaatikko:context-pane-auto-hide'
  const contextPaneVisible = $derived(!contextPaneAutoHide || contextPaneRevealed)
  let triageStatus = $state<'all' | 'unread' | 'readLater' | 'replyLater'>('all')
  type MailWorkspaceTab = { id: string; kind: 'reader'; scope: MailScope }
  const maxMailWorkspaceTabs = 8
  let mailWorkspaceTabs = $state<MailWorkspaceTab[]>([])
  let mailWindowTabInstances: Record<string, MailWindowApp | null> = {}
  let activeMailWorkspace = $state('primary')
  let primaryMailScope: MailScope | null = null
  let primaryContextPaneMode: 'folders' | 'messages' = 'messages'
  // Track where the selection came from: 'unified' for unified section, 'account' for account tree
  let selectionSource = $state<'unified' | 'account' | null>(null)

  function currentMailScope(): MailScope {
    if (activeMailWorkspace !== 'primary') {
      const active = mailWorkspaceTabs.find(tab => tab.id === activeMailWorkspace)
      if (active) return active.scope
    }
    return {
      accountId: selectedAccountId || 'unified',
      folderId: selectedFolderId || 'inbox',
      folderName: selectedFolderName,
      folderType: selectedFolderType || 'inbox',
      ...(selectedThreadId ? {
        threadId: selectedThreadId,
        threadAccountId: selectedConversationAccountId || undefined,
        threadFolderId: selectedConversationFolderId || undefined,
      } : {}),
      searchQuery: messageListRef?.getSearchQuery() || '',
    }
  }

  function workspaceLabel(scope: MailScope, reader: boolean): string {
    if (reader && scope.threadId && scope.threadSubject) return scope.threadSubject
    if (scope.accountId !== 'unified') return scope.folderName || $_('mailWindow.folder')
    const key = ({ inbox: 'allInboxes', archive: 'archived' } as Record<string, string>)[scope.folderType] || scope.folderType
    return $_(`sidebar.${key}`)
  }

  function restoredFolderName(accountId: string, folderType: string | null | undefined, folderId: string | null | undefined, storedName: string | undefined): string {
    if (accountId !== 'unified') return storedName || 'Inbox'
    const keys: Record<string, string> = {
      inbox: 'navigation.allInboxes',
      starred: 'sidebar.starred',
      drafts: 'sidebar.drafts',
      sent: 'sidebar.sent',
      trash: 'sidebar.trash',
      archive: 'sidebar.archived',
      spam: 'sidebar.blocked',
      replyLater: 'replyLater.title',
    }
    const key = keys[folderType || folderId || '']
    return key ? $_(key) : storedName || 'Inbox'
  }

  function addMailTab(scope: MailScope) {
    if (mailWorkspaceTabs.length >= maxMailWorkspaceTabs) {
      toasts.error($_('mailWindow.tabLimit', { values: { count: maxMailWorkspaceTabs } }))
      return
    }
    const tab: MailWorkspaceTab = { id: crypto.randomUUID(), kind: 'reader', scope: { ...scope } }
    mailWorkspaceTabs = [...mailWorkspaceTabs, tab]
    switchMailWorkspace(tab.id)
  }

  function openWorkspaceTab() {
    const scope = currentMailScope()
    if (scope.threadId) openReaderScope(scope)
  }

  function openReaderScope(scope: MailScope) {
    if (scope.threadId) addMailTab(scope)
  }

  function openListThreadInTab(accountId: string, folderId: string, threadId: string, folderType: string, subject: string) {
    const scope = currentMailScope()
    openReaderScope({ ...scope, folderType, threadId, threadAccountId: accountId, threadFolderId: folderId, threadSubject: subject })
  }

  function switchMailWorkspace(id: string) {
    if (activeMailWorkspace === 'primary' && id !== 'primary') {
      primaryMailScope = currentMailScope()
      primaryContextPaneMode = contextPaneMode
    }
    if (id !== activeMailWorkspace && activeMailWorkspace !== 'primary') mailWindowTabInstances[activeMailWorkspace]?.prepareForSuspend()
    activeMailWorkspace = id
    if (id === 'primary') {
      if (primaryMailScope) {
        selectedAccountId = primaryMailScope.accountId
        selectedFolderId = primaryMailScope.folderId
        selectedFolderName = primaryMailScope.folderName || selectedFolderName
        selectedFolderType = primaryMailScope.folderType
        selectedThreadId = primaryMailScope.threadId || null
        selectedConversationAccountId = primaryMailScope.threadAccountId || null
        selectedConversationFolderId = primaryMailScope.threadFolderId || null
      }
      contextPaneMode = primaryContextPaneMode
      setFocusedPane('messageList')
      return
    }
    const tab = mailWorkspaceTabs.find(item => item.id === id)
    if (tab) {
      selectedAccountId = tab.scope.accountId
      selectedFolderId = tab.scope.folderId
      selectedFolderName = tab.scope.folderName || selectedFolderName
      selectedFolderType = tab.scope.folderType
      selectedThreadId = tab.scope.threadId || null
      selectedConversationAccountId = tab.scope.threadAccountId || tab.scope.accountId
      selectedConversationFolderId = tab.scope.threadFolderId || tab.scope.folderId
      contextPaneMode = 'messages'
      setFocusedPane('viewer')
      return
    }
    setFocusedPane('messageList')
  }

  function showFolderBrowser() {
    contextPaneMode = 'folders'
    setFocusedPane('sidebar')
  }

  function revealContextPane() {
    if (contextPaneHideTimer) clearTimeout(contextPaneHideTimer)
    contextPaneRevealed = true
  }

  function scheduleContextPaneHide() {
    if (!contextPaneAutoHide) return
    if (contextPaneHideTimer) clearTimeout(contextPaneHideTimer)
    contextPaneHideTimer = setTimeout(() => {
      contextPaneRevealed = false
      contextPaneHideTimer = null
    }, 350)
  }

  function toggleContextPaneAutoHide() {
    contextPaneAutoHide = !contextPaneAutoHide
    contextPaneRevealed = true
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(CONTEXT_PANE_AUTO_HIDE_KEY, String(contextPaneAutoHide))
    }
  }

  function selectTriageFilter(mode: '' | 'unread' | 'readLater' | 'replyLater') {
    contextPaneMode = 'messages'
    triageStatus = mode === 'unread' ? 'unread' : mode === 'readLater' ? 'readLater' : mode === 'replyLater' ? 'replyLater' : 'all'
    messageListRef?.setQuickFilter(mode)
  }

  function closeWorkspaceTab(id: string) {
    const index = mailWorkspaceTabs.findIndex(tab => tab.id === id)
    if (index < 0) return
    mailWorkspaceTabs = mailWorkspaceTabs.filter(tab => tab.id !== id)
    if (activeMailWorkspace === id) {
      switchMailWorkspace(mailWorkspaceTabs[Math.min(index, mailWorkspaceTabs.length - 1)]?.id || 'primary')
    }
  }

  async function detachWorkspaceTab(id: string) {
    const tab = mailWorkspaceTabs.find(item => item.id === id)
    if (!tab) return
    try {
      const { accountId, folderId, folderType } = tab.scope
      if (!tab.scope.threadId) return
      await OpenReaderWindow(tab.scope.threadAccountId || accountId, tab.scope.threadFolderId || folderId, tab.scope.threadId, folderType)
      closeWorkspaceTab(id)
    } catch (error) { toasts.error(String(error)) }
  }

  function updateWorkspaceScope(id: string, scope: MailScope) {
    mailWorkspaceTabs = mailWorkspaceTabs.map(tab => tab.id === id ? { ...tab, scope } : tab)
  }

  function returnToPrimaryContextForFolderNavigation() {
    if (activeMailWorkspace === 'primary') return
    activeMailWorkspace = 'primary'
    primaryMailScope = null
    primaryContextPaneMode = 'messages'
  }

  // Quick-access rail entries are global UI, but navigation belongs to the
  // primary folder view. Choosing one while a reader tab is active returns to
  // the persistent folder/message context and leaves the email tab open.
  $effect(() => {
    if (activeMailWorkspace === 'primary') return
    const navigate = (event: Event) => {
      const entry = (event as CustomEvent<NavigationEntry>).detail
      if (!entry) return
      let scope: MailScope
      if (entry.kind === 'inboxes') {
        scope = { accountId: 'unified', folderId: 'inbox', folderName: entry.name, folderType: 'inbox' }
      } else if (entry.kind === 'system') {
        scope = { accountId: 'unified', folderId: entry.folderType || 'inbox', folderName: entry.name, folderType: entry.folderType || 'inbox' }
      } else if (entry.kind === 'virtual') {
        scope = { accountId: 'virtual', folderId: entry.folderId || '', folderName: entry.name, folderType: 'virtual' }
      } else {
        const folderInfo = entry.accountId && entry.folderId ? findFolderById(entry.accountId, entry.folderId) : null
        scope = { accountId: entry.accountId || '', folderId: entry.folderId || '', folderName: entry.name, folderType: folderInfo?.type || 'folder' }
      }
      activeMailWorkspace = 'primary'
      primaryMailScope = null
      primaryContextPaneMode = 'messages'
      contextPaneMode = 'messages'
      selectedAccountId = scope.accountId
      selectedFolderId = scope.folderId
      selectedFolderName = scope.folderName || 'Inbox'
      selectedFolderType = scope.folderType
      selectedThreadId = null
      selectedConversationAccountId = null
      selectedConversationFolderId = null
      selectionSource = scope.accountId === 'unified' ? 'unified' : 'account'
      setActiveExtension('mail')
    }
    window.addEventListener('postilaatikko:navigate', navigate)
    return () => window.removeEventListener('postilaatikko:navigate', navigate)
  })

  // Selected conversation state
  let selectedThreadId = $state<string | null>(null)
  let selectedConversationFolderId = $state<string | null>(null)
  let selectedConversationAccountId = $state<string | null>(null)
  // Read-state changes are intentionally excluded from the backend's global
  // undo stack (automatic read timers would otherwise pollute it). Keep a
  // targeted toast undo without diverting Cmd-Z/Ctrl-Z from global LIFO.
  let lastFlagUndo = $state<{ messageIds: string[], restoreRead: boolean } | null>(null)
  let globalUndoInFlight = false
  const targetedUndoInFlight = new Set<string>()

  // Composer state
  let showComposer = $state(false)
  let composerAccountId = $state<string | null>(null)
  let composerInitialMessage = $state<smtp.ComposeMessage | null>(null)
  let composerDraftId = $state<string | null>(null)
  let composerImagesLoaded = $state(false)

  // Mirror composer visibility into the keyboard store so the viewer can
  // suppress its Delete/Backspace shortcut during the composer's mount→focus race.
  $effect(() => {
    setComposerOpen(showComposer)
  })

  // Focus mode state — viewer (or single message) takes the whole window.
  // Always resets on conversation change, on Esc, on back-arrow, and on app reload.
  let focusMode = $state<'off' | 'thread' | 'message'>('off')
  let focusedMessageIdInFocus = $state<string | null>(null)
  const viewerIsOverlay = $derived(isResponsive() || focusMode !== 'off')
  const viewerIsVisible = $derived(getResponsiveView() === 'viewer' || focusMode !== 'off')

  function toggleThreadFocus() {
    if (focusMode === 'thread') {
      focusMode = 'off'
      focusedMessageIdInFocus = null
      return
    }
    focusMode = 'thread'
    focusedMessageIdInFocus = null
  }

  function toggleMessageFocus(messageId: string) {
    if (focusMode === 'message' && focusedMessageIdInFocus === messageId) {
      focusMode = 'off'
      focusedMessageIdInFocus = null
      return
    }
    focusMode = 'message'
    focusedMessageIdInFocus = messageId
  }

  // Auto-reset focus mode when the conversation changes (or is closed).
  // Prevents focus state from leaking across navigation.
  $effect(() => {
    void selectedThreadId
    focusMode = 'off'
    focusedMessageIdInFocus = null
  })

  // Route keyboard pane focus to the viewer while in focus mode so j/k/arrow
  // shortcuts scroll the viewer, then restore the prior pane on exit.
  let focusedPaneBeforeFocusMode: FocusablePane | null = null
  $effect(() => {
    const mode = focusMode
    if (mode !== 'off' && focusedPaneBeforeFocusMode === null) {
      focusedPaneBeforeFocusMode = untrack(() => getFocusedPane())
      setFocusedPane('viewer')
      return
    }
    if (mode === 'off' && focusedPaneBeforeFocusMode !== null) {
      setFocusedPane(focusedPaneBeforeFocusMode)
      focusedPaneBeforeFocusMode = null
    }
  })

  // Shutdown state
  let isShuttingDown = $state(false)

  // Terms acceptance state
  let showTermsDialog = $state(false)

  // Launch-time OAuth credentials warning state
  let showOAuthMissingDialog = $state(false)
  let oauthBuildStatus = $state({ google: true, microsoft: true })
  let pendingOAuthWarning = $state(false)

  // What's New (per-version) dialog state
  let showWhatsNewDialog = $state(false)
  let whatsNewVersion = $state('')
  let whatsNewLastSeen = $state('')
  let whatsNewHistory = $state(false)
  let pendingWhatsNew = $state(false)

  // Certificate TOFU state (for background sync cert errors)
  let showCertDialog = $state(false)
  let pendingCertificate = $state<certificate.CertificateInfo | null>(null)
  let pendingCertAccountId = $state<string | null>(null)

  // Flatpak filesystem permission dialog state
  let showFlatpakFsDialog = $state(false)

  // Handle window close button (title bar X) — hides if background mode, quits if not
  function handleClose() {
    CloseWindow()
  }

  // Handle forced quit (Ctrl+Q) — always quits regardless of background mode
  function handleQuit() {
    RequestQuitApp()
  }

  // bits-ui modal primitives temporarily set body pointer-events to none.
  // Opening the next startup modal in the same reactive turn as the previous
  // one closes can race that cleanup and leave the whole app non-interactive.
  // Give modal teardown two browser frames before advancing the launch queue.
  function nextAnimationFrame(): Promise<void> {
    return new Promise(resolve => requestAnimationFrame(() => resolve()))
  }

  async function waitForStartupModalCleanup() {
    await tick()
    await nextAnimationFrame()
    await nextAnimationFrame()
  }

  function releaseStaleStartupPointerLock() {
    if (document.body.style.pointerEvents === 'none') {
      document.body.style.removeProperty('pointer-events')
    }
  }

  let startupDialogAdvancing = false

  async function advanceStartupDialogs() {
    if (startupDialogAdvancing) return
    startupDialogAdvancing = true

    try {
      await waitForStartupModalCleanup()

      if (showTermsDialog || showOAuthMissingDialog || showWhatsNewDialog) {
        return
      }

      releaseStaleStartupPointerLock()

      if (pendingOAuthWarning) {
        pendingOAuthWarning = false
        showOAuthMissingDialog = true
        return
      }

      if (pendingWhatsNew) {
        pendingWhatsNew = false
        showWhatsNewDialog = true
      }
    } finally {
      startupDialogAdvancing = false
    }
  }

  // Handle terms acceptance
  async function handleTermsAccepted() {
    try {
      await SetTermsAccepted(true)
      showTermsDialog = false
      void advanceStartupDialogs()
    } catch (err) {
      console.error('Failed to save terms acceptance:', err)
    }
  }

  // OAuth warning dismiss — optionally persists the opt-out so the warning
  // stops firing on future launches even when credentials remain missing.
  async function dismissOAuthWarning(dontShowAgain: boolean) {
    if (dontShowAgain) {
      try {
        await SetOAuthWarningDisabled(true)
      } catch (err) {
        console.error('Failed to persist OAuth warning preference:', err)
      }
    }

    showOAuthMissingDialog = false
    void advanceStartupDialogs()
  }

  // What's New acknowledgement — records the current version as seen.
  // Called ONLY on explicit OK click; ESC/outside-click leaves the version
  // unrecorded so the dialog fires again next launch.
  async function acknowledgeWhatsNew() {
    try {
      await SetLastSeenVersion(whatsNewVersion)
    } catch (err) {
      console.error('Failed to persist last-seen version:', err)
    }

    showWhatsNewDialog = false
    whatsNewHistory = false
    void advanceStartupDialogs()
  }

  function closeWhatsNewHistory() {
    showWhatsNewDialog = false
    whatsNewHistory = false
  }

  // Certificate TOFU handlers for background sync
  async function handleBgCertAcceptOnce() {
    if (!pendingCertificate || !pendingCertAccountId) return
    try {
      // Look up the account's IMAP host for the accept call
      const acc = accountStore.accounts.find(a => a.account.id === pendingCertAccountId)
      const host = acc?.account.imapHost || ''
      await AcceptCertificate(host, pendingCertificate, false)
    } catch (err) {
      console.error('Failed to accept certificate:', err)
    }
    showCertDialog = false
    pendingCertificate = null
    pendingCertAccountId = null
  }

  async function handleBgCertAcceptPermanently() {
    if (!pendingCertificate || !pendingCertAccountId) return
    try {
      const acc = accountStore.accounts.find(a => a.account.id === pendingCertAccountId)
      const host = acc?.account.imapHost || ''
      await AcceptCertificate(host, pendingCertificate, true)
    } catch (err) {
      console.error('Failed to accept certificate:', err)
    }
    showCertDialog = false
    pendingCertificate = null
    pendingCertAccountId = null
  }

  function handleBgCertDecline() {
    showCertDialog = false
    pendingCertificate = null
    pendingCertAccountId = null
  }

  // Helper to find folder info by ID from account store
  function findFolderById(accountId: string, folderId: string): { name: string; type: string; path: string } | null {
    const acc = accountStore.accounts.find(a => a.account.id === accountId)
    if (!acc) return null

    function searchTree(trees: folder.FolderTree[]): { name: string; type: string; path: string } | null {
      for (const tree of trees) {
        if (tree.folder?.id === folderId) {
          return { name: tree.folder.name, type: tree.folder.type, path: tree.folder.path }
        }
        if (tree.children) {
          const found = searchTree(tree.children)
          if (found) return found
        }
      }
      return null
    }

    // Check if folders are loaded before searching
    if (!acc.folders || acc.folders.length === 0) return null
    return searchTree(acc.folders)
  }

  // 30-second undo-send countdown (user-approved). The composer queues the
  // send and closes; this bar owns the timer, dispatch and cancel. Restart
  // never auto-sends: the backend expires leftover rows at startup.
  type PendingSendItem = { id: string; accountId: string; draftId: string; payload: string; dispatchAt: string; dispatching?: boolean }
  let pendingSends = $state<PendingSendItem[]>([])
  let pendingTick: ReturnType<typeof setInterval> | undefined
  let pendingNow = $state(Date.now())
  const pendingSendDispatches = new Map<string, Promise<boolean>>()
  let quitRequestInProgress = false

  // Keep the countdown fresh only while there is something pending. An always
  // running timer used to wake the whole app four times per second at idle.
  $effect(() => {
    if (pendingSends.length === 0) return
    pendingTick = setInterval(() => {
      pendingNow = Date.now()
      for (const ps of pendingSends) {
        if (!ps.dispatching && new Date(ps.dispatchAt).getTime() <= pendingNow) {
          void pendingDispatch(ps)
        }
      }
    }, 1000)
    return () => {
      if (pendingTick !== undefined) {
        clearInterval(pendingTick)
        pendingTick = undefined
      }
    }
  })

  function pendingRemove(id: string) {
    pendingSends = pendingSends.filter(p => p.id !== id)
  }

  function pendingDispatch(ps: PendingSendItem): Promise<boolean> {
    const existing = pendingSendDispatches.get(ps.id)
    if (existing) return existing
    if (ps.dispatching) return Promise.resolve(false)
    ps.dispatching = true
    const dispatch = (async (): Promise<boolean> => {
      let message: smtp.ComposeMessage
      try {
        message = JSON.parse(ps.payload)
        await SendMessageWithDraftGuard(ps.accountId, message, ps.draftId)
      } catch (err) {
        // SMTP did not accept the message; preserve its draft and stop quit.
        await CompletePendingSend(ps.id).catch(() => {})
        pendingRemove(ps.id)
        addToast({ type: 'error', message: String(err) })
        return false
      }

      // SMTP accepted the message. Cleanup failures must not retry the send
      // or block quit, since that could deliver a duplicate on the next try.
      if (ps.draftId) {
        await DeleteDraft(ps.draftId).catch(err => console.error('Failed to delete draft after send:', err))
        // Actual send ends the draft's undo history; cancelling a pending send
        // never reaches here.
        await ClearDraftHistory(`${ps.accountId}:${ps.draftId}`).catch(err => console.error('Failed to clear draft history after send:', err))
      }
      await CompletePendingSend(ps.id).catch(err => console.error('Email sent but pending-send cleanup failed:', err))
      pendingRemove(ps.id)
      addToast({ type: 'success', message: $_('composer.messageSent') })
      messageListRef?.handleActionComplete?.(false)
      return true
    })()
    pendingSendDispatches.set(ps.id, dispatch)
    void dispatch.then(
      () => pendingSendDispatches.delete(ps.id),
      () => pendingSendDispatches.delete(ps.id)
    )
    return dispatch
  }

  async function finishQuitRequest() {
    if (quitRequestInProgress) return
    quitRequestInProgress = true
    isShuttingDown = true
    try {
      // Send the Undo Send queue immediately on explicit quit. Await each
      // dispatch, including one already started by the countdown timer.
      const persistedSends = await ListPendingSends() as PendingSendItem[]
      const queuedIDs = new Set(pendingSends.map(ps => ps.id))
      pendingSends = [...pendingSends, ...persistedSends.filter(ps => !queuedIDs.has(ps.id))]
      while (pendingSends.length > 0) {
        const sent = await pendingDispatch(pendingSends[0])
        if (!sent) {
          isShuttingDown = false
          CancelQuitAppRequest()
          return
        }
      }
      await QuitApp()
    } catch (err) {
      console.error('Could not finish quitting after pending sends:', err)
      isShuttingDown = false
      CancelQuitAppRequest()
    } finally {
      quitRequestInProgress = false
    }
  }

  async function pendingCancel(ps: PendingSendItem) {
    try {
      await CancelPendingSend(ps.id)
      pendingRemove(ps.id)
      addToast({ type: 'info', message: $_('pendingSend.cancelled') })
      if (ps.draftId) await handleEditDraft(ps.draftId)
    } catch {
      pendingRemove(ps.id)
      addToast({ type: 'error', message: $_('pendingSend.alreadySending') })
    }
  }

  function pendingRemaining(ps: PendingSendItem): number {
    return Math.max(0, Math.ceil((new Date(ps.dispatchAt).getTime() - pendingNow) / 1000))
  }

  onDestroy(() => {
    if (pendingTick !== undefined) clearInterval(pendingTick)
    if (contextPaneHideTimer) clearTimeout(contextPaneHideTimer)
  })

  onMount(async () => {
    contextPaneAutoHide = localStorage.getItem(CONTEXT_PANE_AUTO_HIDE_KEY) === 'true'
    contextPaneRevealed = !contextPaneAutoHide
    EventsOn('undo:history-open', () => {
      setActiveExtension('mail')
      showUndoHistory = true
    })
    EventsOn('undo:requested', () => { void handleGlobalUndo() })
    EventsOn('whatsnew:requested', () => {
      whatsNewHistory = true
      showWhatsNewDialog = true
    })

    // Listen for notification click events from backend
    try {
      pendingSends = await ListPendingSends() as PendingSendItem[]
    } catch { /* older backend without pending sends */ }
    EventsOn('pending-send:queued', (ps: PendingSendItem) => {
      if (!pendingSends.some(p => p.id === ps.id)) pendingSends = [...pendingSends, ps]
    })
    EventsOn('pending-send:cancelled', (id: string) => pendingRemove(id))
    EventsOn('notification:clicked', (data: { accountId: string; folderId: string; threadId: string }) => {
      // Find folder info for display
      const folderInfo = findFolderById(data.accountId, data.folderId)

      // Switch the rail back to mail in case the user was on an extension
      // tab when the notification fired — without this the message-list
      // state below would update but stay hidden behind the extension pane.
      setActiveExtension('mail')

      // Navigate to the folder (use 'unified' source to highlight under Unified Inbox)
      selectedAccountId = data.accountId
      selectedFolderId = data.folderId
      selectedFolderName = folderInfo?.name || 'Inbox'
      selectedFolderType = folderInfo?.type || 'inbox'
      selectionSource = 'unified'

      // Select the conversation
      selectedThreadId = data.threadId
      selectedConversationAccountId = data.accountId
      selectedConversationFolderId = data.folderId

      // Highlight the thread in the message list (with small delay to ensure list has loaded)
      setTimeout(() => {
        messageListRef?.selectThread(data.threadId)
      }, 100)

      // Persist state
      saveUIState({
        selectedAccountId: data.accountId,
        selectedFolderId: data.folderId,
        selectedFolderName: folderInfo?.name || 'Inbox',
        selectedFolderType: folderInfo?.type || 'inbox',
        selectedThreadId: data.threadId,
        selectedConversationAccountId: data.accountId,
        selectedConversationFolderId: data.folderId,
      })
    })

    // Generic extension-routed notification clicks. The host switches the
    // rail tab here AND stashes the path in a pending-deep-link buffer.
    // The target extension's pane drains the buffer on mount (it isn't
    // mounted yet at the moment we set the tab). Extension-specific path
    // parsing lives in each extension's own pane component.
    EventsOn('extension:open', (data: { extensionId: string; path: string }) => {
      if (!data.extensionId) return
      if (data.path) setPendingDeepLink(data.extensionId, data.path)
      setActiveExtension(data.extensionId)
    })

    // Listen for window show requests (from single-instance activation, notification clicks)
    EventsOn('window:show', () => {
      window.focus()
    })

    // Listen for shutdown event from backend (triggered by OS close signal)
    EventsOn('app:shutting-down', () => {
      isShuttingDown = true
    })
    EventsOn('app:quit-requested', () => {
      void finishQuitRequest()
    })

    // Listen for untrusted certificate events from background sync
    EventsOn('certificate:untrusted', (data: { accountId: string; certificate: certificate.CertificateInfo }) => {
      // Only show if not already showing a cert dialog
      if (!showCertDialog) {
        pendingCertificate = data.certificate
        pendingCertAccountId = data.accountId
        showCertDialog = true
      }
    })

    // Listen for Flatpak filesystem permission dialog event
    EventsOn('flatpak:filesystem-dialog', () => {
      showFlatpakFsDialog = true
    })

    // Listen for external mailto from second instance (routed through backend)
    EventsOn('mailto:external', (data: MailtoData) => {
      handleMailtoData(data)
    })

    // Toast confirmation when a detached composer sends a message
    EventsOn('composer:messageSent', () => {
      addToast({
        type: 'success',
        message: $_('composer.messageSent'),
      })
    })

    // Listen for escape-iframe-focus event (from EmailBody when navigating away from iframe)
    const handleEscapeIframeFocus = () => {
      // Focus the message list container to take keyboard focus away from iframe
      messageListContainerRef?.focus()
    }
    window.addEventListener('escape-iframe-focus', handleEscapeIframeFocus)

    // Load application settings (including theme mode) and apply theme
    const storedThemeMode = await loadSettings()
    await initTheme(storedThemeMode, GetSystemTheme)

    // Release checks are deliberately non-blocking. The checker waits a few
    // seconds before contacting GitHub and silently tolerates offline mode.
    void initializeUpdateChecker()

    try {
      effectiveTitleBarMode = (await GetWindowDecorationStatus()).effective_mode
    } catch (err) {
      console.error('Failed to load window decoration status:', err)
    }

    // Frameless is selected before Wails starts from this same persisted
    // setting. Keep the window hidden while launch-critical dialog state is
    // prepared as well. Revealing the GTK/WebKit window before a first-run
    // portal/focus trap is mounted can leave an invisible modal layer owning
    // pointer input on the first launch.
    const shouldStartHidden = await GetStartHiddenActive()

    // Load image allowlist cache for synchronous checks in EmailBody
    loadImageAllowlist()

    // Check if terms have been accepted
    try {
      const termsAccepted = await GetTermsAccepted()
      if (!termsAccepted) {
        showTermsDialog = true
      }
    } catch (err) {
      console.error('Failed to check terms acceptance:', err)
      // Show dialog on error to be safe
      showTermsDialog = true
    }

    // OAuth credentials warning: surface missing provider creds on every
    // launch unless the user has explicitly opted out. The dialog shows
    // both official providers with a missing/present indicator, so the
    // trigger fires when ANY of them is missing. Actual opening is
    // deferred via $effect so Terms can resolve first.
    try {
      const status = await GetOAuthBuildStatus()
      const anyMissing = !status.google || !status.microsoft
      if (anyMissing) {
        const disabled = await GetOAuthWarningDisabled()
        if (!disabled) {
          oauthBuildStatus = status
          pendingOAuthWarning = true
        }
      }
    } catch (err) {
      console.error('Failed to check OAuth build status:', err)
    }

    // What's New: fire whenever the stored last-seen version differs from
    // the current build's version — including the empty-string case so
    // existing users upgrading to v0.3.0 (or anyone whose DB predates the
    // last_seen_version key) see the release announcement on first launch.
    // The version isn't recorded until the user explicitly clicks OK in
    // the dialog (see acknowledgeWhatsNew); ESC/outside-click leaves it
    // unrecorded so the dialog fires again next launch.
    try {
      const appInfo = await GetAppInfo()
      const lastSeen = await GetLastSeenVersion()
      if (shouldShowAfterLaunch(appInfo.version, lastSeen)) {
        whatsNewVersion = appInfo.version
        whatsNewLastSeen = lastSeen
        whatsNewHistory = false
        pendingWhatsNew = true
      }
    } catch (err) {
      console.error('Failed to check What\'s New state:', err)
    }

    // Start/continue the launch-dialog queue. If Terms is currently open,
    // this is a no-op; accepting Terms will continue the queue.
    void advanceStartupDialogs()

    // Flush launch-dialog state (Terms -> OAuth warning -> What's New) into
    // the DOM before the native window becomes visible. This makes first-run
    // focus/pointer ownership deterministic on GTK/WebKit, especially under
    // KDE Wayland.
    await tick()
    if (!shouldStartHidden) {
      WindowShow()
    }

    // Load persisted UI state
    const uiState = await loadUIState()

    // Load extension registry (enabled extensions, rail tabs) so the rail can
    // render synchronously when the layout mounts.
    await refreshExtensionRegistry()

    // Restore the context pane width (already validated/clamped by loadUIState)
    listWidth = uiState.listWidth

    // Restore folder selection if valid
    if (uiState.selectedAccountId && uiState.selectedFolderId) {
      // Validate account still exists (unless unified inbox)
      const isUnified = uiState.selectedAccountId === 'unified'
      const isVirtualFolder = uiState.selectedAccountId === 'virtual'
      const accountExists = isUnified || isVirtualFolder || accountStore.accounts.some(
        a => a.account.id === uiState.selectedAccountId
      )

      if (accountExists) {
        selectedAccountId = uiState.selectedAccountId
        selectedFolderId = uiState.selectedFolderId
        selectedFolderName = restoredFolderName(uiState.selectedAccountId, uiState.selectedFolderType, uiState.selectedFolderId, uiState.selectedFolderName)
        selectedFolderType = uiState.selectedFolderType

        // Restore conversation selection
        if (uiState.selectedThreadId) {
          selectedThreadId = uiState.selectedThreadId
          selectedConversationAccountId = uiState.selectedConversationAccountId
          selectedConversationFolderId = uiState.selectedConversationFolderId
        }
      }
    }

    // Listen for system theme changes from backend (XDG Settings Portal)
    EventsOn('theme:system-preference', (newTheme: string) => {
      handleSystemThemeEvent(newTheme)
    })

    // Listen for system theme changes via matchMedia (fallback when portal unavailable)
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    mediaQuery.addEventListener('change', (e) => {
      handleMediaQueryChange(e.matches)
    })

    // App is fully initialized — dismiss the inline boot splash from
    // index.html. The CSS transition fades it out; we remove the element
    // shortly after to free the DOM. Done BEFORE the start-hidden check
    // so users in background mode don't see the splash linger on screen
    // before the window hides.
    const splash = document.getElementById('boot-splash')
    if (splash) {
      splash.hidden = true
      setTimeout(() => splash.remove(), 250)
    }

    // Clear the desktop-environment startup indicator. Called after WindowShow()
    // so KDE/Plasma sees the placeholder → real window handoff cleanly (#154).
    // Fired unconditionally so the indicator clears even when starting hidden.
    NotifyStartupComplete()

    // Remove GTK max size constraints that Wails v2 sets at startup
    RefreshWindowConstraints()

    // Initialize responsive layout breakpoint listeners
    initLayout()

    // Register actions for the command palette
    registerCommandHandler('app.compose', () => handleCompose())
    registerCommandHandler('app.search', () => {
      if (getActiveExtension() === 'mail') { showGlobalSearch = true; return }
      if (getActiveExtension() === 'mail') {
        messageListRef?.toggleSearchFocus()
        setFocusedPane('messageList')
      } else {
        getPaneNav('messageList')?.focusSearch?.()
        setFocusedPane('messageList')
      }
    })
    registerCommandHandler('app.syncAll', () => {
      sidebarRef?.toggleSync()
    })
    registerCommandHandler('app.syncFolder', () => {
      messageListRef?.toggleFolderSync()
    })
    registerCommandHandler('app.preferences', () => sidebarRef?.openSettings())
    registerCommandHandler('app.shortcuts', () => openShortcutsOverlay())
    registerCommandHandler('app.undo', () => handleGlobalUndo())
    registerCommandHandler('app.quit', () => handleQuit())
    registerCommandHandler('theme.toggle', () => {
      const next = getIsDarkActive() ? 'light' : 'dark'
      applyTheme(next)
    })
    registerCommandHandler('focus.sidebar', () => setFocusedPane('sidebar'))
    registerCommandHandler('focus.messageList', () => setFocusedPane('messageList'))
    registerCommandHandler('focus.viewer', () => setFocusedPane('viewer'))
    registerCommandHandler('mail.reply', () => {
      const msgId = getLastMessageId()
      if (msgId) handleReply('reply', msgId, viewerRef?.isImagesLoaded(msgId) || false)
    })
    registerCommandHandler('mail.replyAll', () => {
      const msgId = getLastMessageId()
      if (msgId) handleReply('reply-all', msgId, viewerRef?.isImagesLoaded(msgId) || false)
    })
    registerCommandHandler('mail.forward', () => {
      const msgId = getLastMessageId()
      if (msgId) handleReply('forward', msgId, viewerRef?.isImagesLoaded(msgId) || false)
    })
    // List actions use the same target as the keys (checked messages, else the
    // focused row) and are hidden from the panel when there is none.
    for (const listAction of LIST_ACTIONS) {
      registerCommandAvailability(listAction.id, () => listActionAvailable(listAction.id))
    }
    registerCommandHandler('mail.archive', () => runListAction('mail.archive'))
    registerCommandHandler('mail.spam', () => runListAction('mail.spam'))
    registerCommandHandler('mail.trash', () => runListAction('mail.trash'))
    registerCommandHandler('mail.star', () => runListAction('mail.star'))
    registerCommandHandler('mail.markRead', () => runListAction('mail.markRead'))
    registerCommandHandler('mail.markUnread', () => runListAction('mail.markUnread'))
    for (const id of ['mail.reply', 'mail.replyAll', 'mail.forward']) {
      registerCommandAvailability(id, () => selectedThreadId !== null)
    }

    // Check for pending mailto: URL from command line
    try {
      const mailtoData = await GetPendingMailto()
      if (mailtoData && (mailtoData.to?.length > 0 || mailtoData.subject || mailtoData.body)) {
        // Wait a moment for accounts to load
        await new Promise(resolve => setTimeout(resolve, 100))
        handleMailtoData(mailtoData)
      }
    } catch (err) {
      console.error('Failed to check pending mailto:', err)
    }
  })

  // Handle folder selection from sidebar (account tree)
  function handleFolderSelect(
    accountId: string,
    folderId: string,
    folderPath: string,
    folderName: string,
    folderType: string
  ) {
    returnToPrimaryContextForFolderNavigation()
    contextPaneMode = 'messages'
    if (selectedAccountId !== accountId) clearInlineAttachmentCache()
    selectedAccountId = accountId
    selectedFolderId = folderId
    selectedFolderName = folderName
    selectedFolderType = folderType
    selectionSource = 'account'
    selectedThreadId = null // Clear conversation selection when changing folders
    selectedConversationFolderId = null
    selectedConversationAccountId = null
    hideSidebar()

    // Persist state
    saveUIState({
      selectedAccountId: accountId,
      selectedFolderId: folderId,
      selectedFolderName: folderName,
      selectedFolderType: folderType,
      selectedThreadId: null,
      selectedConversationAccountId: null,
      selectedConversationFolderId: null,
    })
  }

  // Handle folder selection from unified inbox section
  function handleUnifiedFolderSelect(
    accountId: string,
    folderId: string,
    folderPath: string,
    folderName: string,
    folderType: string
  ) {
    returnToPrimaryContextForFolderNavigation()
    contextPaneMode = 'messages'
    if (selectedAccountId !== accountId) clearInlineAttachmentCache()
    selectedAccountId = accountId
    selectedFolderId = folderId
    selectedFolderName = folderName
    selectedFolderType = folderType
    selectionSource = 'unified'
    selectedThreadId = null
    selectedConversationFolderId = null
    selectedConversationAccountId = null
    hideSidebar()

    // Persist state
    saveUIState({
      selectedAccountId: accountId,
      selectedFolderId: folderId,
      selectedFolderName: folderName,
      selectedFolderType: folderType,
      selectedThreadId: null,
      selectedConversationAccountId: null,
      selectedConversationFolderId: null,
    })
  }

  // Handle unified inbox selection from sidebar (All Inboxes)
  function handleUnifiedInboxSelect() {
    handleUnifiedSpecialFolderSelect('inbox', 'All Inboxes')
  }

  // Top-level special folders are synthetic aggregate views; child rows keep
  // selecting their real account/folder pair.
  function handleUnifiedSpecialFolderSelect(folderType: string, displayName: string) {
    returnToPrimaryContextForFolderNavigation()
    contextPaneMode = 'messages'
    clearInlineAttachmentCache()
    selectedAccountId = 'unified'
    selectedFolderId = folderType
    // Sidebar passes its stable translation key. Persist the type as the
    // navigation identity and derive the visible text through i18n.
    selectedFolderName = $_(displayName)
    selectedFolderType = folderType
    selectionSource = 'unified'
    selectedThreadId = null
    selectedConversationFolderId = null
    selectedConversationAccountId = null
    hideSidebar()

    // Persist state
    saveUIState({
      selectedAccountId: 'unified',
      selectedFolderId: folderType,
      selectedFolderName: displayName,
      selectedFolderType: folderType,
      selectedThreadId: null,
      selectedConversationAccountId: null,
      selectedConversationFolderId: null,
    })
  }

  // Handle conversation selection from list
  function handleConversationSelect(threadId: string, folderId: string, accountId: string) {
    selectedThreadId = threadId
    selectedConversationFolderId = folderId
    selectedConversationAccountId = accountId
    const activeTab = mailWorkspaceTabs.find(tab => tab.id === activeMailWorkspace)
    if (activeTab?.kind === 'reader') {
      updateWorkspaceScope(activeTab.id, {
        ...activeTab.scope,
        threadId,
        threadAccountId: accountId,
        threadFolderId: folderId,
      })
    }
    showViewer()

    // Persist state
    saveUIState({
      selectedThreadId: threadId,
      selectedConversationAccountId: accountId,
      selectedConversationFolderId: folderId,
    })
  }

  // Resolve an account ID that may be 'unified' to a real account ID.
  // Returns the first real account ID if the input is 'unified' or falsy.
  function resolveAccountId(id: string | null): string | undefined {
    if (id && id !== 'unified') return id
    return accountStore.accounts[0]?.account.id
  }

  // Handle compose button click (new message)
  function handleCompose() {
    // Use the selected account, or the first account if none selected
    const accountId = resolveAccountId(selectedAccountId)
    if (!accountId) return

    // Check if detached mode is preferred
    if (getComposerMode() === 'detached') {
      OpenComposerWindow(accountId, 'new', '', '', '')
      return
    }

    composerAccountId = accountId
    composerInitialMessage = null
    composerDraftId = null
    showComposer = true
  }

  // Handle edit draft (opens composer with existing draft)
  async function handleEditDraft(draftId: string) {
    // Use conversation's account ID, fall back to selected account or first account
    const accountId = resolveAccountId(selectedConversationAccountId) || resolveAccountId(selectedAccountId)
    if (!accountId) return

    try {
      // Load the draft content from backend
      const draft = await GetDraftForEdit(draftId)
      if (!draft) {
        throw new Error('Draft not found')
      }

      composerAccountId = accountId
      composerInitialMessage = draft.message || null
      composerDraftId = draft.draftId
      showComposer = true
    } catch (err) {
      console.error('Failed to load draft:', err)
      addToast({
        type: 'error',
        message: $_('composer.failedToLoadDraft'),
      })
    }
  }

  // Handle compose to a specific email address (from mailto: links in emails)
  function handleComposeToAddress(toAddress: string) {
    // Use conversation's account ID, or selected account, or first account
    const accountId = resolveAccountId(selectedConversationAccountId) || resolveAccountId(selectedAccountId)
    if (!accountId) return

    composerAccountId = accountId
    composerDraftId = null
    // Create a minimal ComposeMessage with just the To address
    composerInitialMessage = new smtp.ComposeMessage({
      from: new smtp.Address({ name: '', address: '' }),
      to: [new smtp.Address({ name: '', address: toAddress })],
      cc: [],
      bcc: [],
      subject: '',
      text_body: '',
      html_body: '',
      attachments: [],
      request_read_receipt: false,
    })
    showComposer = true
  }

  // Handle mailto: URL data (from command line launch)
  interface MailtoData {
    to?: string[]
    cc?: string[]
    bcc?: string[]
    subject?: string
    body?: string
  }

  function handleMailtoData(data: MailtoData, rawMailtoURL?: string) {
    // Use selected account or first account (resolve 'unified' to real account)
    const accountId = resolveAccountId(selectedAccountId)
    if (!accountId) {
      // No accounts available, can't compose
      addToast({
        type: 'error',
        message: $_('toast.noAccountConfigured'),
      })
      return
    }

    // Check if detached mode is preferred for mailto
    if (getMailtoMode() === 'detached' && rawMailtoURL) {
      OpenComposerWindow(accountId, 'new', '', '', rawMailtoURL)
      return
    }

    composerAccountId = accountId
    composerDraftId = null
    composerInitialMessage = new smtp.ComposeMessage({
      from: new smtp.Address({ name: '', address: '' }),
      to: (data.to || []).map(addr => new smtp.Address({ name: '', address: addr })),
      cc: (data.cc || []).map(addr => new smtp.Address({ name: '', address: addr })),
      bcc: (data.bcc || []).map(addr => new smtp.Address({ name: '', address: addr })),
      subject: data.subject || '',
      text_body: data.body || '',
      html_body: '',
      attachments: [],
      request_read_receipt: false,
    })
    showComposer = true
  }

  // Handle reply/reply-all/forward - calls backend API
  async function handleReply(mode: 'reply' | 'reply-all' | 'forward', messageId: string, imagesLoaded?: boolean) {
    // Use conversation's account ID (important for unified inbox), fall back to selected account or first account
    const accountId = resolveAccountId(selectedConversationAccountId) || resolveAccountId(selectedAccountId)
    if (!accountId) return

    // Force detached composer when in focus mode — preserves the focused view
    if (focusMode !== 'off') {
      OpenComposerWindow(accountId, mode, messageId, '', '')
      return
    }

    try {
      // Call backend to prepare the reply message (backend gets account from message)
      const composeMessage = await PrepareReply(messageId, mode)
      composerAccountId = accountId
      composerDraftId = null
      composerInitialMessage = composeMessage
      composerImagesLoaded = imagesLoaded || false
      showComposer = true
    } catch (err) {
      console.error(`Failed to prepare ${mode}:`, err)
      addToast({
        type: 'error',
        message: $_('toast.failedToPrepare', { values: { mode } }),
      })
      // Fallback: open blank composer
      composerAccountId = accountId
      composerDraftId = null
      composerInitialMessage = null
      showComposer = true
    }
  }

  // Close composer
  function closeComposer() {
    showComposer = false
    composerAccountId = null
    composerInitialMessage = null
  }

  // Pane sizing state
  let listWidth = $state(360)

  // Resizing state
  let isResizingList = $state(false)

  function startResizeList(e: MouseEvent) {
    if (isResponsive()) return
    isResizingList = true
    e.preventDefault()
  }

  function handleMouseMove(e: MouseEvent) {
    if (isResizingList) {
      // Measure from the list pane's real rendered position instead of
      // subtracting the configured sidebar width. The configured value is
      // different from the rendered 56px width when the sidebar is collapsed,
      // which caused the divider to jump on the first mouse movement.
      const listLeft = messageListContainerRef?.getBoundingClientRect().left ?? 0
      listWidth = Math.max(
        paneConstraints.list.min,
        Math.min(paneConstraints.list.max, e.clientX - listLeft)
      )
    }
  }

  function handleMouseUp() {
    // Save pane widths if we were resizing
    if (isResizingList) saveUIState({ listWidth })
    isResizingList = false
  }

  // After a synthetic contextmenu event, bits-ui mounts the portal asynchronously.
  // Poll until [role="menu"] appears, then focus the first menuitem.
  function focusContextMenu() {
    let attempts = 0
    const tryFocus = () => {
      const menu = document.querySelector('[role="menu"]') as HTMLElement | null
      if (menu) {
        const firstItem = menu.querySelector('[role="menuitem"]:not([data-disabled])') as HTMLElement | null
        ;(firstItem || menu).focus()
        return
      }
      if (attempts++ < 10) {
        requestAnimationFrame(tryFocus)
      }
    }
    requestAnimationFrame(tryFocus)
  }

  // Track Left Alt held state for Left Alt + Right Alt combo
  let leftAltHeld = false

  function handleGlobalKeyUp(e: KeyboardEvent) {
    if (activeMailWorkspace !== 'primary') return
    if (e.code === 'AltLeft') {
      leftAltHeld = false
    }
  }

  // Global keyboard shortcut handler
  function handleGlobalKeyDown(e: KeyboardEvent) {
    // Secondary workspaces only own tab-level shortcuts. Keep other mail
    // actions on the primary workspace, while allowing workspace navigation
    // and the standard close-tab shortcut from every secondary tab.
    if (activeMailWorkspace !== 'primary') {
      if (document.querySelector('[role="menu"]') || isDialogGuardActive() || showComposer) return
      if ((e.ctrlKey || e.metaKey) && !e.altKey && getActiveExtension() === 'mail') {
        if (e.key.toLowerCase() === 'tab' && mailWorkspaceTabs.length > 0) {
          e.preventDefault()
          const order = ['primary', ...mailWorkspaceTabs.map(tab => tab.id)]
          const currentIndex = Math.max(0, order.indexOf(activeMailWorkspace))
          const step = e.shiftKey ? -1 : 1
          switchMailWorkspace(order[(currentIndex + step + order.length) % order.length])
          return
        }
        if (e.key.toLowerCase() === 'w' && !e.shiftKey) {
          e.preventDefault()
          closeWorkspaceTab(activeMailWorkspace)
          return
        }
      }
      return
    }

    // Track Left Alt press
    if (e.code === 'AltLeft') {
      leftAltHeld = true
    }
    const inInput = isInputElement(e.target)
    const focusedPane = getFocusedPane()
    const hasConversation = selectedThreadId !== null

    // Don't intercept keyboard events when a context menu or dropdown is open
    // (bits-ui portals mount [role="menu"] only while open)
    if (document.querySelector('[role="menu"]')) return

    // Alt+M/Alt+C toggle the message list's folder picker. While it's open the
    // dialog guard below swallows all keys, so close/switch is handled here.
    if (messageListRef?.isFolderPickerOpen()) {
      // `v` is a Gmail move shortcut only outside the picker. In its search
      // field it is ordinary text and must not close the dialog.
      if (KEY.LIST_MOVE_TO(e) && e.key !== 'v') {
        e.preventDefault()
        messageListRef.toggleMoveToDialog()
        return
      }
      if (KEY.LIST_COPY_TO(e)) {
        e.preventDefault()
        messageListRef.toggleCopyToDialog()
        return
      }
    }

    // Don't intercept while a modal dialog has the guard active — keystrokes
    // (especially Ctrl+A) should target dialog inputs, not the background.
    if (isDialogGuardActive()) return

    // Undo-send countdown takes priority: Esc / Cmd-Z cancels the pending
    // send, Cmd-Shift-Enter dispatches it immediately (user-approved).
    if (pendingSends.length > 0) {
      const ps = pendingSends[0]
      if (e.key === 'Escape' && !ps.dispatching) {
        e.preventDefault()
        void pendingCancel(ps)
        return
      }
      if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 'z' && !ps.dispatching) {
        e.preventDefault()
        void pendingCancel(ps)
        return
      }
      if ((e.ctrlKey || e.metaKey) && !e.altKey && e.shiftKey && e.key === 'Enter') {
        e.preventDefault()
        void pendingDispatch(ps)
        return
      }
    }

    // macOS-standard Preferences shortcut. Allow this even when an input has
    // focus so users can reach settings from anywhere in the main window.
    if (e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey && e.code === 'Comma') {
      e.preventDefault()
      sidebarRef?.openSettings()
      return
    }

    // `?` opens the keyboard shortcuts overlay from every rail pane. Not while
    // typing (inInput) and not while the composer is open, so the composer
    // keeps full ownership of its keys. The overlay sets the dialog guard,
    // so a second `?` while it is open is swallowed by the check above.
    if (!inInput && !showComposer && KEY.SHORTCUTS_HELP(e)) {
      e.preventDefault()
      openShortcutsOverlay()
      return
    }

    // Cmd+P / Ctrl+P opens the command palette (Alfred style).
    if (!showComposer && KEY.COMMAND_PALETTE(e)) {
      e.preventDefault()
      openCommandPalette()
      return
    }

    // Extension shortcut dispatch: when the active rail pane is NOT mail, let
    // the extension's registered shortcuts run first. dispatchExtensionShortcut
    // returns true when a handler matched — in that case we treat the event as
    // handled and skip mail's downstream dispatch. Skipped while typing in an
    // input element (consistent with mail's inInput guard below) so extension
    // shortcuts don't fire from inside text fields. See [[extension-sdk-pattern]]
    // and frontend/src/lib/stores/extensionShortcuts.svelte.ts.
    if (!inInput && dispatchExtensionShortcut(e)) {
      e.preventDefault()
      e.stopPropagation()
      return
    }

    // When composer is open, only handle Escape (composer handles its own shortcuts)
    if (showComposer) {
      // Block compose/reply shortcuts that might conflict
      if ((e.ctrlKey || e.metaKey) && ['r', 'f'].includes(e.key.toLowerCase())) {
        e.preventDefault()
        return
      }
      return
    }

    // Handle Ctrl/Cmd shortcuts.
    //
    // Layout: GLOBAL cases first (fire regardless of which rail pane is active),
    // then a guard that returns when an extension is the active rail pane, then
    // MAIL-DOMAIN cases (fire only on mail). The kit's components (extension UI)
    // handle their own list/sidebar shortcuts via local tabindex+keydown +
    // stopPropagation, so those events never reach this handler in the first
    // place. The guard catches the case where Ctrl+R / Ctrl+K / etc. fire
    // with no kit pane DOM-focused but an extension is the active rail pane —
    // we don't want those acting on the (hidden) mail UI.
    const isMailActive = () => getActiveExtension() === 'mail'
    if ((e.ctrlKey || e.metaKey) && !LAYOUT_SYMBOL(e)) {
      // GLOBAL Ctrl/Cmd shortcuts — fire regardless of active rail pane.
      switch (e.key.toLowerCase()) {
        case 'q':
          e.preventDefault()
          handleQuit()
          return
        case 'tab':
        case '`': {
          // When mail workspaces are open, Ctrl/Cmd+Tab cycles those tabs;
          // Ctrl/Cmd+Shift+Tab reverses. Ctrl+` remains the fallback reverse
          // binding on WebKit builds that consume Shift+Tab before keydown.
          if (isMailActive() && mailWorkspaceTabs.length > 0 && e.key.toLowerCase() === 'tab') {
            e.preventDefault()
            const order = ['primary', ...mailWorkspaceTabs.map(tab => tab.id)]
            const currentIndex = Math.max(0, order.indexOf(activeMailWorkspace))
            const step = e.shiftKey ? -1 : 1
            switchMailWorkspace(order[(currentIndex + step + order.length) % order.length])
            return
          }
          // Cycle through rail items: Mail + enabled extensions.
          // Ctrl+Tab        → forward
          // Ctrl+`          → backward
          // (Ctrl+Shift+Tab is intercepted by webkit2gtk before the
          //  keydown event reaches us, so we use Ctrl+` for backward.)
          e.preventDefault()
          const tabs = getRailTabs()
          const order = ['mail', ...tabs.map(t => t.extensionId)]
          if (order.length <= 1) return // only Mail — nothing to cycle
          const current = getActiveExtension()
          const idx = order.indexOf(current)
          const step = e.key === '`' ? -1 : 1
          const next = (idx + step + order.length) % order.length
          setActiveExtension(order[next])
          return
        }
      }

      // Ctrl+S (no shift) — focus the active pane's search input.
      // Mail dispatches to messageListRef; extensions dispatch via the
      // pane-nav registry. Ctrl+Shift+S (sync folder) is mail-only and
      // stays in the mail-domain switch below.
      if (e.key.toLowerCase() === 's' && !e.shiftKey) {
        e.preventDefault()
        if (isMailActive()) {
          messageListRef?.toggleSearchFocus()
          setFocusedPane('messageList')
          return
        }
        getPaneNav('messageList')?.focusSearch?.()
        setFocusedPane('messageList')
        return
      }

      // Below: MAIL-DOMAIN Ctrl/Cmd shortcuts. Guarded so they no-op when an
      // extension is the active rail pane (otherwise Ctrl+R would silently
      // reply to the hidden mail thread while the user is browsing contacts).
      if (!isMailActive()) return

      // MAIL-DOMAIN Ctrl/Cmd cases (guarded above).
      switch (e.key.toLowerCase()) {
        case 'n':
          // Ctrl/Cmd+N — new mail composer. Mail-domain only: when an
          // extension rail is active (e.g., calendar), that extension's
          // shortcut registry handles Ctrl+N before we reach the global
          // switch, opening its own "new X" dialog.
          e.preventDefault()
          handleCompose()
          return
        case 'r': {
          if (!hasConversation) return
          e.preventDefault()
          if (focusedPane === 'viewer' && viewerRef?.hasFocusedMessage()) {
            if (e.shiftKey) {
              viewerRef.replyAll()
              return
            }
            viewerRef.reply()
            return
          }
          const msgId = getLastMessageId()
          if (!msgId) return
          handleReply(e.shiftKey ? 'reply-all' : 'reply', msgId, viewerRef?.isImagesLoaded(msgId) || false)
          return
        }
        case 'f': {
          if (!hasConversation) return
          e.preventDefault()
          if (focusedPane === 'viewer' && viewerRef?.hasFocusedMessage()) {
            viewerRef.forward()
            return
          }
          const msgId = getLastMessageId()
          if (msgId) handleReply('forward', msgId, viewerRef?.isImagesLoaded(msgId) || false)
          return
        }
        case 's':
          // Ctrl+S (no shift) is handled globally above. Only Ctrl+Shift+S
          // (sync current folder) is mail-domain and lives here.
          if (!e.shiftKey) return
          e.preventDefault()
          messageListRef?.toggleFolderSync()
          return
        case 'a':
          // Preserve the browser's native Select All inside text fields.
          // The application-level Ctrl/Cmd+A shortcut is only for mail panes.
          if (!e.shiftKey && inInput) return

          if (e.shiftKey) {
            // Ctrl-Shift-A: Toggle sync all accounts (start sync or cancel if already running)
            e.preventDefault()
            sidebarRef?.toggleSync()
            return
          }
          // Ctrl-A: Select all text in viewer, or select all messages in list
          e.preventDefault()
          if (focusedPane === 'viewer') {
            viewerRef?.selectAllText()
            return
          }
          messageListRef?.selectAll()
          return
        case 'z':
          e.preventDefault()
          handleGlobalUndo()
          return
        case 'l':
          e.preventDefault()
          if (e.shiftKey) {
            // Ctrl-Shift-L: Open "Always Load" dropdown
            viewerRef?.openAlwaysLoadDropdown()
          } else {
            // Ctrl-L: Load images for this message
            viewerRef?.loadImages()
          }
          return
        case 'u':
          e.preventDefault()
          runListAction(e.shiftKey ? 'mail.markUnread' : 'mail.markRead')
          return
        case 'k':
          e.preventDefault()
          runListAction('mail.archive')
          return
        case 'j':
          e.preventDefault()
          runListAction('mail.spam')
          return
      }
      return
    }

    // Right Alt or ContextMenu key: open context menu for focused item in current pane
    // Left Alt + Right Alt: always open folder context menu regardless of pane
    if (e.key === 'ContextMenu' || (e.key === 'Alt' && e.code === 'AltRight')) {
      e.preventDefault()

      // Left Alt + Right Alt combo: always target the selected folder
      if (leftAltHeld || focusedPane === 'sidebar') {
        if (!selectedFolderId) return
        const folderEl = document.querySelector(
          `[data-sidebar-item="folder"][data-folder-id="${selectedFolderId}"], ` +
          `[data-sidebar-item="unified-account"][data-folder-id="${selectedFolderId}"]`
        ) as HTMLElement | null
        if (!folderEl) return
        const rect = folderEl.getBoundingClientRect()
        folderEl.dispatchEvent(new MouseEvent('contextmenu', {
          bubbles: true,
          clientX: rect.right,
          clientY: rect.top + rect.height / 2,
        }))
        focusContextMenu()
        return
      }

      switch (focusedPane) {
        case 'messageList': {
          messageListRef?.openContextMenu()
          focusContextMenu()
          return
        }
        case 'viewer': {
          viewerRef?.openContextMenu()
          focusContextMenu()
          return
        }
      }
      return
    }

    // Handle Alt shortcuts (pane/folder navigation, always work)
    if (e.altKey && !LAYOUT_SYMBOL(e)) {
      // Pane navigation is meaningless in focus mode (other panes hidden)
      if (focusMode !== 'off') return

      // Pane focus cycling (shared predicate — kit's pane components react to
      // the same focusedPane store, so cycling works uniformly across mail
      // and extensions).
      if (KEY.PANE_FOCUS_PREV(e)) {
        e.preventDefault()
        if (isResponsive()) {
          const view = getResponsiveView()
          const mode = getLayoutMode()
          if (view === 'viewer') {
            hideViewer()
            return
          }
          if (mode === 'narrow' && view === 'default') {
            showSidebar()
            return
          }
        }
        focusPreviousPane()
        return
      }
      if (KEY.PANE_FOCUS_NEXT(e)) {
        e.preventDefault()
        if (isResponsive()) {
          const view = getResponsiveView()
          const mode = getLayoutMode()
          if (mode === 'narrow' && view === 'sidebar') {
            hideSidebar()
            return
          }
          if (view === 'default' && selectedThreadId) {
            showViewer()
            return
          }
        }
        focusNextPane()
        return
      }

      // Sidebar item navigation (Alt+Up/Down/J/K). Dispatches to mail's
      // concrete ref when mail is active; otherwise to the kit pane that
      // registered as 'sidebar' via registerPaneNav. This way extensions
      // get the same "global Alt+J/K navigates the sidebar regardless of
      // which pane is currently DOM-focused" behavior mail has.
      if (KEY.SIDEBAR_PREV(e)) {
        e.preventDefault()
        if (isMailActive()) {
          sidebarRef?.selectPreviousFolder()
          return
        }
        getPaneNav('sidebar')?.navigatePrev?.()
        return
      }
      if (KEY.SIDEBAR_NEXT(e)) {
        e.preventDefault()
        if (isMailActive()) {
          sidebarRef?.selectNextFolder()
          return
        }
        getPaneNav('sidebar')?.navigateNext?.()
        return
      }

      // Alt+G / Alt+Shift+G — jump to the first sidebar item (All Inboxes
      // when present) or the last visible one (last account header when all
      // folder trees are collapsed).
      if (KEY.SIDEBAR_FIRST(e)) {
        e.preventDefault()
        if (isMailActive()) {
          sidebarRef?.selectFirstFolder()
          return
        }
        getPaneNav('sidebar')?.navigateFirst?.()
        return
      }
      if (KEY.SIDEBAR_LAST(e)) {
        e.preventDefault()
        if (isMailActive()) {
          sidebarRef?.selectLastFolder()
          return
        }
        getPaneNav('sidebar')?.navigateLast?.()
        return
      }

      // Alt+M / Alt+C — move/copy folder picker for the keyboard-focused
      // message. From the focused viewer this opens the picker for the open
      // conversation instead of the message list. Open path only: the picker
      // sets the dialog guard, so the close/switch press is handled pre-guard
      // near the top of this handler.
      if (KEY.LIST_MOVE_TO(e)) {
        if (!isMailActive()) return
        e.preventDefault()
        if (focusedPane === 'viewer' && selectedThreadId) {
          viewerRef?.toggleFolderPicker('move')
          return
        }
        messageListRef?.toggleMoveToDialog()
        return
      }
      if (KEY.LIST_COPY_TO(e)) {
        if (!isMailActive()) return
        e.preventDefault()
        if (focusedPane === 'viewer' && selectedThreadId) {
          viewerRef?.toggleFolderPicker('copy')
          return
        }
        messageListRef?.toggleCopyToDialog()
        return
      }

      // Alt+Enter — mail sidebar expand/collapse. Keep inline switch for
      // single residual case.
      switch (e.key) {
        case 'Enter':
          // Toggle expand/collapse for focused account header or selected folder with children
          if (sidebarRef?.hasFocusedAccount()) {
            e.preventDefault()
            sidebarRef.toggleFocusedAccount()
          } else if (sidebarRef?.hasSelectedFolderWithChildren()) {
            e.preventDefault()
            sidebarRef.toggleSelectedFolderCollapse()
          }
          return
      }
      return
    }

    // Skip single-key shortcuts if in input field
    if (inInput) return

    // Handle Escape (context-dependent, progressive)
    // Focus mode first, then responsive overlays, then checkboxes, then conversation
    if (e.key === 'Escape') {
      if (focusMode !== 'off') {
        focusMode = 'off'
        focusedMessageIdInFocus = null
        return
      }
      if (isResponsive() && getResponsiveView() === 'viewer') {
        hideViewer()
        return
      }
      if (isResponsive() && getResponsiveView() === 'sidebar') {
        hideSidebar()
        return
      }
      if (messageListRef?.hasCheckedMessages()) {
        // First: clear checkboxes
        messageListRef.clearChecked()
      } else if (selectedThreadId) {
        // Second: close conversation viewer
        selectedThreadId = null
        selectedConversationFolderId = null
        selectedConversationAccountId = null
      }
      return
    }

    // Handle pane-focused navigation shortcuts.
    //
    // Mail-domain: these run for the mail UI's focused pane. The kit's
    // components handle their own list/sidebar navigation via local
    // keydown + stopPropagation (so the events never reach this handler).
    // Guard for safety in case an event slips through while an extension
    // is the active rail pane.
    if (KEY.LIST_PREV(e) || KEY.LIST_PREV_CHECK(e)) {
      if (!isMailActive()) return
      e.preventDefault()
      if (focusedPane === 'sidebar') {
        sidebarRef?.selectPreviousFolder()
      } else if (focusedPane === 'messageList') {
        if (e.shiftKey) {
          messageListRef?.selectPreviousWithCheck()
        } else {
          messageListRef?.selectPrevious()
        }
      } else if (focusedPane === 'viewer') {
        viewerRef?.scrollUp()
      }
      return
    }
    if (KEY.LIST_NEXT(e) || KEY.LIST_NEXT_CHECK(e)) {
      if (!isMailActive()) return
      e.preventDefault()
      if (focusedPane === 'sidebar') {
        sidebarRef?.selectNextFolder()
      } else if (focusedPane === 'messageList') {
        if (e.shiftKey) {
          messageListRef?.selectNextWithCheck()
        } else {
          messageListRef?.selectNext()
        }
      } else if (focusedPane === 'viewer') {
        viewerRef?.scrollDown()
      }
      return
    }

    // Enter and Space — domain-specific button-vs-pane disambiguation logic
    // stays as inline switch (kit components handle their own Enter/Space
    // locally so they don't depend on this dispatch).
    switch (e.key) {
      case 'Enter':
      case 'o':
        // A focused native button owns Enter. Do not swallow its activation
        // based on the app's separate pane-navigation state.
        if (document.activeElement?.tagName === 'BUTTON') {
          return
        }
        if (focusedPane === 'sidebar' && sidebarRef?.hasFocusedAccount()) {
          e.preventDefault()
          sidebarRef.toggleFocusedAccount()
        } else if (focusedPane === 'sidebar' && sidebarRef?.hasSelectedFolderWithChildren()) {
          e.preventDefault()
          sidebarRef.toggleSelectedFolderCollapse()
        } else if (focusedPane === 'messageList') {
          e.preventDefault()
          messageListRef?.openSelected()
        }
        return
      case ' ':  // Space - toggle checkbox on focused message, or expand/collapse account
      case 'x':
        // A focused native button owns Space as well.
        if (document.activeElement?.tagName === 'BUTTON') {
          return
        }
        if (focusedPane === 'sidebar' && sidebarRef?.hasFocusedAccount()) {
          sidebarRef.toggleFocusedAccount()
        } else if (focusedPane === 'sidebar' && sidebarRef?.hasSelectedFolderWithChildren()) {
          sidebarRef.toggleSelectedFolderCollapse()
        } else if (focusedPane === 'messageList') {
          messageListRef?.toggleCheck()
        }
        return
    }

    // g / Shift+G — jump to first / last loaded message (shared predicates —
    // kit list panes consume the same KEY.LIST_FIRST/LIST_LAST locally)
    if (KEY.LIST_FIRST(e)) {
      if (!isMailActive()) return
      if (focusedPane !== 'messageList') return
      e.preventDefault()
      messageListRef?.selectFirst()
      return
    }
    if (KEY.LIST_LAST(e)) {
      if (!isMailActive()) return
      if (focusedPane !== 'messageList') return
      e.preventDefault()
      messageListRef?.selectLast()
      return
    }

    // Single-key shortcuts (Gmail standard shortcuts)
    switch (e.key) {
      case 'c':
        e.preventDefault()
        handleCompose()
        return
      case 'e':
        e.preventDefault()
        if (focusedPane === 'viewer' && selectedThreadId) {
          viewerRef?.archive()
          return
        }
        runListAction('mail.archive')
        return
      case 'v':
        if (!isMailActive()) return
        // Reader-only tabs own their viewer shortcut handler. Let that
        // embedded viewer open the picker instead of routing to the hidden
        // primary conversation viewer.
        if (activeMailWorkspace !== 'primary') return
        e.preventDefault()
        if (focusedPane === 'viewer' && selectedThreadId) {
          viewerRef?.toggleFolderPicker('move')
          return
        }
        messageListRef?.toggleMoveToDialog()
        return
      case 'r': {
        if (!hasConversation) return
        e.preventDefault()
        if (focusedPane === 'viewer' && viewerRef?.hasFocusedMessage()) {
          viewerRef.reply()
          return
        }
        const msgId = getLastMessageId()
        if (msgId) handleReply('reply', msgId, viewerRef?.isImagesLoaded(msgId) || false)
        return
      }
      case 'a': {
        if (!hasConversation) return
        e.preventDefault()
        if (focusedPane === 'viewer' && viewerRef?.hasFocusedMessage()) {
          viewerRef.replyAll()
          return
        }
        const msgId = getLastMessageId()
        if (msgId) handleReply('reply-all', msgId, viewerRef?.isImagesLoaded(msgId) || false)
        return
      }
      case 'f': {
        if (!hasConversation) return
        e.preventDefault()
        if (focusedPane === 'viewer' && viewerRef?.hasFocusedMessage()) {
          viewerRef.forward()
          return
        }
        const msgId = getLastMessageId()
        if (msgId) handleReply('forward', msgId, viewerRef?.isImagesLoaded(msgId) || false)
        return
      }
      case '/':
        e.preventDefault()
        if (isMailActive()) { showGlobalSearch = true; return }
        if (isMailActive()) {
          messageListRef?.toggleSearchFocus()
          setFocusedPane('messageList')
        }
        return
      case '!':
        e.preventDefault()
        if (focusedPane === 'viewer' && selectedThreadId) {
          viewerRef?.spam()
          return
        }
        runListAction('mail.spam')
        return
      case 'z':
        e.preventDefault()
        handleGlobalUndo()
        return
      case 'm':
        if (!KEY.LIST_TOGGLE_READ(e)) return
        runListAction('mail.toggleRead')
        return
      case 'I':
        e.preventDefault()
        if (focusedPane === 'viewer' && selectedThreadId) {
          viewerRef?.markRead()
          return
        }
        runListAction('mail.markRead')
        return
      case 'U':
        e.preventDefault()
        if (focusedPane === 'viewer' && selectedThreadId) {
          viewerRef?.markUnread()
          return
        }
        runListAction('mail.markUnread')
        return
      case 'u':
        if (selectedThreadId) {
          e.preventDefault()
          selectedThreadId = null
          selectedConversationFolderId = null
          selectedConversationAccountId = null
          setFocusedPane('messageList')
        }
        return
      case 's':
        if (focusedPane === 'viewer' && selectedThreadId) {
          e.preventDefault()
          viewerRef?.toggleStar()
          return
        }
        runListAction('mail.star')
        return
      case 'F': {
        // Shift+F: toggle focus mode
        if (!hasConversation) return
        e.preventDefault()
        toggleThreadFocus()
        return
      }
      case '#':
      case 'd': // alias of Delete: move focused/checked message(s) to Trash
      case 'Backspace':
      case 'Delete': {
        if (focusedPane === 'viewer' && viewerRef?.hasFocusedMessage()) {
          if (PERMANENT_DELETE(e)) {
            viewerRef.deletePermanently()
            return
          }
          viewerRef.trash()
          return
        }
        runListAction('mail.trash', { permanent: PERMANENT_DELETE(e) })
        return
      }
    }
  }

  // Get the last message ID from the current conversation (for reply/forward)
  function getLastMessageId(): string | null {
    return viewerRef?.getLastMessageId() ?? null
  }

  // Handle click on pane to set focus
  function handlePaneClick(pane: FocusablePane) {
    setFocusedPane(pane)
  }

  function handleMessageListActionComplete(autoSelectNext: boolean) {
    // Archive, Done, delete and spam remove the current conversation from the
    // active folder. Clear the viewer before the list reloads so it cannot
    // request that now-missing thread and render an empty "(No subject)" pane.
    if (autoSelectNext) {
      selectedThreadId = null
      selectedConversationFolderId = null
      selectedConversationAccountId = null
    } else {
      viewerRef?.refreshFlags()
    }
  }

  // Bulk action handlers
  function showTargetedUndoToast(message: string, operationID: string, messageIds: string[], action: string, undoable = true) {
    const canUndo = undoable && typeof operationID === 'string' && operationID.length > 0
    if (canUndo) publishUndoOperationCreated(operationID, messageIds, action)
    let toastId = ''
    const actions = canUndo
      ? [{ label: $_('common.undo'), onClick: () => { void handleTargetedUndo(toastId, operationID) } }]
      : []
    toastId = toasts.success(message, actions)
  }

  async function handleTargetedUndo(toastId: string, operationID: string) {
    if (!operationID || targetedUndoInFlight.has(operationID)) return
    targetedUndoInFlight.add(operationID)
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
      targetedUndoInFlight.delete(operationID)
    }
  }

  // --- Shared list-action layer (P-029) -------------------------------------
  // One resolver and one executor for every surface that acts on "the selected
  // messages": the key handlers above and the command panel. Checked messages
  // win, then the keyboard-focused row (see lib/actions/target.ts).
  function currentListTarget(): ListTarget {
    return resolveListTarget({
      hasChecked: () => messageListRef?.hasCheckedMessages() ?? false,
      checkedIds: () => messageListRef?.getCheckedMessageIds() ?? [],
      checkedRefs: () => messageListRef?.getCheckedMessageRefs() ?? [],
      focusedIds: () => messageListRef?.getSelectedMessageIds() ?? [],
      focusedRefs: () => messageListRef?.getSelectedMessageRefs() ?? [],
      checkedHasUnstarred: () => messageListRef?.getCheckedHasUnstarred() ?? false,
      selectedStarred: () => messageListRef?.isSelectedStarred() ?? false,
      checkedHasUnread: () => messageListRef?.getCheckedHasUnread() ?? false,
      selectedUnread: () => messageListRef?.isSelectedUnread() ?? false,
    })
  }

  async function resolveActionTarget(target: ActionTarget): Promise<string[]> {
    // The backend validates account ownership and resolves whole-thread targets
    // to message IDs. Existing mutations then retain their undo and sync paths.
    return ResolveMailActionTarget(operations.Target.createFrom(target))
  }

  const listActionExecutor: MessageActionExecutor = {
    archive: (ids) => handleBulkArchive(ids),
    spam: (ids) => handleBulkSpam(ids),
    markRead: (ids) => handleBulkMarkRead(ids),
    markUnread: (ids) => handleBulkMarkUnread(ids),
    toggleStar: (ids, star) => handleBulkToggleStar(ids, star),
    trash: (ids, permanent) => messageListRef?.requestDelete(ids, permanent),
    applyTarget: async (actionId, target, options, desiredState) => {
      try {
        const ids = await resolveActionTarget(target)
        switch (actionId) {
          case 'mail.archive': return handleBulkArchive(ids)
          case 'mail.spam': return handleBulkSpam(ids)
          case 'mail.markRead': return handleBulkMarkRead(ids)
          case 'mail.markUnread': return handleBulkMarkUnread(ids)
          case 'mail.star': return handleBulkToggleStar(ids, desiredState === true)
          case 'mail.trash': return messageListRef?.requestDelete(ids, options?.permanent === true)
        }
      } catch (error) {
        console.error('Mail action target validation failed:', error)
        const errorKey = actionId === 'mail.archive' ? 'toast.failedToArchive'
          : actionId === 'mail.spam' ? 'toast.failedToMarkAsSpam'
            : actionId === 'mail.markRead' ? 'toast.failedToMarkAsRead'
              : actionId === 'mail.markUnread' ? 'toast.failedToMarkAsUnread'
                : actionId === 'mail.star' ? 'toast.failedToUpdateStar'
                  : 'toast.failedToDelete'
        toasts.error($_(errorKey))
      }
    },
  }

  function runListAction(id: string, options?: RunOptions) {
    return runActionOnTarget(id, currentListTarget(), listActionExecutor, options)
  }

  function listActionAvailable(id: string): boolean {
    const def = getListAction(id)
    return def ? def.isAvailable({ target: currentListTarget() }) : false
  }

  async function handleBulkArchive(messageIds: string[]) {
    try {
      await runMessageMutation(messageIds, async () => {
        const result = await ArchiveWithUndo(messageIds)
        if (result.coalesced) return
        showTargetedUndoToast($_('toast.archived'), result.operationId, messageIds, 'archive')
        messageListRef?.clearChecked()
        messageListRef?.handleActionComplete(true)
      })
    } catch (err) {
      console.error('Archive failed:', err)
      addToast({ type: 'error', message: $_('toast.failedToArchive') })
    }
  }

  async function handleBulkSpam(messageIds: string[]) {
    try {
      await runMessageMutation(messageIds, async () => {
        const isSpamFolder = selectedFolderType === 'spam'

        if (isSpamFolder) {
          // If we're in spam folder, mark as NOT spam
          const result = await MarkAsNotSpamWithUndo(messageIds)
          if (result.coalesced) return
          showTargetedUndoToast($_('toast.markedAsNotSpam'), result.operationId, messageIds, 'mark-as-not-spam')
        } else {
          // Otherwise, mark as spam
          const result = await MarkAsSpamWithUndo(messageIds)
          if (result.coalesced) return
          const toastMsg = result.movedToSpam ? $_('toast.markedAsSpam') : $_('toast.deletedFromFolder')
          showTargetedUndoToast(toastMsg, result.operationId, messageIds, 'mark-as-spam', result.movedToSpam)
        }

        messageListRef?.clearChecked()
        messageListRef?.handleActionComplete(true)
      })
    } catch (err) {
      const isSpamFolder = selectedFolderType === 'spam'
      console.error('Spam toggle failed:', err)
      addToast({ type: 'error', message: $_(isSpamFolder ? 'toast.failedToMarkAsNotSpam' : 'toast.failedToMarkAsSpam') })
    }
  }

  async function handleBulkMarkRead(messageIds: string[]) {
    try {
      await runMessageMutation(messageIds, async () => {
        await MarkAsRead(messageIds)
        lastFlagUndo = { messageIds: [...messageIds], restoreRead: false }
        addToast({ type: 'success', message: $_('toast.markedAsRead'), duration: 10_000, actions: [{ label: $_('common.undo'), onClick: handleUndoLastFlag }] })
        messageListRef?.clearChecked()
        messageListRef?.handleActionComplete()
      })
    } catch (err) {
      console.error('Mark as read failed:', err)
      addToast({ type: 'error', message: $_('toast.failedToMarkAsRead') })
    }
  }

  async function handleUndoLastFlag() {
    if (!lastFlagUndo) return
    const { messageIds, restoreRead } = lastFlagUndo
    try {
      await runMessageMutation(messageIds, async () => {
        lastFlagUndo = null
        if (restoreRead) {
          await MarkAsRead(messageIds)
          addToast({ type: 'success', message: $_('toast.markedAsRead') })
        } else {
          await MarkAsUnread(messageIds)
          addToast({ type: 'success', message: $_('toast.markedAsUnread') })
        }
        messageListRef?.handleActionComplete()
      })
    } catch (err) {
      // Keep the target available when a transient IMAP/local error occurs.
      lastFlagUndo = { messageIds, restoreRead }
      console.error('Undo flag change failed:', err)
      addToast({ type: 'error', message: $_(restoreRead ? 'toast.failedToMarkAsRead' : 'toast.failedToMarkAsUnread') })
    }
  }

  async function handleBulkMarkUnread(messageIds: string[]) {
    try {
      await runMessageMutation(messageIds, async () => {
        await MarkAsUnread(messageIds)
        lastFlagUndo = { messageIds: [...messageIds], restoreRead: true }
        addToast({ type: 'success', message: $_('toast.markedAsUnread'), duration: 10_000, actions: [{ label: $_('common.undo'), onClick: handleUndoLastFlag }] })
        messageListRef?.clearChecked()
        messageListRef?.handleActionComplete()
      })
    } catch (err) {
      console.error('Mark as unread failed:', err)
      addToast({ type: 'error', message: $_('toast.failedToMarkAsUnread') })
    }
  }

  async function handleBulkToggleStar(messageIds: string[], shouldStar: boolean) {
    try {
      await runMessageMutation(messageIds, async () => {
        if (shouldStar) {
          await Star(messageIds)
          addToast({ type: 'success', message: $_('toast.starred') })
        } else {
          await Unstar(messageIds)
          addToast({ type: 'success', message: $_('toast.starRemoved') })
        }
        messageListRef?.clearChecked()
        messageListRef?.handleActionComplete()
      })
    } catch (err) {
      console.error('Star toggle failed:', err)
      addToast({ type: 'error', message: $_('toast.failedToUpdateStar') })
    }
  }

  // Cmd-Z/Ctrl-Z and Edit > Undo share the global LIFO path; history rows and
  // action toasts use operation-scoped UndoOperation instead.
  async function handleGlobalUndo() {
    if (globalUndoInFlight) return
    globalUndoInFlight = true
    try {
      const result = await UndoLatestWithResult()
      publishUndoOperationCompleted(result.operationId)
      addToast({ type: 'success', message: $_('toast.undone', { values: { description: result.description } }) })
    } catch (err) {
      console.error('Undo failed:', err)
      addToast({ type: 'error', message: $_('toast.undoFailed') })
    } finally {
      globalUndoInFlight = false
    }
  }
</script>

<svelte:window onmousemove={handleMouseMove} onmouseup={handleMouseUp} onkeydown={handleGlobalKeyDown} onkeyup={handleGlobalKeyUp} />

<div class="flex flex-col h-full w-full overflow-hidden bg-background">
  <!-- Custom Title Bar -->
  {#if getShowTitleBar() && !getNativeTitleBar() && effectiveTitleBarMode === 'custom'}
    <TitleBar onClose={handleClose} />
  {/if}

  <!-- Main Content -->
  <div class="flex flex-1 min-h-0 overflow-hidden relative">
    <ExtensionRail onRevealContext={revealContextPane} onHideContext={scheduleContextPaneHide} />

    {#if getActiveExtension() === 'scheduled'}
      <ScheduledPane />
    {/if}

    {#if getActiveExtension() === 'contacts'}
      <ContactsPane />
    {/if}

    {#if getActiveExtension() === 'calendar'}
      <CalendarPane />
    {/if}

    <!-- Mail layout is ALWAYS mounted; only its visibility is toggled when an
         extension takes over the pane. Unmounting+remounting the mail tree on
         every extension switch was leaking state (zombie listeners) and pinning
         the main thread on the second mount. display:contents keeps the flex
         children as direct flex items so the layout doesn't shift. -->
    <div style:display={getActiveExtension() === 'mail' ? 'contents' : 'none'}>
    <!-- Sidebar (Folder List) -->
    <aside
      class="spark-sidebar-host flex-col {getLayoutMode() === 'narrow' ? `responsive-sidebar-overlay w-72 border-r border-border bg-background ${getResponsiveView() === 'sidebar' ? 'responsive-sidebar-visible' : ''}` : 'flex-shrink-0 border-r border-border bg-muted/30'}"
      style="{getLayoutMode() === 'full' ? `width: ${listWidth}px` : ''}"
      style:display={contextPaneVisible && contextPaneMode === 'folders' ? 'flex' : 'none'}
      onmouseenter={revealContextPane}
      onmouseleave={scheduleContextPaneHide}
      role="presentation"
      onclick={() => handlePaneClick('sidebar')}
    >
      <div class="grid min-h-10 shrink-0 grid-cols-[repeat(4,minmax(0,1fr))_auto] items-center gap-1 overflow-hidden border-b border-border px-2 py-1" aria-label={$_('messageList.quickFilters')}>
        <button type="button" class="min-w-0 rounded-full border px-1.5 py-1 text-center text-[11px] leading-tight font-medium {triageStatus === 'all' ? 'border-primary bg-primary/15 text-primary' : 'border-border text-muted-foreground hover:bg-muted'}" aria-pressed={triageStatus === 'all'} onclick={() => selectTriageFilter('')}>{$_('triage.all')}</button>
        <button type="button" class="min-w-0 rounded-full border px-1.5 py-1 text-center text-[11px] leading-tight font-medium {triageStatus === 'unread' ? 'border-primary bg-primary/15 text-primary' : 'border-border text-muted-foreground hover:bg-muted'}" aria-pressed={triageStatus === 'unread'} onclick={() => selectTriageFilter('unread')}>{$_('triage.unread')}</button>
        <button type="button" class="min-w-0 rounded-full border px-1.5 py-1 text-center text-[11px] leading-tight font-medium {triageStatus === 'readLater' ? 'border-primary bg-primary/15 text-primary' : 'border-border text-muted-foreground hover:bg-muted'}" aria-pressed={triageStatus === 'readLater'} onclick={() => selectTriageFilter('readLater')}>{$_('triage.readLater')}</button>
        <button type="button" class="min-w-0 rounded-full border px-1.5 py-1 text-center text-[11px] leading-tight font-medium {triageStatus === 'replyLater' ? 'border-primary bg-primary/15 text-primary' : 'border-border text-muted-foreground hover:bg-muted'}" aria-pressed={triageStatus === 'replyLater'} onclick={() => selectTriageFilter('replyLater')}>{$_('triage.replyLater')}</button>
        <button type="button" class="ml-auto shrink-0 rounded p-1.5 text-muted-foreground hover:bg-muted" aria-pressed={contextPaneAutoHide} title={contextPaneAutoHide ? $_('triage.keepContextVisible') : $_('triage.autoHideContext')} onclick={toggleContextPaneAutoHide}><Icon icon={contextPaneAutoHide ? 'mdi:pin-outline' : 'mdi:pin-off-outline'} width="16" /></button>
      </div>
      <Sidebar
        navigationEnabled={true}
        onGlobalSearch={() => showGlobalSearch = true}
        bind:this={sidebarRef}
        onFolderSelect={handleFolderSelect}
        onUnifiedFolderSelect={handleUnifiedFolderSelect}
        onCompose={handleCompose}
        onUnifiedInboxSelect={handleUnifiedInboxSelect}
        onUnifiedSpecialFolderSelect={handleUnifiedSpecialFolderSelect}
        onMessagesMoved={() => messageListRef?.handleActionComplete(false)}
        selectedAccountId={selectedAccountId}
        selectedFolderId={selectedFolderId}
        selectionSource={selectionSource}
        isFocused={activeMailWorkspace === 'primary' && getFocusedPane() === 'sidebar'}
        isFlashing={isPaneFlashing('sidebar')}
        showBackButton={getLayoutMode() === 'narrow'}
        onBack={hideSidebar}
        collapsed={false}
        showCollapseControl={false}
      />
    </aside>

    <!-- Scrim for narrow sidebar overlay -->
    {#if getLayoutMode() === 'narrow'}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <div
        role="button"
        tabindex="-1"
        class="responsive-scrim {getResponsiveView() === 'sidebar' ? 'responsive-scrim-visible' : ''}"
        onclick={hideSidebar}
        aria-label={$_('aria.closeSidebar')}
      ></div>
    {/if}

    <!-- Sidebar Resize Handle -->
    <!-- Message List -->
    <section
      bind:this={messageListContainerRef}
      class="spark-mail-list-pane min-h-0 flex flex-col overflow-hidden {isResponsive() ? 'flex-1 min-w-0 border-r border-border bg-background' : 'flex-shrink-0 border-r border-border bg-background'}"
      style="{getLayoutMode() === 'full' ? `width: ${listWidth}px` : ''}"
      style:display={contextPaneVisible && contextPaneMode === 'messages' ? 'flex' : 'none'}
      onmouseenter={revealContextPane}
      onmouseleave={scheduleContextPaneHide}
      role="presentation"
      data-pane="messageList"
      tabindex="-1"
      onclick={() => handlePaneClick('messageList')}
    >
      <div class="grid min-h-10 shrink-0 grid-cols-[repeat(4,minmax(0,1fr))_auto] items-center gap-1 overflow-hidden border-b border-border px-2 py-1" aria-label={$_('messageList.quickFilters')}>
        <button type="button" class="min-w-0 rounded-full border px-1.5 py-1 text-center text-[11px] leading-tight font-medium {triageStatus === 'all' ? 'border-primary bg-primary/15 text-primary' : 'border-border text-muted-foreground hover:bg-muted'}" aria-pressed={triageStatus === 'all'} onclick={() => selectTriageFilter('')}>{$_('triage.all')}</button>
        <button type="button" class="min-w-0 rounded-full border px-1.5 py-1 text-center text-[11px] leading-tight font-medium {triageStatus === 'unread' ? 'border-primary bg-primary/15 text-primary' : 'border-border text-muted-foreground hover:bg-muted'}" aria-pressed={triageStatus === 'unread'} onclick={() => selectTriageFilter('unread')}>{$_('triage.unread')}</button>
        <button type="button" class="min-w-0 rounded-full border px-1.5 py-1 text-center text-[11px] leading-tight font-medium {triageStatus === 'readLater' ? 'border-primary bg-primary/15 text-primary' : 'border-border text-muted-foreground hover:bg-muted'}" aria-pressed={triageStatus === 'readLater'} onclick={() => selectTriageFilter('readLater')}>{$_('triage.readLater')}</button>
        <button type="button" class="min-w-0 rounded-full border px-1.5 py-1 text-center text-[11px] leading-tight font-medium {triageStatus === 'replyLater' ? 'border-primary bg-primary/15 text-primary' : 'border-border text-muted-foreground hover:bg-muted'}" aria-pressed={triageStatus === 'replyLater'} onclick={() => selectTriageFilter('replyLater')}>{$_('triage.replyLater')}</button>
        <button type="button" class="ml-auto shrink-0 rounded p-1.5 text-muted-foreground hover:bg-muted" aria-pressed={contextPaneAutoHide} title={contextPaneAutoHide ? $_('triage.keepContextVisible') : $_('triage.autoHideContext')} onclick={toggleContextPaneAutoHide}><Icon icon={contextPaneAutoHide ? 'mdi:pin-outline' : 'mdi:pin-off-outline'} width="16" /></button>
      </div>
      <div class="flex-1 min-h-0">
        <MessageList
          bind:this={messageListRef}
          accountId={selectedAccountId}
          folderId={selectedFolderId}
          folderName={selectedFolderName}
          folderType={selectedFolderType || 'inbox'}
          onConversationSelect={handleConversationSelect}
          onOpenInTab={openListThreadInTab}
          onReply={handleReply}
          onRowActionComplete={handleMessageListActionComplete}
          onBulkMarkRead={handleBulkMarkRead}
          onBulkAction={(id, options) => runListAction(id, options)}
          isFocused={activeMailWorkspace === 'primary' && getFocusedPane() === 'messageList'}
          isFlashing={isPaneFlashing('messageList')}
          showFolderToggle={true}
          onToggleSidebar={showFolderBrowser}
          showQuickFilters={false}
        />
      </div>
    </section>

    <!-- List Resize Handle -->
    {#if getLayoutMode() === 'full'}
    <button
      type="button"
      class="w-1 cursor-col-resize hover:bg-primary/20 active:bg-primary/40 transition-colors border-0 p-0 {isResizingList
        ? 'bg-primary/40'
        : ''}"
      onmousedown={startResizeList}
      aria-label={$_('aria.resizeMessageList')}
    ></button>
    {/if}

    <!-- Conversation Viewer -->
    <main
      class="spark-viewer-pane flex flex-col min-h-0 overflow-hidden {viewerIsOverlay ? `responsive-viewer-overlay bg-background ${viewerIsVisible ? 'responsive-viewer-visible' : ''}` : 'flex-1 min-w-0 bg-background'}"
      role="presentation"
      data-pane="viewer"
      onclick={() => handlePaneClick('viewer')}
    >
      <div class="flex min-h-10 shrink-0 items-center gap-1 overflow-hidden border-b border-border px-2 py-1">
        <div class="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto" role="tablist" aria-label={$_('mailWindow.workspaces')}>
          {#each mailWorkspaceTabs.filter(tab => tab.kind === 'reader') as tab (tab.id)}
            <div class="flex shrink-0 items-center rounded {activeMailWorkspace === tab.id ? 'bg-muted' : 'hover:bg-muted/60'}">
              <button type="button" role="tab" aria-selected={activeMailWorkspace === tab.id} class="max-w-48 truncate px-3 py-1.5 text-sm {activeMailWorkspace === tab.id ? 'font-medium' : 'text-muted-foreground'}" title={workspaceLabel(tab.scope, true)} onclick={() => switchMailWorkspace(tab.id)}>{workspaceLabel(tab.scope, true)}</button>
              {#if activeMailWorkspace === tab.id}
                <button type="button" class="rounded p-1 text-muted-foreground hover:bg-background" title={$_('mailWindow.detachTab')} aria-label={$_('mailWindow.detachTab')} onclick={() => void detachWorkspaceTab(tab.id)}><Icon icon="mdi:open-in-new" width="16" /></button>
              {/if}
              <button type="button" class="rounded p-1 text-muted-foreground hover:bg-background" title={$_('mailWindow.closeTab')} aria-label={$_('mailWindow.closeTab')} onclick={() => closeWorkspaceTab(tab.id)}><Icon icon="mdi:close" width="16" /></button>
            </div>
          {/each}
        </div>
        <button class="flex shrink-0 items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground hover:bg-muted" onclick={() => OpenMailWindow(selectedAccountId || '', selectedFolderId || '', selectedFolderName, selectedFolderType || 'inbox', selectedThreadId || '', selectedConversationAccountId || '', selectedConversationFolderId || '', messageListRef?.getSearchQuery() || '').catch(error => toasts.error(String(error)))}>
            <Icon icon="mdi:open-in-new" width="16" />{$_('mailWindow.newWindow')}
        </button>
      </div>
      {#if activeMailWorkspace === 'primary'}
      <ConversationViewer
        bind:this={viewerRef}
        threadId={selectedThreadId}
        folderId={selectedConversationFolderId}
        folderType={selectedFolderType}
        accountId={selectedConversationAccountId}
        onReply={handleReply}
        onComposeToAddress={handleComposeToAddress}
        onEditDraft={handleEditDraft}
        onActionComplete={(autoSelectNext) => messageListRef?.handleActionComplete(autoSelectNext)}
        isFocused={activeMailWorkspace === 'primary' && getFocusedPane() === 'viewer'}
        isFlashing={isPaneFlashing('viewer')}
        showBackButton={isResponsive()}
        onBack={() => { focusMode = 'off'; focusedMessageIdInFocus = null; hideViewer() }}
        isViewerVisible={activeMailWorkspace === 'primary' && getActiveExtension() === 'mail' && (focusMode !== 'off' || !isResponsive() || getResponsiveView() === 'viewer')}
        deferMarkAsReadUntilLeave={triageStatus === 'unread' || (selectedFolderType === 'virtual' && virtualFoldersStore.folders.find(item => item.id === selectedFolderId)?.readStatus === 'unread')}
        inFocusMode={focusMode !== 'off'}
        focusModeKind={focusMode === 'off' ? null : focusMode}
        focusedMessageIdInFocus={focusedMessageIdInFocus}
        onToggleThreadFocus={toggleThreadFocus}
        onToggleMessageFocus={toggleMessageFocus}
      />
      {:else}
        {#each mailWorkspaceTabs.filter(tab => tab.kind === 'reader') as tab (tab.id)}
          <div class="flex w-full flex-1 min-w-0 min-h-0 overflow-hidden" style:display={activeMailWorkspace === tab.id ? 'flex' : 'none'}>
            <MailWindowApp bind:this={mailWindowTabInstances[tab.id]} config={tab.scope} embedded readerTab deferMarkAsReadUntilLeave={triageStatus === 'unread' || (tab.scope.folderType === 'virtual' && virtualFoldersStore.folders.find(item => item.id === tab.scope.folderId)?.readStatus === 'unread')} onCloseTab={() => closeWorkspaceTab(tab.id)} active={activeMailWorkspace === tab.id} onScopeChange={scope => updateWorkspaceScope(tab.id, scope)} onNewTab={openWorkspaceTab} onOpenInTab={openReaderScope} />
          </div>
        {/each}
      {/if}
    </main>
    </div>
    {#if showComposer && composerAccountId}
      <div class="compact-composer-host" style:--composer-left-clearance={getLayoutMode() === 'full' ? `${(contextPaneVisible ? listWidth : 0) + 64}px` : '16px'}>
        <Composer
          accountId={composerAccountId}
          initialMessage={composerInitialMessage}
          draftId={composerDraftId}
          imagesLoaded={composerImagesLoaded}
          onClose={closeComposer}
          onSent={closeComposer}
          variant="compact"
        />
      </div>
    {/if}
  </div>
</div>

<!-- Resize cursor overlay when dragging -->
{#if isResizingList}
  <div class="fixed inset-0 cursor-col-resize z-50"></div>
{/if}

<!-- New-release notice stays out of the way of launch-critical dialogs. -->
<UpdateAvailableNotice hidden={showTermsDialog || showOAuthMissingDialog || showWhatsNewDialog || showCertDialog || showFlatpakFsDialog} />

<!-- Toast notifications -->
<ToastContainer />

<!-- Spellcheck right-click suggestion menu (composer) -->
<SpellSuggestionMenu />

<!-- Per-extension settings dialog dispatcher (Settings → Extensions → Edit) -->
<ExtensionSettingsDialog />

<!-- Keyboard shortcuts overlay (`?`, sidebar entry). Mounted only while open
     so no inactive focus-trap root lingers in the DOM. -->
{#if shortcutsOverlay.open}
  <ShortcutsDialog />
{/if}
<UndoHistoryPanel bind:open={showUndoHistory} />

<!-- Command palette (Cmd+P / Ctrl+P, Alfred style quick launcher). -->
{#if pendingSends.length > 0}
  <div class="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 flex items-center gap-3 rounded-full border border-border bg-popover px-4 py-2 shadow-lg" role="status" aria-live="polite">
    <Icon icon="mdi:send-clock-outline" class="w-4 h-4 text-muted-foreground" />
    <span class="text-sm">{$_('pendingSend.countdown', { values: { seconds: pendingRemaining(pendingSends[0]) } })}</span>
    <div class="h-1.5 w-24 overflow-hidden rounded-full bg-muted">
      <div class="h-full bg-primary transition-all" style="width: {Math.min(100, (pendingRemaining(pendingSends[0]) / 30) * 100)}%"></div>
    </div>
    <button
      class="rounded-full px-2.5 py-1 text-sm font-medium hover:bg-muted disabled:opacity-50"
      disabled={pendingSends[0].dispatching}
      onclick={() => void pendingCancel(pendingSends[0])}
    >{$_('pendingSend.cancel')}</button>
  </div>
{/if}

<CommandPalette />

<!-- Shutdown Overlay -->
{#if isShuttingDown}
  <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/80">
    <p class="text-white/90 text-sm font-medium">{$_('window.shuttingDown')}</p>
  </div>
{/if}

<!-- Launch-critical dialogs are mounted only while active. Keeping inactive
     portal/focus-trap roots out of the DOM prevents a stale invisible overlay
     from intercepting pointer input during first-run startup. -->
{#if showTermsDialog}
  <TermsDialog bind:open={showTermsDialog} onAccept={handleTermsAccepted} />
{/if}

<!-- Launch-time OAuth credentials warning. Shows on every launch when one
     or more provider credentials weren't compiled in, unless the user
     opts out via "Don't show again". -->
{#if showOAuthMissingDialog}
  <OAuthMissingDialog
    bind:open={showOAuthMissingDialog}
    oauthStatus={oauthBuildStatus}
    onDismiss={dismissOAuthWarning}
  />
{/if}

<!-- Per-version release announcement. OK click records acknowledgement;
     closing without OK leaves the version unrecorded so the dialog
     fires again next launch. -->
{#if showWhatsNewDialog}
  <WhatsNewDialog
    bind:open={showWhatsNewDialog}
    version={whatsNewVersion}
    lastSeen={whatsNewLastSeen}
    history={whatsNewHistory}
    onAcknowledge={acknowledgeWhatsNew}
    onClose={closeWhatsNewHistory}
  />
{/if}

<!-- Certificate TOFU Dialog (for background sync cert errors) -->
<CertificateDialog
  bind:open={showCertDialog}
  certificate={pendingCertificate}
  onAcceptOnce={handleBgCertAcceptOnce}
  onAcceptPermanently={handleBgCertAcceptPermanently}
  onDecline={handleBgCertDecline}
/>

<!-- Flatpak Filesystem Permission Dialog -->
<AlertDialog.Root bind:open={showFlatpakFsDialog}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{$_('attachment.flatpakOpenTitle')}</AlertDialog.Title>
      <AlertDialog.Description>
        <p class="mb-3">{$_('attachment.flatpakOpenDescription')}</p>
        <pre class="mb-3 rounded bg-muted p-2 text-sm overflow-x-auto"><code>flatpak override --user --filesystem=home io.github.wesleiaqui.eternomail</code></pre>
        <p class="mb-3 text-sm text-destructive">{$_('attachment.flatpakOpenSecurityWarning')}</p>
        <p class="text-sm text-muted-foreground">{$_('attachment.flatpakOpenAlternative')}</p>
      </AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Action onclick={() => showFlatpakFsDialog = false}>{$_('common.ok')}</AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>

<style>
  .compact-composer-host {
    position: absolute;
    z-index: 40;
    right: 20px;
    bottom: 20px;
    width: min(720px, calc(100% - var(--composer-left-clearance) - 20px));
    height: min(540px, calc(100% - 40px));
    min-width: 0;
    overflow: hidden;
    border-radius: 12px;
    box-shadow: 0 8px 28px rgb(0 0 0 / 18%);
  }
  @media (min-width: 800px) and (max-width: 1199px) {
    .compact-composer-host { width: min(600px, calc(100% - var(--composer-left-clearance) - 20px)); }
  }
  @media (max-width: 799px) {
    .compact-composer-host {
      right: 8px;
      bottom: 8px;
      width: calc(100% - 16px);
      height: min(540px, calc(100% - 16px));
    }
  }
</style>

<GlobalSearch bind:open={showGlobalSearch} onSelect={(thread, folder, account) => { handleConversationSelect(thread, folder, account); setFocusedPane('viewer') }} />
