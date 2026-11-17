<script lang="ts">
  import { untrack } from 'svelte'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { GetFolders, GetVirtualFolders, GetFilterRules, SetFolderVisibility } from '../../../../wailsjs/go/app/App'
  import type { folder, virtualfolder, mailfilter } from '../../../../wailsjs/go/models'
  import { accountStore } from '$lib/stores/accounts.svelte'
  import { _ } from '$lib/i18n'
  import { descendantFolderIds, folderDependencies } from '$lib/sidebar/folderVisibility'
  let { open = $bindable(false), accountId, folderIds, onDone }: { open: boolean; accountId: string; folderIds: string[]; onDone?: () => void | Promise<void> } = $props()
  let all = $state<folder.Folder[]>([])
  let views = $state<virtualfolder.VirtualFolder[]>([])
  let rules = $state<mailfilter.Rule[]>([])
  let children = $state(false)
  let loading = $state(false)
  let saving = $state(false)
  let error = $state('')
  let generation = 0
  const descendants = $derived(descendantFolderIds(all, folderIds))
  const targets = $derived(children ? [...folderIds, ...descendants] : folderIds)
  const dependencies = $derived(folderDependencies(targets, views, rules))
  $effect(() => {
    if (!open) return
    dialogGuardOpen()
    return () => dialogGuardClose()
  })
  $effect(() => {
    if (!open) { generation++; return }
    const token = ++generation
    untrack(() => { children = false; error = ''; loading = true })
    Promise.all([GetFolders(accountId), GetVirtualFolders(), GetFilterRules()]).then(([folders, saved, filters]) => {
      if (generation !== token) return
      all = folders || []; views = saved || []; rules = filters || []
    }).catch(err => { if (generation === token) error = String(err) }).finally(() => { if (generation === token) loading = false })
  })
  async function hide() {
    if (saving || loading || error) return
    const owner = accountId, ids = [...targets]
    saving = true
    try {
      await SetFolderVisibility(owner, ids, true, false)
      await accountStore.loadFolders(owner)
      await onDone?.()
      open = false
    } catch (err) { error = String(err) } finally { saving = false }
  }
</script>
<Dialog.Root bind:open>
  <Dialog.Content>
    <Dialog.Header>
      <Dialog.Title>{$_('folderVisibility.hide')}</Dialog.Title>
      <Dialog.Description>{$_('folderVisibility.explanation')}</Dialog.Description>
    </Dialog.Header>
    {#if loading}<p>{$_('common.loading')}</p>{/if}
    {#if descendants.length}
      <label class="flex items-start gap-2 text-sm"><input type="checkbox" bind:checked={children} disabled={saving || loading} class="mt-1" />{$_('folderVisibility.descendants', { values: { count: descendants.length } })}</label>
    {/if}
    {#if dependencies.views.length}<p class="text-sm text-amber-500" role="status">{$_('folderVisibility.viewsWarning')}: {dependencies.views.join(', ')}</p>{/if}
    {#if dependencies.rules.length}<p class="text-sm text-amber-500" role="status">{$_('folderVisibility.rulesWarning')}: {dependencies.rules.join(', ')}</p>{/if}
    <p class="text-xs text-muted-foreground">{$_('folderVisibility.gmailCaveat')}</p>
    {#if error}<p role="alert" class="text-sm text-destructive">{error}</p>{/if}
    <Dialog.Footer>
      <Button variant="outline" disabled={saving} onclick={() => open = false}>{$_('common.cancel')}</Button>
      <Button disabled={saving || loading || !!error} onclick={hide}>{$_('folderVisibility.hide')}</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
