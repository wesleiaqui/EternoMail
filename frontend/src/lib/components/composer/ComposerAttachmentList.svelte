<script lang="ts">
  import Icon from '@iconify/svelte'
  import { formatFileSize, getFileIcon } from './composerUtils'
  import { _ } from '$lib/i18n'

  interface Attachment {
    filename: string
    contentType: string
    size: number
    data: string
  }

  interface Props {
    attachments: Attachment[]
    onRemove: (index: number) => void
    compact?: boolean
  }

  let { attachments, onRemove, compact = false }: Props = $props()
</script>

{#if attachments.length > 0}
  <div class="px-4 py-2 border-t border-border bg-muted/30 {compact ? 'px-6 bg-transparent' : ''}">
    <div class="flex flex-wrap gap-2">
      {#each attachments as attachment, index (index)}
        <div class="flex items-center gap-2 px-3 py-1.5 bg-background border border-border rounded-md text-sm group {compact ? 'rounded-lg py-1' : ''}">
          <Icon icon={getFileIcon(attachment.contentType)} class="w-4 h-4 text-muted-foreground" />
          <span class="max-w-[150px] truncate" title={attachment.filename}>
            {attachment.filename}
          </span>
          <span class="text-xs text-muted-foreground">
            ({formatFileSize(attachment.size)})
          </span>
          <button
            onclick={() => onRemove(index)}
            class="ml-1 p-0.5 text-muted-foreground hover:text-destructive transition-colors opacity-0 group-hover:opacity-100"
            title={$_('attachment.removeAttachment')}
          >
            <Icon icon="mdi:close" class="w-3.5 h-3.5" />
          </button>
        </div>
      {/each}
    </div>
  </div>
{/if}
