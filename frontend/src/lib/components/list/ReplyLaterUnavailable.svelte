<script lang="ts">
  import Icon from '@iconify/svelte'
  import { _ } from '$lib/i18n'
  import { accountStore } from '$lib/stores/accounts.svelte'
  import { laterQueue as replyLaterQueue, type LaterRef as ReplyLaterRef, undoLaterChange } from '$lib/stores/later.svelte'
  import { toasts } from '$lib/stores/toast'

  let { entry }: { entry: ReplyLaterRef } = $props()
  const accountLabel = $derived.by(() => {
    const account = accountStore.accounts.find(item => item.account.id === entry.accountId)?.account
    return account ? `${account.name || account.email} · ${account.email}` : $_('replyLater.unknownAccount')
  })
  const busy = $derived(replyLaterQueue.isBusy(entry.accountId, entry.conversationId))

  async function remove() {
    try {
      const result = await replyLaterQueue.set(entry.accountId, entry.conversationId, false)
      if (!result?.changed) return
      toasts.success($_('replyLater.removed'), [{
        label: $_('common.undo'),
        onClick: () => { void undoLaterChange(result).catch(() => toasts.error($_('toast.undoFailed'))) },
      }])
    } catch (err) {
      toasts.error(`${$_('replyLater.failed')}: ${String(err)}`)
    }
  }
</script>

<article class="mx-3 my-2 flex items-center gap-3 rounded-lg border border-dashed border-border bg-muted/30 px-3 py-3" aria-label={$_('replyLater.unavailable')}>
  <Icon icon="mdi:cloud-question-outline" class="h-5 w-5 shrink-0 text-muted-foreground" />
  <div class="min-w-0 flex-1">
    <p class="text-sm font-medium">{$_('replyLater.unavailable')}</p>
    <p class="truncate text-xs text-muted-foreground">{accountLabel}</p>
    <p class="truncate text-xs text-muted-foreground" title={entry.conversationId}>{$_('replyLater.reference')}: {entry.conversationId}</p>
  </div>
  <button type="button" class="shrink-0 rounded px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50" onclick={remove} disabled={busy}>
    {#if busy}<Icon icon="mdi:loading" class="inline h-3.5 w-3.5 animate-spin" />{:else}{$_('replyLater.remove')}{/if}
  </button>
</article>
