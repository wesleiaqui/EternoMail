<script lang="ts">
  import { _ } from '$lib/i18n'
  let { onInsert, popup = false, expanded = false, limited = false, compact = false }: { onInsert: (value: string) => void; popup?: boolean; expanded?: boolean; limited?: boolean; compact?: boolean } = $props()
</script>
    <details open={expanded} class={compact ? 'relative text-muted-foreground' : 'relative rounded border border-border px-3 py-2'}>
      <summary class={compact ? 'cursor-pointer text-xs' : 'cursor-pointer text-sm font-medium'}>{$_('search.operators')}</summary>
      <div class={popup ? 'absolute right-0 top-full z-50 w-72 rounded border border-border bg-background p-3 shadow-lg' : ''}>
      {#if !compact}<p class="mt-2 text-xs text-muted-foreground">{$_('search.operatorsGmailHint')}</p>{/if}
      <div class="mt-2 flex flex-wrap gap-2">
        {#each [
          {label: $_('search.opUnread'), value: 'is:unread'},
          {label: $_('search.opRead'), value: 'is:read'},
          {label: $_('search.opStarred'), value: 'is:starred'},
          {label: $_('search.opFrom'), value: 'from:'},
          {label: 'subject:', value: 'subject:'},
          {label: $_('search.opTo'), value: 'to:'},
          {label: $_('search.opAttachment'), value: 'has:attachment'},
          {label: $_('search.opFolder'), value: 'label:'},
          {label: $_('search.opExclude'), value: '-'},
          {label: $_('search.opBefore'), value: 'before:'},
          {label: $_('search.opAfter'), value: 'after:'},
        ].filter(item => !limited || ['from:', 'subject:', 'is:unread'].includes(item.value)) as operator (operator.value)}
          <button type="button" class="rounded border border-border px-2 py-1 text-sm hover:bg-muted" onclick={() => onInsert(operator.value)}>{operator.label}</button>
        {/each}
      </div>
      {#if !compact}<p class="mt-2 text-xs text-muted-foreground">{$_('search.opExample')}</p>{/if}
      </div>
    </details>
