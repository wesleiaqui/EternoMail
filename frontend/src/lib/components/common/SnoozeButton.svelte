<script lang="ts">
  import Icon from '@iconify/svelte'
  import { _ } from '$lib/i18n'
  import { withShortcut, ariaKeyshortcuts } from '$lib/keyboard/format'
  import { snoozeDialog } from '$lib/stores/snoozeDialog.svelte'

  let { nativeMenu=false, accountId, conversationId }: { nativeMenu?: boolean; accountId: string; conversationId: string } = $props()
  const label = $derived($_('snooze.title'))
  const usable = $derived(!!accountId && accountId !== 'unified' && accountId !== 'virtual' && !!conversationId)
</script>

<button type="button" data-native-action={nativeMenu ? 'mail.snooze' : undefined}
  class="inline-flex shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring viewer-toolbar-action p-2"
  title={withShortcut(label, 'mail.snooze')} aria-label={label} aria-keyshortcuts={ariaKeyshortcuts('mail.snooze')}
  disabled={!usable}
  onclick={(event) => { event.preventDefault(); event.stopPropagation(); snoozeDialog.show([{ accountId, conversationId }]) }}
  onkeydown={event => event.stopPropagation()}
>
  <Icon icon="mdi:clock-time-four-outline" class="size-5" />
</button>
