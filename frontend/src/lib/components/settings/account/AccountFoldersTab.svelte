<script lang="ts">
  import { untrack } from 'svelte'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import HideFolderDialog from '$lib/components/sidebar/HideFolderDialog.svelte'
  import { GetFolders, GetVirtualFolders, GetFilterRules, SetFolderVisibility } from '../../../../../wailsjs/go/app/App'
  import type { folder, virtualfolder, mailfilter } from '../../../../../wailsjs/go/models'
  import { accountStore } from '$lib/stores/accounts.svelte'
  import { folderDependencies } from '$lib/sidebar/folderVisibility'
  import { _ } from '$lib/i18n'
  let { accountId }: { accountId: string } = $props()
  let folders = $state<folder.Folder[]>([])
  let views = $state<virtualfolder.VirtualFolder[]>([])
  let rules = $state<mailfilter.Rule[]>([])
  let query = $state('')
  let mode = $state('all')
  let selected = $state<string[]>([])
  let busy = $state(false)
  let error = $state('')
  let hideOpen = $state(false)
  let hideIds = $state<string[]>([])
  const visible = $derived(folders.filter(f => (mode !== 'hidden' || f.hidden) && (f.path || f.name).toLocaleLowerCase().includes(query.toLocaleLowerCase())))
  let generation = 0
  $effect(() => { const id = accountId; untrack(() => {selected = []; query = ''; mode = 'all'; void load(id)}) })
  async function load(id = accountId) {
    const token = ++generation
    busy = true; error = ''
    try {
      const [all, saved, filters] = await Promise.all([GetFolders(id), GetVirtualFolders(), GetFilterRules()])
      if (token !== generation) return
      folders = all || []; views = saved || []; rules = filters || []
    } catch (err) { if (token === generation) error = String(err) } finally { if (token === generation) busy = false }
  }
  function toggle(id: string, checked: boolean) { selected = checked ? [...new Set([...selected, id])] : selected.filter(item => item !== id) }
  function hide(ids: string[]) { hideIds = [...ids]; hideOpen = true }
  async function show(ids: string[], refresh: boolean) {
    if (busy || !ids.length) return
    const owner = accountId
    busy = true; error = ''
    try { await SetFolderVisibility(owner, ids, false, refresh); await accountStore.loadFolders(owner) }
    catch (err) { if (accountId === owner) error = String(err) }
    finally {
      if (accountId === owner) {
        const savedError = error
        await load(owner)
        if (savedError && accountId === owner) error = savedError
      }
    }
  }
</script>
<section class="space-y-4" aria-label={$_('folderVisibility.title')}>
  <h3 class="text-lg font-medium">{$_('folderVisibility.title')}</h3>
  <p class="text-sm text-muted-foreground">{$_('folderVisibility.explanation')}</p>
  <div class="flex flex-wrap items-center gap-2">
    <Input class="min-w-40 flex-1" bind:value={query} placeholder={$_('folderVisibility.search')} aria-label={$_('folderVisibility.search')} />
    <select bind:value={mode} class="rounded-md border border-input bg-background px-3 py-2 text-sm" aria-label={$_('folderVisibility.show')}>
      <option value="all">{$_('folderVisibility.all')}</option><option value="hidden">{$_('folderVisibility.hiddenOnly')}</option>
    </select>
  </div>
  {#if error}<p class="text-sm text-destructive" role="alert">{error}</p>{/if}
  <div class="max-h-80 overflow-auto divide-y divide-border rounded-md border border-border" aria-busy={busy}>
    {#each visible as item (item.id)}
      {@const dependencies = folderDependencies([item.id], views, rules)}
      <div class="flex items-start gap-3 p-3" data-folder-row={item.id}>
        <input type="checkbox" class="mt-1" aria-label={$_('folderVisibility.select', { values: { name: item.path || item.name } })} checked={selected.includes(item.id)} onchange={event => toggle(item.id, event.currentTarget.checked)} disabled={busy} />
        <div class="min-w-0 flex-1">
          <p class="break-words text-sm font-medium">{item.path || item.name}</p>
          <p class="text-xs text-muted-foreground">{$_(item.hidden ? 'folderVisibility.hiddenStatus' : 'folderVisibility.visibleStatus')}</p>
          {#if item.hidden && dependencies.views.length}<p class="break-words text-xs text-amber-500">{$_('folderVisibility.viewsWarning')}: {dependencies.views.join(', ')}</p>{/if}
          {#if item.hidden && dependencies.rules.length}<p class="break-words text-xs text-amber-500">{$_('folderVisibility.rulesWarning')}: {dependencies.rules.join(', ')}</p>{/if}
        </div>
        <Button variant={item.hidden ? 'default' : 'outline'} size="sm" disabled={busy} onclick={() => item.hidden ? show([item.id], true) : hide([item.id])}>{$_(item.hidden ? 'folderVisibility.showRefresh' : 'folderVisibility.hideShort')}</Button>
      </div>
    {:else}<p class="p-4 text-sm text-muted-foreground">{$_(busy ? 'common.loading' : 'folderVisibility.noFolders')}</p>{/each}
  </div>
  <div class="flex flex-wrap gap-2">
    <Button variant="ghost" size="sm" disabled={busy} onclick={() => selected = [...new Set([...selected, ...visible.map(item => item.id)])]}>{$_('folderVisibility.selectAll')}</Button>
    <Button variant="ghost" size="sm" disabled={busy} onclick={() => selected = []}>{$_('folderVisibility.selectNone')}</Button>
    <Button variant="outline" size="sm" disabled={busy || !selected.length} onclick={() => show(selected, false)}>{$_('folderVisibility.showSelected')}</Button>
    <Button variant="outline" size="sm" disabled={busy || !selected.length} onclick={() => hide(selected)}>{$_('folderVisibility.hideSelected')}</Button>
  </div>
  <p class="text-xs text-muted-foreground">{$_('folderVisibility.gmailCaveat')}</p>
</section>
<HideFolderDialog bind:open={hideOpen} {accountId} folderIds={hideIds} onDone={() => load()} />
