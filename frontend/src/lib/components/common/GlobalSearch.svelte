<script lang="ts">
  import { _ } from '$lib/i18n'
  import { onDestroy } from 'svelte'
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import { SearchMail, CancelMailSearch } from '../../../../wailsjs/go/app/App'
  import { app, type message } from '../../../../wailsjs/go/models'
  import SearchOperators from './SearchOperators.svelte'
  let { open = $bindable(false), query = $bindable(''), onSubmit }: { open: boolean; query?: string; onSubmit: (query: string) => void } = $props()
  let input = $state<HTMLInputElement>()
  let anchor = $state({ left: 0, top: 48 })
  let results = $state<message.Conversation[]>([])
  let busy = $state(false), error = $state(''), more = $state(false)
  const session = crypto.randomUUID()
  let generation = 0
  $effect(() => {
    const text = query.trim()
    const current = ++generation
    const typedAt = performance.now()
    if (!open) { void CancelMailSearch(session); busy = false; return }
    const rect = document.querySelector('[data-search-anchor]')?.getBoundingClientRect()
    anchor = { left: rect?.left ?? 0, top: rect?.top ?? 48 }
    error = ''; busy = !!text
    if (!text) { results = []; more = false }
    void CancelMailSearch(session)
    const focus = setTimeout(() => { if (document.activeElement !== input) input?.focus() }, 0)
    const timer = setTimeout(async () => {
      if (!text) return
      busy = true
      try {
        const page = await SearchMail(new app.MailSearchRequest({sessionId: session, query: text, accountIds: [], folderIds: [], virtualFolderId: '', cursor: '', limit: 8}))
        if (current !== generation) return
        results = page.conversations || []; more = !!page.cursor
        if (import.meta.env.DEV) console.debug('[search timing]', {phase: 'modal-first-page', query: text, milliseconds: Math.round(performance.now() - typedAt), count: results.length})
        error = (page.issues || []).map(issue => `${issue.accountName}: ${issue.message}`).join(' · ')
      } catch (cause) { if (current === generation && !String(cause).includes('canceled')) error = String(cause) }
      finally { if (current === generation) busy = false }
    }, 250)
    return () => { clearTimeout(timer); clearTimeout(focus) }
  })
  onDestroy(() => { generation++; void CancelMailSearch(session) })
  function submit() { if (!query.trim()) return; onSubmit(query.trim()); open = false }
</script>
<Dialog.Root bind:open>
  <Dialog.Content class="rail-search-flyout fixed grid max-w-none gap-2 overflow-hidden p-4" style={`left:${anchor.left}px;top:${anchor.top}px`} >
    <form class="flex min-w-0 items-center gap-3 pr-7" onsubmit={event => { event.preventDefault(); submit() }}>
      <Icon icon="mdi:magnify" width="24" class="shrink-0 text-primary" />
      <input bind:this={input} bind:value={query} aria-label={$_('searchUI.searchAll')} placeholder={$_('searchUI.searchAllPlaceholder')} type="search" class="min-w-0 flex-1 rounded border border-border bg-muted/40 px-3 py-2 focus:outline-none focus:ring-2 focus:ring-ring" />
    </form>
    <Dialog.Header class="sr-only"><Dialog.Title>{$_('searchUI.allMail')}</Dialog.Title><Dialog.Description>Search all accounts and folders</Dialog.Description></Dialog.Header>
    <p class="text-xs text-muted-foreground">{$_('searchUI.allScope')}</p>
    <div class="min-h-0 overflow-auto" aria-busy={busy}>
      {#if busy}<p role="status">{$_('searchUI.searching')}</p>{/if}
      {#if error}<p role="alert" class="text-sm text-destructive">{error}</p>{/if}
      {#if query.trim() && !busy && results.length === 0 && !error}<p role="status">{$_('searchUI.noMatches')}</p>{/if}
      {#each results as result (`${result.accountId}:${result.threadId}`)}
        <button type="button" class="my-1 block w-full rounded border border-border p-3 text-left hover:bg-muted" onclick={submit}>
          <span class="block truncate font-medium">{result.subject || '(No subject)'}</span>
          <span class="block truncate text-sm text-muted-foreground">{result.accountName} · {result.participants?.map(person => person.name || person.email).join(', ')}</span>
        </button>
      {/each}
      {#if more}<p class="text-xs text-muted-foreground">{$_('searchUI.more')}</p>{/if}
    </div>
    <SearchOperators compact onInsert={value => { query = `${query.trimEnd()} ${value}`.trimStart(); input?.focus() }} />
    <p class="text-xs text-muted-foreground">{$_('searchUI.enterHint')}</p>
  </Dialog.Content>
</Dialog.Root>
<style>
  :global(.rail-search-flyout) { left: 0.25rem; top: 3.1rem; width: min(46rem, calc(100vw - .5rem)); max-height: calc(100dvh - 3.6rem); }
</style>
