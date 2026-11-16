<script lang="ts">
  // Reusable plain-text reply snippets for the composer. Inserting only adds
  // text at the cursor, so the draft, recipients and sender identity are
  // untouched. Snippets are stored locally (settings table), not per account.
  import Icon from '@iconify/svelte'
  import { onMount } from 'svelte'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { _ } from '$lib/i18n'
  import { toasts } from '$lib/stores/toast'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  import { ListQuickReplies, SaveQuickReply, DeleteQuickReply } from '$wailsjs/go/app/App.js'
  import type { settings } from '$wailsjs/go/models'

  interface Props {
    onInsert: (text: string) => void
    compact?: boolean
  }
  let { onInsert, compact = false }: Props = $props()

  let replies = $state<settings.QuickReply[]>([])
  let menuOpen = $state(false)
  let manageOpen = $state(false)
  // Editing form: id '' means "new"
  let editId = $state('')
  let editName = $state('')
  let editBody = $state('')
  let saving = $state(false)

  async function load() {
    try {
      replies = (await ListQuickReplies()) ?? []
    } catch (err) {
      console.error('Failed to load quick replies:', err)
      toasts.error($_('composer.quickRepliesLoadFailed'))
    }
  }

  onMount(load)

  // Keep the global key handler out of the way while the manage dialog is open.
  let guarded = false
  $effect(() => {
    if (manageOpen && !guarded) {
      dialogGuardOpen()
      guarded = true
    }
    if (!manageOpen && guarded) {
      dialogGuardClose()
      guarded = false
    }
  })

  function startNew() {
    editId = ''
    editName = ''
    editBody = ''
  }

  function startEdit(reply: settings.QuickReply) {
    editId = reply.id
    editName = reply.name
    editBody = reply.body
  }

  function openManage() {
    startNew()
    manageOpen = true
  }

  async function save() {
    if (saving) return
    saving = true
    try {
      await SaveQuickReply({ id: editId, name: editName, body: editBody } as settings.QuickReply)
      await load()
      startNew()
    } catch (err) {
      console.error('Failed to save quick reply:', err)
      toasts.error($_('composer.quickReplySaveFailed'))
    } finally {
      saving = false
    }
  }

  async function remove(id: string) {
    try {
      await DeleteQuickReply(id)
      await load()
      if (editId === id) startNew()
    } catch (err) {
      console.error('Failed to delete quick reply:', err)
      toasts.error($_('composer.quickReplyDeleteFailed'))
    }
  }

  const canSave = $derived(editName.trim() !== '' && editBody.trim() !== '')
</script>

<DropdownMenu.Root bind:open={menuOpen} onOpenChange={(o) => { if (o) void load() }}>
  <DropdownMenu.Trigger
    class="{compact ? 'p-1.5 rounded-md' : 'flex items-center gap-1'} hover:text-foreground hover:bg-muted transition-colors"
    data-native-action="composer.quickReplies" title={$_('composer.quickReplies')}
    aria-label={$_('composer.quickReplies')}
  >
    <Icon icon="mdi:message-reply-text-outline" class="w-4 h-4" />
    {#if !compact}{$_('composer.quickReplies')}{/if}
  </DropdownMenu.Trigger>
  <DropdownMenu.Content align="start" class="max-w-xs">
    {#each replies as reply (reply.id)}
      <DropdownMenu.Item onSelect={() => onInsert(reply.body)}>
        <span class="truncate" title={reply.body}>{reply.name}</span>
      </DropdownMenu.Item>
    {:else}
      <div class="px-2 py-1.5 text-xs text-muted-foreground">{$_('composer.quickRepliesEmpty')}</div>
    {/each}
    <DropdownMenu.Separator />
    <DropdownMenu.Item onSelect={openManage}>
      <Icon icon="mdi:pencil-outline" class="w-4 h-4 mr-2" />
      {$_('composer.quickRepliesManage')}
    </DropdownMenu.Item>
  </DropdownMenu.Content>
</DropdownMenu.Root>

<Dialog.Root bind:open={manageOpen}>
  <Dialog.Content class="max-w-lg max-h-[90vh] overflow-y-auto">
    <Dialog.Header>
      <Dialog.Title>{$_('composer.quickRepliesManage')}</Dialog.Title>
      <Dialog.Description>{$_('composer.quickRepliesDescription')}</Dialog.Description>
    </Dialog.Header>

    <ul class="space-y-1">
      {#each replies as reply (reply.id)}
        <li class="flex items-center gap-2 rounded-md px-2 py-1 hover:bg-muted {editId === reply.id ? 'bg-muted' : ''}">
          <span class="flex-1 truncate text-sm" title={reply.body}>{reply.name}</span>
          <button
            type="button"
            class="p-1 rounded hover:bg-background"
            onclick={() => startEdit(reply)}
            title={$_('common.edit')}
            aria-label={$_('common.edit')}
          >
            <Icon icon="mdi:pencil" class="w-4 h-4" />
          </button>
          <button
            type="button"
            class="p-1 rounded hover:bg-background text-destructive"
            onclick={() => remove(reply.id)}
            title={$_('common.delete')}
            aria-label={$_('common.delete')}
          >
            <Icon icon="mdi:delete-outline" class="w-4 h-4" />
          </button>
        </li>
      {/each}
    </ul>

    <form class="space-y-2" onsubmit={(e) => { e.preventDefault(); void save() }}>
      <Input
        bind:value={editName}
        placeholder={$_('composer.quickReplyName')}
        aria-label={$_('composer.quickReplyName')}
      />
      <textarea
        bind:value={editBody}
        rows="5"
        placeholder={$_('composer.quickReplyBody')}
        aria-label={$_('composer.quickReplyBody')}
        class="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      ></textarea>
      <Dialog.Footer>
        {#if editId}
          <Button type="button" variant="ghost" onclick={startNew}>{$_('common.cancel')}</Button>
        {/if}
        <Button type="submit" disabled={!canSave || saving}>
          {editId ? $_('common.save') : $_('common.add')}
        </Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
