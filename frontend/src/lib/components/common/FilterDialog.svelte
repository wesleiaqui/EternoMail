<script lang="ts">
  import { onDestroy } from 'svelte'
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import {
    GetAccounts,
    GetMessage,
    GetMessageListID,
    CreateFilterRule,
    UpdateFilterRule,
    PreviewFilterRule,
    ApplyFilterRuleToExisting,
    GetFolderTree,
  } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { account, mailfilter } from '../../../../wailsjs/go/models'
  import { toasts } from '$lib/stores/toast'
  import { _ } from '$lib/i18n'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'
  import FolderPickerDialog from './FolderPickerDialog.svelte'

  interface Props {
    open: boolean
    // Edit mode: the rule being edited. Create mode: null.
    rule?: mailfilter.Rule | null
    // Create mode prefill: the message the filter is derived from.
    messageId?: string
    accountId?: string
    onSaved?: (ruleId: string) => void
  }

  let { open = $bindable(false), rule = null, messageId = '', accountId = '', onSaved }: Props = $props()

  const editMode = $derived(Boolean(rule && rule.id))

  let accounts = $state<account.Account[]>([])

  // Form state
  let name = $state('')
  let scopeAll = $state(true)
  let scopeAccountIds = $state<string[]>([])
  let useFrom = $state(true)
  let fromValue = $state('')
  let useTo = $state(false)
  let toValue = $state('')
  let useSubject = $state(false)
  let subjectValue = $state('')
  let useListId = $state(false)
  let listIdValue = $state('')
  let actionMove = $state(true)
  let destFolderId = $state('')
  let destFolderLabel = $state('')
  let actionMarkRead = $state(false)
  let actionArchive = $state(false)
  let actionTrash = $state(false)
  let applyToExisting = $state(false)

  let showFolderPicker = $state(false)
  let previewCount = $state<number | null>(null)
  let previewLoading = $state(false)
  let saving = $state(false)
  let initializing = $state(true)
  let error = $state('')
  let warning = $state('')
  let initFailed = $state(false)
  let alive = true
  const newRuleId = crypto.randomUUID()
  onDestroy(() => { alive = false; ++previewGeneration; if (previewTimer) clearTimeout(previewTimer) })
  let previewTimer: ReturnType<typeof setTimeout> | null = null
  let previewGeneration = 0

  $effect(() => {
    if (open) {
      dialogGuardOpen()
      return () => dialogGuardClose()
    }
  })

  const checked = (e: Event) => (e.currentTarget as HTMLInputElement).checked

  function splitPatterns(value: string): string[] {
    return value.split('\n').map(item => item.trim()).filter(item => item.length > 0)
  }

  function buildRule(): mailfilter.Rule {
    const built = mailfilter.Rule.createFrom(rule && rule.id ? rule : {})
    built.id = rule?.id || newRuleId
    built.name = name.trim()
    built.enabled = rule?.enabled ?? true
    built.accountId = ''
    built.accountIds = scopeAll ? [] : [...scopeAccountIds]
    built.folderIds = rule?.folderIds ?? []
    built.fromContains = useFrom ? splitPatterns(fromValue) : []
    built.toContains = useTo ? splitPatterns(toValue) : []
    built.subjectContains = useSubject ? splitPatterns(subjectValue) : []
    built.listIdContains = useListId ? splitPatterns(listIdValue) : []
    built.destFolderId = actionMove ? destFolderId : ''
    built.actionMarkRead = actionMarkRead
    built.actionArchive = actionArchive
    built.actionTrash = actionTrash
    return built
  }

  async function loadAccounts() {
    try {
      accounts = (await GetAccounts()) || []
    } catch (err) {
      console.error('Failed to load accounts:', err)
      initFailed = true
      error = `${$_('filters.loadFailed')}: ${String(err)}`
      accounts = []
    }
  }

  async function folderLabel(folderId: string, forAccountId: string): Promise<string> {
    try {
      const tree = await GetFolderTree(forAccountId)
      const walk = (nodes: { folder?: { id: string; path: string; name: string }; children?: unknown[] }[]): string => {
        for (const node of nodes || []) {
          if (node.folder?.id === folderId) return node.folder.path || node.folder.name
          const child = walk((node.children || []) as typeof nodes)
          if (child) return child
        }
        return ''
      }
      return walk(tree as Parameters<typeof walk>[0])
    } catch {
      return ''
    }
  }

  async function prefillFromMessage(id: string) {
    try {
      const [msg, listId] = await Promise.all([GetMessage(id), GetMessageListID(id).catch(() => { warning = $_('filters.listIdUnavailable'); return '' })])
      if (!alive) return
      const sender = msg.fromEmail || msg.fromName || ''
      fromValue = sender
      useFrom = Boolean(sender)
      try {
        const tos = JSON.parse(msg.toList || '[]') as { name?: string; email?: string }[]
        toValue = tos.map(item => item.email || item.name || '').filter(Boolean).join('\n')
      } catch {
        toValue = ''
      }
      useTo = false
      subjectValue = msg.subject || ''
      useSubject = false
      listIdValue = listId || ''
      useListId = false
      name = sender ? $_('filters.ruleForSender', { values: { sender } }) : ''
    } catch (err) {
      if (alive) { initFailed = true; error = `${$_('filters.loadFailed')}: ${String(err)}` }
    }
  }

  function prefillFromRule(existing: mailfilter.Rule) {
    name = existing.name
    scopeAll = !(existing.accountIds && existing.accountIds.length > 0) && !existing.accountId
    scopeAccountIds = existing.accountIds?.length ? [...existing.accountIds] : (existing.accountId ? [existing.accountId] : [])
    useFrom = (existing.fromContains || []).length > 0
    fromValue = (existing.fromContains || []).join('\n')
    useTo = (existing.toContains || []).length > 0
    toValue = (existing.toContains || []).join('\n')
    useSubject = (existing.subjectContains || []).length > 0
    subjectValue = (existing.subjectContains || []).join('\n')
    useListId = (existing.listIdContains || []).length > 0
    listIdValue = (existing.listIdContains || []).join('\n')
    actionMove = Boolean(existing.destFolderId)
    destFolderId = existing.destFolderId || ''
    actionMarkRead = Boolean(existing.actionMarkRead)
    actionArchive = Boolean(existing.actionArchive)
    actionTrash = Boolean(existing.actionTrash)
    destFolderLabel = ''
    if (destFolderId) {
      // Resolve the label after accounts load.
      void (async () => {
        for (const acc of accounts) {
          const label = await folderLabel(destFolderId, acc.id)
          if (!alive) return
          if (label) { destFolderLabel = `${acc.name}: ${label}`; return }
        }
      })()
    }
  }

  $effect(() => {
    if (!open) { previewCount = null; return }
    void (async () => {
      await loadAccounts()
      if (!alive) return
      if (rule && rule.id) {
        prefillFromRule(rule)
        initializing = false
        return
      }
      if (messageId) await prefillFromMessage(messageId)
      if (alive) initializing = false
    })()
  })

  function selectRelocation(kind: 'move' | 'archive' | 'trash') {
    return (checked: boolean) => {
      if (!checked) {
        if (kind === 'move') actionMove = false
        if (kind === 'archive') actionArchive = false
        if (kind === 'trash') actionTrash = false
        return
      }
      actionMove = kind === 'move'
      actionArchive = kind === 'archive'
      actionTrash = kind === 'trash'
    }
  }
  const setMove = selectRelocation('move')
  const setArchive = selectRelocation('archive')
  const setTrash = selectRelocation('trash')

  function toggleScopeAccount(id: string, checked: boolean) {
    scopeAccountIds = checked ? [...scopeAccountIds, id] : scopeAccountIds.filter(item => item !== id)
  }

  function handleFolderSelected(folderId: string, folderName: string, destAccountId: string) {
    showFolderPicker = false
    destFolderId = folderId
    const acc = accounts.find(item => item.id === destAccountId)
    destFolderLabel = acc ? `${acc.name}: ${folderName}` : folderName
    schedulePreview()
  }

  const hasCriteria = $derived(
    (useFrom && splitPatterns(fromValue).length > 0) ||
    (useTo && splitPatterns(toValue).length > 0) ||
    (useSubject && splitPatterns(subjectValue).length > 0) ||
    (useListId && splitPatterns(listIdValue).length > 0)
  )
  const hasAction = $derived(actionMarkRead || (actionMove && Boolean(destFolderId)) || actionArchive || actionTrash)
  const canSave = $derived(name.trim().length > 0 && hasCriteria && hasAction && (scopeAll || scopeAccountIds.length > 0) && !saving && !initializing && !initFailed && !error && (!applyToExisting || (!previewLoading && previewCount !== null && previewCount <= 500)))

  function schedulePreview() {
    ++previewGeneration
    previewCount = null
    previewLoading = true
    if (previewTimer) clearTimeout(previewTimer)
    previewTimer = setTimeout(() => { void runPreview() }, 400)
  }

  async function runPreview() {
    const generation = ++previewGeneration
    previewLoading = true
    error = ''
    try {
      const count = await PreviewFilterRule(buildRule())
      if (generation !== previewGeneration) return
      previewCount = count
    } catch (err) {
      if (generation !== previewGeneration) return
      previewCount = null
      error = `${$_('filters.previewFailed')}: ${String(err)}`
      console.error('Filter preview failed:', err)
    } finally {
      if (generation === previewGeneration) previewLoading = false
    }
  }

  $effect(() => {
    if (!open || initializing || initFailed) return
    // Re-preview on any form change.
    void [fromValue, toValue, subjectValue, listIdValue, useFrom, useTo, useSubject, useListId, scopeAll, scopeAccountIds.join(',')]
    schedulePreview()
  })

  async function saveRule(built: mailfilter.Rule) {
    if (editMode) {
      await UpdateFilterRule(built)
      toasts.success($_('filters.updated'))
      return
    }
    await CreateFilterRule(built)
    toasts.success($_('filters.created'))
  }

  async function handleSave() {
    if (!canSave) return
    saving = true
    try {
      const built = buildRule()
      await saveRule(built)
      if (applyToExisting && previewCount !== null && previewCount <= 500) {
        const ruleId = built.id
        if (ruleId) {
          try {
            const applied = await ApplyFilterRuleToExisting(ruleId)
            toasts.success($_('filters.appliedToExisting', { values: { count: applied } }))
          } catch (err) {
            toasts.error(`${$_('filters.applyFailed')}: ${String(err)}`)
          }
        }
      }
      open = false
      onSaved?.(built.id)
    } catch (err) {
      toasts.error(`${$_('filters.saveFailed')}: ${String(err)}`)
    } finally {
      saving = false
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content class="relative sm:max-w-[560px]">
    <Dialog.Header>
      <Dialog.Title>{editMode ? $_('filters.editTitle') : $_('filters.createTitle')}</Dialog.Title>
      <Dialog.Description>{$_('filters.description')}</Dialog.Description>
    </Dialog.Header>

    <fieldset disabled={initializing || saving} class="flex flex-col gap-4 py-2 max-h-[calc(90vh-180px)] overflow-y-auto pr-1">
      <!-- Scope -->
      <div class="flex flex-col gap-2">
        <Label>{$_('filters.scope')}</Label>
        <label class="flex items-center gap-2 text-sm">
          <input type="checkbox" class="h-4 w-4" checked={scopeAll} onchange={(e) => { scopeAll = checked(e) }} />
          {$_('filters.scopeAllAccounts')}
        </label>
        {#if rule?.folderIds?.length}
          <p class="text-xs text-muted-foreground">{$_('filters.legacyFolderScope', { values: { count: rule.folderIds.length } })}</p>
        {/if}
        {#if !scopeAll}
          <div class="flex flex-col gap-1 pl-6">
            {#each accounts as acc (acc.id)}
              <label class="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  class="h-4 w-4"
                  checked={scopeAccountIds.includes(acc.id)}
                  onchange={(e) => toggleScopeAccount(acc.id, checked(e))}
                />
                {acc.name} <span class="text-muted-foreground">{acc.email}</span>
              </label>
            {/each}
          </div>
        {/if}
      </div>

      <!-- Name -->
      <div class="flex flex-col gap-2">
        <Label for="filter-name">{$_('filters.ruleName')}</Label>
        <Input disabled={initializing} id="filter-name" bind:value={name} placeholder={$_('filters.ruleNamePlaceholder')} />
      </div>

      <!-- Conditions -->
      <div class="flex flex-col gap-2">
        <Label>{$_('filters.conditions')}</Label>
        <p class="text-xs text-muted-foreground">{$_('filters.patternHint')}</p>
        <div class="flex items-center gap-2">
          <input type="checkbox" class="h-4 w-4 shrink-0" aria-label={$_('filters.from')} checked={useFrom} onchange={(e) => { useFrom = checked(e) }} />
          <span class="w-24 shrink-0 text-sm">{$_('filters.from')}</span>
          <textarea class="min-h-9 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:opacity-50" rows="1" aria-label={$_('filters.from')} bind:value={fromValue} disabled={!useFrom || initializing} placeholder={$_('filters.fromPlaceholder')}></textarea>
        </div>
        <div class="flex items-center gap-2">
          <input type="checkbox" class="h-4 w-4 shrink-0" aria-label={$_('filters.to')} checked={useTo} onchange={(e) => { useTo = checked(e) }} />
          <span class="w-24 shrink-0 text-sm">{$_('filters.to')}</span>
          <textarea class="min-h-9 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:opacity-50" rows="1" aria-label={$_('filters.to')} bind:value={toValue} disabled={!useTo || initializing} placeholder={$_('filters.toPlaceholder')}></textarea>
        </div>
        <div class="flex items-center gap-2">
          <input type="checkbox" class="h-4 w-4 shrink-0" aria-label={$_('filters.subject')} checked={useSubject} onchange={(e) => { useSubject = checked(e) }} />
          <span class="w-24 shrink-0 text-sm">{$_('filters.subject')}</span>
          <textarea class="min-h-9 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:opacity-50" rows="1" aria-label={$_('filters.subject')} bind:value={subjectValue} disabled={!useSubject || initializing} placeholder={$_('filters.subjectPlaceholder')}></textarea>
        </div>
        <div class="flex items-center gap-2">
          <input type="checkbox" class="h-4 w-4 shrink-0" aria-label={$_('filters.listId')} checked={useListId} onchange={(e) => { useListId = checked(e) }} />
          <span class="w-24 shrink-0 text-sm">{$_('filters.listId')}</span>
          <textarea class="min-h-9 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:opacity-50" rows="1" aria-label={$_('filters.listId')} bind:value={listIdValue} disabled={!useListId || initializing} placeholder={$_('filters.listIdPlaceholder')}></textarea>
        </div>
      </div>

      <!-- Actions -->
      <div class="flex flex-col gap-2">
        <Label>{$_('filters.actions')}</Label>
        <div class="flex items-center gap-2">
          <input type="checkbox" class="h-4 w-4 shrink-0" aria-label={$_('filters.actionMove')} checked={actionMove} onchange={(e) => setMove(checked(e))} />
          <span class="text-sm">{$_('filters.actionMove')}</span>
          <Button variant="outline" size="sm" class="ml-auto max-w-[60%] truncate" disabled={!actionMove} onclick={() => { showFolderPicker = true }}>
            {destFolderLabel || $_('filters.chooseFolder')}
          </Button>
        </div>
        <div class="flex flex-wrap items-center gap-4">
          <label class="flex items-center gap-2 text-sm">
            <input type="checkbox" class="h-4 w-4" checked={actionMarkRead} onchange={(e) => { actionMarkRead = checked(e) }} />
            {$_('filters.actionMarkRead')}
          </label>
          <label class="flex items-center gap-2 text-sm">
            <input type="checkbox" class="h-4 w-4" checked={actionArchive} onchange={(e) => setArchive(checked(e))} />
            {$_('filters.actionArchive')}
          </label>
          <label class="flex items-center gap-2 text-sm">
            <input type="checkbox" class="h-4 w-4" checked={actionTrash} onchange={(e) => setTrash(checked(e))} />
            {$_('filters.actionTrash')}
          </label>
        </div>
      </div>

      <!-- Preview -->
      <div class="rounded-md border border-border bg-muted/40 px-3 py-2 text-sm">
        <div class="font-medium">{$_('filters.previewTitle')}</div>
        <div class="text-muted-foreground">
          {#if previewLoading}
            {$_('filters.previewRunning')}
          {:else if previewCount === null}
            {$_('filters.previewEmpty')}
          {:else}
            {$_('filters.previewCount', { values: { count: previewCount } })}
          {/if}
        </div>
      </div>

      {#if useListId}
        <p class="text-xs text-muted-foreground">{$_('filters.listIdCoverage')}</p>
      {/if}

      <!-- Apply to existing -->
      <label class="flex items-start gap-2 text-sm">
        <input type="checkbox" class="mt-0.5 h-4 w-4" disabled={previewCount === null || previewCount > 500 || previewLoading} checked={applyToExisting} onchange={(e) => { applyToExisting = checked(e) }} />
        <span>
          {$_('filters.applyToExisting')}
          <span class="block text-xs text-muted-foreground">{$_('filters.applyToExistingHint')}</span>
        </span>
      </label>
    </fieldset>

    {#if warning}<p role="status" class="text-xs text-muted-foreground">{warning}</p>{/if}
    {#if error}<p role="alert" class="text-sm text-destructive">{error}</p>{/if}
    <Dialog.Footer>
      <Button variant="outline" onclick={() => { open = false }}>{$_('common.cancel')}</Button>
      <Button disabled={!canSave} onclick={() => void handleSave()}>
        {#if saving}<Icon icon="mdi:loading" class="mr-2 h-4 w-4 animate-spin" />{/if}
        {editMode ? $_('filters.save') : $_('filters.create')}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<FolderPickerDialog
  bind:open={showFolderPicker}
  title={$_('filters.chooseFolder')}
  initialAccountId={accountId}
  {accounts}
  excludeFolderId=""
  onSelect={handleFolderSelected}
/>
