<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import Icon from '@iconify/svelte'
  import MessageList from './lib/components/list/MessageList.svelte'
  import ConversationViewer from './lib/components/viewer/ConversationViewer.svelte'
  import ToastContainer from './lib/components/ui/toast/ToastContainer.svelte'
  import VirtualFolderDialog from './lib/components/sidebar/VirtualFolderDialog.svelte'
  import { _ } from './lib/i18n'
  import { accountStore } from './lib/stores/accounts.svelte'
  import { virtualFoldersStore } from './lib/stores/virtualFolders.svelte'
  import { getFocusedPane, setFocusedPane, isInputElement } from './lib/stores/keyboard.svelte'
  import { isDialogGuardActive } from './lib/stores/dialogGuard'
  import { KEY } from './lib/keyboard/shortcuts'
  import { createMailWindowState, navigateMailWindow, selectMailWindowConversation, type MailScope } from './lib/windows/mailWindow'
  import { toasts } from './lib/stores/toast'
  import { publishUndoOperationCreated, publishUndoOperationCompleted } from './lib/components/list/undoMutationEvents'
  import { runMessageMutation } from './lib/components/list/mutationInFlight'
  import type { virtualfolder } from '../wailsjs/go/models'
  import { GetAccounts, GetFolders, GetFolderTree, GetMessage, OpenMailWindow, OpenComposerWindow, MarkAsRead, MarkAsUnread, Star, Unstar, MarkAsSpamWithUndo, MarkAsNotSpamWithUndo, ArchiveWithUndo, UndoOperation } from '../wailsjs/go/app/App'
  import { EventsEmit, WindowSetTitle } from '../wailsjs/runtime/runtime'

  let { config, embedded = false, readerTab = false, onCloseTab, active = true, onScopeChange, onNewTab, onOpenInTab }: { config: MailScope; embedded?: boolean; readerTab?: boolean; onCloseTab?: () => void; active?: boolean; onScopeChange?: (scope: MailScope) => void; onNewTab?: () => void; onOpenInTab?: (scope: MailScope) => void } = $props()
  // A process per window gives each component/store its own search, checked
  // rows, scroll memory and reader mode. Primary UI state is never persisted.
  let windowState = $state(createMailWindowState(untrack(() => config)))
  let scopes = $state<MailScope[]>([])
  let loading = $state(true)
  let navigationError = $state('')
  let listWidth = $state(360)
  let readerOnly = $state(false)
  let folderBrowserOpen = $state(false)
  let folderSearch = $state('')
  let showVirtualFolderDialog = $state(false)
  let pendingSearchQuery = $state('')
  let pendingSearchFolderId = $state<string | null>(null)
  let list = $state<MessageList | undefined>(undefined)
  let viewer = $state<ConversationViewer | undefined>(undefined)
  const scopeKey = (scope: MailScope) => JSON.stringify([scope.accountId, scope.folderId])
  const currentKey = $derived(scopeKey(windowState.scope))
  const scopeGroups = $derived([
    { id: 'unified', name: $_('navigation.virtualCard'), items: scopes.filter(scope => scope.accountId === 'unified' || scope.accountId === 'virtual') },
    ...accountStore.accounts.map(item => ({ id: item.account.id, name: item.account.name || item.account.email, items: scopes.filter(scope => scope.accountId === item.account.id) })),
  ].map(group => ({ ...group, items: group.items.filter(scope => !folderSearch.trim() || `${label(scope)} ${group.name}`.toLocaleLowerCase().includes(folderSearch.trim().toLocaleLowerCase())) })).filter(group => group.items.length))

  $effect(() => { if (embedded && !readerTab) folderBrowserOpen = true })

  function label(scope: MailScope) {
    if (scope.threadId && scope.threadSubject) return scope.threadSubject
    if (scope.accountId === 'unified' && scope.folderType === 'all') return $_('mailWindow.allMail')
    if (scope.accountId === 'unified' && scope.folderType === 'spam') return $_('mailWindow.spam')
    if (scope.accountId === 'unified') return $_(`sidebar.${({ inbox: 'allInboxes', archive: 'archived' }[scope.folderType] || scope.folderType)}`)
    return scope.folderName
  }
  async function loadNavigation() {
    try {
      const accounts = (await GetAccounts()) ?? []
      const entries = await Promise.all(accounts.map(async account => ({
        account, folders: (await GetFolderTree(account.id)) ?? [], loading: false, syncing: false, error: null, lastSync: null,
      })))
      accountStore.accounts = entries
      const real = await Promise.all(accounts.map(async account => ((await GetFolders(account.id)) ?? []).map(folder => ({
        accountId: account.id, folderId: folder.id, folderName: `${account.name || account.email} · ${folder.path}`, folderType: folder.type,
      }))))
      await virtualFoldersStore.load(true)
      scopes = [
        ...['inbox', 'sent', 'drafts', 'trash', 'spam', 'archive', 'all', 'starred'].map(type => ({ accountId: 'unified', folderId: type, folderName: '', folderType: type })),
        ...virtualFoldersStore.folders.map(folder => ({ accountId: 'virtual', folderId: folder.id, folderName: folder.name, folderType: 'virtual' })),
        ...real.flat(),
      ]
      navigationError = ''
    } catch (error) { navigationError = String(error) }
    finally { loading = false }
  }
  function navigate(value: string) {
    const scope = scopes.find(scope => scopeKey(scope) === value)
    if (!scope) return
    windowState = navigateMailWindow(windowState, scope)
    onScopeChange?.({ ...scope, searchQuery: list?.getSearchQuery() || '', ...(windowState.thread ? { threadId: windowState.thread.threadId, threadAccountId: windowState.thread.accountId, threadFolderId: windowState.thread.folderId } : {}) })
    readerOnly = false
    setFocusedPane('messageList')
  }
  $effect(() => {
    if (scopeKey(config) === scopeKey(windowState.scope)) return
    windowState = navigateMailWindow(windowState, config)
    readerOnly = false
    setFocusedPane('messageList')
  })
  function select(threadId: string, folderId: string, accountId: string) {
    windowState = selectMailWindowConversation(windowState, threadId, folderId, accountId)
    onScopeChange?.({ ...windowState.scope, threadId, threadAccountId: accountId, threadFolderId: folderId, searchQuery: list?.getSearchQuery() || '' })
  }
  async function run(action: () => Promise<unknown>) {
    try { await action() }
    catch (error) { toasts.error(String(error)) }
  }
  async function newWindow() {
    const { accountId, folderId, folderName, folderType } = windowState.scope
    await run(() => OpenMailWindow(accountId, folderId, folderName, folderType, windowState.thread?.threadId || '', windowState.thread?.accountId || '', windowState.thread?.folderId || '', list?.getSearchQuery() || ''))
  }
  async function compose() {
    const accountId = accountStore.accounts.some(item => item.account.id === windowState.scope.accountId)
      ? windowState.scope.accountId : accountStore.accounts[0]?.account.id
    if (accountId) await run(() => OpenComposerWindow(accountId, 'new', '', '', ''))
  }
  async function reply(mode: 'reply' | 'reply-all' | 'forward', messageId: string) {
    // Row menus can target an unselected row from a different account.
    await run(async () => {
      const message = await GetMessage(messageId)
      if (!message?.accountId) throw new Error($_('mailWindow.messageUnavailable'))
      await OpenComposerWindow(message.accountId, mode, messageId, '', '')
    })
  }
  function refresh() {
    const scope = windowState.scope
    EventsEmit('messages:updated', { accountId: scope.accountId, folderId: scope.folderId })
    // Cached reads refresh on focus; existing list policy owns explicit header work.
    list?.handleActionComplete(false)
  }
  async function archive(ids: string[]) {
    await run(() => runMessageMutation(ids, async () => {
      const result = await ArchiveWithUndo(ids)
      const operation = result.operationId
      publishUndoOperationCreated(operation, ids, 'archive')
      toasts.success($_('toast.archived'), [{ label: $_('common.undo'), onClick: () => run(async () => {
        await UndoOperation(operation)
        publishUndoOperationCompleted(operation)
        refresh()
      }) }])
      list?.handleActionComplete(true)
    }))
  }
  async function bulkAction(id: string, options?: { permanent?: boolean }) {
    const ids = list?.getCheckedMessageIds() ?? []
    if (!ids.length) return
    if (id === 'mail.archive') { await archive(ids); return }
    if (id === 'mail.trash') { list?.requestDelete(ids, options?.permanent === true); return }
    if (id === 'mail.markRead' || id === 'mail.markUnread') {
      await run(() => runMessageMutation(ids, async () => {
        if (id === 'mail.markRead') await MarkAsRead(ids)
        else await MarkAsUnread(ids)
        list?.clearChecked()
        refresh()
      }))
      return
    }
    if (id === 'mail.star') {
      const shouldStar = list?.getCheckedHasUnstarred() ?? true
      await run(() => runMessageMutation(ids, async () => {
        if (shouldStar) await Star(ids)
        else await Unstar(ids)
        list?.clearChecked()
        refresh()
      }))
      return
    }
    if (id === 'mail.spam') {
      await run(() => runMessageMutation(ids, async () => {
        const spamFolder = windowState.scope.folderType === 'spam'
        const result = spamFolder ? await MarkAsNotSpamWithUndo(ids) : await MarkAsSpamWithUndo(ids)
        publishUndoOperationCreated(result.operationId, ids, spamFolder ? 'mark-as-not-spam' : 'mark-as-spam')
        list?.clearChecked()
        refresh()
      }))
    }
  }
  function closeReader() {
    if (readerTab) {
      onCloseTab?.()
      return
    }
    readerOnly = false
    windowState = { ...windowState, thread: null }
    onScopeChange?.({ ...windowState.scope, threadId: undefined, threadAccountId: undefined, threadFolderId: undefined, searchQuery: list?.getSearchQuery() || '' })
    setFocusedPane('messageList')
  }
  function keydown(event: KeyboardEvent) {
    if (!active || isInputElement(event.target) || isDialogGuardActive() || event.defaultPrevented) return
    if ((event.metaKey || event.ctrlKey) && event.shiftKey && event.key.toLowerCase() === 'n') { event.preventDefault(); if (embedded) onNewTab?.(); else void newWindow(); return }
    if (event.ctrlKey || event.metaKey || event.altKey) return
    if (event.key === 'Escape') { closeReader(); return }
    if (event.key === '/') { event.preventDefault(); list?.toggleSearchFocus(); return }
    if (event.key === 'c') { event.preventDefault(); void compose(); return }
    if (getFocusedPane() === 'viewer') {
      if (event.key === 'r') { event.preventDefault(); viewer?.reply(); return }
      if (event.key === 'a') { event.preventDefault(); viewer?.replyAll(); return }
      if (event.key === 'f') { event.preventDefault(); viewer?.forward(); return }
      if (KEY.LIST_NEXT(event)) { event.preventDefault(); viewer?.scrollDown(); return }
      if (KEY.LIST_PREV(event)) { event.preventDefault(); viewer?.scrollUp(); return }
      return
    }
    if (KEY.LIST_NEXT(event)) { event.preventDefault(); list?.selectNext(); return }
    if (KEY.LIST_PREV(event)) { event.preventDefault(); list?.selectPrevious(); return }
    if (KEY.LIST_OPEN(event)) { event.preventDefault(); list?.openSelected(); setFocusedPane('viewer'); return }
    if (KEY.LIST_TOGGLE_CHECK(event)) { event.preventDefault(); list?.toggleCheck() }
  }
  onMount(() => {
    setFocusedPane(readerTab ? 'viewer' : 'messageList')
    if (!readerTab) void loadNavigation()
    if (config.searchQuery) requestAnimationFrame(() => list?.setSearchQuery(config.searchQuery || ''))
  })
  $effect(() => { if (!embedded) WindowSetTitle(`${label(windowState.scope)} — Postilaatikko`) })
  $effect(() => {
    const request = virtualFoldersStore.saveSearchRequest
    if (!request) return
    virtualFoldersStore.clearSaveSearchRequest()
    pendingSearchQuery = request.query
    pendingSearchFolderId = request.folderId
    showVirtualFolderDialog = true
  })
</script>

<svelte:window onkeydown={keydown} onfocus={() => { if (active) { refresh(); if (!readerTab) void loadNavigation() } }} />
<div class="mail-window {embedded ? 'h-full' : 'h-screen'} flex flex-col overflow-hidden bg-background text-foreground">
  {#if !readerTab}
  <header class="flex items-center gap-2 border-b border-border p-2">
    {#if embedded}
      <button type="button" class="rounded p-2 hover:bg-muted" aria-label={$_('mailWindow.folder')} aria-expanded={folderBrowserOpen} title={$_('mailWindow.folder')} onclick={() => folderBrowserOpen = !folderBrowserOpen}><Icon icon="mdi:folder-open-outline" width="20" /></button>
    {/if}
    <select class="min-w-0 flex-1 rounded border border-border bg-background p-2 text-sm" aria-label={$_('mailWindow.folder')} value={currentKey} onchange={event => navigate(event.currentTarget.value)} disabled={loading}>
      {#if !scopes.some(scope => scopeKey(scope) === currentKey)}<option value={currentKey}>{label(windowState.scope)}</option>{/if}
      {#each scopes as scope (scopeKey(scope))}<option value={scopeKey(scope)}>{label(scope)}</option>{/each}
    </select>
    <button class="rounded p-2 hover:bg-muted" title={$_('mailWindow.newWindow')} aria-label={$_('mailWindow.newWindow')} onclick={newWindow}><Icon icon="mdi:open-in-new" width="20" /></button>
    <button class="rounded p-2 hover:bg-muted" title={$_('mailWindow.compose')} aria-label={$_('mailWindow.compose')} onclick={compose}><Icon icon="mdi:pencil" width="20" /></button>
    <label class="width-control text-xs">{$_('mailWindow.listWidth')}<input type="range" min="280" max="600" step="10" bind:value={listWidth} /></label>
    {#if windowState.thread}<button class="rounded p-2 hover:bg-muted" onclick={() => readerOnly = !readerOnly} aria-pressed={readerOnly} title={$_('mailWindow.focus')} aria-label={$_('mailWindow.focus')}><Icon icon="mdi:arrow-expand" width="20" /></button>{/if}
  </header>
  {/if}
  {#if navigationError}<div role="alert" class="p-2 text-sm">{navigationError}<button class="underline ml-2" onclick={loadNavigation}>{$_('common.retry')}</button></div>{/if}
  <div class="flex flex-1 min-h-0 overflow-hidden">
  {#if embedded && !readerTab && folderBrowserOpen}
    <aside class="w-64 shrink-0 min-h-0 overflow-y-auto border-r border-border bg-muted/20 p-2" aria-label={$_('mailWindow.folder')}>
      <input type="search" bind:value={folderSearch} class="mb-2 h-9 w-full rounded border border-border bg-background px-2 text-sm" aria-label={$_('common.search')} placeholder={$_('common.search')} />
      {#if loading}<p class="p-2 text-sm text-muted-foreground">{$_('common.loading')}</p>{/if}
      {#each scopeGroups as group (group.id)}
        <section class="mb-3">
          <h2 class="px-2 py-1 text-xs font-semibold text-muted-foreground">{group.name}</h2>
          {#each group.items as scope (scopeKey(scope))}
            <button type="button" class="w-full truncate rounded px-2 py-1.5 text-left text-sm hover:bg-muted {currentKey === scopeKey(scope) ? 'bg-primary/10 font-medium' : ''}"
              style={`padding-left: ${8 + (scope.folderType === 'folder' ? Math.max(0, scope.folderName.split('/').length - 1) * 12 : 0)}px`}
              aria-current={currentKey === scopeKey(scope) ? 'page' : undefined} onclick={() => navigate(scopeKey(scope))}>{label(scope)}</button>
          {/each}
        </section>
      {/each}
    </aside>
  {/if}
  <main class:reader-only={readerOnly} class:has-thread={!!windowState.thread} class="flex flex-1 min-w-0 min-h-0 overflow-hidden" style={`--list-width: ${listWidth}px`}>
    {#if !readerTab}
    <section class="mail-list min-h-0 overflow-hidden border-r border-border" role="presentation" onclick={() => setFocusedPane('messageList')}>
      <MessageList bind:this={list} accountId={windowState.scope.accountId} folderId={windowState.scope.folderId} folderName={label(windowState.scope)} folderType={windowState.scope.folderType}
        isFocused={active && getFocusedPane() === 'messageList'} onConversationSelect={select} onOpenInTab={(accountId, folderId, threadId, folderType, subject) => onOpenInTab?.({ ...windowState.scope, threadId, threadAccountId: accountId, threadFolderId: folderId, folderType, threadSubject: subject })} onReply={reply}
        onSearchQueryChange={searchQuery => onScopeChange?.({ ...windowState.scope, searchQuery })}
        onRowActionComplete={auto => { if (auto) windowState = { ...windowState, thread: null } }}
        onBulkMarkRead={ids => void run(() => MarkAsRead(ids))} onBulkAction={bulkAction} enableCommandPalette={false} />
    </section>
    {/if}
    <section class="mail-reader flex-1 min-w-0 min-h-0" role="presentation" onclick={() => setFocusedPane('viewer')}>
      {#if windowState.thread}
        {@const thread = windowState.thread}
        <ConversationViewer bind:this={viewer} threadId={thread.threadId} folderId={thread.folderId} accountId={thread.accountId} folderType={windowState.scope.folderType}
          isViewerVisible={active} isFocused={active && getFocusedPane() === 'viewer'} showBackButton onBack={closeReader} onReply={reply}
          onEditDraft={draftId => run(() => OpenComposerWindow(thread.accountId, 'new', '', draftId, ''))}
          onToggleThreadFocus={readerTab ? undefined : () => readerOnly = !readerOnly} inFocusMode={readerOnly}
          onActionComplete={() => list?.handleActionComplete(true)}
          onComposeToAddress={address => run(() => OpenComposerWindow(thread.accountId, 'new', '', '', `mailto:${encodeURIComponent(address)}`))} />
      {:else}<div class="h-full flex items-center justify-center text-muted-foreground text-sm">{$_('mailWindow.selectMessage')}</div>{/if}
    </section>
  </main>
  </div>
</div>
<ToastContainer />
<VirtualFolderDialog bind:open={showVirtualFolderDialog} virtualFolder={null} accounts={accountStore.accounts}
  initialSearchQuery={pendingSearchQuery} initialFolderId={pendingSearchFolderId}
  onRefreshFolders={loadNavigation}
  onSave={async value => { await virtualFoldersStore.save(value as virtualfolder.VirtualFolder); await loadNavigation() }} />
<style>
  .mail-list { width: var(--list-width); flex-shrink: 0; }
  .reader-only .mail-list { display: none; }
  .width-control { display: flex; align-items: center; gap: 0.5rem; }
  .width-control input { width: 80px; }
  @media (max-width: 800px) {
    .mail-list { width: 100%; }
    .has-thread .mail-list { display: none; }
    main:not(.has-thread) .mail-reader { display: none; }
    .width-control { display: none; }
  }
</style>
