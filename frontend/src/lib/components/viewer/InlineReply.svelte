<script lang="ts">
  import { onMount } from 'svelte'
  import Composer from '$lib/components/composer/Composer.svelte'
  // @ts-ignore - Wails-generated imports
  import { PrepareReply } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - Wails-generated types
  import { smtp } from '../../../../wailsjs/go/models'
  import Icon from '@iconify/svelte'
  import { _ } from '$lib/i18n'

  interface Props {
    accountId: string
    messageId: string
    mode: 'reply' | 'reply-all'
    imagesLoaded?: boolean
    onClose: () => void
    onSent: () => void
    onFallback: () => void
  }

  let { accountId, messageId, mode, imagesLoaded = false, onClose, onSent, onFallback }: Props = $props()
  let initialMessage = $state<smtp.ComposeMessage | null>(null)
  let failed = $state(false)

  onMount(() => {
    let current = true
    void PrepareReply(messageId, mode).then((message: smtp.ComposeMessage | null) => {
      if (current) initialMessage = message
    }).catch(error => {
      console.error('Failed to prepare inline reply:', error)
      if (!current) return
      failed = true
      onFallback()
    })
    return () => { current = false }
  })
</script>

<section class="mt-6 mx-auto h-[min(520px,65vh)] min-h-[360px] max-w-5xl overflow-hidden rounded-xl" aria-label={$_(mode === 'reply-all' ? 'composer.replyAll' : 'composer.reply')}>
  {#if initialMessage}
    <Composer
      {accountId}
      {messageId}
      {initialMessage}
      imagesLoaded={imagesLoaded}
      variant="compact"
      onClose={onClose}
      onSent={onSent}
    />
  {:else if !failed}
    <div class="flex h-full items-center justify-center gap-2 rounded-xl border border-border bg-background text-sm text-muted-foreground" role="status">
      <Icon icon="mdi:loading" class="h-4 w-4 animate-spin" />
      {$_('common.loading')}
    </div>
  {/if}
</section>
