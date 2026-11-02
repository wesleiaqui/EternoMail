<script lang="ts">
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { folder, account } from '../../../../wailsjs/go/models'
  import { GetFolderTree } from '../../../../wailsjs/go/app/App'
  import { _ } from '$lib/i18n'
  import { filterFolders, type FolderChoice } from '$lib/sidebar/folderSearch'
  import { prioritizeSourceAccount } from './folderPickerOrder'

  interface PickerNode {
    folder: FolderChoice
    children: PickerNode[]
  }
  interface PickerRow {
    kind: 'account' | 'folder'
    account: account.Account
    node?: PickerNode
    depth?: number
  }

  interface Props {
    open: boolean
    title: string
    initialAccountId: string
    accounts: account.Account[]
    excludeFolderId?: string
    onSelect: (folderId: string, folderName: string, accountId: string) => void
  }
  let { open = $bindable(false), title, initialAccountId, accounts, excludeFolderId = '', onSelect }: Props = $props()
  let searchQuery = $state('')
  let selectedAccountId = $state('all')
  let folderTrees = $state<Record<string, PickerNode[]>>({})
  let foldersLoading = $state(false)
  let failedAccounts = $state<string[]>([])
  let expandedFolders = $state<Record<string, boolean>>({})
  let focusedIndex = $state(-1)
  let searchInput = $state<HTMLInputElement | null>(null)
  let listEl = $state<HTMLDivElement | null>(null)
  let active = $state(false)
  let generation = 0

  function mapTree(nodes: folder.FolderTree[], acc: account.Account): PickerNode[] {
    return (nodes || []).flatMap(tree => {
      if (!tree.folder) return mapTree(tree.children || [], acc)
      const item = tree.folder
      const choice: FolderChoice = {
        id: item.id,
        name: item.name,
        path: item.path,
        type: item.type,
        accountId: acc.id,
        accountName: acc.name,
        accountEmail: acc.email,
        label: `${acc.name} ${acc.email} ${item.path} ${item.name}`,
      }
      return [{ folder: choice, children: mapTree(tree.children || [], acc) }]
    })
  }

  $effect(() => {
    if (!open) { generation++; searchQuery = ''; active = false; return }
    const current = ++generation
    selectedAccountId = 'all'
    foldersLoading = true
    folderTrees = {}
    failedAccounts = []
    const jobs = orderedAccounts.map(async item => ({ account: item, nodes: mapTree(await GetFolderTree(item.id), item) }))
    Promise.allSettled(jobs).then(results => {
      if (current !== generation || !open) return
      folderTrees = Object.fromEntries(results.flatMap(result => result.status === 'fulfilled' ? [[result.value.account.id, result.value.nodes]] : []))
      failedAccounts = results.flatMap((result, index) => result.status === 'rejected' ? [orderedAccounts[index].name] : [])
      foldersLoading = false
    })
    const timer = setTimeout(() => { active = true; searchInput?.focus() }, 0)
    return () => clearTimeout(timer)
  })

  const orderedAccounts = $derived(prioritizeSourceAccount(accounts, initialAccountId))
  const sourceAccount = $derived(orderedAccounts.find(item => item.id === initialAccountId))
  const allFolders = $derived(orderedAccounts.flatMap(item => flatten(folderTrees[item.id] || [])))
  const matchingFolders = $derived(filterFolders(allFolders.filter(item => item.id !== excludeFolderId && (selectedAccountId === 'all' || item.accountId === selectedAccountId)), searchQuery))
  const matchingIDs = $derived(new Set(matchingFolders.map(item => item.id)))
  const visibleRows = $derived(buildRows())
  const displayFolders = $derived(visibleRows.flatMap(row => row.kind === 'folder' && row.node ? [row.node.folder] : []))

  function flatten(nodes: PickerNode[]): FolderChoice[] {
    return nodes.flatMap(node => [node.folder, ...flatten(node.children)])
  }

  function subtreeMatches(node: PickerNode): boolean {
    return matchingIDs.has(node.folder.id) || node.children.some(subtreeMatches)
  }

  function appendRows(rows: PickerRow[], nodes: PickerNode[], acc: account.Account, depth: number, searching: boolean) {
    for (const node of nodes) {
      const hasMatch = subtreeMatches(node)
      if (searching && !hasMatch) continue
      rows.push({ kind: 'folder', account: acc, node, depth })
      if (node.children.length && (searching || expandedFolders[node.folder.id])) {
        appendRows(rows, node.children, acc, depth + 1, searching)
      }
    }
  }

  function buildRows(): PickerRow[] {
    const rows: PickerRow[] = []
    const searching = searchQuery.trim().length > 0
    for (const acc of orderedAccounts) {
      if (selectedAccountId !== 'all' && selectedAccountId !== acc.id) continue
      const accountRows: PickerRow[] = []
      appendRows(accountRows, folderTrees[acc.id] || [], acc, 0, searching)
      if (!accountRows.length) continue
      rows.push({ kind: 'account', account: acc }, ...accountRows)
    }
    return rows
  }

  $effect(() => { void displayFolders; focusedIndex = displayFolders.length ? 0 : -1 })
  $effect(() => { if (focusedIndex >= 0) listEl?.querySelectorAll<HTMLElement>('[role="option"]')[focusedIndex]?.scrollIntoView({ block: 'nearest' }) })

  function select(item: FolderChoice) { onSelect(item.id, item.name, item.accountId) }
  function toggleExpanded(item: FolderChoice) { expandedFolders[item.id] = !expandedFolders[item.id] }

  function handleKeydown(event: KeyboardEvent) {
    if (!open || !active || event.altKey || event.metaKey || event.ctrlKey) return
    const target = event.target as HTMLElement | null
    if (target !== searchInput && (!target || !listEl?.contains(target))) return
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault(); event.stopPropagation()
      if (displayFolders.length) focusedIndex = (focusedIndex + (event.key === 'ArrowDown' ? 1 : -1) + displayFolders.length) % displayFolders.length
    }
    if (target !== searchInput && (event.key === 'ArrowLeft' || event.key === 'ArrowRight')) {
      const row = (target as HTMLElement).closest<HTMLElement>('[data-folder-id]')
      const id = row?.dataset.folderId
      const item = displayFolders.find(folder => folder.id === id)
      const hasChildren = item ? (findNode(folderTrees[item.accountId] || [], item.id)?.children.length ?? 0) > 0 : false
      if (item && hasChildren && event.key === 'ArrowRight' && !expandedFolders[item.id]) { event.preventDefault(); event.stopPropagation(); toggleExpanded(item) }
      if (item && hasChildren && event.key === 'ArrowLeft' && expandedFolders[item.id]) { event.preventDefault(); event.stopPropagation(); toggleExpanded(item) }
    }
    if (event.key === 'Enter' && focusedIndex >= 0 && displayFolders[focusedIndex]) {
      event.preventDefault(); event.stopPropagation(); select(displayFolders[focusedIndex])
    }
  }

  function findNode(nodes: PickerNode[], id: string): PickerNode | null {
    for (const node of nodes) {
      if (node.folder.id === id) return node
      const found = findNode(node.children, id)
      if (found) return found
    }
    return null
  }
</script>

<svelte:window onkeydown={handleKeydown} />
<Dialog.Root bind:open>
  <Dialog.Content class="max-w-lg">
    <Dialog.Header><Dialog.Title>{title}</Dialog.Title></Dialog.Header>
    <p class="text-xs text-muted-foreground">{$_('search.sourceAccount')}: {sourceAccount?.name || sourceAccount?.email || initialAccountId}</p>
    <input bind:this={searchInput} bind:value={searchQuery} type="search" aria-label={$_('contextMenu.searchFolders')}
      placeholder={$_('contextMenu.searchFolders')} class="h-10 w-full rounded-md border border-input bg-background px-3 text-sm" />
    {#if accounts.length > 1}
      <Select.Root value={selectedAccountId} onValueChange={(value) => { if (value) selectedAccountId = value }}>
        <Select.Trigger><Select.Value>{selectedAccountId === 'all' ? $_('search.allAccounts') : accounts.find(item => item.id === selectedAccountId)?.name}</Select.Value></Select.Trigger>
        <Select.Content>
          <Select.Item value="all" label={$_('search.allAccounts')} />
          {#each orderedAccounts as item (item.id)}<Select.Item value={item.id} label={item.name || item.email} />{/each}
        </Select.Content>
      </Select.Root>
    {/if}
    {#if failedAccounts.length}<p class="text-xs text-destructive" role="status">{$_('search.unavailableAccounts')}: {failedAccounts.join(', ')}</p>{/if}
    <div bind:this={listEl} class="border border-border rounded-md max-h-72 overflow-y-auto" role="listbox" aria-label={title} aria-busy={foldersLoading}>
      {#if foldersLoading}
        <p class="p-3 text-sm text-muted-foreground">{$_('common.loading')}</p>
      {:else if visibleRows.length}
        {#each visibleRows as row (row.kind === 'account' ? `account:${row.account.id}` : row.node!.folder.id)}
          {#if row.kind === 'account'}
            <div class="px-3 pt-4 pb-2" data-account-heading={row.account.id}>
              <p class="text-sm font-medium text-muted-foreground">{row.account.name || row.account.email}</p>
              <p class="text-xs text-muted-foreground">{row.account.email}</p>
            </div>
          {:else if row.node}
            {@const item = row.node.folder}
            {@const hasChildren = row.node.children.length > 0}
            <div class="flex items-center pr-2 hover:bg-muted/50 {displayFolders[focusedIndex]?.id === item.id ? 'bg-muted/50' : ''}" style={`padding-left: ${12 + (row.depth || 0) * 20}px`} data-folder-id={item.id}>
              {#if hasChildren}
                <button type="button" class="h-8 w-7 shrink-0 grid place-items-center text-muted-foreground hover:text-foreground" aria-label={$_(expandedFolders[item.id] || searchQuery.trim() ? 'search.collapseFolder' : 'search.expandFolder')} aria-expanded={!!(expandedFolders[item.id] || searchQuery.trim())} onclick={() => toggleExpanded(item)}>
                  <Icon icon={expandedFolders[item.id] || searchQuery.trim() ? 'mdi:chevron-down' : 'mdi:chevron-right'} class="w-4 h-4" />
                </button>
              {:else}
                <span class="w-7 shrink-0"></span>
              {/if}
              <button type="button" role="option" aria-selected={displayFolders[focusedIndex]?.id === item.id}
                class="min-w-0 flex-1 flex gap-2 items-start py-2 text-left"
                aria-label={`${item.path || item.name} · ${item.accountName} · ${item.accountEmail}`}
                onclick={() => select(item)}>
                <Icon icon="mdi:folder-outline" class="w-4 h-4 shrink-0 mt-0.5" />
                <span class="min-w-0 flex-1 text-sm break-words">{item.name || item.path}{#if searchQuery.trim() && item.path !== item.name}<span class="block text-xs text-muted-foreground">{item.path}</span>{/if}</span>
              </button>
            </div>
          {/if}
        {/each}
      {:else}<p class="p-3 text-sm text-muted-foreground">{$_('contextMenu.noFoldersAvailable')}</p>{/if}
    </div>
    {#if matchingFolders.length > 200}<p class="text-xs text-muted-foreground">{$_('search.refineFolders')}</p>{/if}
    <Dialog.Footer><Button variant="outline" onclick={() => (open = false)}>{$_('common.cancel')}</Button></Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
