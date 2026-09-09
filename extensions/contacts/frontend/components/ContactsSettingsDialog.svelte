<script lang="ts">
  // ContactsSettingsDialog — the Contacts extension's own settings dialog.
  // Holds the extension's OAuth Credentials section (per-extension slots:
  // google-contacts, microsoft-contacts) and the Write Access section
  // (per-OAuth-source enable/disable buttons; OAuth sources route through
  // the WriteAccessAccountPicker flow).
  //
  // Opens via Settings → Extensions → Edit button on the Contacts row.

  import { Contacts_ReauthorizeSource, Contacts_CancelSourceAuthorization } from '$wailsjs/go/app/App'
  import { _ } from 'svelte-i18n'
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import OAuthCredsSlotEditor from '$lib/components/kit/OAuthCredsSlotEditor.svelte'
  import WriteAccessAccountPicker from '$lib/components/oauth/WriteAccessAccountPicker.svelte'
  import { contactSourcesStore } from '$extensions/contacts/frontend/stores/contactSources.svelte'
  import { toasts } from '$lib/stores/toast'
  // @ts-ignore - wailsjs bindings
  import { SetContactSourceWritable } from '$wailsjs/go/app/App'
  // @ts-ignore - wailsjs bindings
  import type { v1 } from '$wailsjs/go/models'

  interface Props {
    open: boolean
    onClose?: () => void
  }

  let { open = $bindable(false), onClose }: Props = $props()

  // Per-source write-access toggle in-flight state. Keyed by source id so
  // multiple buttons can show spinners independently if the user clicks more
  // than one before the first finishes. Only used for CardDAV's direct
  // writable-flag flip; the OAuth picker manages its own granting state.
  let pendingWriteAccess = $state<Record<string, boolean>>({})

  // Picker state — only one picker open at a time.
  let pickerOpen = $state(false)
  let reauthorizing = $state<string | null>(null)
  async function reauthorize(sourceId: string) {
    reauthorizing = sourceId
    try {
      await Contacts_ReauthorizeSource(sourceId)
      await contactSourcesStore.load()
      toasts.success($_('contactSource.authorized'))
    } catch (err) {
      toasts.error(String(err))
    } finally {
      reauthorizing = null
    }
  }

  let pickerProvider = $state<'microsoft'>('microsoft')
  let pickerSourceID = $state('')
  let pickerSourceName = $state('')

  // Refresh the source list whenever the dialog opens — the user may have
  // added/removed sources since last open, or the write-access picker may
  // have flipped writable. Cheap (single Wails call returning a small array).
  $effect(() => {
    if (open) {
      void contactSourcesStore.load()
    }
  })

  // Source rows surfaced in the Write Access section. CardDAV and Microsoft
  // appear here regardless of state — Google Contacts is read-only.
  // dialog is the canonical on/off lever for write access. Per-row button
  // text flips: "Enable" when read-only, "Disable" when writable. Local is
  // always writable so it's excluded.
  //
  // The companion WriteAccessBanner in the Contacts pane is the surface for
  // ENABLE-only (it filters out writable rows so the banner doesn't clutter
  // the list once everything's set up). Disable lives only here.
  const writeAccessRows = $derived(
    contactSourcesStore.sources.filter(
      (s) => s.type === 'microsoft' || s.type === 'carddav',
    ),
  )

  // Disable write access on a source. Pure flag flip via
  // SetContactSourceWritable(id, false) — works for the supported external
  // source types. Note: for Microsoft this does NOT revoke the OAuth
  // token at the provider; it just stops Aerion from using it. If the user
  // re-enables later, the existing token is reused if still valid, so the
  // write-access picker won't need to re-grant. To fully revoke, the user
  // goes to the provider's account settings.
  async function disableWriteAccess(source: v1.ContactSource) {
    pendingWriteAccess[source.id] = true
    try {
      await SetContactSourceWritable(source.id, false)
      await contactSourcesStore.load()
      toasts.success($_('contacts.settings.writeAccessDisabled', { values: { name: source.name } }))
    } catch (err) {
      console.error('Disable write access failed:', err)
      toasts.error((err as Error)?.message ?? $_('contacts.settings.writeAccessDisableFailed'))
    } finally {
      delete pendingWriteAccess[source.id]
    }
  }

  // Single entry point for "enable write access on this source." Same end
  // state across providers (writable=true); only the precondition differs:
  // CardDAV is a pure flag flip via SetContactSourceWritable; Microsoft opens
  // the account picker dialog, which dispatches into Contacts_EnableWriteAccess
  // on confirm.
  async function enableWriteAccess(source: v1.ContactSource) {
    if (source.type === 'carddav') {
      pendingWriteAccess[source.id] = true
      try {
        await SetContactSourceWritable(source.id, true)
        await contactSourcesStore.load()
        toasts.success($_('contacts.settings.writeAccessEnabled', { values: { name: source.name } }))
      } catch (err) {
        console.error('Enable write access failed:', err)
        toasts.error((err as Error)?.message ?? $_('contacts.settings.writeAccessCanceled'))
      } finally {
        delete pendingWriteAccess[source.id]
      }
      return
    }

    openWriteAccessPicker(source)
  }

  // Open the account-picker consent flow for a Microsoft
  // source. Used both to grant write access initially and to reauthorize a
  // source whose token grant went stale — re-running Contacts_EnableWriteAccess
  // refreshes the tokens (writable stays on).
  function openWriteAccessPicker(source: v1.ContactSource) {
    if (source.type !== 'microsoft') return
    pickerProvider = source.type
    pickerSourceID = source.id
    pickerSourceName = source.name
    pickerOpen = true
  }

  async function onPickerCompleted() {
    await contactSourcesStore.load()
  }

  function handleClose() {
    open = false
    onClose?.()
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) handleClose() }}>
  <Dialog.Content class="max-w-2xl">
    <Dialog.Header>
      <Dialog.Title>{$_('contacts.settings.title')}</Dialog.Title>
      <Dialog.Description>
        {$_('contacts.settings.description')}
      </Dialog.Description>
    </Dialog.Header>

    <div class="space-y-4 mt-2 max-h-[60vh] overflow-y-auto pr-1">
      <section>
        <h3 class="text-sm font-semibold text-foreground mb-2">{$_('contacts.settings.oauthHeading')}</h3>
        <p class="text-xs text-muted-foreground mb-3">
          {$_('contacts.settings.oauthDescription')}
        </p>

        <div class="space-y-3">
          <OAuthCredsSlotEditor
            configID="google-contacts"
            extensionID="contacts"
            label={$_('contacts.settings.googleLabel')}
            secretRequired={true}
          />
          <OAuthCredsSlotEditor
            configID="microsoft-contacts"
            extensionID="contacts"
            label={$_('contacts.settings.microsoftLabel')}
            secretRequired={false}
          />
        </div>
      </section>

      {#if writeAccessRows.length > 0}
        <section>
          <div class="space-y-2 mb-4">
            {#each contactSourcesStore.sources.filter(s => s.type === 'google' || s.type === 'microsoft') as source (source.id)}
              <div class="flex items-center gap-2">
                <span class="text-sm flex-1">{source.name}</span>
                <Button size="sm" variant="outline" disabled={reauthorizing !== null} onclick={() => reauthorize(source.id)}>{$_('contactSource.authorizeContacts')}</Button>
                {#if reauthorizing === source.id}
                  <Button size="sm" variant="ghost" onclick={() => Contacts_CancelSourceAuthorization()}>{$_('common.cancel')}</Button>
                {/if}
              </div>
            {/each}
          </div>
          <h3 class="text-sm font-semibold text-foreground mb-2">{$_('contacts.settings.writeAccessHeading')}</h3>
          <p class="text-xs text-muted-foreground mb-3">
            {$_('contacts.settings.writeAccessDescription')}
          </p>

          <div class="space-y-2">
            {#each writeAccessRows as source (source.id)}
              <div class="flex items-center justify-between gap-3 rounded-md border border-border p-3">
                <div class="flex items-center gap-3 min-w-0">
                  <Icon
                    icon={source.type === 'microsoft' ? 'mdi:microsoft' : 'mdi:server'}
                    class="w-5 h-5 text-muted-foreground flex-shrink-0"
                  />
                  <div class="min-w-0">
                    <div class="text-sm font-medium text-foreground truncate">{source.name}</div>
                    <div class="text-xs text-muted-foreground">
                      {source.writable
                        ? $_('contacts.settings.writeAccessRowSubtitleEnabled')
                        : $_('contacts.settings.writeAccessRowSubtitle')}
                    </div>
                  </div>
                </div>
                {#if source.writable}
                  <div class="flex items-center gap-2 flex-shrink-0">
                    {#if source.type === 'microsoft'}
                      <Button
                        size="sm"
                        variant="ghost"
                        onclick={() => openWriteAccessPicker(source)}
                        disabled={pendingWriteAccess[source.id]}
                      >
                        <Icon icon="mdi:refresh" class="w-4 h-4 mr-1" />
                        {$_('contacts.settings.reauthorizeWriteAccess')}
                      </Button>
                    {/if}
                    <Button
                      size="sm"
                      variant="outline"
                      onclick={() => disableWriteAccess(source)}
                      disabled={pendingWriteAccess[source.id]}
                    >
                      {#if pendingWriteAccess[source.id]}
                        <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
                      {/if}
                      {$_('contacts.settings.disableWriteAccess')}
                    </Button>
                  </div>
                {:else}
                  <Button
                    size="sm"
                    onclick={() => enableWriteAccess(source)}
                    disabled={pendingWriteAccess[source.id]}
                  >
                    {#if pendingWriteAccess[source.id]}
                      <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
                    {/if}
                    {$_('contacts.settings.enableWriteAccess')}
                  </Button>
                {/if}
              </div>
            {/each}
          </div>
        </section>
      {/if}
    </div>

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" onclick={handleClose}>{$_('contacts.settings.close')}</Button>
    </div>
  </Dialog.Content>
</Dialog.Root>

<WriteAccessAccountPicker
  bind:open={pickerOpen}
  provider={pickerProvider}
  sourceID={pickerSourceID}
  sourceName={pickerSourceName}
  onCompleted={onPickerCompleted}
/>
