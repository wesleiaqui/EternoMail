<script lang="ts">
  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import { Button } from '$lib/components/ui/button'
  import Switch from '$lib/components/ui/switch/Switch.svelte'
  import { ConfirmDialog } from '$lib/components/ui/confirm-dialog'
  import {
    GetFilterRules,
    UpdateFilterRule,
    DeleteFilterRule,
    ReorderFilterRules,
  } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { mailfilter } from '../../../../wailsjs/go/models'
  import { toasts } from '$lib/stores/toast'
  import { _ } from '$lib/i18n'
  import FilterDialog from '../common/FilterDialog.svelte'

  let rules = $state<mailfilter.Rule[]>([])
  let loading = $state(true)
  let busy = $state(false)
  let showEditor = $state(false)
  let editingRule = $state<mailfilter.Rule | null>(null)
  let deleteTarget = $state<mailfilter.Rule | null>(null)

  async function load() {
    loading = true
    try {
      rules = (await GetFilterRules()) || []
    } catch (err) {
      console.error('Failed to load filter rules:', err)
      toasts.error(`${$_('filters.loadFailed')}: ${String(err)}`)
      rules = []
    } finally {
      loading = false
    }
  }

  onMount(() => { void load() })

  function criteriaSummary(rule: mailfilter.Rule): string {
    const parts: string[] = []
    if (rule.fromContains?.length) parts.push(`${$_('filters.from')}: ${rule.fromContains.join(', ')}`)
    if (rule.toContains?.length) parts.push(`${$_('filters.to')}: ${rule.toContains.join(', ')}`)
    if (rule.subjectContains?.length) parts.push(`${$_('filters.subject')}: ${rule.subjectContains.join(', ')}`)
    if (rule.listIdContains?.length) parts.push(`${$_('filters.listId')}: ${rule.listIdContains.join(', ')}`)
    return parts.join(' · ')
  }

  function actionSummary(rule: mailfilter.Rule): string {
    const parts: string[] = []
    if (rule.destFolderId) parts.push($_('filters.actionMove'))
    if (rule.actionMarkRead) parts.push($_('filters.actionMarkRead'))
    if (rule.actionArchive) parts.push($_('filters.actionArchive'))
    if (rule.actionTrash) parts.push($_('filters.actionTrash'))
    return parts.join(' · ')
  }

  async function toggleEnabled(rule: mailfilter.Rule, enabled: boolean) {
    if (busy) return
    busy = true
    try {
      const updated = mailfilter.Rule.createFrom(rule)
      updated.enabled = enabled
      await UpdateFilterRule(updated)
      rule.enabled = enabled
    } catch (err) {
      toasts.error(`${$_('filters.saveFailed')}: ${String(err)}`)
      await load()
    } finally { busy = false }
  }

  async function move(index: number, direction: -1 | 1) {
    if (busy) return
    const target = index + direction
    if (index < 0 || target < 0 || target >= rules.length) return
    busy = true
    const reordered = [...rules]
    const [item] = reordered.splice(index, 1)
    reordered.splice(target, 0, item)
    try {
      await ReorderFilterRules(reordered.map(rule => rule.id))
      rules = reordered.map((rule, orderIndex) => mailfilter.Rule.createFrom({ ...rule, orderIndex }))
    } catch (err) {
      toasts.error(`${$_('filters.saveFailed')}: ${String(err)}`)
      await load()
    } finally { busy = false }
  }

  async function confirmDelete() {
    if (!deleteTarget || busy) return
    busy = true
    try {
      await DeleteFilterRule(deleteTarget.id)
      rules = rules.filter(rule => rule.id !== deleteTarget!.id)
      toasts.success($_('filters.deleted'))
    } catch (err) {
      toasts.error(`${$_('filters.deleteFailed')}: ${String(err)}`)
    } finally {
      busy = false
      deleteTarget = null
    }
  }

  function openCreate() {
    editingRule = null
    showEditor = true
  }

  function openEdit(rule: mailfilter.Rule) {
    editingRule = mailfilter.Rule.createFrom(rule)
    showEditor = true
  }
</script>

<div class="flex flex-col gap-3">
  <div class="flex items-start justify-between gap-2">
    <p class="text-sm text-muted-foreground">{$_('filters.listDescription')}</p>
    <Button size="sm" variant="outline" onclick={openCreate}>
      <Icon icon="mdi:plus" class="mr-1 h-4 w-4" />
      {$_('filters.newFilter')}
    </Button>
  </div>

  {#if loading}
    <div class="flex items-center justify-center py-6">
      <Icon icon="mdi:loading" class="h-5 w-5 animate-spin text-muted-foreground" />
    </div>
  {:else if rules.length === 0}
    <p class="py-4 text-sm text-muted-foreground">{$_('filters.empty')}</p>
  {:else}
    <div class="flex flex-col gap-2">
      {#each rules as rule, index (rule.id)}
        <div class="flex items-center gap-2 rounded-md border border-border px-3 py-2 {!rule.enabled ? 'opacity-60' : ''}">
          <div class="flex min-w-0 flex-1 flex-col">
            <span class="truncate text-sm font-medium">{rule.name}</span>
            <span class="truncate text-xs text-muted-foreground">{criteriaSummary(rule)}</span>
            <span class="truncate text-xs text-muted-foreground">{actionSummary(rule)}</span>
          </div>
          <div class="flex shrink-0 items-center gap-1">
            <Button variant="ghost" size="icon" class="h-7 w-7" disabled={busy || index === 0} title={$_('filters.moveUp')} onclick={() => void move(index, -1)}>
              <Icon icon="mdi:arrow-up" class="h-4 w-4" />
            </Button>
            <Button variant="ghost" size="icon" class="h-7 w-7" disabled={busy || index === rules.length - 1} title={$_('filters.moveDown')} onclick={() => void move(index, 1)}>
              <Icon icon="mdi:arrow-down" class="h-4 w-4" />
            </Button>
            <Switch disabled={busy} checked={rule.enabled} onCheckedChange={(value) => void toggleEnabled(rule, value)} />
            <Button variant="ghost" size="icon" class="h-7 w-7" disabled={busy} title={$_('filters.edit')} onclick={() => openEdit(rule)}>
              <Icon icon="mdi:pencil-outline" class="h-4 w-4" />
            </Button>
            <Button variant="ghost" size="icon" class="h-7 w-7 text-destructive" disabled={busy} title={$_('filters.delete')} onclick={() => { deleteTarget = rule }}>
              <Icon icon="mdi:delete-outline" class="h-4 w-4" />
            </Button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if showEditor}
  <FilterDialog bind:open={showEditor} rule={editingRule} onSaved={() => void load()} />
{/if}

<ConfirmDialog
  open={Boolean(deleteTarget)}
  title={$_('filters.deleteTitle')}
  description={$_('filters.deleteDescription', { values: { name: deleteTarget?.name ?? '' } })}
  confirmLabel={$_('filters.delete')}
  variant="destructive"
  onConfirm={() => void confirmDelete()}
  onCancel={() => { deleteTarget = null }}
/>
