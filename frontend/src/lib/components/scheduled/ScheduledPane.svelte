<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import Icon from '@iconify/svelte'
  import { _ } from '$lib/i18n'
  import { parseRepeatSummary } from '$lib/scheduledRepeat'
  import { accountStore } from '$lib/stores/accounts.svelte'
  import { setActiveExtension } from '$lib/stores/uiState.svelte'
  import { createMainWindowApi } from '$lib/composerApi'
  import { sendLaterBackend, type ScheduledJob, type ScheduledPage } from '$lib/scheduledSendApi'
  import SendLaterDialog from '$lib/components/composer/SendLaterDialog.svelte'

  const backend = sendLaterBackend()
  const composerApi = createMainWindowApi()
  const pageSize = 40
  let accountId = $state('')
  let history = $state(false)
  let offset = $state(0)
  let page = $state<ScheduledPage>({ jobs: [], total: 0, keyAvailable: false })
  let loading = $state(false)
  let error = $state('')
  let connectionNote = $state('')
  let busyIds = $state<string[]>([])
  let autoRefresh = $state(true)
  let setupOpen = $state(false)
  let rescheduleId = $state('')
  let rescheduleDue = $state('')
  let polling = false
  let pollCursor = 0
  let generation = 0
  let destroyed = false
  let setupWasOpen = false

  $effect(() => {
    const selection = { accountId, history, offset }
    untrack(() => { void load(selection, true) })
  })

  $effect(() => {
    if (setupOpen) { setupWasOpen = true; return }
    if (setupWasOpen) { setupWasOpen = false; untrack(() => { void load(undefined, true) }) }
  })

  onMount(() => {
    const interval = setInterval(() => { if (autoRefresh) void refreshBatch() }, 15000)
    return () => { destroyed = true; generation++; clearInterval(interval) }
  })

  async function load(selection = { accountId, history, offset }, refresh = false) {
    const request = ++generation
    loading = true
    if (refresh) error = ''
    try {
      const result = await backend.GetScheduledPage(selection.accountId, selection.history, pageSize, selection.offset)
      if (destroyed || request !== generation) return
      page = result
      if (offset > 0 && offset >= result.total) offset = Math.max(0, Math.floor((result.total - 1) / pageSize) * pageSize)
      if (refresh) void refreshBatch()
    } catch (err) {
      if (!destroyed && request === generation) error = String(err)
    } finally {
      if (!destroyed && request === generation) loading = false
    }
  }

  async function perform(job: ScheduledJob, action: string, background = false) {
    if (busyIds.includes(job.id) || !page.keyAvailable) return
    busyIds = [...busyIds, job.id]
    if (!background) error = ''
    try {
      const result = await backend.UpdateScheduledMessage(job.id, action)
      if (destroyed) return
      if (result.error) {
        if (background) connectionNote = $_('sendLater.cachedStatus')
        if (!background) error = result.error
      }
    } catch (err) {
      if (destroyed) return
      if (background) connectionNote = $_('sendLater.cachedStatus')
      if (!background) error = String(err)
    } finally {
      if (!destroyed) busyIds = busyIds.filter(id => id !== job.id)
    }
  }

  async function update(job: ScheduledJob, action: string) {
    await perform(job, action)
    if (!destroyed) await load()
  }

  async function refreshBatch() {
    if (destroyed || polling || !page.keyAvailable) return
    const candidates = page.jobs.filter(job => !busyIds.includes(job.id) && job.state !== 'cancelled'
      && (job.state !== 'sent' || ['pending', 'retry'].includes(job.draftCleanup)))
    if (!candidates.length) return
    polling = true
    connectionNote = ''
    const start = pollCursor % candidates.length
    const batch = Array.from({ length: Math.min(2, candidates.length) }, (_, index) => candidates[(start + index) % candidates.length])
    pollCursor += batch.length
    try {
      await Promise.all(batch.map(job => perform(job, 'refresh', true)))
      if (!destroyed) await load()
    } finally { polling = false }
  }

  function localTime(value: string): string {
    const date = new Date(value)
    const pad = (value: number) => String(value).padStart(2, '0')
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
  }

  function startReschedule(job: ScheduledJob) {
    rescheduleId = job.id
    rescheduleDue = localTime(new Date(Math.max(Date.now() + 3600000, new Date(job.dueAt).getTime())).toISOString())
  }

  async function reschedule(job: ScheduledJob, event: SubmitEvent) {
    event.preventDefault()
    const date = new Date(rescheduleDue)
    if (!Number.isFinite(date.getTime()) || date.getTime() <= Date.now()) { error = $_('sendLater.futureTime'); return }
    if (busyIds.includes(job.id)) return
    busyIds = [...busyIds, job.id]
    error = ''
    try {
      const result = await backend.RescheduleMessage(job.id, date.toISOString())
      if (destroyed) return
      error = result.error || ''
      rescheduleId = ''
      await load()
    } catch (err) {
      if (!destroyed) error = String(err)
    } finally { if (!destroyed) busyIds = busyIds.filter(id => id !== job.id) }
  }

  function accountName(job: ScheduledJob) {
    const account = accountStore.accounts.find(entry => entry.account.id === job.accountId)?.account
    return account?.name || account?.email || job.from || job.accountId
  }

  function chooseAccount(value: string) { accountId = value; offset = 0; pollCursor = 0 }
  function chooseHistory(value: boolean) { history = value; offset = 0; pollCursor = 0 }
</script>

<section class="flex min-w-0 flex-1 flex-col bg-background" aria-label={$_('sendLater.scheduledView')}>
  <header class="flex flex-wrap items-center gap-3 border-b border-border px-5 py-4">
    <button type="button" onclick={() => setActiveExtension('mail')} class="rounded p-2 hover:bg-muted" title={$_('sendLater.backToMail')} aria-label={$_('sendLater.backToMail')}><Icon icon="mdi:arrow-left" class="h-5 w-5" /></button>
    <h1 class="text-lg font-semibold">{$_('sendLater.scheduledView')}</h1>
    <label class="sr-only" for="scheduled-account">{$_('sendLater.accountFilter')}</label>
    <select id="scheduled-account" value={accountId} onchange={event => chooseAccount(event.currentTarget.value)} class="rounded-md border border-border bg-background px-3 py-2 text-sm">
      <option value="">{$_('sendLater.allAccounts')}</option>
      {#each accountStore.accounts as entry (entry.account.id)}<option value={entry.account.id}>{entry.account.name || entry.account.email}</option>{/each}
    </select>
    <div class="flex gap-1" aria-label={$_('sendLater.statusFilter')}>
      <button type="button" aria-pressed={!history} onclick={() => chooseHistory(false)} class="rounded-md px-3 py-2 text-sm {history ? 'hover:bg-muted' : 'bg-muted font-medium'}">{$_('sendLater.active')}</button>
      <button type="button" aria-pressed={history} onclick={() => chooseHistory(true)} class="rounded-md px-3 py-2 text-sm {history ? 'bg-muted font-medium' : 'hover:bg-muted'}">{$_('sendLater.history')}</button>
    </div>
    <button type="button" onclick={() => load(undefined, true)} disabled={loading} class="rounded-md border border-border px-3 py-2 text-sm disabled:opacity-50">{$_('sendLater.refresh')}</button>
    <button type="button" data-native-action="scheduled.connection" onclick={() => setupOpen = true} disabled={!accountId} title={!accountId ? $_('sendLater.chooseSetupAccount') : undefined} class="rounded-md border border-border px-3 py-2 text-sm disabled:opacity-50">{$_('sendLater.connection')}</button>
  </header>
  <div class="flex flex-wrap items-center justify-between gap-2 px-5 py-3 text-xs text-muted-foreground">
    <p>{$_('sendLater.lastConfirmed')}</p>
    <label class="flex items-center gap-2"><input type="checkbox" bind:checked={autoRefresh} />{$_('sendLater.autoRefresh')}</label>
  </div>
  {#if error}<p role="alert" class="px-5 pb-3 text-sm text-destructive break-words">{error}</p>{/if}
  {#if connectionNote}<p role="status" class="px-5 pb-3 text-xs text-muted-foreground">{connectionNote}</p>{/if}
  {#if !page.keyAvailable}<p class="px-5 pb-3 text-sm text-muted-foreground">{$_('sendLater.unlockForActions')}</p>{/if}
  <div class="min-h-0 flex-1 overflow-auto px-5" aria-busy={loading}>
    {#if !page.jobs.length}
      <div class="py-16 text-center text-muted-foreground"><Icon icon="mdi:clock-outline" class="mx-auto mb-3 h-10 w-10" /><p>{$_('sendLater.empty')}</p></div>
    {:else}
      <table class="w-full min-w-[680px] text-sm">
        <caption class="sr-only">{$_('sendLater.scheduledView')}</caption>
        <thead><tr class="border-b border-border text-left text-xs text-muted-foreground"><th class="py-3 pr-4">{$_('sendLater.message')}</th><th class="py-3 pr-4">{$_('sendLater.deliveryTime')}</th><th class="py-3 pr-4">{$_('sendLater.deliveryStatus')}</th><th class="py-3">{$_('sendLater.actions')}</th></tr></thead>
        <tbody>
          {#each page.jobs as job (job.id)}
            <tr data-native-row tabindex="-1" class="border-b border-border align-top">
              <td class="max-w-xs py-4 pr-4">
                <p class="truncate font-medium" title={job.subject}>{job.previewAvailable ? job.subject || $_('sendLater.noSubject') : $_('sendLater.previewUnavailable')}</p>
                <p class="mt-1 truncate text-xs text-muted-foreground">{accountName(job)}</p>
                {#if job.to}<p class="mt-1 truncate text-xs text-muted-foreground" title={job.to}>{job.to}{(job.toCount || 0) > 3 ? ` (+${(job.toCount || 0) - 3})` : ''}</p>{/if}
              </td>
              <td class="py-4 pr-4 whitespace-nowrap"><time datetime={job.dueAt}>{new Date(job.dueAt).toLocaleString(undefined, { timeZoneName: 'short' })}</time>
                {#if parseRepeatSummary(job.repeat)}{@const rule = parseRepeatSummary(job.repeat)}<p class="mt-1 flex items-center gap-1 text-xs text-muted-foreground"><Icon icon="mdi:repeat" class="h-3.5 w-3.5" />{$_(`sendLater.repeatSummary.${rule?.unit}`, { values: { count: rule?.every } })}</p>{/if}</td>
              <td class="max-w-sm py-4 pr-4">
                <p>{$_(`sendLater.status.${job.state}`)}</p>
                {#if job.code}<p class="mt-1 text-xs text-muted-foreground">{$_(`sendLater.errors.${job.code}`)}</p>{/if}
                {#if job.state === 'sent'}<p class="mt-1 text-xs text-muted-foreground">{$_(`sendLater.cleanup.${job.draftCleanup || 'pending'}`)}</p>{/if}
              </td>
              <td class="py-4"><div class="flex flex-wrap gap-3 text-xs">
                <button type="button" data-native-action="scheduled.refresh" onclick={() => update(job, 'refresh')} disabled={!page.keyAvailable || busyIds.includes(job.id)} class="underline disabled:opacity-50">{busyIds.includes(job.id) ? $_('sendLater.updating') : $_('sendLater.refresh')}</button>
                {#if ['awaiting_ack', 'cancellation_awaiting_ack', 'reschedule_awaiting_ack'].includes(job.state)}<button type="button" data-native-action="scheduled.retry" onclick={() => update(job, 'retry')} disabled={!page.keyAvailable || busyIds.includes(job.id)} class="underline disabled:opacity-50">{$_('sendLater.retry')}</button>{/if}
                {#if ['queued', 'retry', 'awaiting_ack', 'failed'].includes(job.state)}<button type="button" data-native-action="scheduled.reschedule" onclick={() => startReschedule(job)} disabled={!page.keyAvailable || busyIds.includes(job.id)} class="underline disabled:opacity-50">{$_('sendLater.reschedule')}</button>{/if}
                {#if ['queued', 'retry', 'awaiting_ack', 'reschedule_awaiting_ack'].includes(job.state)}<button type="button" data-native-action="scheduled.cancel" onclick={() => update(job, 'cancel')} disabled={!page.keyAvailable || busyIds.includes(job.id)} class="underline disabled:opacity-50">{$_('sendLater.cancelDelivery')}</button>{/if}
              </div></td>
            </tr>
            {#if rescheduleId === job.id}
              <tr class="border-b border-border"><td colspan="4" class="pb-4">
                <form onsubmit={event => reschedule(job, event)} class="flex flex-wrap items-end gap-3 rounded-md bg-muted p-3">
                  <label class="text-xs">{$_('sendLater.newTime')}<input type="datetime-local" bind:value={rescheduleDue} required class="mt-1 block rounded border border-border bg-background p-2 text-sm" /></label>
                  <button type="submit" disabled={busyIds.includes(job.id)} class="rounded bg-primary px-3 py-2 text-xs text-primary-foreground disabled:opacity-50">{$_('sendLater.reschedule')}</button>
                  <button type="button" onclick={() => rescheduleId = ''} class="px-3 py-2 text-xs">{$_('common.cancel')}</button>
                  <p class="w-full text-xs text-muted-foreground">{$_('sendLater.timeOnlyChange')}</p>
                </form>
              </td></tr>
            {/if}
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
  <footer class="flex items-center justify-between border-t border-border px-5 py-3 text-xs text-muted-foreground">
    <span>{page.total ? offset + 1 : 0}–{Math.min(offset + pageSize, page.total)} / {page.total}</span>
    <div class="flex gap-3"><button type="button" onclick={() => offset = Math.max(0, offset - pageSize)} disabled={offset === 0 || loading} class="disabled:opacity-50">{$_('sendLater.previousPage')}</button><button type="button" onclick={() => offset += pageSize} disabled={offset + pageSize >= page.total || loading} class="disabled:opacity-50">{$_('sendLater.nextPage')}</button></div>
  </footer>
</section>

{#if accountId}<SendLaterDialog bind:open={setupOpen} {accountId} api={composerApi} canSchedule={false} sending={false} setupOnly onSchedule={async () => {}} />{/if}
