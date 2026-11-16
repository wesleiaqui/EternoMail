<script lang="ts">
  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import { _ } from '$lib/i18n'
  import { replyLaterQueue } from '$lib/stores/replyLater.svelte'
  import { readLaterQueue } from '$lib/stores/readLater.svelte'
  import { toasts } from '$lib/stores/toast'
  import { UndoOperation } from '../../../../wailsjs/go/app/App'
  import { withShortcut, ariaKeyshortcuts } from '$lib/keyboard/format'
  import ShortcutHint from '$lib/components/shortcuts/ShortcutHint.svelte'

  let { nativeMenu=false, accountId, conversationId, kind, variant = 'toolbar', onComplete }: {
    nativeMenu?: boolean; accountId: string; conversationId: string; kind: 'readLater' | 'replyLater'; variant?: 'toolbar' | 'row' | 'menu'; onComplete?: () => void
  } = $props()
  const queue = $derived(kind === 'readLater' ? readLaterQueue : replyLaterQueue)
  const inQueue = $derived(queue.has(accountId, conversationId))
  const busy = $derived(queue.isBusy(accountId, conversationId))
  const label = $derived($_(`${kind}.${inQueue ? 'remove' : 'add'}`))
  const shortcut = $derived(`mail.${kind}`)
  onMount(() => { void Promise.all([readLaterQueue.load(), replyLaterQueue.load()]).catch(() => {}) })

  async function toggle(event: MouseEvent) {
    event.preventDefault(); event.stopPropagation()
    const target = !inQueue
    try {
      const result = kind === 'readLater'
        ? await readLaterQueue.setWithUndo(accountId, conversationId, target)
        : await replyLaterQueue.set(accountId, conversationId, target)
      if (result?.changed) toasts.success($_(`${kind}.${target ? 'added' : 'removed'}`), result.operationId ? [{
        label: $_('common.undo'),
        onClick: () => { void UndoOperation(result.operationId).catch(() => toasts.error($_('toast.undoFailed'))) },
      }] : undefined)
      onComplete?.()
    } catch (err) { toasts.error(`${$_(`${kind}.failed`)}: ${String(err)}`) }
  }
</script>

<button type="button" data-native-action={nativeMenu && variant==='toolbar' ? `mail.${kind}` : undefined}
  class="inline-flex shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring {variant === 'row' ? `h-6 w-6 ${inQueue ? 'text-primary' : 'opacity-0 group-hover:opacity-100 focus-visible:opacity-100'}` : variant === 'menu' ? 'w-full justify-start gap-2 px-3 py-2 text-sm' : 'viewer-toolbar-action p-2'}"
  title={withShortcut(label, shortcut)} aria-label={label} aria-pressed={inQueue} aria-busy={busy} aria-keyshortcuts={ariaKeyshortcuts(shortcut)}
  role={variant === 'menu' ? 'menuitem' : undefined}
  disabled={busy || !queue.ready || !accountId || accountId === 'unified' || accountId === 'virtual' || !conversationId}
  onclick={toggle} onkeydown={event => event.stopPropagation()}
>
  <Icon icon={busy ? 'mdi:loading' : kind === 'readLater' ? 'mdi:book-clock-outline' : 'mdi:clock-outline'} class="{variant === 'toolbar' ? 'size-5' : 'size-4'} {busy ? 'animate-spin' : ''} {inQueue ? 'text-primary' : ''}" />
  {#if variant === 'menu'}{label}<ShortcutHint id={shortcut} />{/if}
</button>
