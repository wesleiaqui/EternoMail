<script lang="ts">
  import Icon from '@iconify/svelte'
  import NavigationCards from './NavigationCards.svelte'
  import { replyLaterQueue } from '$lib/stores/replyLater.svelte'
  import { navigationCards } from '$lib/stores/navigationCards.svelte'
  import type { NavigationEntry } from '$lib/sidebar/cards'
  import { folderChoices, filterFolders } from '$lib/sidebar/folderSearch'
  import { onMount } from 'svelte'
  import AccountSection from './AccountSection.svelte'
  import AccountDialog from '$lib/components/settings/AccountDialog.svelte'
  import DeleteAccountDialog from '$lib/components/settings/DeleteAccountDialog.svelte'
  import SettingsDialog from '$lib/components/settings/SettingsDialog.svelte'
  import VirtualFolderDialog from './VirtualFolderDialog.svelte'
  import ConfirmDialog from '$lib/components/ui/confirm-dialog/ConfirmDialog.svelte'
  import { Button } from '$lib/components/ui/button'
  import Avatar from '$lib/components/kit/Avatar.svelte'
  import { accountStore } from '$lib/stores/accounts.svelte'
  import { contactSourcesStore } from '$lib/stores/contactSources.svelte'
  import { contactPhotos } from '$lib/stores/contactPhotos.svelte'
  import { isAccountExpanded, setAccountExpanded, setFolderCollapsed, getUIState, getUIStateVersion, saveUIState, setActiveExtension } from '$lib/stores/uiState.svelte'
  import { setFocusedPane } from '$lib/stores/keyboard.svelte'
  import { withShortcut, ariaKeyshortcuts } from '$lib/keyboard/format'
  import { openShortcutsOverlay } from '$lib/stores/shortcutsOverlay.svelte'
  import { openCommandPalette } from '$lib/stores/commandPalette.svelte'
  import { _ } from '$lib/i18n'
  // @ts-ignore - wailsjs path
  import { account, folder } from '../../../../wailsjs/go/models'
  // @ts-ignore - wailsjs path
  import { getSidebarWidth } from '$lib/stores/uiState.svelte'
  import { virtualFoldersStore } from '$lib/stores/virtualFolders.svelte'
  import { sidebarPins } from '$lib/stores/sidebarPins.svelte'
  import type { PinnedFolder } from '$lib/sidebar/pins'
  import { sidebarLayout } from '$lib/stores/sidebarLayout.svelte'
  import { isFolderFavorite, favoritesFull, toggleFolderFavorite } from '$lib/sidebar/favorites'
  import type { virtualfolder } from '../../../../wailsjs/go/models'
  import { SyncFolders } from '../../../../wailsjs/go/app/App'

  // Folder item type for flat navigation list
  interface FolderNavItem {
    navigation?: NavigationEntry
    type: 'unified' | 'unified-account' | 'account-header' | 'folder'
    accountId?: string
    folderId?: string
    folderPath?: string
    folderName: string
    folderType?: string
  }

  // Track focused account header for keyboard navigation
  let focusedAccountId = $state<string | null>(null)

  // Ref to scrollable container for auto-scroll
  let scrollContainer: HTMLDivElement | null = null

  // Track expanded state for each account (reactive, synced with persisted state)
  let expandedAccounts = $state<Record<string, boolean>>({})

  // Initialize expanded state from persisted storage
  // Depends on both accounts list AND UI state version (so it re-runs when persisted state loads)
  $effect(() => {
    // Read version to create dependency - effect re-runs when UI state finishes loading
    const _version = getUIStateVersion()

    const newExpanded: Record<string, boolean> = {}
    for (const acc of accountStore.accounts) {
      newExpanded[acc.account.id] = isAccountExpanded(acc.account.id)
    }
    expandedAccounts = newExpanded
  })

  // Toggle account expansion
  function toggleAccountExpanded(accountId: string) {
    const newValue = !expandedAccounts[accountId]
    expandedAccounts[accountId] = newValue
    setAccountExpanded(accountId, newValue)
  }

  // Track collapsed state for folders with children (reactive, synced with persisted state)
  let collapsedFolders = $state<Record<string, boolean>>({})

  // Initialize collapsed state from persisted storage and prune stale entries
  $effect(() => {
    const _version = getUIStateVersion()

    // Collect all folder IDs that exist in current accounts
    const allFolderIds = new Set<string>()
    const collectIds = (trees: folder.FolderTree[]) => {
      for (const tree of trees) {
        if (tree.folder) allFolderIds.add(tree.folder.id)
        if (tree.children) collectIds(tree.children)
      }
    }
    for (const acc of accountStore.accounts) {
      collectIds(acc.folders || [])
    }

    // Read persisted collapsed state, keep only entries for existing folders
    const persisted = getUIState().collapsedFolders
    const newCollapsed: Record<string, boolean> = {}
    let hasStale = false
    for (const folderId of Object.keys(persisted)) {
      if (allFolderIds.has(folderId)) {
        newCollapsed[folderId] = persisted[folderId]
      } else {
        hasStale = true
      }
    }
    collapsedFolders = newCollapsed

    // Persist cleaned state if stale entries were pruned
    if (hasStale) {
      saveUIState({ collapsedFolders: newCollapsed })
    }
  })

  // Toggle folder collapse
  function toggleFolderCollapsed(folderId: string) {
    const isCurrentlyCollapsed = collapsedFolders[folderId] !== false
    const newValue = !isCurrentlyCollapsed
    collapsedFolders = { ...collapsedFolders, [folderId]: newValue }
    setFolderCollapsed(folderId, newValue)
  }

  interface Props {
    onGlobalSearch?: () => void
    onFolderSelect?: (accountId: string, folderId: string, folderPath: string, folderName: string, folderType: string) => void
    onUnifiedFolderSelect?: (accountId: string, folderId: string, folderPath: string, folderName: string, folderType: string) => void
    onUnifiedInboxSelect?: () => void
    onUnifiedSpecialFolderSelect?: (type: string, displayName: string) => void
    onCompose?: () => void
    onMessagesMoved?: () => void
    selectedAccountId?: string | null
    selectedFolderId?: string | null
    selectionSource?: 'unified' | 'account' | null
    isFocused?: boolean
    isFlashing?: boolean
    showBackButton?: boolean
    onBack?: () => void
    collapsed?: boolean
    onToggleCollapsed?: () => void
    navigationEnabled?: boolean
  }

  let {
    onGlobalSearch,
    onFolderSelect,
    onUnifiedFolderSelect,
    onUnifiedInboxSelect,
    onUnifiedSpecialFolderSelect,
    onCompose,
    onMessagesMoved,
    selectedAccountId = null,
    selectedFolderId = null,
    selectionSource = null,
    isFocused: _isFocused = false,
    isFlashing = false,
    showBackButton = false,
    onBack,
    collapsed = false,
    onToggleCollapsed,
    navigationEnabled = true,
  }: Props = $props()

  // Dialog state
  let showAccountDialog = $state(false)
  let showDeleteDialog = $state(false)
  let showSettingsDialog = $state(false)
  let showAllFolders = $state(false)
  let allFoldersQuery = $state('')
  let allFoldersSearchInput = $state<HTMLInputElement | null>(null)
  const allFolderChoices = $derived(accountStore.accounts.flatMap(item => folderChoices(item.account.id, item.account.name, item.account.email, item.folders)))
  const matchingFolders = $derived(filterFolders(allFolderChoices, allFoldersQuery))
  $effect(() => {
    if (!showAllFolders) { allFoldersQuery = ''; return }
    if (!allFoldersSearchInput) return
    const timer = setTimeout(() => allFoldersSearchInput?.focus(), 0)
    return () => clearTimeout(timer)
  })
  let showVirtualFolderDialog = $state(false)
  let editingVirtualFolder = $state<virtualfolder.VirtualFolder | null>(null)
  let showDeleteVirtualFolderDialog = $state(false)
  let deletingVirtualFolder = $state<virtualfolder.VirtualFolder | null>(null)
  // Presets use the saved expanded width as their durable value. This keeps
  // manual resizing and the Settings selector in one source of truth.
  const sidebarDensity = $derived(getSidebarWidth() >= 350 ? 'sidebar-density-large' : getSidebarWidth() >= 310 ? 'sidebar-density-medium' : 'sidebar-density-compact')
  const settingsAccount = $derived(accountStore.accounts[0]?.account ?? null)
  const settingsPhoto = $derived(contactPhotos.get(settingsAccount?.email ?? ''))
  let editingAccount = $state<account.Account | null>(null)
  let deletingAccount = $state<account.Account | null>(null)

  export function openSettings() {
    showSettingsDialog = true
  }

  $effect(() => {
    const emails = accountStore.accounts.map(item => item.account.email).filter(Boolean)
    if (emails.length) void contactPhotos.ensure(emails)
  })

  // Load accounts and contact sources on mount
  onMount(() => {
    // Load accounts, then trigger comprehensive sync on launch
    accountStore.load().then(async () => {
      try {
        await accountStore.syncAllComplete()
      } catch (err) {
        console.error('Failed to sync on launch:', err)
      }
    })

    contactSourcesStore.load()
    void virtualFoldersStore.load()
    void replyLaterQueue.load().catch(() => {})
  })

  $effect(() => {
    if (!navigationEnabled) return
    const navigate = (event: Event) => selectNavigationEntry((event as CustomEvent<NavigationEntry>).detail)
    window.addEventListener('postilaatikko:navigate', navigate)
    return () => window.removeEventListener('postilaatikko:navigate', navigate)
  })

  // Pins are loaded with the UI state, which can finish after this sidebar mounts.
  $effect(() => {
    void getUIStateVersion()
    sidebarPins.sync()
    sidebarLayout.sync()
  })

  // CSS order of a sidebar block. Home has no order and sorts first; blocks
  // follow in the saved order, and the bottom controls are pinned to the end.
  $effect(() => {
    void getUIStateVersion()
    const entries: NavigationEntry[] = [
      { id: 'inboxes', kind: 'inboxes', name: $_('navigation.allInboxes'), ...folderCounts(item => item.type === 'inbox') },
      { id: 'replyLater', kind: 'system', folderType: 'replyLater', name: $_('replyLater.title'), count: replyLaterQueue.ready ? replyLaterQueue.count : undefined },
      ...[...primaryFolders, ...secondaryFolders].map(item => ({ id: item.type, kind: 'system' as const, name: $_(item.label), folderType: item.type, ...folderCounts(folder => folder.type === item.type) })),
      ...sidebarPins.list.map(pin => ({ id: `folder:${pin.accountId}:${pin.folderId}`, kind: 'folder' as const, accountId: pin.accountId, folderId: pin.folderId, name: pin.name, path: pin.path, ...countsForFolder(pin.accountId, pin.folderId) })),
      ...virtualFoldersStore.folders.map(item => ({ id: `virtual:${item.id}`, kind: 'virtual' as const, folderId: item.id, name: item.name, unreadCount: virtualFoldersStore.unreadCounts[item.id] || 0 })),
    ]
    navigationCards.sync(entries)
  })
  function selectNavigationEntry(entry: NavigationEntry) {
    setActiveExtension('mail')
    if (entry.kind === 'inboxes') { handleUnifiedInboxSelect(); return }
    if (entry.kind === 'system') { selectPrimaryFolder(entry.folderType || 'inbox'); return }
    if (entry.kind === 'virtual') {
      const vf = virtualFoldersStore.folders.find(item => item.id === entry.folderId)
      if (vf) selectVirtualFolder(vf)
      return
    }
    selectPinnedFolder({ accountId: entry.accountId!, folderId: entry.folderId!, name: entry.name, path: entry.path || entry.name })
  }

  function folderCounts(match: (item: folder.Folder) => boolean): { totalCount: number; unreadCount: number } {
    let totalCount = 0
    let unreadCount = 0
    const visit = (trees: folder.FolderTree[]) => {
      for (const tree of trees) {
        if (tree.folder && match(tree.folder)) {
          totalCount += tree.folder.totalCount || 0
          unreadCount += tree.folder.unreadCount || 0
        }
        if (tree.children) visit(tree.children)
      }
    }
    for (const acc of accountStore.accounts) visit(acc.folders || [])
    return { totalCount, unreadCount }
  }

  function countsForFolder(accountId: string | undefined, folderId: string | undefined) {
    const account = accountStore.accounts.find(item => item.account.id === accountId)
    if (!account || !folderId) return { totalCount: 0, unreadCount: 0 }
    const visit = (trees: folder.FolderTree[]): folder.Folder | null => {
      for (const tree of trees) {
        if (tree.folder?.id === folderId) return tree.folder
        const child = visit(tree.children || [])
        if (child) return child
      }
      return null
    }
    const item = visit(account.folders || [])
    return { totalCount: item?.totalCount || 0, unreadCount: item?.unreadCount || 0 }
  }

  // Opens a pinned folder. Its type comes from the loaded folder tree so special
  // folders keep their behavior; a folder that has gone missing opens as a plain folder.
  function selectPinnedFolder(pin: PinnedFolder) {
    const account = accountStore.accounts.find(item => item.account?.id === pin.accountId)
    const found = account ? findFolderById(account.folders || [], pin.folderId) : null
    handleFolderSelect(pin.accountId, pin.folderId, pin.path, pin.name, found?.type || 'folder')
  }

  function findFolderById(trees: folder.FolderTree[], id: string): folder.Folder | null {
    for (const tree of trees) {
      if (tree.folder?.id === id) return tree.folder
      const child = findFolderById(tree.children || [], id)
      if (child) return child
    }
    return null
  }

  function selectVirtualFolder(item: virtualfolder.VirtualFolder) {
    onFolderSelect?.('virtual', item.id, '', item.name, 'virtual')
  }

  function openVirtualFolderDialog(item: virtualfolder.VirtualFolder | null = null) {
    pendingVirtualFolderQuery = ''
    pendingVirtualFolderFolderId = null
    editingVirtualFolder = item
    showVirtualFolderDialog = true
  }

  async function refreshVirtualFolderFolders() {
    const gmailAccounts = accountStore.accounts.filter(item =>
      item.account.enabled && item.account.imapHost?.toLowerCase() === 'imap.gmail.com',
    )
    const outcomes = await Promise.allSettled(gmailAccounts.map(async item => {
      try {
        await SyncFolders(item.account.id)
      } finally {
        await accountStore.loadFolders(item.account.id)
      }
    }))
    if (outcomes.some(outcome => outcome.status === 'rejected')) {
      throw new Error('One or more Gmail label lists could not be refreshed')
    }
  }

  // "Save as virtual folder" requests arrive from the message list's search
  // bar via the store; this sidebar owns the dialog.
  let pendingVirtualFolderQuery = $state('')
  let pendingVirtualFolderFolderId = $state<string | null>(null)
  $effect(() => {
    const request = virtualFoldersStore.saveSearchRequest
    if (!request) return
    virtualFoldersStore.clearSaveSearchRequest()
    editingVirtualFolder = null
    pendingVirtualFolderQuery = request.query
    pendingVirtualFolderFolderId = request.folderId
    showVirtualFolderDialog = true
  })

  // Asks in the app's own dialog. The browser's native confirm dialog returns
  // false in the macOS webview (Wails does not provide the JavaScript confirm
  // panel), which made every delete silently do nothing.
  function requestDeleteVirtualFolder(item: virtualfolder.VirtualFolder) {
    deletingVirtualFolder = item
    showDeleteVirtualFolderDialog = true
  }

  async function confirmDeleteVirtualFolder() {
    const item = deletingVirtualFolder
    deletingVirtualFolder = null
    if (!item) return
    try {
      await virtualFoldersStore.remove(item.id)
      if (selectedAccountId === 'virtual' && selectedFolderId === item.id) onUnifiedInboxSelect?.()
    } catch (error) {
      console.error('Failed to delete virtual folder:', error)
    }
  }

  // Handle unified inbox selection (All Inboxes)
  function handleUnifiedInboxSelect() {
    onUnifiedInboxSelect?.()
  }

  // Handle folder selection
  function handleFolderSelect(accountId: string, folderId: string, folderPath: string, folderName: string, folderType: string) {
    accountStore.selectFolder(accountId, folderId, folderPath, folderName)
    onFolderSelect?.(accountId, folderId, folderPath, folderName, folderType)
  }

  function selectPrimaryFolder(type: string) {
    const label = type === 'replyLater' ? 'replyLater.title' : [...primaryFolders, ...secondaryFolders].find(item => item.type === type)?.label || type
    onUnifiedSpecialFolderSelect?.(type, label)
  }

  function openAllFolders(): void {
    // The folders panel intentionally does not render over the compact rail.
    // Expand first so "More" has the same useful result in both layouts.
    if (collapsed) {
      onToggleCollapsed?.()
      showAllFolders = true
      return
    }
    showAllFolders = !showAllFolders
  }

  const primaryFolders = [
    { type: 'starred', label: 'sidebar.starred', icon: 'mdi:pin-outline' },
    { type: 'drafts', label: 'sidebar.drafts', icon: 'mdi:file-outline' },
    { type: 'sent', label: 'sidebar.sent', icon: 'mdi:send-outline' },
    { type: 'trash', label: 'sidebar.trash', icon: 'mdi:delete-outline' },
  ]

  const secondaryFolders = [
    { type: 'archive', label: 'sidebar.archived', icon: 'mdi:check' },
    { type: 'spam', label: 'sidebar.blocked', icon: 'mdi:thumb-down-outline' },
  ]

  // Open add account dialog
  function openAddAccount() {
    editingAccount = null
    showAccountDialog = true
  }

  // Open edit account dialog
  function openEditAccount(acc: account.Account) {
    editingAccount = acc
    showAccountDialog = true
  }

  // Open delete confirmation
  function openDeleteAccount(acc: account.Account) {
    deletingAccount = acc
    showDeleteDialog = true
  }

  // Sync all accounts (comprehensive sync)
  export async function syncAllAccounts() {
    try {
      await accountStore.syncAllComplete()
    } catch (err) {
      console.error('Sync failed:', err)
      // Error is already stored in account store
    }
  }

  // Cancel all running syncs
  export async function cancelSync() {
    try {
      await accountStore.cancelAllSyncs()
    } catch (err) {
      console.error('Failed to cancel sync:', err)
    }
  }

  // Toggle sync (start if not running, cancel if running) - for keyboard shortcut
  export async function toggleSync() {
    if (accountStore.isAnySyncing) {
      await cancelSync()
    } else {
      await syncAllAccounts()
    }
  }
  function buildFolderNavList(): FolderNavItem[] {
    return navigationCards.state.cards.filter(c => !c.hidden && !c.collapsed).flatMap(c => c.entries).flatMap(id => {
      const entry = navigationCards.entries.find(e => e.id === id)
      return entry ? [{ type: 'folder' as const, folderId: entry.folderId || entry.id, folderName: entry.name, navigation: entry }] : []
    })
  }


  // Get current folder index in navigation list
  function getCurrentFolderIndex(): number {
    const navList = buildFolderNavList()

    // Check if an account header is focused
    if (focusedAccountId) {
      return navList.findIndex(item =>
        item.type === 'account-header' && item.accountId === focusedAccountId
      )
    }

    // Check if Unified Inbox is selected (All Inboxes)
    if (selectedAccountId === 'unified') {
      return navList.findIndex(item => item.type === 'unified')
    }

    // Check selectionSource to find the correct item
    if (selectionSource === 'unified') {
      // Looking for unified-account item
      return navList.findIndex(item =>
        item.type === 'unified-account' && item.folderId === selectedFolderId
      )
    } else {
      // Looking for regular folder item
      return navList.findIndex(item =>
        item.type === 'folder' && item.folderId === selectedFolderId
      )
    }
  }

  // Navigate to previous folder (exposed for keyboard navigation)
  export function selectPreviousFolder() {
    const navList = buildFolderNavList()
    if (navList.length === 0) return

    const currentIndex = getCurrentFolderIndex()
    const newIndex = currentIndex <= 0 ? 0 : currentIndex - 1

    selectFolderByIndex(navList, newIndex)
  }

  // Navigate to next folder (exposed for keyboard navigation)
  export function selectNextFolder() {
    const navList = buildFolderNavList()
    if (navList.length === 0) return

    const currentIndex = getCurrentFolderIndex()
    const newIndex = currentIndex >= navList.length - 1 ? navList.length - 1 : currentIndex + 1

    selectFolderByIndex(navList, newIndex)
  }

  // Jump to the top sidebar item — All Inboxes when present (Alt+G)
  export function selectFirstFolder() {
    const navList = buildFolderNavList()
    if (navList.length === 0) return
    selectFolderByIndex(navList, 0)
  }

  // Jump to the last visible item: last folder of the last expanded account,
  // or the last account header when all trees are collapsed (Alt+Shift+G)
  export function selectLastFolder() {
    const navList = buildFolderNavList()
    if (navList.length === 0) return
    selectFolderByIndex(navList, navList.length - 1)
  }

  // Scroll an item into view
  function scrollItemIntoView(item: FolderNavItem) {
    if (!scrollContainer) return

    // Build selector based on item type
    let selector: string | null = null
    if (item.type === 'unified') {
      selector = '[data-sidebar-nav-item="unified"], [data-sidebar-item="unified"]'
    } else if (item.type === 'unified-account' && item.folderId) {
      selector = `[data-sidebar-item="unified-account"][data-folder-id="${item.folderId}"]`
    } else if (item.type === 'account-header' && item.accountId) {
      selector = `[data-sidebar-item="account-header"][data-account-id="${item.accountId}"]`
    } else if (item.type === 'folder' && item.folderId) {
      selector = `[data-sidebar-item="folder"][data-folder-id="${item.folderId}"]`
    }

    if (selector) {
      const element = scrollContainer.querySelector(selector)
      element?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  }

  // Select folder by index in nav list
  function selectFolderByIndex(navList: FolderNavItem[], index: number) {
    const item = navList[index]
    if (!item) return
    if (item.navigation) { selectNavigationEntry(item.navigation); return }

    // Clear account header focus when selecting a folder
    if (item.type !== 'account-header') {
      focusedAccountId = null
    }

    if (item.type === 'unified') {
      onUnifiedInboxSelect?.()
    } else if (item.type === 'unified-account' && item.accountId && item.folderId && item.folderPath) {
      // Select from unified section - uses onUnifiedFolderSelect
      onUnifiedFolderSelect?.(item.accountId, item.folderId, item.folderPath, item.folderName, item.folderType || 'inbox')
    } else if (item.type === 'account-header' && item.accountId) {
      // Focus on account header (Enter/Space will toggle expand)
      focusedAccountId = item.accountId
    } else if (item.type === 'folder' && item.accountId && item.folderId && item.folderPath) {
      // Select from account tree - uses handleFolderSelect
      handleFolderSelect(item.accountId, item.folderId, item.folderPath, item.folderName, item.folderType || 'folder')
    }

    // Scroll the selected item into view
    scrollItemIntoView(item)
  }

  // Toggle expand/collapse for the focused account (called on Enter/Space/Alt+Enter)
  export function toggleFocusedAccount() {
    if (focusedAccountId) {
      toggleAccountExpanded(focusedAccountId)
    }
  }

  // Check if an account header is focused
  export function hasFocusedAccount(): boolean {
    return focusedAccountId !== null
  }

  // Check if the currently selected folder has children
  export function hasSelectedFolderWithChildren(): boolean {
    if (!selectedFolderId || selectionSource !== 'account') return false
    return folderHasChildren(selectedFolderId)
  }

  // Toggle collapse for the currently selected folder
  export function toggleSelectedFolderCollapse(): void {
    if (!selectedFolderId || selectionSource !== 'account') return
    if (!folderHasChildren(selectedFolderId)) return
    toggleFolderCollapsed(selectedFolderId)
  }

  // Check if a folder has children by searching the account folder trees
  function folderHasChildren(folderId: string): boolean {
    for (const acc of accountStore.accounts) {
      const found = findTreeNode(acc.folders || [], folderId)
      if (found) return (found.children && found.children.length > 0) || false
    }
    return false
  }

  // Find a FolderTree node by folder ID
  function findTreeNode(trees: folder.FolderTree[], folderId: string): folder.FolderTree | null {
    for (const tree of trees) {
      if (tree.folder?.id === folderId) return tree
      if (tree.children) {
        const found = findTreeNode(tree.children, folderId)
        if (found) return found
      }
    }
    return null
  }
</script>

<div class="spark-sidebar spark-sidebar-rebuilt {sidebarDensity} {collapsed ? 'spark-sidebar-collapsed' : ''} relative flex flex-col h-full {isFlashing ? 'pane-focus-flash' : ''}">
  <div class="sidebar-rebuilt-toolbar">
    <button type="button" class="sidebar-compose-button" onclick={onCompose} title={withShortcut($_('sidebar.compose'), 'app.compose')} aria-label={$_('sidebar.compose')} aria-keyshortcuts={ariaKeyshortcuts('app.compose')}>
      <Icon icon="mdi:pencil-plus-outline" class="h-5 w-5" />
      <span data-sidebar-label>{$_('sidebar.compose')}</span>
    </button>
    {#if showBackButton}
      <button type="button" class="sidebar-toolbar-button" title={$_('responsive.back')} aria-label={$_('aria.closeSidebar')} onclick={onBack}>
        <Icon icon="mdi:close" class="h-5 w-5" />
      </button>
    {/if}
  </div>

  <div class="sidebar-content sidebar-rebuilt-content flex-1 overflow-y-auto scrollbar-thin" bind:this={scrollContainer}>
    {#if accountStore.loading}
      <div class="flex items-center justify-center py-8">
        <Icon icon="mdi:loading" class="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    {:else if accountStore.accounts.length === 0}
      <!-- Empty State -->
      <div class="flex flex-col items-center justify-center py-8 px-4 text-center">
        <Icon icon="mdi:email-plus-outline" class="w-12 h-12 text-muted-foreground mb-3" />
        <h3 class="text-sm font-medium mb-1">{$_('sidebar.noAccountsYet')}</h3>
        <p class="text-xs text-muted-foreground mb-4">
          {$_('sidebar.addFirstAccount')}
        </p>
        <Button size="sm" onclick={openAddAccount}>
          <Icon icon="mdi:plus" class="w-4 h-4 mr-1" />
          {$_('sidebar.addAccount')}
        </Button>
      </div>
    {:else}
      <nav class="sidebar-primary-navigation" aria-label={$_('sidebar.navigation')}>
        <div style:order="1" class:hidden={collapsed}>
          <button type="button" class="sidebar-primary-link" onclick={onGlobalSearch}><Icon icon="mdi:magnify" class="h-5 w-5" />{$_('search.global')}</button>
          <NavigationCards {selectedFolderId} onSelect={selectNavigationEntry}
            onEdit={(entry) => { const item = virtualFoldersStore.folders.find(f => f.id === entry.folderId); if (item) openVirtualFolderDialog(item) }}
            onDelete={(entry) => { const item = virtualFoldersStore.folders.find(f => f.id === entry.folderId); if (item) requestDeleteVirtualFolder(item) }} />
          <button type="button" class="sidebar-primary-link" onclick={() => openVirtualFolderDialog()}><Icon icon="mdi:plus" class="h-5 w-5" />{$_('navigation.addVirtual')}</button>

        </div>

        <button type="button" class="sidebar-more-button" style:order="100" onclick={openAllFolders} aria-expanded={showAllFolders}>
          <Icon icon="mdi:folder-multiple-outline" class="h-6 w-6" />
          <span data-sidebar-label>{$_('sidebar.allFolders')}</span>
        </button>
        <button type="button" class="sidebar-collapse-nav-button" style:order="100" onclick={onToggleCollapsed} title={collapsed ? $_('sidebar.expand') : $_('sidebar.collapse')} aria-label={collapsed ? $_('sidebar.expand') : $_('sidebar.collapse')}>
          <Icon icon={collapsed ? 'mdi:chevron-double-right' : 'mdi:chevron-double-left'} class="h-5 w-5" />
          <span data-sidebar-label>{collapsed ? $_('sidebar.expand') : $_('sidebar.collapse')}</span>
        </button>
      </nav>
    {/if}
  </div>

  <div class="sidebar-bottom-actions">
    <div class="sidebar-bottom-row" role="toolbar" aria-label={$_('sidebar.bottomActions')}>
      <button type="button" class="sidebar-bottom-button" onclick={toggleSync} title={withShortcut(accountStore.isAnySyncing ? $_('sidebar.clickToCancel') : $_('sidebar.syncAllAccounts'), 'app.syncAll')} aria-label={accountStore.isAnySyncing ? $_('sidebar.clickToCancel') : $_('sidebar.syncAllAccounts')}>
        <Icon icon={accountStore.isAnySyncing ? 'mdi:sync' : 'mdi:refresh'} class="h-5 w-5 {accountStore.isAnySyncing ? 'animate-spin' : ''}" />
      </button>

      <button type="button" class="sidebar-bottom-button" data-sidebar-command-palette onclick={openCommandPalette} title={withShortcut($_('commandPalette.title'), 'app.commandPalette')} aria-label={$_('commandPalette.title')} aria-keyshortcuts={ariaKeyshortcuts('app.commandPalette')} aria-haspopup="dialog">
        <Icon icon="mdi:console-line" class="h-5 w-5" />
      </button>

      <button type="button" class="sidebar-bottom-button" data-sidebar-shortcuts onclick={openShortcutsOverlay} title={withShortcut($_('shortcuts.open'), 'app.shortcuts')} aria-label={$_('shortcuts.open')} aria-keyshortcuts={ariaKeyshortcuts('app.shortcuts')} aria-haspopup="dialog">
        <Icon icon="mdi:keyboard-outline" class="h-5 w-5" />
      </button>

      <span class="flex-1"></span>

      <button class="sidebar-bottom-button sidebar-settings-entry" type="button" data-sidebar-settings onclick={() => (showSettingsDialog = true)} title={withShortcut($_('sidebar.settings'), 'app.preferences')} aria-label={$_('sidebar.settings')}>
        {#if settingsAccount}
          <Avatar email={settingsAccount.email} name={settingsAccount.name} size={24} photoData={settingsPhoto?.data} photoMediaType={settingsPhoto?.mediaType} />
        {:else}
          <Icon icon="mdi:cog-outline" class="h-5 w-5" />
        {/if}
        {#if contactSourcesStore.hasErrors}<span class="sidebar-settings-error"></span>{/if}
      </button>
    </div>
  </div>

  {#if showAllFolders}
    <div
      class="fixed inset-0 z-20 bg-black/20"
      onclick={() => (showAllFolders = false)}
      tabindex="-1"
      aria-hidden="true"
    ></div>
    <aside class="sidebar-all-folders-panel" aria-label={$_('sidebar.allFolders')}>
      <header class="sidebar-all-folders-header">
        <h2>{$_('sidebar.allFolders')}</h2>
        <button type="button" class="sidebar-toolbar-button" onclick={() => (showAllFolders = false)} aria-label={$_('aria.dismiss')}>
          <Icon icon="mdi:close" class="h-5 w-5" />
        </button>
      </header>
      <div class="px-4 pb-3 relative">
        <label class="sr-only" for="all-folders-search">{$_('contextMenu.searchFolders')}</label>
        <input id="all-folders-search" bind:this={allFoldersSearchInput} bind:value={allFoldersQuery}
          type="search" placeholder={$_('contextMenu.searchFolders')} class="w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground" />
      </div>
      <div class="sidebar-all-folders-scroll scrollbar-thin">
        {#if allFoldersQuery.trim()}
          {#each matchingFolders.slice(0, 200) as item (item.id)}
            <div class="group flex items-center rounded-md hover:bg-muted">
              <button type="button" class="min-w-0 flex-1 px-4 py-2 text-left rounded-md"
                onclick={() => { handleFolderSelect(item.accountId, item.id, item.path, item.name, item.type); showAllFolders = false }}>
                <span class="block text-sm text-foreground break-words">{item.path || item.name}</span>
                <span class="block text-xs text-muted-foreground">{item.accountName} · {item.accountEmail}</span>
              </button>
              <button type="button"
                class="mr-2 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded hover:bg-background focus-visible:opacity-100 {isFolderFavorite(item.accountId, item.id) ? 'text-amber-500' : 'text-muted-foreground/60 opacity-0 group-hover:opacity-100'}"
                title={$_('navigation.favorite')} aria-label={$_('navigation.favorite')}
                aria-pressed={isFolderFavorite(item.accountId, item.id)}
                disabled={!isFolderFavorite(item.accountId, item.id) && favoritesFull()}
                onclick={() => toggleFolderFavorite(item.accountId, { id: item.id, path: item.path, name: item.name })}>
                <Icon icon={isFolderFavorite(item.accountId, item.id) ? 'mdi:star' : 'mdi:star-outline'} class="h-4 w-4" />
              </button>
            </div>
          {:else}
            <p class="p-4 text-sm text-muted-foreground" role="status">{$_('contextMenu.noFoldersAvailable')}</p>
          {/each}
          {#if matchingFolders.length > 200}<p class="px-4 text-xs text-muted-foreground">{$_('search.refineFolders')}</p>{/if}
        {:else}
        {#each accountStore.accounts as accWithFolders (accWithFolders.account.id)}
          <AccountSection
            account={accWithFolders.account}
            folders={accWithFolders.folders}
            loading={accWithFolders.loading}
            syncing={accWithFolders.syncing}
            error={accWithFolders.error}
            selectedFolderId={accountStore.selectedFolder?.folderId ?? ''}
            {selectionSource}
            isHeaderFocused={focusedAccountId === accWithFolders.account.id}
            isExpanded={expandedAccounts[accWithFolders.account.id] ?? true}
            syncError={accountStore.getSyncError(accWithFolders.account.id)}
            {collapsedFolders}
            {onMessagesMoved}
            onFolderSelect={(accountId, folderId, folderPath, folderName, folderType) => {
              handleFolderSelect(accountId, folderId, folderPath, folderName, folderType)
              showAllFolders = false
            }}
            onToggleExpanded={() => toggleAccountExpanded(accWithFolders.account.id)}
            onToggleFolderCollapse={toggleFolderCollapsed}
            onEdit={() => openEditAccount(accWithFolders.account)}
            onDelete={() => openDeleteAccount(accWithFolders.account)}
            onSync={() => accountStore.syncAccount(accWithFolders.account.id)}
          />
        {/each}
        {/if}
        <button type="button" class="sidebar-all-folders-add" onclick={openAddAccount}>
          <Icon icon="mdi:plus" class="h-5 w-5" /> {$_('sidebar.addAccount')}
        </button>
      </div>
    </aside>
  {/if}
</div>

<!-- Account Dialog -->
<AccountDialog
  bind:open={showAccountDialog}
  editAccount={editingAccount}
  onClose={() => {
    showAccountDialog = false
    editingAccount = null
    setFocusedPane('messageList')
  }}
/>

<!-- Delete Confirmation Dialog -->
<DeleteAccountDialog
  bind:open={showDeleteDialog}
  account={deletingAccount}
  onClose={() => {
    showDeleteDialog = false
    deletingAccount = null
    setFocusedPane('messageList')
  }}
/>

<!-- Settings Dialog -->
<SettingsDialog
  bind:open={showSettingsDialog}
  onClose={() => {
    showSettingsDialog = false
    setFocusedPane('messageList')
  }}
/>

<VirtualFolderDialog
  bind:open={showVirtualFolderDialog}
  virtualFolder={editingVirtualFolder}
  accounts={accountStore.accounts}
  initialSearchQuery={pendingVirtualFolderQuery}
  initialFolderId={pendingVirtualFolderFolderId}
  onRefreshFolders={refreshVirtualFolderFolders}
  onSave={(value) => virtualFoldersStore.save(value as virtualfolder.VirtualFolder)}
/>

<ConfirmDialog
  bind:open={showDeleteVirtualFolderDialog}
  title={$_('sidebar.deleteVirtualFolderTitle')}
  description={$_('sidebar.deleteVirtualFolderDescription', { values: { name: deletingVirtualFolder?.name ?? '' } })}
  confirmLabel={$_('common.delete')}
  variant="destructive"
  onConfirm={() => void confirmDeleteVirtualFolder()}
  onCancel={() => { deletingVirtualFolder = null }}
/>

<svelte:window onkeydown={(e) => {
  if (showAllFolders && e.key === 'Escape') {
    if (allFoldersQuery) { e.preventDefault(); allFoldersQuery = '' }
    else showAllFolders = false
  }
}} />
