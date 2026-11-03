<script lang="ts">
  import { onDestroy } from 'svelte'
  import SearchOperators from './SearchOperators.svelte'
  import { decodeHtmlEntities } from '$lib/utils/decodeHtmlEntities'
  import * as Dialog from '$lib/components/ui/dialog'
  import { SearchMail, CancelMailSearch, ReauthorizeAccount } from '../../../../wailsjs/go/app/App'
  import { accountStore } from '$lib/stores/accounts.svelte'
  import { app, type message } from '../../../../wailsjs/go/models'
  import { _ } from '$lib/i18n'
  let { open = $bindable(false), onSelect }: { open: boolean; onSelect: (thread: string, folder: string, account: string) => void } = $props()
  let query = $state(''), accountId = $state(''), cursor = $state(''), busy = $state(false)
  let results = $state<message.Conversation[]>([]), issues = $state<app.MailSearchIssue[]>([]), error = $state('')
  let input = $state<HTMLInputElement | null>(null)
  const session = crypto.randomUUID()
  let generation = 0
  let searched = $state(false)
  $effect(() => {
    if (!open) { generation++; void CancelMailSearch(session); busy = false; return }
    const timer = setTimeout(() => input?.focus(), 0)
    return () => clearTimeout(timer)
  })
  onDestroy(() => { void CancelMailSearch(session) })
  function insertOperator(value: string) {
    const start = input?.selectionStart ?? query.length
    const end = input?.selectionEnd ?? query.length
    const prefix = start > 0 && !/\s/.test(query[start - 1]) ? ' ' : ''
    const suffix = end < query.length && !/\s/.test(query[end]) ? ' ' : ''
    query = query.slice(0, start) + prefix + value + suffix + query.slice(end)
    cursor = ''
    requestAnimationFrame(() => {
      input?.focus()
      const position = start + prefix.length + value.length
      input?.setSelectionRange(position, position)
    })
  }
  async function search(more = false) {
    const current = ++generation
    busy = true; error = ''; searched = true
    if (!more) { results = []; cursor = ''; issues = [] }
    try {
      const page = await SearchMail(new app.MailSearchRequest({ sessionId: session, query: query.trim(), accountIds: accountId ? [accountId] : [], virtualFolderId: '', cursor: more ? cursor : '', limit: 30 }))
      if (current !== generation || !open) return
      const merged = more ? [...results, ...(page.conversations || [])] : page.conversations || []
      results = [...new Map(merged.map(c => [`${c.accountId}:${c.threadId}`, c])).values()].sort((a, b) => new Date(b.latestDate as unknown as string).getTime() - new Date(a.latestDate as unknown as string).getTime())
      cursor = page.cursor || ''; issues = page.issues || []
    } catch (e) { if (current === generation) error = String(e) }
    finally { if (current === generation) busy = false }
  }
</script>
<Dialog.Root bind:open>
  <Dialog.Content class="grid-rows-[auto_auto_minmax(0,1fr)] w-[calc(100vw-2rem)] max-w-4xl max-h-[85vh] overflow-hidden">
    <Dialog.Header><Dialog.Title>{$_('search.global')}</Dialog.Title></Dialog.Header>
    <form class="flex min-w-0 flex-col gap-2 sm:flex-row" onsubmit={(event) => { event.preventDefault(); void search() }}>
      <input bind:this={input} bind:value={query} oninput={() => { cursor = '' }} type="search" class="w-full min-w-0 flex-1 rounded border border-border bg-background px-3 py-2" placeholder="is:unread label:work" aria-label={$_('search.global')} />
      <select bind:value={accountId} class="w-full min-w-0 rounded border border-border bg-background px-2 sm:w-40" aria-label={$_('search.accountScope')} onchange={() => void search()}>
        <option value="">{$_('search.allAccounts')}</option>
        {#each accountStore.accounts as item (item.account.id)}<option value={item.account.id}>{item.account.name || item.account.email}</option>{/each}
      </select>
      <button type="submit" class="shrink-0 rounded bg-primary px-3 py-2 text-primary-foreground">{$_('search.searchButton')}</button>
    </form>
    <div class="min-h-0 min-w-0 overflow-y-auto overflow-x-hidden pr-1">
    <SearchOperators onInsert={insertOperator} />
    <p class="my-2 text-xs leading-5 text-muted-foreground">{$_('search.serverHint')}</p>
    {#if error}<p role="alert" class="break-words text-sm text-destructive">{error}</p>{/if}
    {#each issues as issue (issue.accountId + issue.message)}
      <p class="mb-2 break-words text-xs text-destructive" role="status">{issue.accountName}: {issue.message}
        {#if issue.reauthorize}<button type="button" class="underline" onclick={() => ReauthorizeAccount(issue.accountId).then(() => search())}>{$_('search.reauthorize')}</button>{/if}
      </p>
    {/each}
    <div class="min-w-0 space-y-2" aria-busy={busy}>
      {#each results as item (`${item.accountId}:${item.threadId}`)}
        <button type="button" class="block w-full min-w-0 overflow-hidden rounded-lg border border-border p-3 text-left hover:bg-muted" onclick={() => { onSelect(item.threadId, item.folderId || '', item.accountId || ''); open = false }}>
          <span class="block truncate text-sm font-medium">{item.subject || $_('messageList.noSubject')}</span>
          <span class="block truncate text-xs text-muted-foreground">{item.accountName} · {item.participants?.map(p => p.name || p.email).join(', ')} · {new Date(item.latestDate as unknown as string).toLocaleString()}</span>
          <span class="block truncate text-sm text-muted-foreground">{decodeHtmlEntities(item.snippet)}</span>
        </button>
      {/each}
    </div>
    {#if searched && !busy && !error && results.length === 0}<p role="status" class="text-sm text-muted-foreground">{$_('search.noMatches')}</p>{/if}
    {#if busy}<p role="status" class="text-sm text-muted-foreground">{$_('common.loading')}</p>{/if}
    {#if cursor}<button type="button" disabled={busy} onclick={() => void search(true)} class="rounded border border-border px-3 py-2">{$_('search.loadMore')}</button>{/if}
    </div>
  </Dialog.Content>
</Dialog.Root>
