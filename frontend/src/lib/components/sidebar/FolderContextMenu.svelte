<script lang="ts">
  import { onMount } from 'svelte'
  import { registerMenuAction } from '$lib/nativeMenu'
  import Icon from '@iconify/svelte'
  import { ContextMenu as ContextMenuPrimitive } from 'bits-ui'
  import {
    ContextMenuContent,
    ContextMenuItem,
    ContextMenuSeparator,
  } from '$lib/components/ui/context-menu'
  import {
    MarkAllFolderMessagesAsRead,
    MarkAllFolderMessagesAsUnread,
    GetFolderContentPolicy,
    SetFolderContentPolicy,
  } from '../../../../wailsjs/go/app/App'
  import { toasts } from '$lib/stores/toast'
  import { sidebarPins } from '$lib/stores/sidebarPins.svelte'
  import type { Snippet } from 'svelte'
  import { _ } from '$lib/i18n'

  interface Props {
    active?: boolean
    folderId: string
    // Set together to offer pinning the folder to the sidebar (P-007).
    accountId?: string
    folder?: { id: string; path: string; name: string; type?: string }
    children?: Snippet
  }

  let {
    active=false,
    folderId,
    accountId,
    folder,
    children,
  }: Props = $props()

  onMount(()=>{
    const valid=()=>active&&!!document.querySelector('[data-native-mail-primary="true"]')&&!document.querySelector('[data-composer-root]')
    const removes=[
      registerMenuAction('folder.read',handleMarkAllRead,valid),registerMenuAction('folder.unread',handleMarkAllUnread,valid),
      registerMenuAction('folder.pin',handleTogglePin,()=>valid()&&!!accountId&&!!folder),
      registerMenuAction('folder.download',async()=>{await loadContentPolicy();if(policy)await setContentPolicy(policy.bodyPrefetch?'disabled':'enabled')},()=>valid()&&!!accountId&&!!folder&&!policySaving),
      registerMenuAction('folder.defaultDownload',()=>setContentPolicy('inherit'),()=>valid()&&!!accountId&&!!folder&&!policySaving),
    ];return ()=>removes.forEach(remove=>remove())
  })
  let pinned = $derived(accountId !== undefined && sidebarPins.has(accountId, folderId))
  let policy = $state<{ bodyPrefetch: boolean; explicit: boolean; source: string } | null>(null)
  let policyLoading = $state(false)
  let policySaving = $state(false)

  function handleTogglePin() {
    if (!accountId || !folder) return
    sidebarPins.toggle(accountId, folder)
  }

  async function loadContentPolicy() {
    if (!accountId || !folder) return
    policyLoading = true
    try {
      policy = await GetFolderContentPolicy(accountId, folderId)
    } catch (err) {
      console.error('Failed to load folder content policy:', err)
      policy = null
    } finally {
      policyLoading = false
    }
  }

  async function setContentPolicy(mode: 'enabled' | 'disabled' | 'inherit') {
    if (!accountId || !folder || policySaving) return
    policySaving = true
    try {
      await SetFolderContentPolicy(accountId, folderId, mode)
      await loadContentPolicy()
    } catch (err) {
      console.error('Failed to update folder content policy:', err)
      toasts.error($_('toast.failedToSave'))
    } finally {
      policySaving = false
    }
  }

  async function handleMarkAllRead() {
    try {
      await MarkAllFolderMessagesAsRead(folderId)
      toasts.success($_('toast.markedAllAsRead'))
    } catch (err) {
      console.error('Mark all as read failed:', err)
      toasts.error($_('toast.failedToMarkAllAsRead'))
    }
  }

  async function handleMarkAllUnread() {
    try {
      await MarkAllFolderMessagesAsUnread(folderId)
      toasts.success($_('toast.markedAllAsUnread'))
    } catch (err) {
      console.error('Mark all as unread failed:', err)
      toasts.error($_('toast.failedToMarkAllAsUnread'))
    }
  }
</script>

<ContextMenuPrimitive.Root onOpenChange={(open: boolean) => { if (open) void loadContentPolicy() }}>
  <ContextMenuPrimitive.Trigger>
    {#if children}
      {@render children()}
    {/if}
  </ContextMenuPrimitive.Trigger>

  <ContextMenuContent>
    <ContextMenuItem onSelect={handleMarkAllRead}>
      <Icon icon="mdi:email-check-outline" class="mr-2 h-4 w-4" />
      {$_('contextMenu.markAllAsRead')}
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleMarkAllUnread}>
      <Icon icon="mdi:email-outline" class="mr-2 h-4 w-4" />
      {$_('contextMenu.markAllAsUnread')}
    </ContextMenuItem>
    {#if accountId !== undefined && folder !== undefined}
      <ContextMenuSeparator />
      <ContextMenuItem
        disabled={policyLoading || policySaving || policy === null}
        onSelect={() => void setContentPolicy(policy?.bodyPrefetch ? 'disabled' : 'enabled')}
      >
        <Icon icon={policy?.bodyPrefetch ? 'mdi:check-circle-outline' : 'mdi:circle-outline'} class="mr-2 h-4 w-4" />
        {$_('contextMenu.autoDownloadContent')}
      </ContextMenuItem>
      <ContextMenuItem disabled class="whitespace-normal text-xs text-muted-foreground">
        {policyLoading ? $_('common.loading') : $_('contextMenu.autoDownloadContentHelp')}
      </ContextMenuItem>
      {#if policy?.explicit}
        <ContextMenuItem disabled={policySaving} onSelect={() => void setContentPolicy('inherit')}>
          <Icon icon="mdi:restore" class="mr-2 h-4 w-4" />
          {$_('contextMenu.useDefaultContentPolicy')}
        </ContextMenuItem>
      {/if}
      <ContextMenuItem onSelect={handleTogglePin}>
        <Icon icon={pinned ? 'mdi:pin-off-outline' : 'mdi:pin-outline'} class="mr-2 h-4 w-4" />
        {pinned ? $_('contextMenu.unpinFromSidebar') : $_('contextMenu.pinToSidebar')}
      </ContextMenuItem>
    {/if}
  </ContextMenuContent>
</ContextMenuPrimitive.Root>
