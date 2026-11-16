<script lang="ts">
  import Icon from '@iconify/svelte'
  import { GetMessageAttachments, SaveAttachmentAsForAccount, SaveAllAttachmentsForAccount, OpenAttachmentForAccount, SaveEncryptedAttachmentAs, OpenEncryptedAttachment, SaveAllEncryptedAttachments, OpenFile, OpenFolder } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { message as messageModels } from '../../../../wailsjs/go/models'
  import { toasts } from '$lib/stores/toast'
  import { _ } from '$lib/i18n'

  // Decrypted attachment metadata (from encrypted messages, not in DB)
  interface DecryptedAttachment {
    filename: string
    contentType: string
    size: number
    isInline: boolean
    contentId: string
  }

  interface Props {
    accountId: string
    messageId: string
    encryptedAttachments?: DecryptedAttachment[]
  }

  let { accountId, messageId, encryptedAttachments }: Props = $props()

  // State
  let attachments = $state<messageModels.Attachment[]>([])
  let loading = $state(false)
  let downloadingIds = $state<Set<string>>(new Set())
  let savingAll = $state(false)
  let isEncrypted = $derived(encryptedAttachments != null && encryptedAttachments.length > 0)
  // Expansion belongs to an account-qualified message, never the next mail.
  let expandedFor = $state('')
  const owner = $derived(`${accountId}:${messageId}`)
  const expanded = $derived(expandedFor === owner)
  const items = $derived(isEncrypted
    ? (encryptedAttachments ?? []).map((attachment,index)=>({key:`encrypted:${index}`,attachment,encrypted:true}))
    : attachments.map(attachment=>({key:attachment.id,attachment,encrypted:false})))
  function toggleExpanded() { expandedFor = expanded ? '' : owner }
  function openItem(item: typeof items[number]) {
    return item.encrypted ? handleEncryptedOpen(item.attachment as DecryptedAttachment) : handleOpen(item.attachment as messageModels.Attachment)
  }
  function saveItem(item: typeof items[number]) {
    return item.encrypted ? handleEncryptedDownload(item.attachment as DecryptedAttachment) : handleDownload(item.attachment as messageModels.Attachment)
  }
  function busy(item: typeof items[number]) {
    return downloadingIds.has(item.encrypted ? item.attachment.filename : (item.attachment as messageModels.Attachment).id)
  }

  // Determine if we're in encrypted mode


  // Load attachments when messageId changes (only for non-encrypted)
  $effect(() => {
    if (encryptedAttachments && encryptedAttachments.length > 0) {
      attachments = []
      return
    }
    if (messageId) {
      loadAttachments(messageId, accountId)
    } else {
      attachments = []
    }
  })

  async function loadAttachments(msgId: string, accId: string) {
    attachments = []
    loading = true
    try {
      const result = await GetMessageAttachments(accId, msgId)
      if (messageId !== msgId || accountId !== accId) return
      attachments = result || []
    } catch (err) {
      console.error('Failed to load attachments:', err)
      if (messageId !== msgId || accountId !== accId) return
      attachments = []
    } finally {
      if (messageId === msgId && accountId === accId) loading = false
    }
  }

  async function handleDownload(att: messageModels.Attachment) {
    downloadingIds = new Set([...downloadingIds, att.id])
    try {
      const path = await SaveAttachmentAsForAccount(accountId, messageId, att.id)
      if (path) {
        toasts.success($_('toast.attachmentSaved', { values: { filename: att.filename } }), [
          {
            label: $_('attachment.openFile'),
            onClick: () => OpenFile(path)
          },
          {
            label: $_('attachment.showInFolder'),
            onClick: () => OpenFolder(path)
          }
        ])
      }
    } catch (err) {
      console.error('Failed to save attachment:', err)
      toasts.error($_('toast.failedToSaveAttachment', { values: { filename: att.filename } }))
    } finally {
      downloadingIds = new Set([...downloadingIds].filter(id => id !== att.id))
    }
  }

  async function handleOpen(att: messageModels.Attachment) {
    downloadingIds = new Set([...downloadingIds, att.id])
    try {
      await OpenAttachmentForAccount(accountId, messageId, att.id)
    } catch (err) {
      console.error('Failed to open attachment:', err)
      toasts.error($_('toast.failedToOpenAttachment', { values: { filename: att.filename } }))
    } finally {
      downloadingIds = new Set([...downloadingIds].filter(id => id !== att.id))
    }
  }

  async function handleEncryptedDownload(att: DecryptedAttachment) {
    const key = att.filename
    downloadingIds = new Set([...downloadingIds, key])
    try {
      const path = await SaveEncryptedAttachmentAs(messageId, att.filename)
      if (path) {
        toasts.success($_('toast.attachmentSaved', { values: { filename: att.filename } }), [
          {
            label: $_('attachment.openFile'),
            onClick: () => OpenFile(path)
          },
          {
            label: $_('attachment.showInFolder'),
            onClick: () => OpenFolder(path)
          }
        ])
      }
    } catch (err) {
      console.error('Failed to save encrypted attachment:', err)
      toasts.error($_('toast.failedToSaveAttachment', { values: { filename: att.filename } }))
    } finally {
      downloadingIds = new Set([...downloadingIds].filter(id => id !== key))
    }
  }

  async function handleEncryptedOpen(att: DecryptedAttachment) {
    const key = att.filename
    downloadingIds = new Set([...downloadingIds, key])
    try {
      await OpenEncryptedAttachment(messageId, att.filename)
    } catch (err) {
      console.error('Failed to open encrypted attachment:', err)
      toasts.error($_('toast.failedToOpenAttachment', { values: { filename: att.filename } }))
    } finally {
      downloadingIds = new Set([...downloadingIds].filter(id => id !== key))
    }
  }

  async function handleSaveAll() {
    const count = items.length
    savingAll = true
    try {
      if (isEncrypted) {
        const folder = await SaveAllEncryptedAttachments(messageId)
        if (folder) {
          toasts.success($_('toast.attachmentsSaved', { values: { count } }), [
            {
              label: $_('attachment.openFolder'),
              onClick: () => OpenFolder(folder)
            }
          ])
        }
      } else {
        const folder = await SaveAllAttachmentsForAccount(accountId, messageId)
        if (folder) {
          toasts.success($_('toast.attachmentsSaved', { values: { count } }), [
            {
              label: $_('attachment.openFolder'),
              onClick: () => OpenFolder(folder)
            }
          ])
        }
      }
    } catch (err) {
      console.error('Failed to save all attachments:', err)
      toasts.error($_('toast.failedToSaveAttachments'))
    } finally {
      savingAll = false
    }
  }

  function getFileIcon(contentType: string): string {
    if (contentType.startsWith('image/')) return 'mdi:file-image'
    if (contentType.startsWith('video/')) return 'mdi:file-video'
    if (contentType.startsWith('audio/')) return 'mdi:file-music'
    if (contentType === 'application/pdf') return 'mdi:file-pdf-box'
    if (contentType.includes('word') || contentType === 'application/msword') return 'mdi:file-word'
    if (contentType.includes('excel') || contentType === 'application/vnd.ms-excel') return 'mdi:file-excel'
    if (contentType.includes('powerpoint') || contentType === 'application/vnd.ms-powerpoint') return 'mdi:file-powerpoint'
    if (contentType.includes('zip') || contentType.includes('compressed')) return 'mdi:folder-zip'
    if (contentType === 'text/plain') return 'mdi:file-document-outline'
    if (contentType === 'text/html') return 'mdi:language-html5'
    return 'mdi:file'
  }

  function formatSize(bytes: number): string {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
  }
</script>

{#if loading && !isEncrypted}
  <div class="flex items-center gap-2 text-sm text-muted-foreground" role="status">
    <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
    {$_('attachment.loading')}
  </div>
{:else if items.length > 0}
  <section class="attachment-set min-w-0" data-attachment-count={items.length} data-attachment-expanded={expanded} aria-label={$_('attachment.listLabel')}>
    <div class="attachment-line flex min-w-0 items-center gap-2">
      <button type="button" onkeydown={(event) => event.stopPropagation()} class="attachment-toggle flex min-w-0 items-center gap-1 rounded-md text-sm text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring {items.length>3 || expanded ? 'flex-1' : 'shrink-0'}"
        aria-expanded={expanded} title={$_(expanded?'attachment.collapse':'attachment.expand')} aria-label={$_(expanded?'attachment.collapse':'attachment.expand')}
        onclick={toggleExpanded}>
        <Icon icon={expanded?'mdi:chevron-down':'mdi:chevron-right'} class="h-4 w-4 shrink-0" />
        {#if items.length>3 || expanded}<span class="truncate">{$_('attachment.listLabel')} ({items.length})</span>{/if}
      </button>
      {#if items.length<=3 && !expanded}
        <div class="attachment-chips flex flex-1 min-w-0 gap-2 overflow-x-auto">
          {#each items as item (item.key)}
            <div data-native-row tabindex="-1" class="attachment-chip flex min-w-[8rem] flex-1 items-center rounded-md border border-border bg-muted/30">
              <button type="button" onkeydown={(event) => event.stopPropagation()} class="flex min-w-0 flex-1 items-center gap-1.5 rounded-md p-1.5 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                title={item.attachment.filename} aria-label={`${$_('attachment.open')}: ${item.attachment.filename}`} data-native-action="attachment.open" disabled={busy(item)} onclick={()=>openItem(item)}>
                <Icon icon={busy(item)?'mdi:loading':getFileIcon(item.attachment.contentType)} class="h-4 w-4 shrink-0 text-primary" />
                <span class="min-w-0"><span class="block truncate text-xs font-medium">{item.attachment.filename}</span><span class="block whitespace-nowrap text-[10px] text-muted-foreground">{formatSize(item.attachment.size)}</span></span>
              </button>
              <button type="button" onkeydown={(event) => event.stopPropagation()} class="shrink-0 rounded-md p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                title={`${$_('attachment.download')}: ${item.attachment.filename}`} aria-label={`${$_('attachment.download')}: ${item.attachment.filename}`} data-native-action="attachment.save" disabled={busy(item)} onclick={()=>saveItem(item)}>
                <Icon icon="mdi:download" class="h-3.5 w-3.5" />
              </button>
            </div>
          {/each}
        </div>
      {/if}
      {#if items.length>1}
        <button type="button" onkeydown={(event) => event.stopPropagation()} class="shrink-0 flex items-center gap-1 rounded-md p-1.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          title={$_('attachment.saveAll',{values:{count:items.length}})} aria-label={$_('attachment.saveAll',{values:{count:items.length}})} data-native-action="attachment.saveAll" onclick={handleSaveAll} disabled={savingAll}>
          {#if items.length>3 || expanded}<span class="whitespace-nowrap">{$_('attachment.saveAllShort')}</span>{/if}
          <Icon icon={savingAll?'mdi:loading':'mdi:download-multiple'} class="h-4 w-4" />
        </button>
      {/if}
    </div>
    {#if expanded}
      <ul class="mt-2 space-y-1" aria-label={$_('attachment.listLabel')}>
        {#each items as item (item.key)}
          <li data-native-row tabindex="-1" class="flex min-w-0 items-center gap-2 rounded-md border border-border bg-muted/20 p-2">
            <Icon icon={getFileIcon(item.attachment.contentType)} class="h-5 w-5 shrink-0 text-primary" />
            <div class="min-w-0 flex-1"><span class="block break-all text-sm">{item.attachment.filename}</span><span class="text-xs text-muted-foreground">{formatSize(item.attachment.size)} {item.attachment.isInline?$_('attachment.inline'):''}</span></div>
            <button type="button" onkeydown={(event) => event.stopPropagation()} class="shrink-0 rounded-md p-2 hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-label={`${$_('attachment.open')}: ${item.attachment.filename}`} title={$_('attachment.open')} data-native-action="attachment.open" disabled={busy(item)} onclick={()=>openItem(item)}><Icon icon={busy(item)?'mdi:loading':'mdi:open-in-new'} class="h-4 w-4" /></button>
            <button type="button" onkeydown={(event) => event.stopPropagation()} class="shrink-0 rounded-md p-2 hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-label={`${$_('attachment.download')}: ${item.attachment.filename}`} title={$_('attachment.download')} data-native-action="attachment.save" disabled={busy(item)} onclick={()=>saveItem(item)}><Icon icon="mdi:download" class="h-4 w-4" /></button>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}

<style>
  .attachment-chips { scrollbar-width: thin; scrollbar-color: hsl(var(--border)) transparent; }
</style>
