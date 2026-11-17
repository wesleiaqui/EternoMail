<script lang="ts">
  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import { _ } from '$lib/i18n'
  import { laterQueue, undoLaterChange } from '$lib/stores/later.svelte'
  import { toasts } from '$lib/stores/toast'
  import { withShortcut, ariaKeyshortcuts } from '$lib/keyboard/format'
  import ShortcutHint from '$lib/components/shortcuts/ShortcutHint.svelte'

  let { nativeMenu=false, accountId, conversationId, variant = 'toolbar', onComplete }: {
    nativeMenu?: boolean; accountId: string; conversationId: string; variant?: 'toolbar' | 'row' | 'menu'; onComplete?: () => void
  } = $props()
  const queue = laterQueue
  const inQueue = $derived(queue.has(accountId, conversationId))
  const busy = $derived(queue.isBusy(accountId, conversationId))
  const label = $derived($_(`readLater.${inQueue ? 'remove' : 'add'}`))
  const shortcut = 'mail.readLater'
  onMount(() => { void laterQueue.load().catch(() => {}) })

  async function toggle(event: MouseEvent) {
    event.preventDefault(); event.stopPropagation()
    const target = !inQueue
    try {
      const result = await laterQueue.setWithUndo(accountId, conversationId, target)
      if (result?.changed) toasts.success($_(`readLater.${target ? 'added' : 'removed'}`), result.operationId ? [{
        label: $_('common.undo'),
        onClick: () => { void undoLaterChange(result).catch(() => toasts.error($_('toast.undoFailed'))) },
      }] : undefined)
      onComplete?.()
    } catch (err) { toasts.error(`${$_(`readLater.failed`)}: ${String(err)}`) }
  }
</script>

<button style:width={variant === 'toolbar' ? 'auto' : undefined} style:padding-inline={variant === 'toolbar' ? '0.5rem' : undefined} type="button" data-native-action={nativeMenu && variant==='toolbar' ? 'mail.readLater' : undefined}
  class="{inQueue ? 'text-primary bg-primary/15' : ''} inline-flex whitespace-nowrap shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring {variant === 'row' ? `h-6 w-6 ${inQueue ? 'text-primary' : 'opacity-0 group-hover:opacity-100 focus-visible:opacity-100'}` : variant === 'menu' ? 'w-full justify-start gap-2 px-3 py-2 text-sm' : 'viewer-toolbar-action p-2'}"
  title={withShortcut(label, shortcut)} aria-label={label} aria-pressed={inQueue} aria-busy={busy} aria-keyshortcuts={ariaKeyshortcuts(shortcut)}
  role={variant === 'menu' ? 'menuitem' : undefined}
  disabled={busy || !queue.ready || !accountId || accountId === 'unified' || accountId === 'virtual' || !conversationId}
  onclick={toggle} onkeydown={event => event.stopPropagation()}
>
  <Icon icon={busy ? 'mdi:loading' : 'mdi:bookmark-outline'} class="{variant === 'toolbar' ? 'size-5' : 'size-4'} {busy ? 'animate-spin' : ''} {inQueue ? 'text-primary' : ''}" />
  {#if variant === 'menu'}{label}<ShortcutHint id={shortcut} />{:else if variant === 'toolbar'}<span class="ml-1 text-sm">{inQueue ? $_('readLater.remove') : $_('readLater.title')}</span>{/if}
</button>
