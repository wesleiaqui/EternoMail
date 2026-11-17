<script lang="ts">
  import { untrack } from 'svelte'
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { _ } from '$lib/i18n'
  import type { AccountWithFolders } from '$lib/stores/accounts.svelte'
  import type { folder, virtualfolder } from '../../../../wailsjs/go/models'
  import { filterFolders } from '$lib/sidebar/folderSearch'

  interface Props {
    open: boolean
    virtualFolder: virtualfolder.VirtualFolder | null
    accounts: AccountWithFolders[]
    // Prefill when creating a virtual folder from an active search.
    initialSearchQuery?: string
    initialFolderId?: string | null
    onRefreshFolders: () => Promise<void>
    onSave: (value: {
      id: string
      name: string
      orderIndex: number
      folderIds: string[]
      folderMode: string
      readStatus: string
      replyStatus: string
      starStatus: string
      dateWindow: string
      searchQuery: string
    }) => Promise<void>
  }

  let { open = $bindable(false), virtualFolder, accounts, initialSearchQuery = '', initialFolderId = null, onRefreshFolders, onSave }: Props = $props()
  const compactSave = $derived(!virtualFolder && !!initialSearchQuery && !initialFolderId)
  let name = $state('')
  let selectedIDs = $state<string[]>([])
  let folderMode = $state('include')
  let readStatus = $state('all')
  let replyStatus = $state('all')
  let starStatus = $state('all')
  let dateWindow = $state('all')
  let searchQuery = $state('')
  let saving = $state(false)
  let refreshing = $state(false)
  let error = $state('')
  let folderQuery = $state('')

  const selectableFolders = $derived.by(() => {
    const result: { id: string; label: string }[] = []
    const visit = (accountName: string, trees: folder.FolderTree[]) => {
      for (const tree of trees || []) {
        if (tree.folder) result.push({ id: tree.folder.id, label: `${accountName} · ${tree.folder.path || tree.folder.name}` })
        if (tree.children?.length) visit(accountName, tree.children)
      }
    }
    for (const account of accounts) visit(account.account.name || account.account.email, account.folders)
    return result
  })

  // The checklist shows only folders matching the search. Selection is kept
  // for folders that are filtered out.
  const visibleFolders = $derived(filterFolders(selectableFolders, folderQuery))
  const visibleSelectedCount = $derived(visibleFolders.filter((item) => selectedIDs.includes(item.id)).length)

  // Reset the editable state once per dialog open. The reads of editable
  // state (selectedIDs) must stay untracked: writing a fresh array while the
  // effect depends on it loops forever (effect_update_depth_exceeded in dev,
  // a hang in production).
  $effect(() => {
    if (!open) return
    untrack(() => {
      name = virtualFolder?.name || initialSearchQuery || ''
      selectedIDs = [...(virtualFolder?.folderIds || [])]
      if (!virtualFolder && initialFolderId && !selectedIDs.includes(initialFolderId)) {
        selectedIDs = [...selectedIDs, initialFolderId]
      }
      folderMode = virtualFolder?.folderMode || (initialSearchQuery && !initialFolderId ? 'exclude' : 'include')
      readStatus = virtualFolder?.readStatus || 'all'
      replyStatus = virtualFolder?.replyStatus || 'all'
      starStatus = virtualFolder?.starStatus || 'all'
      dateWindow = virtualFolder?.dateWindow || 'all'
      searchQuery = virtualFolder?.searchQuery || initialSearchQuery || ''
      folderQuery = ''
      error = ''
    })
  })

  function toggleFolder(id: string) {
    selectedIDs = selectedIDs.includes(id) ? selectedIDs.filter((item) => item !== id) : [...selectedIDs, id]
  }

  async function refreshFolders() {
    refreshing = true
    error = ''
    try {
      await onRefreshFolders()
    } catch (cause) {
      console.error('Failed to refresh Gmail labels:', cause)
      error = $_('sidebar.virtualFolderRefreshFailed')
    } finally {
      refreshing = false
    }
  }

  function setVisibleSelection(selected: boolean) {
    const visibleIDs = new Set(visibleFolders.map((item) => item.id))
    selectedIDs = selected
      ? [...new Set([...selectedIDs, ...visibleIDs])]
      : selectedIDs.filter((id) => !visibleIDs.has(id))
  }

  async function submit() {
    const trimmed = name.trim()
    if (!trimmed) {
      error = $_('sidebar.virtualFolderNameRequired')
      return
    }
    if (selectedIDs.length === 0 && folderMode === 'include') {
      error = $_('sidebar.virtualFolderChooseFolder')
      return
    }
    saving = true
    error = ''
    try {
      await onSave({
        id: virtualFolder?.id || '',
        name: trimmed,
        orderIndex: virtualFolder?.orderIndex || 0,
        folderIds: selectedIDs,
        folderMode,
        readStatus,
        replyStatus,
        starStatus,
        dateWindow,
        searchQuery: searchQuery.trim(),
      })
      open = false
    } catch (cause) {
      console.error('Failed to save virtual folder:', cause)
      error = $_('sidebar.virtualFolderSaveFailed')
    } finally {
      saving = false
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content class="max-h-[85vh] max-w-lg overflow-y-auto">
    <Dialog.Header>
      <Dialog.Title>{compactSave ? $_('search.saveAsVirtualFolder') : virtualFolder ? $_('sidebar.editVirtualFolder') : $_('sidebar.newVirtualFolder')}</Dialog.Title>
    </Dialog.Header>
    <label class="grid gap-1 text-sm">
      <span>{$_('sidebar.virtualFolderName')}</span>
      <input bind:value={name} maxlength="100" class="h-10 rounded-md border border-input bg-background px-3" />
    </label>
    {#if searchQuery.trim()}
      <div class="grid gap-1 text-sm">
        <span>{$_('sidebar.virtualFolderSavedQuery')}</span>
        <code class="rounded-md border border-input bg-muted px-3 py-2 text-xs break-all">{searchQuery}</code>
      </div>
    {/if}
    {#if compactSave}
      <p class="text-sm text-muted-foreground">All accounts, all folders. Saved folders rerun this search; no messages are moved.</p>
    {:else}
    <label class="grid gap-1 text-sm">
      <span>{$_('sidebar.virtualFolderSelection')}</span>
      <select bind:value={folderMode} class="h-10 rounded-md border border-input bg-background px-3">
        <option value="include">{$_('sidebar.virtualFolderIncludeSelected')}</option>
        <option value="exclude">{$_('sidebar.virtualFolderExcludeSelected')}</option>
      </select>
    </label>
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <label class="grid gap-1 text-sm">
        <span>{$_('sidebar.virtualFolderReadStatus')}</span>
        <select bind:value={readStatus} class="h-10 rounded-md border border-input bg-background px-2 text-xs sm:text-sm">
          <option value="all">{$_('sidebar.virtualFolderAny')}</option>
          <option value="unread">{$_('sidebar.virtualFolderUnread')}</option>
          <option value="read">{$_('sidebar.virtualFolderRead')}</option>
        </select>
      </label>
      <label class="grid gap-1 text-sm">
        <span>{$_('sidebar.virtualFolderStarStatus')}</span>
        <select bind:value={starStatus} class="h-10 rounded-md border border-input bg-background px-2 text-xs sm:text-sm">
          <option value="all">{$_('sidebar.virtualFolderAny')}</option>
          <option value="starred">{$_('sidebar.virtualFolderStarred')}</option>
          <option value="unstarred">{$_('sidebar.virtualFolderUnstarred')}</option>
        </select>
      </label>
      <label class="grid gap-1 text-sm">
        <span>{$_('sidebar.virtualFolderReplyStatus')}</span>
        <select bind:value={replyStatus} class="h-10 rounded-md border border-input bg-background px-2 text-xs sm:text-sm">
          <option value="all">{$_('sidebar.virtualFolderAny')}</option>
          <option value="replied">{$_('sidebar.virtualFolderReplied')}</option>
          <option value="unreplied">{$_('sidebar.virtualFolderUnreplied')}</option>
        </select>
      </label>
      <label class="grid gap-1 text-sm">
        <span>{$_('sidebar.virtualFolderTimePeriod')}</span>
        <select bind:value={dateWindow} class="h-10 rounded-md border border-input bg-background px-2 text-xs sm:text-sm">
          <option value="all">{$_('sidebar.virtualFolderAllTime')}</option>
          <option value="7d">{$_('sidebar.virtualFolderLast7Days')}</option>
          <option value="30d">{$_('sidebar.virtualFolderLast30Days')}</option>
          <option value="90d">{$_('sidebar.virtualFolderLast90Days')}</option>
          <option value="1y">{$_('sidebar.virtualFolderLastYear')}</option>
        </select>
      </label>
    </div>
    <p class="text-xs text-muted-foreground">{$_('sidebar.virtualFolderCriteriaHelp')}</p>
    <div class="flex gap-2">
      <input
        type="search"
        bind:value={folderQuery}
        placeholder={$_('sidebar.virtualFolderSearchFolders')}
        aria-label={$_('sidebar.virtualFolderSearchFolders')}
        disabled={selectableFolders.length === 0}
        class="h-9 min-w-0 flex-1 rounded-md border border-input bg-background px-3 text-sm"
      />
      <Button variant="outline" size="icon" disabled={refreshing} onclick={refreshFolders} title={$_('sidebar.virtualFolderRefreshFolders')} aria-label={$_('sidebar.virtualFolderRefreshFolders')}>
        <Icon icon={refreshing ? 'mdi:loading' : 'mdi:refresh'} class={refreshing ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} />
      </Button>
    </div>
    <div class="flex flex-wrap items-center justify-between gap-2 text-xs">
      <span class="text-muted-foreground">
        {$_('sidebar.virtualFolderVisibleSelection', { values: { selected: visibleSelectedCount, total: visibleFolders.length } })}
      </span>
      <div class="flex gap-1">
        <button
          type="button"
          class="rounded px-2 py-1 text-primary hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
          disabled={visibleFolders.length === 0 || visibleSelectedCount === visibleFolders.length}
          onclick={() => setVisibleSelection(true)}
        >{$_('sidebar.virtualFolderSelectVisible')}</button>
        <button
          type="button"
          class="rounded px-2 py-1 text-primary hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
          disabled={visibleSelectedCount === 0}
          onclick={() => setVisibleSelection(false)}
        >{$_('sidebar.virtualFolderClearVisible')}</button>
      </div>
    </div>
    <div class="max-h-72 overflow-y-auto rounded-md border border-border p-2">
      {#if selectableFolders.length === 0}
        <p class="p-3 text-sm text-muted-foreground">{$_('sidebar.virtualFolderNoFolders')}</p>
      {:else if visibleFolders.length === 0}
        <p class="p-3 text-sm text-muted-foreground">{$_('sidebar.virtualFolderNoFolderMatches')}</p>
      {:else}
        {#each visibleFolders as item (item.id)}
          <label class="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-muted/60">
            <input type="checkbox" checked={selectedIDs.includes(item.id)} onchange={() => toggleFolder(item.id)} />
            <span class="truncate">{item.label}</span>
          </label>
        {/each}
      {/if}
    </div>
    {/if}
    {#if error}<p class="text-sm text-destructive" role="alert">{error}</p>{/if}
    <Dialog.Footer>
      <Button variant="outline" onclick={() => (open = false)}>{$_('common.cancel')}</Button>
      <Button disabled={saving || (!compactSave && selectableFolders.length === 0)} onclick={submit}>
        {#if saving}<Icon icon="mdi:loading" class="mr-1 h-4 w-4 animate-spin" />{/if}
        {$_('common.save')}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
